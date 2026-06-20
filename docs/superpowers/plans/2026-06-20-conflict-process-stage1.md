# Conflict Resolution as a Process — Stage 1 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Introduce a first-class `process` abstraction to the TUI (a single active-process slot that owns input, drawing, and the jobs it starts), and re-home merge/rebase conflict resolution onto it as the first process — replacing the self-reopening conflict popup.

**Architecture:** A new `process` interface mirrors the existing `surface`/`overlay` interfaces one level up. `Model.proc` holds the single active process; while it is non-nil it is the interface **lock** — all keys route to it, it owns the screen, every other popup/surface/command is inert, and it shows an indicator. A process is a small state machine that *presents passive windows* and *consumes their intent*, drives the existing small git jobs via `startOp`, reacts to each job's completion, and is **started by detecting** a conflicted merge/rebase. The conflict popup leaves the popup pile.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`). Reuses the existing engine conflict ops (`ResolveConflict`, `ResolveConflictHunks`, `MarkAllResolved`, `ContinueOp`, `AbortOp`), the `hunkPicker` surface, and `domain.ConflictState`.

## Global Constraints

- **The process slot is the lock.** While `m.proc != nil`: all key input goes to `m.proc.update`; render is `m.proc.render`; mouse is swallowed; opening any popup/surface or starting any op from a panel is impossible because those handlers are never reached. The gate sits **just below the decision modal** and **above everything else** (modal → process → actionMenu → cheat-sheet → overlay → stackTop → diffView → panels), in dispatch, render, and mouse — the same ordering in all three (the existing "routing invariant").
- **Never trap the user** (see the spec's "Never trap the user"): the process always offers **Cancel** (stop the in-flight job, re-read state) and **Leave** (step out, repo as-is → non-blocking notice that does NOT re-grab the slot). Slot/notice clears for good only when the repo is no longer conflicted.
- **Start by detection, not handoff:** the process is started by noticing `len(m.status.Conflicts()) > 0` (covers relaunch into a half-finished rebase). It is NOT started by a merge/rebase op "handing off."
- **No behavior loss:** every per-file action, the line-editor hand-off, mark-all-resolved, continue, and abort that the old popup offered remain available, with identical git effects.
- **Reuse, don't reinvent:** the file-list rendering, the conflict per-file actions, and the `hunkPicker` are ported/driven, not rewritten.
- **Verify:** `go build ./cmd/gg` + `./test.sh unit` after each task; `./test.sh race` before merge.

## File Structure

| File | Responsibility |
|---|---|
| `internal/tui/process.go` (new) | The `process` interface + `Model.proc` accessor helpers + the indicator render. |
| `internal/tui/process_test.go` (new) | Gate tests using a tiny test-only stub process. |
| `internal/tui/conflict_process.go` (new) | The conflict-resolution process: state machine, the windows it presents, the jobs it drives. |
| `internal/tui/conflict_process_test.go` (new) | The conflict-process state-machine tests (ported from the old popup tests). |
| `internal/tui/model.go` | Add `proc` field; the dispatch gate; route `opFinishedMsg` to the process; start-by-detection; drop `reopenConflict` + the `x`→`openConflictPopup` wiring. |
| `internal/tui/view.go` | The render gate; the notice already at ~line 278 becomes the "resume" affordance. |
| `internal/tui/mouse.go` | The mouse gate. |
| `internal/tui/conflict_popup.go` | Gutted: the file-list *drawing* moves into the process as a passive window; the self-managing popup (field, update, reopen) is removed. |
| `internal/tui/conflict_picker.go` | `newConflictPicker.apply` no longer nils a popup / sets `reopenConflict`; it reports back to the process. |
| `CHANGELOG.md` | Note the process model (Task 7). |

---

### Task 1: The `process` interface, slot, and the three gates

**Files:** Create `internal/tui/process.go`, `internal/tui/process_test.go`; modify `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/mouse.go`.

**Interfaces:**
- Produces:
  ```go
  type process interface {
      update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
      render(m Model, below string) string
      finished(m Model, res engine.Result, err error) (Model, tea.Cmd)
      indicator(m Model) string
  }
  ```
  and `Model.proc process` (nil = no active process). Later tasks implement this with the conflict process.

- [ ] **Step 1: Write the failing gate test** (`process_test.go`)

Use a tiny stub process to prove the gate in isolation (no conflict yet). The stub records keys and renders a sentinel.

```go
package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

type stubProcess struct {
	keys    []string
	left    bool // set true on "q" to prove a process can release the slot
}

func (p *stubProcess) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.String() == "q" {
		p.left = true
		m.proc = nil // release the slot
		return m, nil
	}
	p.keys = append(p.keys, msg.String())
	return m, nil
}
func (p *stubProcess) render(m Model, below string) string { return "STUBPROC" }
func (p *stubProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) {
	return m, nil
}
func (p *stubProcess) indicator(m Model) string { return "stub running" }

func TestProcessOwnsInputAndRender(t *testing.T) {
	p := &stubProcess{}
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}, proc: p}

	// a key that would normally open the bookmark switcher must reach the process, not open anything
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	m = u.(Model)
	if m.overlayTop() != nil {
		t.Fatal("no overlay may open while a process owns the slot")
	}
	if len(p.keys) != 1 || p.keys[0] != "g" {
		t.Fatalf("the process must receive the key, got %v", p.keys)
	}
	// render is the process's
	if got := m.render(); got != "STUBPROC" {
		t.Fatalf("render must be the process's, got %q", got)
	}
	// the process can release the slot
	u, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("the process released the slot; proc must be nil")
	}
}
```

- [ ] **Step 2: Run it — FAIL** (`go test ./internal/tui/ -run TestProcessOwnsInputAndRender`): `Model has no field proc` / `m.render undefined behavior`.

- [ ] **Step 3: Create `process.go`** with the interface (the block above) plus the indicator helper:

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// process owns the interface while active. The single active process (Model.proc)
// IS the interface lock: while it is non-nil all input routes to it, it owns the
// screen, and every other popup/surface/panel command is inert. A process is a
// set of jobs with its own rules; conflict resolution is the first one. Mirrors
// the surface and overlay interfaces, one level up.
type process interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
	finished(m Model, res engine.Result, err error) (Model, tea.Cmd)
	indicator(m Model) string
}
```

- [ ] **Step 4: Add the field + gates.**

In `model.go`, add to the `Model` struct (near `stack`/`overlays`):
```go
	proc process // the single active long-running process; nil = none. IS the interface lock.
