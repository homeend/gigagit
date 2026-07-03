# T — Fullscreen Maximize Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `T` toggles the focused panel (any small left-column panel OR Commits) to fill the entire terminal body; `t` keeps meaning "whole left column".

**Architecture:** A second pin pair (`fullMaxed`/`fullMax`) layered over the existing `leftMaxed`/`leftMax`, applied as a final override block in `layout()` (the single geometry source of truth). Because `T` never touches the `t` pin, "T again returns to where you were" falls out for free. Rendering, hit-testing, and paging all read `layout()`, so most surfaces need no changes.

**Tech Stack:** Go 1.26, Bubble Tea TUI (`internal/tui`), table-driven tests with a real `Model` fixture.

**Spec:** `docs/superpowers/specs/2026-07-03-fullscreen-maximize-design.md`

## Global Constraints

- **Worktree:** ALL work happens in `/mnt/t/others/gigagit/.claude/worktrees/fullscreen-maximize` on branch `feat/fullscreen-maximize`. `cd` there first and verify with `git branch --show-current` before every task. Never touch the main checkout.
- **TDD:** every task writes its failing test first, watches it fail, then implements.
- **Test commands** run from the worktree root: `go test ./internal/tui/ -run '<Name>' -v`.
- `internal/tui` never imports `internal/git` (archtest-guarded); nothing in this plan needs new imports beyond what the touched files already have.
- Commit messages end with:
  ```
  Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
  Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y
  ```
- New test file is `internal/tui/maximize_full_test.go` (safe name: no GOOS/GOARCH suffix).

## Key existing code (read before starting any task)

- `internal/tui/model.go:167-168` — the `leftMax`/`leftMaxed` fields the new pair mirrors.
- `internal/tui/model.go:1133-1142` — the `t` key case.
- `internal/tui/model.go:2286-2308` — `canMaximizeLeft` + `focusOrder`.
- `internal/tui/model.go:2236-2255` — `activateTab` (re-pins `leftMax` on tab switch).
- `internal/tui/viewstate.go:32-111` — `layout()`; the left-maximize block is lines 96-106, Commits geometry lines 108-109.
- `internal/tui/view.go:412-452` — `renderInterface` body assembly (left column loop + right switch + JoinHorizontal).
- `internal/tui/maximize_left_test.go` — the test style to mirror; `maxModel()` fixture (120×40, Branches/Files/Staged visible), `keyMsg("t")` + `m.Update(...)` dispatch pattern, `u.(Model)` to unwrap.

---

### Task 1: Pin state + `fullMaxActive` + layout override

**Files:**
- Modify: `internal/tui/model.go:167-168` (fields), `internal/tui/model.go` next to `canMaximizeLeft` (~line 2286)
- Modify: `internal/tui/viewstate.go` (`layout()`, insert after the Commits geometry at lines 108-109, before `return g`)
- Test: `internal/tui/maximize_full_test.go` (create)

**Interfaces:**
- Consumes: `Model.leftColumnPanels() []panel`, `layoutGeom{boxH, pos, leftW, rightW, bodyH}`, `maxModel()` test fixture.
- Produces: fields `fullMaxed bool` / `fullMax panel`; methods `Model.fullMaxActive() bool` and `Model.canFullMaximize() bool` (exact semantics below) — Tasks 2–4 call these names.

- [ ] **Step 1: Write the failing layout tests**

Create `internal/tui/maximize_full_test.go`:

```go
package tui

import (
	"slices"
	"testing"
)

func TestLayoutFullscreenLeftPanel(t *testing.T) {
	m := maxModel() // 120×40: Branches/Files/Staged + Commits
	m.fullMaxed = true
	m.fullMax = panelFiles
	g := m.layout()

	if g.boxH[panelFiles] != g.bodyH {
		t.Errorf("pinned boxH = %d, want bodyH %d", g.boxH[panelFiles], g.bodyH)
	}
	if g.pos[panelFiles] != (point{0, 1}) {
		t.Errorf("pinned pos = %v, want {0,1}", g.pos[panelFiles])
	}
	if g.leftW != g.w {
		t.Errorf("leftW = %d, want full width %d", g.leftW, g.w)
	}
	if g.rightW != 0 {
		t.Errorf("rightW = %d, want 0", g.rightW)
	}
	for _, p := range []panel{panelBranches, panelStaged, panelCommits} {
		if g.boxH[p] != 0 {
			t.Errorf("%v boxH = %d, want 0 (hidden)", p, g.boxH[p])
		}
	}
}

func TestLayoutFullscreenCommits(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelCommits
	g := m.layout()

	if g.boxH[panelCommits] != g.bodyH || g.pos[panelCommits] != (point{0, 1}) {
		t.Errorf("commits boxH=%d pos=%v, want bodyH at {0,1}", g.boxH[panelCommits], g.pos[panelCommits])
	}
	if g.rightW != g.w || g.leftW != 0 {
		t.Errorf("leftW=%d rightW=%d, want 0 and %d", g.leftW, g.rightW, g.w)
	}
	for _, p := range []panel{panelBranches, panelFiles, panelStaged} {
		if g.boxH[p] != 0 {
			t.Errorf("%v boxH = %d, want 0 (hidden)", p, g.boxH[p])
		}
	}
}

// Fullscreen wins over a t column-pin underneath (the ladder's top level).
func TestLayoutFullscreenBeatsColumnPin(t *testing.T) {
	m := maxModel()
	m.leftMaxed = true
	m.leftMax = panelFiles
	m.fullMaxed = true
	m.fullMax = panelFiles
	g := m.layout()
	if g.leftW != g.w || g.boxH[panelCommits] != 0 {
		t.Errorf("leftW=%d commits boxH=%d, want full width and hidden commits", g.leftW, g.boxH[panelCommits])
	}
}

// Stale pin: fullMax not in the visible set ⇒ normal split, never a blank screen.
func TestLayoutFullscreenStalePinFallsBack(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelRemotes // NOT the active top tab (panelBranches)
	g := m.layout()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged, panelCommits} {
		if g.boxH[p] <= 0 {
			t.Errorf("fallback: %v should be visible, boxH=%d", p, g.boxH[p])
		}
	}
}

// Full-screen-ish surfaces win: files view, stash list, file preview all need
// their column, so an active one suspends the fullscreen pin (it resumes when
// the surface closes — the flag is not cleared).
func TestFullMaxActiveYieldsToSurfaces(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelFiles
	if !m.fullMaxActive() {
		t.Fatal("baseline: pin should be active")
	}
	fv := m
	fv.filesView = &contentPopup{}
	if fv.fullMaxActive() {
		t.Error("filesView active: pin must yield")
	}
	sv := m
	sv.stashView = &stashView{}
	if sv.fullMaxActive() {
		t.Error("stashView active: pin must yield")
	}
	pv := m
	pv.filesPreview = &contentPopup{}
	if pv.fullMaxActive() {
		t.Error("filesPreview active: pin must yield")
	}
}

func TestCanFullMaximize(t *testing.T) {
	m := maxModel()
	for _, p := range []panel{panelBranches, panelFiles, panelStaged, panelCommits} {
		m.focus = p
		if !m.canFullMaximize() {
			t.Errorf("focus %v: want canFullMaximize", p)
		}
	}
	fv := m
	fv.filesView = &contentPopup{}
	if fv.canFullMaximize() {
		t.Error("filesView active: T must be inert")
	}
}
```

(Field types confirmed at model.go:89-104: `filesView *contentPopup`, `stashView *stashView`, `filesPreview *contentPopup` — all same-package structs, empty literals compile.)

- [ ] **Step 2: Run tests to verify they fail**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/fullscreen-maximize
go test ./internal/tui/ -run 'TestLayoutFullscreen|TestFullMaxActive|TestCanFullMaximize' -v
```
Expected: compile FAILURE — `m.fullMaxed undefined`, `m.fullMaxActive undefined`.

- [ ] **Step 3: Implement fields, predicates, layout block**

In `model.go`, directly below the `leftMax`/`leftMaxed` fields (lines 167-168):

```go
	fullMax   panel // the pinned fullscreen panel (valid only when fullMaxed)
	fullMaxed bool  // T has maximized fullMax to fill the entire body
```

In `model.go`, directly below `canMaximizeLeft` (~line 2295):

```go
// canFullMaximize reports whether T can pin the focused panel fullscreen:
// focus is a small left-column panel or Commits, and no surface that needs
// its own column is up (files view owns the left column; stash list and file
// preview own the right one).
func (m Model) canFullMaximize() bool {
	if m.filesView != nil || m.stashView != nil || m.filesPreview != nil {
		return false
	}
	return m.focus == panelCommits || slices.Contains(m.leftColumnPanels(), m.focus)
}

