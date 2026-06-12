# Left/Right Arrow Window Focus Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `←`/`→` switch window focus horizontally — between the left column and Commits in normal mode, and between the file tree and Commits inside the files view, with vertical movement following the focused side (spec: `docs/superpowers/specs/2026-06-13-arrow-focus-design.md`).

**Architecture:** Two new Model fields: `lastLeftPanel panel` (zero value `panelBranches` — `←`'s return target, recorded whenever focus moves off a left panel) and `filesTreeFocused bool` (which side of the files view owns vertical movement; `m.focus` stays `panelCommits` for the view's whole life). A `panelFocused(p)` render helper makes focus visible by blurring the Commits panel while the tree side is focused.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss. TUI-only — no engine/CLI/agent-skill changes.

**Branch:** `feat/arrow-focus` off `main`.

**Conventions for every task:** TDD (failing test → watch it fail → implement → pass); `gofmt -w` on touched files; commit messages end with
`Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

## File map

| File | Change |
|---|---|
| `internal/tui/model.go` | `lastLeftPanel` + `filesTreeFocused` fields; `left`/`right` cases; record on `tab`/`shift+tab`; reset flag on open + narrow-resize close |
| `internal/tui/model_test.go` | extend `keyMsg` with `left`/`right` |
| `internal/tui/focus_test.go` | **new** — normal-mode arrow tests |
| `internal/tui/files_view.go` | focus-aware key handler; focused border in `renderFilesView` |
| `internal/tui/files_view_test.go` | focus/movement/render/tooltip tests; update the swallow test (tab now toggles) |
| `internal/tui/view.go` | `panelFocused` helper, used by `renderPanel` |
| `internal/tui/tooltip.go` | suppress while the tree side is focused |
| `internal/tui/footer.go` | files-view footer line mentions focus keys |
| `internal/tui/help.go` | Global `←/→` row; rewritten files-view section |
| `README.md`, `CHANGELOG.md` | docs |

---

### Task 1: Normal-mode `←`/`→` + `lastLeftPanel`

**Files:**
- Modify: `internal/tui/model_test.go` (the `keyMsg` helper, ~line 9)
- Modify: `internal/tui/model.go`
- Create: `internal/tui/focus_test.go`

**Context:** The normal-key switch in `Update` (model.go) has `case "tab":` / `case "shift+tab":` at ~line 278 doing plain modular cycling; there are no `"left"`/`"right"` cases anywhere in the file (no conflicts). `panel` is an int enum: `panelBranches`(0), `panelWorktrees`, `panelStatus`, `panelCommits`. The `markModel()` test helper (mark_test.go) builds a model with branches+commits, initialized maps, `focus: panelBranches`. Below 40 columns `layout()` has no left column. Focus keys are deliberately ungated on running/loading (same as tab today).

- [ ] **Step 1: Extend `keyMsg`**

In `internal/tui/model_test.go`, add two cases to the `keyMsg` switch (next to `"up"`/`"down"`):

```go
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
```

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/focus_test.go`:

```go
package tui

import "testing"

func TestRightArrowFocusesCommitsFromEachLeftPanel(t *testing.T) {
	for _, p := range []panel{panelBranches, panelWorktrees, panelStatus} {
		m := markModel()
		m.width, m.height = 80, 24
		m.focus = p
		u, _ := m.Update(keyMsg("right"))
		m = u.(Model)
		if m.focus != panelCommits {
			t.Fatalf("focus = %v after right from %v, want commits", m.focus, p)
		}
		if m.lastLeftPanel != p {
			t.Fatalf("lastLeftPanel = %v, want %v", m.lastLeftPanel, p)
		}
	}
}

func TestLeftArrowReturnsToLastLeftPanel(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelStatus
	u, _ := m.Update(keyMsg("right"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	if m.focus != panelStatus {
		t.Fatalf("focus = %v, want status (the last left panel)", m.focus)
	}
}

func TestLeftArrowAfterTabRemembersLeftPanel(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelStatus
	u, _ := m.Update(keyMsg("tab")) // status -> commits, must record status
	m = u.(Model)
	if m.focus != panelCommits {
		t.Fatalf("setup: focus = %v, want commits", m.focus)
	}
	u, _ = m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelStatus {
		t.Fatalf("focus = %v, want status (recorded by tab)", got)
	}
}

func TestLeftArrowDefaultsToBranches(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelCommits // never visited a left panel
	u, _ := m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelBranches {
		t.Fatalf("focus = %v, want branches (zero-value default)", got)
	}
}

func TestArrowFocusEdgesNoOp(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("right")) // nothing right of commits
	if got := u.(Model).focus; got != panelCommits {
		t.Fatalf("focus = %v, right on commits must no-op", got)
	}
	m.focus = panelWorktrees
	u, _ = m.Update(keyMsg("left")) // nothing left of the left column
	if got := u.(Model).focus; got != panelWorktrees {
		t.Fatalf("focus = %v, left on a left panel must no-op", got)
	}
}

func TestLeftArrowNoOpOnNarrowTerminal(t *testing.T) {
	m := markModel()
	m.width, m.height = 30, 24 // no left column below 40
	m.focus = panelCommits
	u, _ := m.Update(keyMsg("left"))
	if got := u.(Model).focus; got != panelCommits {
		t.Fatalf("focus = %v, left must no-op with no left column", got)
	}
}
```

