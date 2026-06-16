# Switch-to-worktree Prompt Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** In the TUI, pressing `s` on a branch already checked out in another worktree opens a modal offering to jump to that worktree instead of failing with git's "already checked out" error.

**Architecture:** Jumping to a worktree is navigation (re-root the UI + cd the shell on exit via the existing `reRoot`/`switchTarget` mechanism), not a git operation — the branch is already checked out there and git state does not change. So the whole feature lives in the TUI `s` handler. The existing centered decision modal (`decisionState`) is reused, extended with an optional frontend-resolution callback so enter/esc can call `reRoot` directly instead of replying to an engine op channel. `engine.SmartSwitch` is **not** touched.

**Tech Stack:** Go 1.26, Bubble Tea (Elm-style value-receiver `Model`), package `internal/tui`.

## Global Constraints

- `internal/tui` must **not** import `internal/git` (archtest-guarded). This feature needs no new git access — it reads the already-loaded `m.worktrees`.
- TUI `Model` is a value receiver; mutable cross-copy state (`modal`) is a pointer field. Do not capture a stale `Model` value in a closure — pass the live model into the resolution callback (see Task 1).
- Decision modals render via `renderModal`, which reads only `m.modal.req.Prompt` and `m.modal.req.Options`. A frontend modal must populate a valid `engine.DecisionRequest`.
- Tests use a real `git` in a temp dir via `newRepoDir`/`runGit`/`loadModel` (package `internal/tui`).
- Scope is **TUI only**; CLI and `engine.SmartSwitch` are unchanged.

---

### Task 1: Frontend-resolved jump-to-worktree modal on `s`

**Files:**
- Modify: `internal/tui/op.go` — add `onResolve` field to `decisionState` (around lines 67-72).
- Modify: `internal/tui/model.go` — modal key handler honors `onResolve` (around lines 277-282); `case "s"` redirects when the branch lives in another worktree (around lines 430-433).
- Modify: `internal/tui/avail.go` — add `worktreeForBranch` helper (next to `selectedWorktree`, around line 48).
- Test: `internal/tui/switch_to_worktree_test.go` (new).

**Interfaces:**
- Consumes: `m.worktrees []model.Worktree` (fields `.Branch`, `.Path`), `m.currentWorktree string`, `m.reRoot(path string) (tea.Model, tea.Cmd)`, `m.selectedBranch() (model.Branch, bool)`, `m.canSwitchBranch() bool`, `abortOption([]string) string`, `engine.DecisionRequest{ID, Prompt, Options}`.
- Produces:
  - `decisionState.onResolve func(m Model, opt string) (tea.Model, tea.Cmd)` — when non-nil, the modal key handler calls it (with the live, modal-cleared model and the chosen option) instead of sending to the engine reply channel.
  - `(Model).worktreeForBranch(branch string) (model.Worktree, bool)` — a loaded worktree other than the current one that has `branch` checked out.

- [ ] **Step 1: Write the failing test (modal opens, no git op)**

Create `internal/tui/switch_to_worktree_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"
)

// selectBranchRow moves the Branches selection to the row named name.
func selectBranchRow(t *testing.T, m *Model, name string) {
	t.Helper()
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == name {
			return
		}
	}
	t.Fatalf("%s not in branches panel: %+v", name, m.branches)
}

func TestSKeyOnOtherWorktreeBranchOpensJumpModal(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	updated, cmd := m.Update(keyMsg("s"))
	m = updated.(Model)

	if m.running {
		t.Fatal("jumping to a worktree is navigation; no git op should start")
	}
	if cmd != nil {
		t.Fatal("opening the modal should not return a command")
	}
	if m.modal == nil {
		t.Fatal("s on an other-worktree branch should open the jump modal")
	}
	if got := m.modal.req.Options; len(got) != 2 || got[0] != "go to worktree" || got[1] != "cancel" {
		t.Fatalf("modal options = %v", got)
	}
	if !strings.Contains(m.modal.req.Prompt, "feature/e") {
		t.Fatalf("prompt should name the branch: %q", m.modal.req.Prompt)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestSKeyOnOtherWorktreeBranchOpensJumpModal -v`
Expected: FAIL — `s` runs `SmartSwitch` (so `m.running` is true / `cmd != nil` and `m.modal == nil`).

- [ ] **Step 3: Add the `onResolve` field to `decisionState`**

In `internal/tui/op.go`, extend the struct (it currently has `req`, `reply`, `sel`):