// fullMaxActive reports whether the T pin is currently driving the layout.
// Same surface-yield rule as canFullMaximize (the pin is suspended, not
// cleared, while such a surface is up) plus the stale-pin guard: a pin that
// fell out of the visible set falls back to the normal split rather than
// blanking the screen. On a narrow (<40) terminal leftColumnPanels() is
// empty, so a left-panel pin deactivates itself there too.
func (m Model) fullMaxActive() bool {
	if !m.fullMaxed || m.filesView != nil || m.stashView != nil || m.filesPreview != nil {
		return false
	}
	return m.fullMax == panelCommits || slices.Contains(m.leftColumnPanels(), m.fullMax)
}
```

In `viewstate.go` `layout()`, after the Commits geometry (current lines 108-109: `g.boxH[panelCommits] = bodyH; g.pos[panelCommits] = point{leftW, 1}`) and before `return g`:

```go
	// Fullscreen: a T-pinned panel takes the entire body — both columns. Runs
	// last so it overrides both the normal split and a t column-pin underneath
	// (which stays set: dropping fullscreen returns to it). fullMaxActive
	// carries the stale-pin fallback and yields to the files view / stash list
	// / file preview, which need their own column.
	if m.fullMaxActive() {
		for _, p := range append(m.leftColumnPanels(), panelCommits) {
			if p != m.fullMax {
				delete(g.boxH, p)
				delete(g.pos, p)
			}
		}
		g.boxH[m.fullMax] = bodyH
		g.pos[m.fullMax] = point{0, 1}
		if m.fullMax == panelCommits {
			g.leftW, g.rightW = 0, w
		} else {
			g.leftW, g.rightW = w, 0
		}
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -run 'TestLayoutFullscreen|TestFullMaxActive|TestCanFullMaximize' -v
```
Expected: PASS (all 6).

Also run the whole package to catch regressions (the existing maximize/layout/mouse tests must stay green):

```bash
go test ./internal/tui/
```
Expected: ok.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/viewstate.go internal/tui/maximize_full_test.go
git commit -m "feat(tui): fullscreen pin state + layout override for T

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y"
```

---

### Task 2: The T / t / esc key ladder

**Files:**
- Modify: `internal/tui/model.go:1133-1142` (the `t` case; add `T` beside it), `internal/tui/model.go:1552-1567` (the `esc` chain)
- Test: `internal/tui/maximize_full_test.go` (append)

**Interfaces:**
- Consumes: `canFullMaximize()`, `canMaximizeLeft()`, fields `fullMaxed`/`fullMax`/`leftMaxed`/`leftMax` (Task 1).
- Produces: key behavior only — no new names.

- [ ] **Step 1: Write the failing ladder tests**

Append to `maximize_full_test.go`:

```go
// press dispatches one key and unwraps the model.
func press(t *testing.T, m Model, key string) Model {
	t.Helper()
	u, _ := m.Update(keyMsg(key))
	return u.(Model)
}

func TestFullscreenToggleT(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles

	m = press(t, m, "T")
	if !m.fullMaxed || m.fullMax != panelFiles {
		t.Fatalf("after T: fullMaxed=%v fullMax=%v", m.fullMaxed, m.fullMax)
	}
	if m.leftMaxed {
		t.Fatal("T must not set the t column pin")
	}
	m = press(t, m, "T")
	if m.fullMaxed {
		t.Fatal("second T must restore")
	}
}

func TestFullscreenOnCommits(t *testing.T) {
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "T")
	if !m.fullMaxed || m.fullMax != panelCommits {
		t.Fatalf("after T on Commits: fullMaxed=%v fullMax=%v", m.fullMaxed, m.fullMax)
	}
	// t stays inert on Commits, fullscreen or not.
	m = press(t, m, "t")
	if m.leftMaxed || !m.fullMaxed {
		t.Fatalf("t on fullscreen Commits: leftMaxed=%v fullMaxed=%v, want false/true", m.leftMaxed, m.fullMaxed)
	}
}

// t → T → T lands back on column-maximized: the t pin survives underneath.
func TestLadderColumnThenFullscreenThenBack(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "t")
	m = press(t, m, "T")
	if !m.fullMaxed || !m.leftMaxed {
		t.Fatalf("t then T: fullMaxed=%v leftMaxed=%v, want both", m.fullMaxed, m.leftMaxed)
	}
	m = press(t, m, "T")
	if m.fullMaxed || !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("T again: fullMaxed=%v leftMaxed=%v leftMax=%v, want column-maximized Files", m.fullMaxed, m.leftMaxed, m.leftMax)
	}
}

// t while fullscreen drops exactly one level: to column-maximized, never a
// hidden double-toggle back to normal.
func TestLadderTDropsFullscreenToColumn(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m = press(t, m, "t")
	if m.fullMaxed {
		t.Fatal("t while fullscreen must clear the fullscreen pin")
	}
	if !m.leftMaxed || m.leftMax != panelFiles {
		t.Fatalf("t while fullscreen: leftMaxed=%v leftMax=%v, want column pin on Files", m.leftMaxed, m.leftMax)
	}
}

func TestEscExitsFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m = press(t, m, "esc")
	if m.fullMaxed {
		t.Fatal("esc must exit fullscreen")
	}
}

// esc is the lowest-priority consumer: an active filter clears first and the
// same press must NOT also drop fullscreen.
func TestEscPrefersFilterOverFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m.filterQuery = "x"
	m = press(t, m, "esc")
	if m.filterQuery != "" {
		t.Fatal("esc should clear the filter")
	}
	if !m.fullMaxed {
		t.Fatal("the filter-clearing esc must not also exit fullscreen")
	}
	m = press(t, m, "esc")
	if m.fullMaxed {
		t.Fatal("second esc exits fullscreen")
	}
}

func TestFullscreenInertInFilesView(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m.filesView = &contentPopup{}
	m = press(t, m, "T")
	if m.fullMaxed {
		t.Fatal("T must be inert while the files view owns the screen")
	}
}
```

NOTE: the files view may intercept the key before the global switch reaches `case "T"`. Either route satisfies the test — the assertion only cares that `fullMaxed` stays false. If the empty `&contentPopup{}` makes the files-view key handler panic, seed whatever minimal fields its update path dereferences (look at how `files_view.go` tests construct it) rather than weakening the assertion.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui/ -run 'TestFullscreen|TestLadder|TestEsc' -v
```
Expected: FAIL — `T` is currently unbound (fullMaxed never set), esc does nothing.

- [ ] **Step 3: Implement the handlers**

In `model.go`, replace the `t` case (current lines 1133-1142) with the ladder version, and add `T` directly after:

```go
		case "t": // toggle maximize of the focused left-column panel
			if m.canMaximizeLeft() {
				switch {
				case m.fullMaxed:
					// Drop one level: fullscreen → column-maximize. Never a
					// hidden double-toggle of the pin underneath.
					m.fullMaxed = false
					m.leftMaxed = true
					m.leftMax = m.focus
				case m.leftMaxed && m.leftMax == m.focus:
					m.leftMaxed = false
				default:
					m.leftMaxed = true
					m.leftMax = m.focus
				}
			}
			return m, nil
		case "T": // toggle fullscreen of the focused panel (left panel or Commits)
			if m.canFullMaximize() {
				if m.fullMaxed && m.fullMax == m.focus {
					m.fullMaxed = false // back to whatever t-state sits underneath
				} else {
					m.fullMaxed = true
					m.fullMax = m.focus
				}
			}
			return m, nil
```

In the `esc` case (current lines 1552-1567), add the fullscreen exit as the LAST link, and give the filter branch an explicit return so one press does one thing:

```go
		case "esc":
			if m.mark != nil {
				m.mark = nil
				return m, nil
			}
			// A committed @-highlight clears first (it never reorders/filters, so it
			// is the lighter state to drop).
			if m.highlightQuery != "" {
				m.highlightQuery = ""
				return m, nil
			}
			// filterPanel is intentionally left set — filterActive() gates on a
			// non-empty query, so the residue is inert.
			if m.filterQuery != "" {
				m.filterQuery = ""
				return m, nil
			}
			// Lowest priority: with nothing lighter to drop, esc exits a T
			// fullscreen (back to the t-state underneath — never-trap rule).
			if m.fullMaxed {
				m.fullMaxed = false
				return m, nil
			}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -run 'TestFullscreen|TestLadder|TestEsc' -v && go test ./internal/tui/
```
Expected: PASS, package ok (existing `t` toggle tests in `maximize_left_test.go` must stay green — the non-fullscreen branches are unchanged).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/maximize_full_test.go
git commit -m "feat(tui): T fullscreen toggle + t/esc ladder

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y"
```

---

### Task 3: Focus containment + tab re-pin

**Files:**
- Modify: `internal/tui/model.go:2297-2308` (`focusOrder`), `internal/tui/model.go:2326-2338` (`leftReturnTarget`), `internal/tui/model.go:1328-1338` (`right`/`left` arrow cases), `internal/tui/model.go:2251-2253` (`activateTab` re-pin)
- Test: `internal/tui/maximize_full_test.go` (append)

**Interfaces:**
- Consumes: `fullMaxActive()`, `fullMaxed`/`fullMax` (Task 1); the `press(t, m, key) Model` test helper (defined at the top of Task 2's test additions in `maximize_full_test.go`); `activateTab`, `focusOrder`, `nextInOrder`.
- Produces: behavior only — no new names.

- [ ] **Step 1: Write the failing focus tests**

Append to `maximize_full_test.go`:

```go
func TestFocusOrderCollapsesFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	got := m.focusOrder()
	if len(got) != 1 || got[0] != panelFiles {
		t.Fatalf("fullscreen focusOrder = %v, want [Files]", got)
	}
	// tab cycles nowhere
	m = press(t, m, "tab")
	if m.focus != panelFiles {
		t.Fatalf("tab moved focus to %v, want pinned Files", m.focus)
	}
}

// A stale fullscreen pin must not trap focus on a hidden panel: focusOrder
// falls back to the normal order, mirroring layout's stale-pin fallback.
func TestFocusOrderStaleFullscreenPinFallsBack(t *testing.T) {
	m := maxModel()
	m.fullMaxed = true
	m.fullMax = panelRemotes // not visible
	got := m.focusOrder()
	want := []panel{panelBranches, panelFiles, panelStaged, panelCommits}
	if !slices.Equal(got, want) {
		t.Fatalf("stale pin focusOrder = %v, want %v", got, want)
	}
}

func TestArrowsStayInsideFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	m = press(t, m, "right")
	if m.focus != panelFiles {
		t.Fatalf("→ moved focus to hidden %v", m.focus)
	}

	c := maxModel()
	c.focus = panelCommits
	c = press(t, c, "T")
	c = press(t, c, "left")
	if c.focus != panelCommits {
		t.Fatalf("← moved focus to hidden %v", c.focus)
	}
}

// ctrl+→ while fullscreen re-pins fullscreen to the newly shown tab (mirrors
// the leftMaxed re-pin in activateTab). From Commits the pin transfers to the
// activated left tab instead of stranding focus on a hidden box.
func TestTabSwitchRepinsFullscreen(t *testing.T) {
	m := maxModel()
	m.focus = panelBranches
	m = press(t, m, "T")
	m = m.activateTab(panelWorktrees)
	if !m.fullMaxed || m.fullMax != panelWorktrees {
		t.Fatalf("after tab switch: fullMaxed=%v fullMax=%v, want Worktrees pinned", m.fullMaxed, m.fullMax)
	}

	c := maxModel()
	c.focus = panelCommits
	c = press(t, c, "T")
	c = c.activateTab(panelWorktrees)
	if !c.fullMaxed || c.fullMax != panelWorktrees || c.focus != panelWorktrees {
		t.Fatalf("from Commits: fullMaxed=%v fullMax=%v focus=%v, want Worktrees", c.fullMaxed, c.fullMax, c.focus)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui/ -run 'TestFocusOrderCollapsesFullscreen|TestFocusOrderStaleFullscreen|TestArrowsStayInside|TestTabSwitchRepins' -v
```
Expected: FAIL — focusOrder still returns the normal set, arrows escape, no re-pin.

- [ ] **Step 3: Implement**

`focusOrder` (model.go:2297) — new first branch:

```go
func (m Model) focusOrder() []panel {
	// While a panel is fullscreen it is the only target — everything else is
	// hidden. fullMaxActive (not the raw flag) so a stale/yielded pin falls
	// back to the normal order instead of trapping focus on a hidden panel.
	if m.fullMaxActive() {
		return []panel{m.fullMax}
	}
	// While a left panel is maximized, focus collapses to that panel and Commits
	// — the other left panels are hidden, so they must not be tab targets.
	if m.leftMaxed {
		return []panel{m.leftMax, panelCommits}
	}
	... // rest unchanged
```

`leftReturnTarget` (model.go:2326) — new first branch (defensive parity with the leftMaxed one):

```go
	if m.fullMaxActive() && m.fullMax != panelCommits { // fullscreen: only left target
		return m.fullMax
	}
	if m.leftMaxed { // maximized: the pinned panel is the only left target
		return m.leftMax
	}
```

Arrow cases (model.go:1328-1338) — gate both with `fullMaxActive`:

```go
		case "right":
			if m.focus != panelCommits && !m.fullMaxActive() {
				m = m.rememberLeftFocus()
				m.focus = panelCommits
			}
		case "left":
			// No-op when already in the left column, when the narrow layout has
			// no left column to focus, and when Commits is fullscreen (the left
			// column is hidden).
			if m.focus == panelCommits && (m.width <= 0 || m.width >= 40) && !m.fullMaxActive() {
				m.focus = m.leftReturnTarget()
			}
```

`activateTab` (model.go:2251) — after the existing leftMaxed re-pin:

```go
	if m.leftMaxed { // re-pin the newly shown tab so it stays full-height
		m.leftMax = m.focus
	}
	if m.fullMaxed { // keep fullscreen on the newly shown tab (incl. from Commits)
		m.fullMax = m.focus
	}
```

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -run 'TestFocusOrder|TestArrows|TestTabSwitch' -v && go test ./internal/tui/
```
Expected: PASS, package ok.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/model.go internal/tui/maximize_full_test.go
git commit -m "feat(tui): fullscreen focus containment + tab re-pin

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y"
```

---

### Task 4: Rendering — no degenerate columns

**Files:**
- Modify: `internal/tui/view.go:419-449` (`renderInterface` body assembly)
- Test: `internal/tui/maximize_full_test.go` (append)

**Interfaces:**
- Consumes: `layout()` geometry from Task 1 (`g.boxH[panelCommits]` is 0 / `g.rightW` is 0 under a left-panel fullscreen; `g.leftW` is 0 under a Commits fullscreen); the `press(t, m, key) Model` test helper from Task 2's test additions.
- Produces: behavior only.

- [ ] **Step 1: Write the failing render tests**

Append to `maximize_full_test.go` (add `"strings"` to the imports):

```go
// View must not draw a degenerate 0-width column: no Commits box under a
// left-panel fullscreen, no left boxes (tab labels included) under a Commits
// fullscreen, and never a panic.
func TestViewFullscreenLeftPanelHidesCommits(t *testing.T) {
	m := maxModel()
	m.focus = panelFiles
	m = press(t, m, "T")
	v := m.View()
	if strings.Contains(v, "Commits (") {
		t.Error("fullscreen Files: Commits box should not render")
	}
}

func TestViewFullscreenCommitsHidesLeftColumn(t *testing.T) {
	m := maxModel()
	m.focus = panelCommits
	m = press(t, m, "T")
	v := m.View()
	if !strings.Contains(v, "Commits (") {
		t.Error("fullscreen Commits: Commits box missing")
	}
	if strings.Contains(v, "Branches") || strings.Contains(v, "Staged") {
		t.Error("fullscreen Commits: left-column labels should not render")
	}
}
```

NOTE: if `footer`/`statusLine` happen to contain "Branches"/"Staged"/"Commits (" text, tighten the assertions to the body only (split `v` on newlines and check rows 1..bodyH). Check the actual failure output before changing the implementation.

- [ ] **Step 2: Run tests to verify they fail**

```bash
go test ./internal/tui/ -run 'TestViewFullscreen' -v
```
Expected: FAIL (both boxes currently render, squeezed).

- [ ] **Step 3: Implement**

In `renderInterface` (view.go): delete the free-standing `cmRows, _, cmDecos := m.commitBody(g.rightW, g.boxH[panelCommits])` at line 419 (its only consumer is the `default` case below), and replace the `right` switch and the join (lines 440-449) with:

```go
	var right string
	switch {
	case g.boxH[panelCommits] <= 0:
		// a fullscreen left panel owns the whole body — no right column at all
	case m.filesPreview != nil:
		right = m.renderFilePreview(g.rightW, g.boxH[panelCommits])
	case m.stashView != nil:
		right = m.renderStashList(g.rightW, g.boxH[panelCommits])
	default:
		cmRows, _, cmDecos := m.commitBody(g.rightW, g.boxH[panelCommits])
		right = m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, cmDecos, g.rightW, g.boxH[panelCommits])
	}
	// One side can be empty (a T fullscreen hides the other column entirely);
	// join only when both exist so no zero-width block leaks artifacts.
	var body string
	switch {
	case left == "":
		body = right
	case right == "":
		body = left
	default:
		body = lipgloss.JoinHorizontal(lipgloss.Top, left, right)
	}
```

The left column needs NO change: the existing loop already skips `boxH<=0` panels, so under a Commits fullscreen it produces zero boxes and `left` is `""`, and under a left-panel fullscreen it renders the one pinned box at `g.leftW == w`.

Mouse/tab hit-testing needs NO change: `panelAt` skips `boxH<=0`, and `tabClickAt` is only reachable through `panelAt`.

- [ ] **Step 4: Run tests to verify they pass**

```bash
go test ./internal/tui/ -run 'TestViewFullscreen' -v && go test ./internal/tui/
```
Expected: PASS, package ok.

- [ ] **Step 5: Eyeball it live** (per the run-before-claiming-done rule)

```bash
go build -o ./gg ./cmd/gg && ls -la ./gg
```
Report the ABSOLUTE path `/mnt/t/others/gigagit/.claude/worktrees/fullscreen-maximize/gg` so the human can try `T` on each panel, `t`/`T`/`esc` ladder, ctrl+←/→ while fullscreen.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/view.go internal/tui/maximize_full_test.go
git commit -m "feat(tui): render T fullscreen without degenerate columns

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y"
```

---

### Task 5: Advertise + docs

**Files:**
- Modify: `internal/tui/footer.go:81-84` (after the `maximize` entry), `internal/tui/help.go:36` (after the `t` line), `CHANGELOG.md`, `README.md` (only if it lists keybindings — check)
- Test: existing `TestHelpFooterCoverage` drift guard (help_test.go:42)

**Interfaces:**
- Consumes: `canFullMaximize()` (Task 1), `panelLen`, `opsIdle`, the `footerBinding` struct + `scopeWindow` already in footer.go.
- Produces: none.

- [ ] **Step 1: Add the footer binding**

In `footer.go`, directly after the `{"maximize", "t", ...}` entry (lines 81-84):

```go
	{"fullscreen", "T", "[T] full", func(m Model) bool {
		// Same stricter gate as t: don't advertise fullscreening an empty box.
		return m.opsIdle() && m.canFullMaximize() && m.panelLen(m.focus) > 0
	}, scopeWindow},
```

- [ ] **Step 2: Add the help entry**

In `help.go`, directly after the `t` line (line 36), matching its `r(key, text)` style:

```go
	r("T", "fullscreen the focused panel — any left-column panel or Commits — to fill the whole screen (T or esc restores; t drops back to the left column)")
```

Adjust punctuation/wrapping to match the neighboring lines exactly (open the file and copy the local idiom).

- [ ] **Step 3: Run the drift guard + full package**

```bash
go test ./internal/tui/ -run 'TestHelpFooterCoverage' -v && go test ./internal/tui/
```
Expected: PASS — the guard sees `T` in both footer and help.

- [ ] **Step 4: Update docs**

- `CHANGELOG.md`: add an entry under the unreleased/top section following the existing entry style, e.g. `- TUI: T fullscreens the focused panel (any left-column panel or Commits) to the whole terminal; t still maximizes to the left column. esc or T restores; t drops fullscreen back to the column. Tab switching re-pins the fullscreen to the newly shown tab.`
- `README.md`: `grep -n "\[t\]\|maximize" README.md` — if the keybinding table/list mentions `t`, add `T` beside it in the same style; if not, skip.
- `CLAUDE.md` / agentskill: NOT needed (no architecture or CLI surface change).

- [ ] **Step 5: Full verification**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/fullscreen-maximize
./test.sh
```
Expected: vet+gofmt clean, unit green, e2e green (no CLI change, so e2e is regression-only).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): advertise T fullscreen in footer, help, changelog

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_01CYnco5oyNBHdVbjdHQH94y"
```

---

## Final gate (before handing to the human for merge)

```bash
./test.sh race
```
Expected: green. Then request final code review per the repo workflow; the human merges.