```

In `model.go` `Update`, `tea.KeyMsg` arm, **immediately after the `m.modal` block and before the `m.actionMenu` block**:
```go
		if m.proc != nil {
			return m.proc.update(m, msg)
		}
```

In `model.go`, route a finished job to the process. In the `opFinishedMsg` handler (where `m.running = false` is set), after the existing finish/reload logic, add:
```go
		if m.proc != nil {
			return m.proc.finished(m, msg.res, msg.err)
		}
```
(Keep the existing reload; the process's `finished` decides the next window from the refreshed Model.)

In `view.go` `render()`, **immediately after the `m.modal` block and before the `m.actionMenu` block**:
```go
	if m.proc != nil {
		_, h := m.overlayDims()
		return clipToHeight(m.proc.render(m, clipToHeight(m.renderInterface(), h)), h)
	}
```

In `mouse.go` `handleMouse`, **immediately after the `m.modal` swallow and before the rest**:
```go
	if m.proc != nil {
		return m, nil // a process owns the keyboard; mouse is swallowed (v1)
	}
```

- [ ] **Step 5: Run the test — PASS.** `go test ./internal/tui/ -run TestProcessOwnsInputAndRender`.
- [ ] **Step 6: Build + unit.** `go build ./cmd/gg && ./test.sh unit`.
- [ ] **Step 7: Commit.**
```bash
git add internal/tui/process.go internal/tui/process_test.go internal/tui/model.go internal/tui/view.go internal/tui/mouse.go
git commit -m "feat(tui): process abstraction + active-process slot gate (input/render/mouse/finished)"
```

---

### Task 2: The conflict process — Listing + start-by-detection + Leave

**Files:** Create `internal/tui/conflict_process.go`, `internal/tui/conflict_process_test.go`; modify `internal/tui/model.go` (start-by-detection), `internal/tui/conflict_popup.go` (extract the file-list drawing as a reusable helper).

**Interfaces:**
- Consumes: `process` (Task 1), `domain.ConflictState`, `m.status.Conflicts()`.
- Produces:
  ```go
  type confState int
  const ( confListing confState = iota; confPicking; confWorking; confReporting; confFinishing )
  type conflictProcess struct {
      st    confState
      files []model.FileStatus // conflicted files, refreshed from status
      sel   int
      src   domain.ConflictState
      inProgress string // "merge"/"rebase"/""
      // (later tasks add: the picker, the last error, the running label)
  }
  func startConflictProcess(m Model) (Model, tea.Cmd) // fills the slot from current status
  ```

- [ ] **Step 1: Failing test — detection fills the slot, Leave drops it** (`conflict_process_test.go`)

```go
func conflictModel(files ...string) Model {
	var fs []model.FileStatus
	for _, p := range files {
		fs = append(fs, model.FileStatus{Path: p /* both-sides conflict shape per the helper the tests already use */})
	}
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	m.status = model.WorkingTreeStatus{Files: fs /* mark them unmerged so Conflicts() returns them */}
	return m
}