```go
// decisionState holds an in-flight modal decision. Engine-driven decisions
// answer over reply; frontend-only decisions (e.g. jump-to-worktree) set
// onResolve instead — the modal key handler calls it with the live,
// modal-cleared model and the chosen option, and returns its result.
type decisionState struct {
	req       engine.DecisionRequest
	reply     chan engine.DecisionResponse
	sel       int
	onResolve func(m Model, opt string) (tea.Model, tea.Cmd)
}
```

(`tea` is already imported in `op.go`.)

- [ ] **Step 4: Make the modal handler honor `onResolve`**

In `internal/tui/model.go`, replace the `enter`/`esc` cases inside `if m.modal != nil {` (lines ~277-282):

```go
			case "enter":
				opt := m.modal.req.Options[m.modal.sel]
				if r := m.modal.onResolve; r != nil {
					m.modal = nil
					return r(m, opt)
				}
				m.modal.reply <- engine.DecisionResponse{Option: opt}
				m.modal = nil
			case "esc":
				opt := abortOption(m.modal.req.Options)
				if r := m.modal.onResolve; r != nil {
					m.modal = nil
					return r(m, opt)
				}
				m.modal.reply <- engine.DecisionResponse{Option: opt}
				m.modal = nil
```

- [ ] **Step 5: Add the `worktreeForBranch` helper**

In `internal/tui/avail.go`, after `selectedWorktree` (around line 48):

```go
// worktreeForBranch returns a loaded worktree other than the current one that
// has branch checked out, if any — the case where SmartSwitch would fail
// because git refuses to check a branch out in two worktrees at once.
func (m Model) worktreeForBranch(branch string) (model.Worktree, bool) {
	for _, w := range m.worktrees {
		if w.Branch == branch && w.Path != m.currentWorktree {
			return w, true
		}
	}
	return model.Worktree{}, false
}
```

(`internal/tui/avail.go` already imports `internal/model` — `selectedWorktree` returns `model.Worktree`.)

- [ ] **Step 6: Redirect the `s` handler to the modal**

The redirect lives **inside** `if m.canSwitchBranch()` — this is correct because `canSwitchBranch` only excludes `b.IsHead`, and `IsHead` is set from the `*` marker of `for-each-ref %(HEAD)` (`internal/git/branch_parse.go:29`), which marks **only the current worktree's HEAD**. A branch checked out in *another* worktree has `IsHead == false`, so the gate passes and the redirect fires. (Verified — do not move the block outside the gate.)

In `internal/tui/model.go`, in `case "s":`, replace the `canSwitchBranch` block (lines ~430-433):

```go
			if m.canSwitchBranch() {
				b, _ := m.selectedBranch()
				if wt, ok := m.worktreeForBranch(b.Name); ok {
					wtPath := wt.Path
					m.modal = &decisionState{
						req: engine.DecisionRequest{
							ID:      "switch-to-worktree",
							Prompt:  b.Name + " is checked out in another worktree:\n" + wtPath,
							Options: []string{"go to worktree", "cancel"},
						},
						onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
							if opt == "go to worktree" {
								return m.reRoot(wtPath)
							}
							return m, nil
						},
					}
					return m, nil
				}
				return m.startOp(engine.SmartSwitch{Branch: b.Name})
			}
```

- [ ] **Step 7: Run the test to verify it passes**

Run: `go test ./internal/tui/ -run TestSKeyOnOtherWorktreeBranchOpensJumpModal -v`
Expected: PASS

- [ ] **Step 8: Add the "go to worktree" resolution test**

Append to `internal/tui/switch_to_worktree_test.go`:

```go
func TestJumpModalGoSwitchesToWorktree(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	u, _ := m.Update(keyMsg("s"))
	m = u.(Model)
	// selection 0 == "go to worktree"
	u, cmd := m.Update(keyMsg("enter"))
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("enter should dismiss the modal")
	}
	want, _ := filepath.EvalSymlinks(wt)
	got, _ := filepath.EvalSymlinks(m.switchTarget)
	if got != want {
		t.Fatalf("switchTarget = %q, want %q", got, want)
	}
	if cmd == nil {
		t.Fatal("jumping to a worktree should return a reload command")
	}
}
```

- [ ] **Step 9: Add the cancel/esc no-op test**

Append:

```go
func TestJumpModalCancelDoesNothing(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-feat-e")
	runGit(t, dir, "worktree", "add", "-b", "feature/e", wt, "main")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/e")

	u, _ := m.Update(keyMsg("s"))
	m = u.(Model)
	u, cmd := m.Update(keyMsg("esc")) // abortOption -> "cancel"
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("esc should dismiss the modal")
	}
	if m.switchTarget != "" {
		t.Fatalf("esc must not switch: switchTarget = %q", m.switchTarget)
	}
	if cmd != nil {
		t.Fatal("esc should not return a command")
	}
}
```