- [ ] **Step 3: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestRightArrow|TestLeftArrow|TestArrowFocus' -v`
Expected: COMPILE FAILURE — `m.lastLeftPanel` undefined.

- [ ] **Step 4: Implement**

In `internal/tui/model.go`:

**(a)** Add the field next to `focus`:

```go
	focus         panel
	lastLeftPanel panel // ←'s return target; zero value = panelBranches
```

(adjust the surrounding field alignment; `sel`/`sortModes`/`headTimes` follow).

**(b)** Add a helper near `panelLen`:

```go
// rememberLeftFocus records the focused panel as ←'s return target when it
// is one of the left-column panels. Called before any focus reassignment.
func (m Model) rememberLeftFocus() Model {
	if m.focus != panelCommits {
		m.lastLeftPanel = m.focus
	}
	return m
}
```

**(c)** Rework the tab cases and add the arrow cases in the normal-key switch:

```go
		case "tab":
			m = m.rememberLeftFocus()
			m.focus = (m.focus + 1) % panelCount
		case "shift+tab":
			m = m.rememberLeftFocus()
			m.focus = (m.focus - 1 + panelCount) % panelCount
		case "right":
			if m.focus != panelCommits {
				m = m.rememberLeftFocus()
				m.focus = panelCommits
			}
		case "left":
			// No-op when already in the left column, and when the narrow
			// layout has no left column to focus.
			if m.focus == panelCommits && (m.width <= 0 || m.width >= 40) {
				m.focus = m.lastLeftPanel
			}
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestRightArrow|TestLeftArrow|TestArrowFocus' -v`
Expected: PASS (6 tests)

- [ ] **Step 6: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/model.go internal/tui/model_test.go internal/tui/focus_test.go
git commit -m "feat(tui): left/right arrows switch focus between the left column and Commits"
```

---

### Task 2: Files-view focus — `filesTreeFocused` + key handling

**Files:**
- Modify: `internal/tui/model.go`
- Modify: `internal/tui/files_view.go` (`updateFilesViewKey`)
- Test: `internal/tui/files_view_test.go`

**Context:** `updateFilesViewKey` (files_view.go) owns ALL keys while the view is open. Today: `up/k`/`down/j` → `moveCommitUnderFilesView(∓1)` (clamps the Commits selection, fires a hash-tagged follow-live reload, dedupes); `ctrl+up/down` → `p.move(∓1)` on the tree; `pgup/pgdn` → `p.move(∓m.filesPageRows())` on the tree; `tab` falls into "everything else swallowed". `m.pageStep()` is the Commits panel's 25%-viewport jump (`m.focus` is pinned to `panelCommits` while the view is open). The `l` open case (model.go, `case "l":`) sets `filesView/filesTitle/filesHash`; the `tea.WindowSizeMsg` case auto-closes the view below 40 columns. Test helpers: `filesModel()` (2 commits, FakeRunner with a 3-line tree: heading + 2 files), `openFilesView(t, m)`, `pressType`, `keyMsg` (has `left`/`right` after Task 1). The existing `TestFilesViewSwallowsActionKeys` asserts tab is swallowed — that assertion CHANGES in this task (tab now toggles focus).

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/files_view_test.go`:

```go
func TestFilesViewArrowsAndTabSwitchFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	if m.filesTreeFocused {
		t.Fatal("the view must open with the commit list focused")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("left must focus the tree")
	}
	u, _ = m.Update(keyMsg("left")) // already leftmost: no-op
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("left on the tree must keep it focused")
	}
	u, _ = m.Update(keyMsg("right"))
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("right must focus the commit list")
	}
	u, _ = m.Update(keyMsg("tab"))
	m = u.(Model)
	if !m.filesTreeFocused {
		t.Fatal("tab must toggle to the tree")
	}
	u, _ = m.Update(keyMsg("shift+tab"))
	m = u.(Model)
	if m.filesTreeFocused {
		t.Fatal("shift+tab must toggle back to the commit list")
	}
}

func TestFilesViewMovementFollowsFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	// Commits focused: j moves the commit selection and fires a reload.
	u, cmd := m.Update(keyMsg("j"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 || cmd == nil {
		t.Fatalf("sel = %d cmd-nil=%v, want commit move + reload", m.sel[panelCommits], cmd == nil)
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Tree focused: j moves the tree cursor, not the commit selection.
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	u, cmd = m.Update(keyMsg("j"))
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d after j, want 1", m.filesView.sel)
	}
	if m.sel[panelCommits] != 1 || cmd != nil {
		t.Fatal("j with the tree focused must not touch commits or fire a reload")
	}
}

func TestFilesViewPagingFollowsFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	// Commits focused: pgdown pages the commit selection via ONE reload.
	u, cmd := m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d after pgdown, want 1 (clamped page jump)", m.sel[panelCommits])
	}
	if cmd == nil {
		t.Fatal("paging commits must fire a follow-live reload")
	}
	u, _ = m.Update(cmd())
	m = u.(Model)
	// Tree focused: pgdown pages the tree.
	u, _ = m.Update(keyMsg("left"))
	m = u.(Model)
	before := m.sel[panelCommits]
	u, cmd = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if m.filesView.sel == 0 {
		t.Fatal("pgdown with the tree focused must move the tree cursor")
	}
	if m.sel[panelCommits] != before || cmd != nil {
		t.Fatal("pgdown with the tree focused must not touch commits")
	}
}

func TestFilesViewCtrlArrowsAlwaysScrollTree(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("ctrl+down")) // commits focused
	m = u.(Model)
	if m.filesView.sel != 1 {
		t.Fatalf("tree sel = %d, ctrl+down must scroll the tree from the commits side", m.filesView.sel)
	}
	u, _ = m.Update(keyMsg("left")) // tree focused
	m = u.(Model)
	u, _ = m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if m.filesView.sel != 2 {
		t.Fatalf("tree sel = %d, ctrl+down must scroll the tree from the tree side too", m.filesView.sel)
	}
}

func TestFilesViewCloseResetsTreeFocus(t *testing.T) {
	for _, close := range []string{"l", "esc"} {
		m := openFilesView(t, filesModel())
		u, _ := m.Update(keyMsg("left"))
		m = u.(Model)
		u, _ = m.Update(keyMsg(close))
		m = u.(Model)
		if m.filesView != nil {
			t.Fatalf("%s must close the view", close)
		}
		m = openFilesView(t, m)
		if m.filesTreeFocused {
			t.Fatalf("reopening after %s-close must start commits-focused", close)
		}
	}
}

func TestFilesViewNarrowResizeResetsTreeFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	u, _ = m.Update(tea.WindowSizeMsg{Width: 30, Height: 24})
	m = u.(Model)
	if m.filesView != nil || m.filesTreeFocused {
		t.Fatal("the narrow auto-close must clear the view AND the tree focus")
	}
}
```

And UPDATE the existing `TestFilesViewSwallowsActionKeys`: delete its trailing tab block

```go
	before := m.focus
	m = pressType(t, m, tea.KeyTab)
	if m.focus != before || m.filesView == nil {
		t.Fatal("tab must be swallowed while the view is open")
	}
```

and replace it with:

```go
	// tab is no longer swallowed — it toggles focus (covered by
	// TestFilesViewArrowsAndTabSwitchFocus); m.focus itself must never move.
	m = pressType(t, m, tea.KeyTab)
	if m.focus != panelCommits || m.filesView == nil {
		t.Fatal("tab inside the view must not move m.focus off the commits panel")
	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run TestFilesView -v`
Expected: COMPILE FAILURE — `m.filesTreeFocused` undefined.

- [ ] **Step 3: Add the field**

In `internal/tui/model.go`, extend the files-view field block:

```go
	filesView       *contentPopup // commit files tree replacing the left column; nil = closed
	filesTitle      string        // "Files <short-hash> <subject>", updated with the content
	filesHash       string        // commit the view wants; gates stale async results
	filesTreeFocused bool         // true = the tree side owns vertical movement (←/→/tab)
```

(gofmt will realign.)

- [ ] **Step 4: Wire the resets in model.go**

**(a)** In the `case "l":` open path, after `m.filesHash = c.Hash`:

```go
				m.filesTreeFocused = false // always open on the commit list
```

**(b)** In the `tea.WindowSizeMsg` narrow auto-close, next to `m.filesView = nil`:

```go
			m.filesTreeFocused = false
```

- [ ] **Step 5: Rework `updateFilesViewKey`**

In `internal/tui/files_view.go`, replace the post-typing-mode `switch msg.String()` block with:

```go
	switch msg.String() {
	case "q":
		return m, tea.Quit // q quits the app, view or not (top-level key)
	case "esc":
		if p.query != "" { // first esc clears the committed search
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.filesView = nil
		m.filesTreeFocused = false
		return m, nil
	case "l":
		m.filesView = nil
		m.filesTreeFocused = false
		return m, nil
	case "/":
		p.typing = true
		p.query = ""
		p.sel = 0
	case "left":
		m.filesTreeFocused = true
	case "right":
		m.filesTreeFocused = false
	case "tab", "shift+tab":
		m.filesTreeFocused = !m.filesTreeFocused
	case "up", "k":
		if m.filesTreeFocused {
			p.move(-1)
			return m, nil
		}
		return m.moveCommitUnderFilesView(-1)
	case "down", "j":
		if m.filesTreeFocused {
			p.move(1)
			return m, nil
		}
		return m.moveCommitUnderFilesView(1)
	case "ctrl+up": // always the tree, from either side
		p.move(-1)
	case "ctrl+down":
		p.move(1)
	case "pgup":
		if m.filesTreeFocused {
			p.move(-m.filesPageRows())
			return m, nil
		}
		return m.moveCommitUnderFilesView(-m.pageStep())
	case "pgdown":
		if m.filesTreeFocused {
			p.move(m.filesPageRows())
			return m, nil
		}
		return m.moveCommitUnderFilesView(m.pageStep())
	}
	return m, nil
```

Also update the function's doc comment to:

```go
// updateFilesViewKey routes keys while the files view is open. ←/→/tab pick
// which side owns vertical movement (filesTreeFocused); the commits side
// keeps the follow-live reload; ctrl+↑/↓ always scrolls the tree; /-search,
// close keys and quit are focus-independent; everything else is swallowed.
```

- [ ] **Step 6: Run the tests to verify they pass**

Run: `go test ./internal/tui -run TestFilesView -v`
Expected: PASS (all, including the updated swallow test)

- [ ] **Step 7: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/model.go internal/tui/files_view.go internal/tui/files_view_test.go
git commit -m "feat(tui): files view focus — arrows/tab pick the side, movement follows it"
```

---

### Task 3: Visible focus — `panelFocused` helper, tree border, tooltip gate

**Files:**
- Modify: `internal/tui/view.go` (`renderPanel`, ~lines 255 and 274)
- Modify: `internal/tui/files_view.go` (`renderFilesView`, last line)
- Modify: `internal/tui/tooltip.go` (top of `tooltip()`, before `p := m.focus` at ~line 22)
- Test: `internal/tui/files_view_test.go`

**Context:** `renderPanel` decides the `> ` reverse-video row highlight with `focused := i == selInWin && p == m.focus` and the border style with `if p == m.focus`. `renderFilesView` ends with `return bluredPanel.Render(strings.Join(lines, "\n"))`. `tooltip()` (tooltip.go) starts from `p := m.focus` and returns `(lines []string, x, y int, ok bool)`; it fires when the focused panel's selected row is truncated. Styles: `focusedPanel` (blue border) / `bluredPanel` (gray). Tests can assert on `ansi.Strip`-ed render output: the `> ` prefix appears only on the focused side's selected row.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/files_view_test.go` (add `"github.com/charmbracelet/x/ansi"` to imports):