func TestConflictProcessStartsAndLeaves(t *testing.T) {
	m := conflictModel("a.go", "b.go")
	m, _ = startConflictProcess(m)
	cp, ok := m.proc.(*conflictProcess)
	if !ok || cp.st != confListing || len(cp.files) != 2 {
		t.Fatalf("start must fill the slot with a Listing conflict process over the 2 files")
	}
	// Leave releases the slot without re-grabbing
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = u.(Model)
	if m.proc != nil {
		t.Fatal("Leave must release the slot")
	}
}
```
(Use the same `model.FileStatus` conflict shape the existing `conflict_popup_test.go` uses — copy its file-construction helper.)

- [ ] **Step 2: Run — FAIL** (`startConflictProcess`/`conflictProcess` undefined).

- [ ] **Step 3: Extract the file-list drawing** from `conflict_popup.go`'s `renderConflictPopup` into a reusable, popup-free helper that takes the data, so the process can present it:
```go
// conflictListBox draws the conflicted-file list window (no popup state).
func conflictListBox(m Model, files []model.FileStatus, sel int, src domain.ConflictState, inProgress string) string { /* ported body of renderConflictPopup */ }
```

- [ ] **Step 4: Implement the conflict process (Listing + Leave).** In `conflict_process.go`:
```go
func startConflictProcess(m Model) (Model, tea.Cmd) {
	files := m.status.Conflicts()
	if len(files) == 0 {
		return m, nil
	}
	cp := &conflictProcess{st: confListing, files: files, src: m.conflict}
	m.proc = cp
	return m, m.loadInProgressCmd() // reuse the existing probe; it sets inProgress via a msg (routed to the process in a later step)
}

func (p *conflictProcess) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch p.st {
	case confListing:
		switch msg.String() {
		case "L", "esc": // Leave
			m.proc = nil
			return m, nil
		case "up", "k":
			if p.sel > 0 { p.sel-- }
		case "down", "j":
			if p.sel < len(p.files)-1 { p.sel++ }
		}
		// (Task 3 adds the resolve actions; Task 5 adds continue/abort.)
	}
	return m, nil
}

func (p *conflictProcess) render(m Model, below string) string {
	w, h := m.overlayDims()
	switch p.st {
	case confListing:
		return overlayCenter(clipToHeight(below, h), conflictListBox(m, p.files, p.sel, p.src, p.inProgress), w, h)
	}
	return below
}

func (p *conflictProcess) finished(m Model, res engine.Result, err error) (Model, tea.Cmd) { return m, nil } // Task 3
func (p *conflictProcess) indicator(m Model) string { return "Resolving conflicts — [L]eave" } // grows in later tasks
```

- [ ] **Step 5: Start-by-detection.** In `model.go`, where conflicts are detected after a data load (`dataLoadedMsg`, near `m.conflict = msg.conflict`), replace the old `x`-triggered open with: if there are conflicts and no process is active, fill the slot:
```go
		if m.proc == nil && len(m.status.Conflicts()) > 0 && !m.conflictDismissed {
			return startConflictProcess(m)
		}