- [ ] **Step 10: Add the no-conflict regression test (still SmartSwitch)**

Append:

```go
func TestSKeyOnLocalBranchStillSmartSwitches(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/local")

	m := loadModel(t, repo)
	m.focus = panelBranches
	selectBranchRow(t, &m, "feature/local")

	u, cmd := m.Update(keyMsg("s"))
	m = u.(Model)

	if m.modal != nil {
		t.Fatal("a branch not checked out elsewhere must not open the jump modal")
	}
	if !m.running || cmd == nil {
		t.Fatal("s on a normal branch should start SmartSwitch")
	}
}
```

- [ ] **Step 11: Run the full new test file**

Run: `go test ./internal/tui/ -run 'TestSKeyOnOtherWorktreeBranchOpensJumpModal|TestJumpModal|TestSKeyOnLocalBranchStillSmartSwitches' -v`
Expected: PASS (all four)

- [ ] **Step 12: Run the package suite to catch regressions**

Run: `go test ./internal/tui/`
Expected: PASS (the existing `TestEnterOnWorktreePanelSwitches` and modal/decision tests still pass — engine-driven modals have `onResolve == nil` and keep the reply-channel path).

- [ ] **Step 13: Commit**

```bash
git add internal/tui/op.go internal/tui/model.go internal/tui/avail.go internal/tui/switch_to_worktree_test.go
git commit -m "feat(tui): s on an other-worktree branch offers to jump to that worktree

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Docs — help pane and changelog

**Files:**
- Modify: `internal/tui/help.go` (the `s` line, around line 16).
- Modify: `CHANGELOG.md` (Unreleased → Added).
- Check: `README.md` — update only if it has a Branches-panel key table that describes `s`.

**Interfaces:**
- Consumes: nothing new. Documentation only.
- Produces: nothing consumed by other tasks.

- [ ] **Step 1: Update the help pane line for `s`**

In `internal/tui/help.go`, replace the `s` row (line ~16):

```go
		r("s", "switch to the selected branch (SmartSwitch); if it's checked out in another worktree, offers to jump there"),
```

- [ ] **Step 2: Verify help still renders (no test asserts this exact string, but build it)**

Run: `go build ./... && go test ./internal/tui/ -run TestHelp`
Expected: PASS (or "no tests to run" for the filter — the point is the build succeeds).

- [ ] **Step 3: Add the CHANGELOG entry**

In `CHANGELOG.md`, under `## [Unreleased]`, add an `### Added` entry (create the `### Added` subsection if absent, above `### Fixed`):

```markdown
### Added
- TUI: pressing `s` on a branch that's already checked out in **another
  worktree** now opens a modal offering to **jump to that worktree** (re-root
  the UI and cd there on exit), instead of failing with git's "already checked
  out" error. Choosing *cancel* / `esc` does nothing; branches not checked out
  elsewhere still `SmartSwitch` as before.
```

Note: do **not** run `git add -A`. The shared checkout may carry unrelated parallel work; stage only this feature's files. You are in the dedicated worktree, but keep the habit.

- [ ] **Step 4: Check README**

Run: `grep -n '"s"\|\[s\]witch\| s \b' README.md` (and skim any Branches/keybinding table). If `s` is documented in a key table, mirror the help-pane wording. If not, skip — no change needed.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/help.go CHANGELOG.md
# add README.md too ONLY if you changed it
git commit -m "docs: document s jump-to-worktree behavior (help, changelog)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification

- [ ] Run the race suite before declaring done: `./test.sh race` (or at minimum `go test -race ./internal/tui/`).
- [ ] Confirm `git status` in the worktree shows only this feature's files; the spec + plan + the four touched source files + tests + docs.

## Self-Review notes (author)

- **Spec coverage:** behavior (scan worktrees → modal → reRoot / cancel) → Task 1 Steps 6, 8, 9; no "switch anyway" option → modal offers only two options (Step 6); gate unchanged (`canSwitchBranch` untouched) → Step 6 keeps it; no new popup type / no async query → reuses `decisionState`, scans in-model `m.worktrees`; SmartSwitch untouched → no engine file in the file list; docs → Task 2.
- **Type consistency:** `onResolve func(m Model, opt string) (tea.Model, tea.Cmd)` defined in Task 1 Step 3 and used identically in Steps 4 and 6; `worktreeForBranch(branch string) (model.Worktree, bool)` defined Step 5, used Step 6.
- **No stale-capture footgun:** the closure captures only `wtPath` (a string); the live model is passed into `onResolve` by the handler after it nils `m.modal`.