```go
func TestFilesViewFocusIsVisible(t *testing.T) {
	m := openFilesView(t, filesModel())
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "> 1111111 one") {
		t.Fatalf("commits focused: the selected commit must carry the > prefix:\n%s", out)
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	out = ansi.Strip(m.render())
	if strings.Contains(out, "> 1111111 one") {
		t.Fatalf("tree focused: the commits row must lose the > prefix:\n%s", out)
	}
	if !strings.Contains(out, "> ") {
		t.Fatalf("tree focused: the tree cursor must still render:\n%s", out)
	}
}

func TestPanelFocusedRespectsTreeFocus(t *testing.T) {
	m := openFilesView(t, filesModel())
	if !m.panelFocused(panelCommits) {
		t.Fatal("commits must read focused while the commit side is active")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if m.panelFocused(panelCommits) {
		t.Fatal("commits must read blurred while the tree side is active")
	}
	u, _ = m.Update(keyMsg("right"))
	if !u.(Model).panelFocused(panelCommits) {
		t.Fatal("right must hand focus back to commits")
	}
}

func TestTooltipSuppressedWhileTreeFocused(t *testing.T) {
	m := filesModel()
	m.commits[0].Subject = strings.Repeat("x", 200) // force row truncation
	m = openFilesView(t, m)
	if _, _, _, ok := m.tooltip(); !ok {
		t.Fatal("setup: tooltip expected for the truncated commit row")
	}
	u, _ := m.Update(keyMsg("left"))
	m = u.(Model)
	if _, _, _, ok := m.tooltip(); ok {
		t.Fatal("tooltip must be suppressed while the tree is focused")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui -run 'TestFilesViewFocusIsVisible|TestPanelFocused|TestTooltipSuppressedWhileTree' -v`
Expected: COMPILE FAILURE — `m.panelFocused` undefined.

- [ ] **Step 3: Implement**

**(a)** In `internal/tui/view.go`, add near `renderPanel`:

```go
// panelFocused reports whether p should render as the focused panel. While
// the files view's tree side is focused, the Commits panel renders blurred
// even though m.focus still points at it (focus stays there so selection
// and follow-live machinery keep working).
func (m Model) panelFocused(p panel) bool {
	return p == m.focus && !(m.filesView != nil && m.filesTreeFocused)
}
```

In `renderPanel`, change

```go
			focused := i == selInWin && p == m.focus
```

to

```go
			focused := i == selInWin && m.panelFocused(p)
```

and

```go
	style := bluredPanel
	if p == m.focus {
		style = focusedPanel
	}
```

to

```go
	style := bluredPanel
	if m.panelFocused(p) {
		style = focusedPanel
	}
```

**(b)** In `internal/tui/files_view.go`, change `renderFilesView`'s final return to:

```go
	style := bluredPanel
	if m.filesTreeFocused {
		style = focusedPanel
	}
	return style.Render(strings.Join(lines, "\n"))
```

and update the function's doc comment's last sentence from "Blurred border: focus stays on the Commits panel." to "The border follows filesTreeFocused; the Commits panel blurs via panelFocused while the tree side is active."

**(c)** In `internal/tui/tooltip.go`, insert at the very top of `tooltip()` (before `p := m.focus`):

```go
	// While the files view's tree side is focused, the commits selection is
	// not the active row — describing it would be misleading.
	if !m.panelFocused(m.focus) {
		return nil, 0, 0, false
	}
```