```
Add a `conflictDismissed bool` set true by Leave (so Leave drops to the notice without re-grabbing) and cleared when conflicts reach zero. (The notice at view.go:278 already renders when conflicts exist; it becomes the "resume" affordance — Task 6 wires a key to re-enter.)

- [ ] **Step 6: Run the test — PASS.** Build + unit.
- [ ] **Step 7: Commit.** `refactor(tui): conflict process — Listing + start-by-detection + Leave`

---

### Task 3: Per-file resolve jobs — Working / Reporting / re-read

**Files:** `internal/tui/conflict_process.go` (+ test).

Port the per-file actions from `updateConflictPopupKey` (keep ours `C`, keep theirs `i`, mark resolved `m`, keep modified `k`, delete `d`, keep base `b`, mark-all `A`) into the Listing handler. Each:
1. sets `p.st = confWorking` (+ a running label),
2. `return m.startOp(engine.ResolveConflict{Path: f.Path, Action: action})` (NO `reopenConflict`, NO popup nil — the process owns it).

Implement `finished`: on success, re-read conflicts from the refreshed `m.status.Conflicts()`, set `p.files`, and go to `confListing` (or `confFinishing` if a continue/abort completed — Task 5); on error, `p.st = confReporting` with the message; `render` shows progress in `confWorking` and the error in `confReporting`; a key in `confReporting` acks → back to `confListing`.

TDD: a Listing→resolve→finished(success, fewer files)→Listing test; a finished(error)→Reporting→ack→Listing test.

Commit: `feat(tui): conflict process drives per-file resolve jobs (Working/Reporting)`

---

### Task 4: The line-editor hand-off (Picking)

**Files:** `internal/tui/conflict_process.go`, `internal/tui/conflict_picker.go` (+ test).

`enter` on a both-sides file (the existing `ConflictBothSides` gate) → the process loads the file and shows the `hunkPicker` as **its** window: store the picker on the process (`p.picker *hunkPicker`, `p.st = confPicking`), route keys to it in `confPicking`, and render it full-screen.

Change `newConflictPicker.apply` (conflict_picker.go:61-66): drop `m.popSurface()`, `m.conflictPopup = nil`, `m.reopenConflict = true`; instead set the process back to `confWorking` and `return m.startOp(engine.ResolveConflictHunks{...})`. The picker's `esc` returns the process to `confListing` (not `popSurface`). The picker thus no longer lives on the surface stack while in a process — it is the process's Picking window.

TDD: Listing→enter(both-sides)→Picking(picker present)→apply→Working→finished→Listing; and Picking→esc→Listing.

Commit: `feat(tui): conflict process hosts the line editor as its Picking window`

---

### Task 5: Continue / Abort / Cancel / Finishing

**Files:** `internal/tui/conflict_process.go` (+ test).

- `c` (continue) — allowed only when `len(p.files) == 0` and `p.inProgress != ""`: `confWorking` → `startOp(engine.ContinueOp{})`; on success `confFinishing` → release the slot (`m.proc = nil`) and clear `conflictDismissed`.
- `a` (abort) — `confWorking` → `startOp(engine.AbortOp{})`; on success release the slot + clear dismiss.
- **Cancel** in `confWorking`: a key (e.g. `ctrl+x`) cancels the in-flight job (`m.opCancel`) and returns to `confListing`; `finished(err==context.Canceled)` re-reads and lands in `confListing`, never `Reporting`.
- `confFinishing` releases the slot; the slot/notice clears for good because conflicts are now zero.

TDD: all-resolved→continue→finished→slot released; abort→slot released; Working→cancel→Listing.

Commit: `feat(tui): conflict process continue/abort/cancel + Finishing releases the slot`

---

### Task 6: Remove the old popup; resume from the notice; port tests

**Files:** `internal/tui/conflict_popup.go`, `internal/tui/model.go`, `internal/tui/view.go`, `internal/tui/conflict_popup_test.go` → `conflict_process_test.go`.

- Delete `conflictPopup` the field, `updateConflictPopupKey`, `openConflictPopup`, `reopenConflict`, and the reopen block (model.go:328-331), the dispatch block (model.go:412), the render block (view.go:191) and its tooltip clause (view.go:173). Keep `conflictListBox` (now owned by the process) and `loadInProgressCmd`.
- The `x` key + the notice (view.go:278) now **resume** the process: `x` (or the notice's key) → `startConflictProcess(m)` + clear `conflictDismissed`.
- Port `conflict_popup_test.go` to drive the process (`startConflictProcess` + `m.Update(key)`); delete tests that asserted the old popup field / reopen flag, replacing them with the equivalent process-state assertions.

Commit: `refactor(tui): remove the self-reopening conflict popup; resume the process from the notice`

---

### Task 7: Indicator, help/footer, changelog

**Files:** `internal/tui/process.go` (or conflict_process.go), `internal/tui/help.go`, `internal/tui/footer.go`, `CHANGELOG.md`.

- Render the active process's `indicator(m)` in the status line while `m.proc != nil` (reuse the existing running-indicator slot at view.go:311).
- Help/footer: document the conflict process keys (resolve actions, enter=line editor, c/a, Cancel, Leave) and the "resume" affordance; remove stale conflict-popup help.
- `CHANGELOG.md` (Added/Changed): conflict resolution is now a process — it locks the interface to the resolution flow, shows progress, and always offers Cancel/Leave; it resumes from the notice and survives relaunch into a half-finished rebase.
- Run `./test.sh race`.

Commit: `docs(tui): process indicator + conflict help/footer + changelog`

---

## Out of scope (later, separate plans)

- **Stage 2:** move the plain help/cheat-sheet, reword, rename popups onto the popup pile.
- **Stage 3:** unify the popup pile + full-screen pile into one window system (the process becomes a client).
- Generalizing the process model to other flows (interactive rebase, interrupted pull) — the interface is shaped for it, but no second process is built here.