(Match the function's actual return signature — if it returns named values or a different order, adapt the zero-value return accordingly, keeping `ok=false`.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui -run 'TestFilesViewFocusIsVisible|TestPanelFocused|TestTooltipSuppressedWhileTree' -v`
Expected: PASS (3 tests)

- [ ] **Step 5: Whole package + vet + commit**

```bash
go test ./internal/tui && go vet ./... && gofmt -l internal cmd
git add internal/tui/view.go internal/tui/files_view.go internal/tui/tooltip.go internal/tui/files_view_test.go
git commit -m "feat(tui): focus is visible — panelFocused blurs Commits while the tree side is active"
```

---

### Task 4: Footer, help, docs, full gate

**Files:**
- Modify: `internal/tui/footer.go` (the files-view override line, ~line 71)
- Modify: `internal/tui/help.go`
- Modify: `README.md`, `CHANGELOG.md`

**Context:** `footerLine()` has a files-view override returning a literal string. `helpContent()` is the `?` table; `TestHelpFooterCoverage` only requires registry keys to have help rows (no registry changes here, so it stays green automatically). The existing footer test `TestFooterSwitchesToFilesViewMode` asserts the substring `"[esc/l] close"` — the new line must keep it.

- [ ] **Step 1: Footer line**

In `internal/tui/footer.go`, replace the files-view return with:

```go
		return "files: [←/→ tab] focus  [↑/↓] move  [ctrl+↑/↓] tree  [/] search  [esc/l] close  [q] quit"
```

Run: `go test ./internal/tui -run TestFooter -v` — expect PASS.

- [ ] **Step 2: Help rows**

In `internal/tui/help.go`:

**(a)** In the Global section, directly after `r("tab/shift+tab", "cycle panel focus forward / backward"),` add:

```go
		r("←/→", "focus the left column / the Commits panel"),
```

**(b)** Replace the whole `h("Commit files view (l)")` block (currently 6 rows) with:

```go
		h("Commit files view (l)"),
		r("←/→ tab", "switch focus between the file tree and the commit list"),
		r("↑/k ↓/j", "move on the focused side (commits side reloads the tree)"),
		r("pgup/pgdn", "page the focused side"),
		r("ctrl+↑/↓", "scroll the file tree from either side (wheel: 3)"),
		r("/", "search file paths (enter keeps it, esc cancels)"),
		r("esc", "clear the search, then close"),
		r("l", "close"),
```

Run: `go test ./internal/tui -run TestHelp -v` — expect PASS.

- [ ] **Step 3: README.md**

**(a)** Directly after the `shift+tab` row (`| \`shift+tab\` | move focus backwards |`), add:

```markdown
| `←`/`→` | focus the left column / the Commits panel (inside the files view: switch between the file tree and the commit list) |
```

**(b)** Replace the `l` row's parenthetical so the row reads:

```markdown
| `l` | on the Commits panel: show the selected commit's files as a directory tree in the left column (`←`/`→`/`tab` switch focus between the tree and the commit list; movement keys act on the focused side — the commits side reloads the tree; `ctrl+↑`/`ctrl+↓` always scroll the tree; `/` searches paths; `esc`/`l` close) |
```

- [ ] **Step 4: CHANGELOG.md**

Under `## [Unreleased]` / `### Added`, insert as the FIRST subsection (above `#### Commit files view`):

```markdown
#### Arrow-key window focus
- TUI: `←`/`→` switch focus horizontally — `→` from a left panel focuses
  Commits, `←` returns to the last-focused left panel. Inside the commit
  files view, `←`/`→`/`tab` switch between the file tree and the commit
  list; vertical movement follows the focused side and `ctrl+↑`/`ctrl+↓`
  always scroll the tree. Focus is visible: the border and row highlight
  follow it.
```

- [ ] **Step 5: Full gate**

Run: `./test.sh race`
Expected: all stages green.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/footer.go internal/tui/help.go README.md CHANGELOG.md
git commit -m "docs(tui): footer/help/README rows for arrow-key window focus"
```

---

## Final review checklist (for the orchestrator)

- Spec coverage: normal-mode arrows + lastLeftPanel + tab recording (T1); files-view focus state, movement-follows-focus, ctrl-always-tree, resets (T2); visible focus + tooltip gate (T3); footer/help/docs (T4).
- After all tasks: dispatch the final holistic reviewer, fix Important findings, then `superpowers:finishing-a-development-branch`.
