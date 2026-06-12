# Help Window + Generic Content Popup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `?` opens a searchable help window listing every TUI key binding, built on a new generic read-only content popup (type-to-filter + scrolling, keyboard and mouse wheel).

**Architecture:** A reusable `contentPopup` (new `internal/tui/content_popup.go`) renders any `[]contentLine` as a centered overlay with cursor-driven scrolling via the existing `windowRows` helper and repo-popup-style type-to-filter. `internal/tui/help.go` feeds it a hand-maintained grouped table of all bindings. Mouse wheel support comes from `tea.WithMouseCellMotion()` plus a `tea.MouseMsg` case in `Update`.

**Tech Stack:** Go 1.26, Bubble Tea v1.3.10, lipgloss. Spec: `docs/superpowers/specs/2026-06-12-help-window-design.md`.

**Worktree:** `/mnt/t/others/gigagit/.claude/worktrees/tui-help`, branch `feat/tui-help`. Run all commands from the worktree root.

**Project invariants (read before coding):**
- `Model` is a **value receiver**; Bubble Tea copies it on every `Update`. Popup state MUST live behind a pointer field (`contentPopup *contentPopup`) — mutations to a value field silently vanish.
- Popup key handlers **swallow every key** (no fallthrough to global handlers); `esc` cancels, `ctrl+c` quits.
- All width math is display-aware (`lipgloss.Width` / `ansi.StringWidth`), never `len()`.
- Test helpers already exist: `keyMsg(s)` (`model_test.go:9`), `ansi.Strip` for asserting on rendered output.

---

### Task 1: contentPopup core — data, filtering, cursor

**Files:**
- Create: `internal/tui/content_popup.go`
- Create: `internal/tui/content_popup_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/content_popup_test.go`:

```go
package tui

import (
	"fmt"
	"testing"
)

// contentLines builds n filterable lines named line-00 … line-NN.
func contentLines(n int) []contentLine {
	out := make([]contentLine, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contentLine{text: fmt.Sprintf("line-%02d detail", i)})
	}
	return out
}

func TestContentVisibleNoQueryReturnsAll(t *testing.T) {
	p := newContentPopup("T", contentLines(4))
	if got := len(p.visible()); got != 4 {
		t.Fatalf("visible() = %d lines, want 4", got)
	}
}

func TestContentVisibleFiltersAndKeepsMatchingHeadings(t *testing.T) {
	p := newContentPopup("T", []contentLine{
		{text: "Alpha", heading: true},
		{text: "apple one"},
		{text: "Beta", heading: true},
		{text: "berry two"},
	})
	p.query = "BERRY" // case-insensitive
	vis := p.visible()
	if len(vis) != 2 || !vis[0].heading || vis[0].text != "Beta" || vis[1].text != "berry two" {
		t.Fatalf("visible() = %+v, want [Beta(heading), berry two]", vis)
	}
}

func TestContentVisibleHeadingsAreNotMatchTargets(t *testing.T) {
	p := newContentPopup("T", []contentLine{
		{text: "Alpha", heading: true},
		{text: "apple one"},
	})
	p.query = "alpha" // matches only the heading text → nothing survives
	if vis := p.visible(); len(vis) != 0 {
		t.Fatalf("visible() = %+v, want empty (headings are never matched)", vis)
	}
}

func TestContentMoveClamps(t *testing.T) {
	p := newContentPopup("T", contentLines(10))
	p.move(-5)
	if p.sel != 0 {
		t.Errorf("move(-5) from 0: sel = %d, want 0", p.sel)
	}
	p.move(100)
	if p.sel != 9 {
		t.Errorf("move(100): sel = %d, want 9", p.sel)
	}
	p.move(-3)
	if p.sel != 6 {
		t.Errorf("move(-3) from 9: sel = %d, want 6", p.sel)
	}
}

func TestContentMoveOnEmptyVisible(t *testing.T) {
	p := newContentPopup("T", contentLines(3))
	p.query = "zzz-no-match"
	p.move(1)
	if p.sel != 0 {
		t.Errorf("move on empty visible: sel = %d, want 0", p.sel)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestContent -v`
Expected: compile error — `undefined: contentLine`, `newContentPopup`.

- [ ] **Step 3: Write the implementation**

Create `internal/tui/content_popup.go`:

```go
package tui

import (
	"strings"
)

// contentLine is one display line of a contentPopup. heading lines are section
// headers: the filter never matches them, and they survive filtering only
// while at least one non-heading line beneath them (before the next heading)
// matches.
type contentLine struct {
	text    string
	heading bool
}

// contentPopup is a generic read-only viewer popup: any list of lines with
// repo-popup-style type-to-filter search and cursor-driven scrolling. The
// help window is its first consumer.
type contentPopup struct {
	title string
	lines []contentLine // full, unfiltered content
	query string        // case-insensitive substring over non-heading lines
	sel   int           // cursor index into the FILTERED view
}

func newContentPopup(title string, lines []contentLine) *contentPopup {
	return &contentPopup{title: title, lines: lines}
}

// visible returns the filtered lines in display order: non-heading lines
// matching query, plus each heading that still has a matching line.
func (p *contentPopup) visible() []contentLine {
	if p.query == "" {
		return p.lines
	}
	q := strings.ToLower(p.query)
	out := make([]contentLine, 0, len(p.lines))
	var pending *contentLine // last heading not yet emitted
	for i := range p.lines {
		l := p.lines[i]
		if l.heading {
			pending = &p.lines[i]
			continue
		}
		if strings.Contains(strings.ToLower(l.text), q) {
			if pending != nil {
				out = append(out, *pending)
				pending = nil
			}
			out = append(out, l)
		}
	}
	return out
}

// move shifts the cursor by delta, clamped to the visible range.
func (p *contentPopup) move(delta int) {
	n := len(p.visible())
	p.sel += delta
	if p.sel > n-1 {
		p.sel = n - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run TestContent -v`
Expected: PASS (5 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/tui/content_popup.go internal/tui/content_popup_test.go
git commit -m "feat(tui): contentPopup core — filterable lines with heading retention"
```

---

### Task 2: key handling, rendering, Model wiring

**Files:**
- Modify: `internal/tui/content_popup.go` (append key handler + render)
- Modify: `internal/tui/model.go` (Model field at ~line 41, key routing at ~line 140, `?` is NOT added here — Task 4)
- Modify: `internal/tui/view.go` (`render()` at line 80: compositing + tooltip suppression)
- Modify: `internal/tui/model_test.go` (keyMsg: add ctrl+up/ctrl+down)
- Modify: `internal/tui/content_popup_test.go`, `internal/tui/tooltip_test.go`, `internal/tui/fit_test.go`

**Geometry contract (from the spec):** visible rows = `termH − 7` (double border 2, vertical padding 2, title 1, blank-after-title 1, hint 1), floored at 3. The hint line sits directly under the last row — no blank line before it, or the box exceeds the viewport at 24 rows.

- [ ] **Step 1: Add ctrl+up/ctrl+down to the keyMsg test helper**

In `internal/tui/model_test.go`, add two cases to the `switch s` in `keyMsg` (before `default:`):

```go
	case "ctrl+up":
		return tea.KeyMsg{Type: tea.KeyCtrlUp}
	case "ctrl+down":
		return tea.KeyMsg{Type: tea.KeyCtrlDown}
```

- [ ] **Step 2: Write the failing tests**

Append to `internal/tui/content_popup_test.go`:

```go
// contentModel is an 80×24 model with an open content popup of n lines.
// At 24 rows the popup shows contentPageRows() = 24-7 = 17 rows, so n=5
// fits and n=30 overflows.
func contentModel(n int) Model {
	m := Model{width: 80, height: 24}
	m.contentPopup = newContentPopup("Test content", contentLines(n))
	return m
}

func TestContentPageRows(t *testing.T) {
	m := Model{width: 80, height: 24}
	if got := m.contentPageRows(); got != 17 {
		t.Errorf("contentPageRows at h=24 = %d, want 17", got)
	}
	m.height = 8
	if got := m.contentPageRows(); got != 3 {
		t.Errorf("contentPageRows at h=8 = %d, want floor 3", got)
	}
}

func TestContentPopupFitsViewport(t *testing.T) {
	m := contentModel(5)
	for i := 0; i < 4; i++ { // cursor to the last line — still no scrolling
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	for i := 0; i < 5; i++ {
		if !strings.Contains(out, fmt.Sprintf("line-%02d", i)) {
			t.Fatalf("fitting content must be fully visible; line-%02d missing:\n%s", i, out)
		}
	}
	if strings.Contains(out, "5/5") {
		t.Error("no position indicator when the content fits")
	}
}

func TestContentPopupOverflowScrolls(t *testing.T) {
	m := contentModel(30)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "line-00") || strings.Contains(out, "line-29") {
		t.Fatalf("initial window must show the top, not the bottom:\n%s", out)
	}
	if !strings.Contains(out, "1/30") {
		t.Errorf("overflowing content must show a position indicator:\n%s", out)
	}
	u, _ := m.Update(keyMsg("pgdown")) // +17
	m = u.(Model)
	u, _ = m.Update(keyMsg("pgdown")) // clamps to 29
	m = u.(Model)
	out = ansi.Strip(m.render())
	if !strings.Contains(out, "line-29") || strings.Contains(out, "line-00") {
		t.Fatalf("after paging to the end the window must show the bottom:\n%s", out)
	}
	if !strings.Contains(out, "30/30") {
		t.Errorf("indicator must track the cursor:\n%s", out)
	}
}

func TestContentPopupStepSizes(t *testing.T) {
	m := contentModel(30)
	p := m.contentPopup
	u, _ := m.Update(keyMsg("ctrl+down"))
	m = u.(Model)
	if p.sel != 5 {
		t.Errorf("ctrl+down: sel = %d, want 5", p.sel)
	}
	u, _ = m.Update(keyMsg("pgdown"))
	m = u.(Model)
	if p.sel != 22 {
		t.Errorf("pgdown: sel = %d, want 22 (5+17)", p.sel)
	}
	u, _ = m.Update(keyMsg("ctrl+up"))
	m = u.(Model)
	if p.sel != 17 {
		t.Errorf("ctrl+up: sel = %d, want 17", p.sel)
	}
	u, _ = m.Update(keyMsg("pgup"))
	m = u.(Model)
	if p.sel != 0 {
		t.Errorf("pgup: sel = %d, want 0", p.sel)
	}
}

func TestContentPopupSearchWhileScrolled(t *testing.T) {
	m := contentModel(30)
	for i := 0; i < 25; i++ {
		u, _ := m.Update(keyMsg("down"))
		m = u.(Model)
	}
	for _, r := range "line-2" { // matches line-20 … line-29 only
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	p := m.contentPopup
	if p.sel != 0 {
		t.Errorf("query change must reset the cursor: sel = %d, want 0", p.sel)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "> line-20") {
		t.Fatalf("after searching while scrolled, the view must start at the first match:\n%s", out)
	}
}

func TestContentPopupNoMatch(t *testing.T) {
	m := contentModel(5)
	for _, r := range "zzz" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "(no match)") {
		t.Fatalf("want (no match) marker:\n%s", out)
	}
}

func TestContentPopupEscTwoStage(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("x"))
	m = u.(Model)
	u, _ = m.Update(keyMsg("esc")) // first esc clears the query
	m = u.(Model)
	if m.contentPopup == nil {
		t.Fatal("first esc must only clear the query")
	}
	if m.contentPopup.query != "" {
		t.Fatalf("query = %q, want empty", m.contentPopup.query)
	}
	u, _ = m.Update(keyMsg("esc")) // second esc closes
	m = u.(Model)
	if m.contentPopup != nil {
		t.Fatal("second esc must close the popup")
	}
}

func TestContentPopupEnterCloses(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.contentPopup != nil {
		t.Fatal("enter must close the read-only popup")
	}
}

func TestContentPopupSwallowsGlobalKeys(t *testing.T) {
	m := contentModel(5)
	u, _ := m.Update(keyMsg("p")) // global: SmartPull — must NOT fire
	m = u.(Model)
	if m.running {
		t.Fatal("p must not start an operation while the popup is open")
	}
	if m.contentPopup == nil || m.contentPopup.query != "p" {
		t.Fatalf("typed rune must go to the filter query, got %+v", m.contentPopup)
	}
}

func TestContentPopupFitBounds(t *testing.T) {
	for _, sz := range []struct{ w, h int }{{80, 24}, {40, 10}, {30, 8}} {
		m := contentModel(40)
		m.width, m.height = sz.w, sz.h
		out := m.render()
		lines := strings.Split(out, "\n")
		if len(lines) > sz.h {
			t.Errorf("%dx%d: %d lines rendered, must be <= %d", sz.w, sz.h, len(lines), sz.h)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w > sz.w {
				t.Errorf("%dx%d: line %d is %d cols, must be <= %d", sz.w, sz.h, i, w, sz.w)
			}
		}
	}
}
```

Add the needed imports to the test file's import block: `"strings"`, `"github.com/charmbracelet/lipgloss"`, `"github.com/charmbracelet/x/ansi"` (keep `"fmt"`, `"testing"`).

Append to `internal/tui/tooltip_test.go`:

```go
func TestTooltipSuppressedByContentPopup(t *testing.T) {
	m := tooltipModel()
	m.contentPopup = newContentPopup("T", contentLines(2))
	out := ansi.Strip(m.render())
	if strings.Contains(out, longPath) {
		t.Fatal("content popup view must not contain the tooltip")
	}
}
```

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestContent|TestTooltipSuppressedByContentPopup' -v`
Expected: compile error — `m.contentPopup` undefined, `contentPageRows` undefined.

- [ ] **Step 4: Wire the Model field and key routing**

In `internal/tui/model.go`, add the pointer field after `pendingSwitchBranch` (line ~41):

```go
	contentPopup        *contentPopup // generic read-only viewer (help window)
```

In `Update`'s `tea.KeyMsg` case, add routing after the `branchPopup` check (line ~140-142), before the `filterTyping` block:

```go
			if m.contentPopup != nil {
				return m.updateContentPopupKey(msg)
			}
```

- [ ] **Step 5: Implement the key handler and renderer**

Append to `internal/tui/content_popup.go` (extend the import block with `"fmt"` and `tea "github.com/charmbracelet/bubbletea"`):

```go
// contentFastStep is the ctrl+↑/↓ jump; contentWheelStep the mouse-wheel tick.
const (
	contentFastStep  = 5
	contentWheelStep = 3
)

// contentPageRows is the popup's visible row capacity: terminal height minus
// chrome (double border 2, vertical padding 2, title 1, blank after title 1,
// hint line 1), floored so tiny terminals still show a few rows.
func (m Model) contentPageRows() int {
	_, h := m.overlayDims()
	n := h - 7
	if n < 3 {
		n = 3
	}
	return n
}

// updateContentPopupKey handles all keys while the viewer is open. It swallows
// everything (no fallthrough to global handlers).
func (m Model) updateContentPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.contentPopup
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.query != "" { // first esc clears the filter, second closes
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.contentPopup = nil
		return m, nil
	case tea.KeyEnter:
		m.contentPopup = nil
		return m, nil
	case tea.KeyUp:
		p.move(-1)
	case tea.KeyDown:
		p.move(1)
	case tea.KeyCtrlUp:
		p.move(-contentFastStep)
	case tea.KeyCtrlDown:
		p.move(contentFastStep)
	case tea.KeyPgUp:
		p.move(-m.contentPageRows())
	case tea.KeyPgDown:
		p.move(m.contentPageRows())
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
	case tea.KeySpace:
		p.query += " "
		p.sel = 0
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.sel = 0
	}
	return m, nil
}

// renderContentPopup draws the viewer box (composited by render via
// overlayCenter). Headings render bold, the cursor row reversed; the window
// follows the cursor via the same windowRows helper the panels use.
func (m Model) renderContentPopup() string {
	p := m.contentPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)

	vis := p.visible()
	rows := make([]string, len(vis))
	for i, l := range vis {
		switch {
		case l.heading:
			rows[i] = titleStyle.Render(truncate(l.text, inner))
		case i == p.sel:
			rows[i] = selectedRow.Render(truncate("> "+l.text, inner))
		default:
			rows[i] = truncate("  "+l.text, inner)
		}
	}
	capRows := m.contentPageRows()
	win, _ := windowRows(rows, capRows, p.sel)

	title := p.title
	if p.query != "" {
		title += "  /" + p.query
	}
	var b strings.Builder
	b.WriteString(truncate(title, inner) + "\n\n")
	if len(win) == 0 {
		b.WriteString("  (no match)\n")
	}
	for _, r := range win {
		b.WriteString(r + "\n")
	}
	hint := "[esc] close"
	if len(vis) > capRows {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	b.WriteString(truncate(hint, inner))
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
```

- [ ] **Step 6: Composite in render() and suppress the tooltip**

In `internal/tui/view.go` `render()` (line 80), extend the tooltip-suppression condition and add the popup branch before the final `return bg`:

```go
	if m.popup == nil && m.repoPopup == nil && m.settings == nil && m.branchPopup == nil && m.contentPopup == nil {
		if lines, x, y, ok := m.tooltip(); ok {
			w, h := m.overlayDims()
			bg = overlayAt(bg, strings.Join(lines, "\n"), x, y, w, h)
		}
	}
```

```go
	if m.contentPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderContentPopup(), w, h)
	}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/tui/ -run 'TestContent|TestTooltipSuppressedByContentPopup' -v`
Expected: PASS. If `TestContentPopupFitBounds` fails on line count, recount the chrome — the box (border+padding+title+blank+rows+hint) must total ≤ termH; adjust nothing else.

- [ ] **Step 8: Run the full TUI suite**

Run: `go test ./internal/tui/`
Expected: PASS (no existing test broken by the suppression-condition edit).

- [ ] **Step 9: Commit**

```bash
git add internal/tui/content_popup.go internal/tui/content_popup_test.go internal/tui/model.go internal/tui/view.go internal/tui/model_test.go internal/tui/tooltip_test.go
git commit -m "feat(tui): contentPopup keys, scrolling, and rendering wired into the Model"
```

---

### Task 3: mouse wheel scrolling

**Files:**
- Modify: `internal/tui/model.go` (new `tea.MouseMsg` case in `Update`)
- Modify: `internal/tui/run.go` (program options)
- Modify: `internal/tui/content_popup_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/content_popup_test.go`:

```go
func wheelMsg(up bool) tea.MouseMsg {
	b := tea.MouseButtonWheelDown
	if up {
		b = tea.MouseButtonWheelUp
	}
	return tea.MouseMsg{Button: b, Action: tea.MouseActionPress}
}

func TestContentPopupWheelScrolls(t *testing.T) {
	m := contentModel(30)
	p := m.contentPopup
	u, _ := m.Update(wheelMsg(false))
	m = u.(Model)
	if p.sel != 3 {
		t.Errorf("wheel down: sel = %d, want 3", p.sel)
	}
	u, _ = m.Update(wheelMsg(true))
	m = u.(Model)
	if p.sel != 0 {
		t.Errorf("wheel up: sel = %d, want 0", p.sel)
	}
}

func TestMouseIgnoredWithoutContentPopup(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(wheelMsg(false)) // must not panic or change state
	if u.(Model).contentPopup != nil {
		t.Fatal("mouse must be inert when no content popup is open")
	}
}
```

Add `tea "github.com/charmbracelet/bubbletea"` to the test file's imports.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Wheel|MouseIgnored' -v`
Expected: `TestContentPopupWheelScrolls` FAILS (sel stays 0 — no MouseMsg case yet); `TestMouseIgnoredWithoutContentPopup` may already pass (unknown msg falls through) — that's fine.

- [ ] **Step 3: Handle MouseMsg in Update**

In `internal/tui/model.go` `Update`, add a case after the `tea.KeyMsg` case (same level as `case opEventMsg:`):

```go
	case tea.MouseMsg:
		// Mouse support is scoped to the content popup (spec non-goal: no
		// panel clicks/wheel). Wheel ticks move the cursor like ctrl-arrows.
		if m.contentPopup != nil && msg.Action == tea.MouseActionPress {
			switch msg.Button {
			case tea.MouseButtonWheelUp:
				m.contentPopup.move(-contentWheelStep)
			case tea.MouseButtonWheelDown:
				m.contentPopup.move(contentWheelStep)
			}
		}
```

- [ ] **Step 4: Enable mouse reporting in run.go**

In `internal/tui/run.go`, change the program construction:

```go
	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion())
```

(Trade-off noted in the spec: while gg runs, terminals need shift+drag for native text selection — standard for full-screen TUIs.)

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run 'Wheel|MouseIgnored' -v` then `go build ./cmd/gg`
Expected: PASS; clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/run.go internal/tui/content_popup_test.go
git commit -m "feat(tui): mouse wheel scrolls the content popup"
```

---

### Task 4: help content, `?` binding, footer + drift guard

**Files:**
- Create: `internal/tui/help.go`
- Create: `internal/tui/help_test.go`
- Modify: `internal/tui/model.go` (`?` key in the normal-key switch)
- Modify: `internal/tui/view.go` (footer extracted to a const, `[?] help` added)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/help_test.go`:

```go
package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestHelpOpensWithQuestionMark(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if m.contentPopup == nil {
		t.Fatal("? must open the help popup")
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "Help") || !strings.Contains(out, "pull") {
		t.Fatalf("help window must show bindings:\n%s", out)
	}
}

func TestHelpSearchFindsBinding(t *testing.T) {
	m := Model{width: 80, height: 24}
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	for _, r := range "stash" {
		u, _ = m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "stash") {
		t.Fatalf("searching 'stash' must keep the stash row:\n%s", out)
	}
	if strings.Contains(out, "reload") {
		t.Fatalf("non-matching rows must be filtered out:\n%s", out)
	}
}

// TestHelpFooterCoverage is the drift guard: every [x]-abbreviated key in the
// footer must appear as the key column of some help row. The key column is
// the row's first whitespace-delimited field; alternates are /-separated
// (e.g. "q/ctrl+c").
func TestHelpFooterCoverage(t *testing.T) {
	re := regexp.MustCompile(`\[([^\]]+)\]`)
	var keys []string
	for _, mch := range re.FindAllStringSubmatch(footerText, -1) {
		keys = append(keys, mch[1])
	}
	if len(keys) < 10 {
		t.Fatalf("footer parse looks broken, got keys %v", keys)
	}
	lines := helpContent()
	for _, k := range keys {
		found := false
		for _, l := range lines {
			if l.heading {
				continue
			}
			f := strings.Fields(l.text)
			if len(f) == 0 {
				continue
			}
			for _, alt := range strings.Split(f[0], "/") {
				if alt == k {
					found = true
				}
			}
		}
		if !found {
			t.Errorf("footer key %q has no help row (helpContent key column)", k)
		}
	}
}

func TestHelpNotOpenedWhileAnotherPopupIsOpen(t *testing.T) {
	m := Model{width: 80, height: 24}
	m.repoPopup = &repoPopup{}
	u, _ := m.Update(keyMsg("?"))
	m = u.(Model)
	if m.contentPopup != nil {
		t.Fatal("? must be swallowed by the open popup")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run TestHelp -v`
Expected: compile error — `footerText`, `helpContent` undefined.

- [ ] **Step 3: Extract the footer const and add `[?] help`**

In `internal/tui/view.go`, above `renderInterface` (line ~136), add:

```go
// footerText abbreviates the global keys; TestHelpFooterCoverage enforces
// that every [x] key here has a row in helpContent.
const footerText = "[p]ull [P]ush [s]witch [b]ranch [S]tash [u]ndo [w]orktree [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [?] help  [q] quit"
```

and inside `renderInterface` replace the literal:

```go
	footer := truncate(footerText, g.w)
```

- [ ] **Step 4: Write the help content and `?` binding**

Create `internal/tui/help.go`:

```go
package tui

// helpContent is the hand-maintained table behind the ? help window: every
// key binding in the TUI, grouped by context. The key column is the first
// whitespace-delimited field; /-separated alternates (e.g. "q/ctrl+c") are
// matched individually by the footer drift guard (help_test.go).
func helpContent() []contentLine {
	h := func(s string) contentLine { return contentLine{text: s, heading: true} }
	r := func(key, desc string) contentLine {
		return contentLine{text: padRight(key, 16) + desc}
	}
	return []contentLine{
		h("Global"),
		r("p", "pull (SmartPull: autostash, ff/rebase decisions)"),
		r("P", "push the current branch (sets upstream)"),
		r("s", "switch to the selected branch (SmartSwitch)"),
		r("S", "stash tracked changes"),
		r("u", "undo the last commit (soft, ref-only)"),
		r("w", "worktree popup for the selected existing branch"),
		r("W", "worktree popup on a new templated branch"),
		r("R", "repo switcher popup"),
		r(",", "settings (agent skill install)"),
		r("o", "cycle the focused panel's sort order"),
		r("/", "filter the focused panel"),
		r("tab/shift+tab", "cycle panel focus forward / backward"),
		r("↑/k ↓/j", "move the selection"),
		r("pgup/pgdn", "page the selection (25% of the viewport)"),
		r("esc", "clear the active filter"),
		r("r", "reload all panels"),
		r("?", "this help window"),
		r("q/ctrl+c", "quit"),
		h("Branches panel"),
		r("b", "create a branch off the selected one (popup)"),
		r("B", "create a branch and switch to it"),
		r("d", "delete the selected branch"),
		h("Worktrees panel"),
		r("enter", "switch into the selected worktree"),
		r("d", "remove the selected worktree"),
		h("Filter mode (/)"),
		r("enter", "keep the filter and leave input mode"),
		r("esc", "cancel the filter"),
		r("backspace", "delete; any typed text narrows the list"),
		h("Worktree popup (w/W)"),
		r("enter/tab", "next input field / confirm"),
		r("e", "edit the previewed branch name (new-branch mode)"),
		r("w/enter", "create the worktree"),
		r("W", "create the worktree and switch to it"),
		r("esc", "cancel"),
		h("Branch popup (b/B)"),
		r("enter", "create the branch"),
		r("esc", "cancel"),
		h("Repo switcher (R)"),
		r("enter", "switch to the selected repository"),
		r("ctrl+d", "forget the selected repository"),
		r("esc", "close (type to filter)"),
		h("Settings (,)"),
		r("↑/↓", "move between agents"),
		r("space/enter", "install / refresh the selected agent skill"),
		r("esc", "close"),
		h("Decision modal"),
		r("↑/k ↓/j", "choose an option"),
		r("enter", "confirm the option"),
		r("esc", "abort the operation"),
		h("Help window (?)"),
		r("↑/↓ ctrl+↑/↓", "scroll by 1 / by 5 (mouse wheel: 3)"),
		r("pgup/pgdn", "scroll by a page"),
		r("esc", "clear the search, then close"),
		r("enter", "close (type to search)"),
	}
}
```

In `internal/tui/model.go`, add the `?` case to the normal-key switch (next to `case ","`):

```go
			case "?":
				m.contentPopup = newContentPopup("Help — keys", helpContent())
```

(Ungated on running/loading: the help window is read-only and harmless; while loading, `View()` shows the loading line anyway.)

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run TestHelp -v`
Expected: PASS (4 tests). If `TestHelpFooterCoverage` fails, the missing key tells you exactly which help row to add — fix help.go, not the test.

Check the Settings popup keys against `internal/tui/settings_popup.go` (lines 49-80) before finishing: up/down/space/enter/esc — if the actual handler differs from the table above, fix the table to match the code.

- [ ] **Step 6: Run the full suite**

Run: `go test ./internal/tui/` then `go vet ./... && gofmt -l .`
Expected: PASS, no vet/gofmt complaints.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/help.go internal/tui/help_test.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): ? opens the searchable help window listing every key"
```

---

### Task 5: docs

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `README.md` (key table, line ~36-54)
- Modify: `.claude/skills/adding-tui-windows/SKILL.md` (window taxonomy)

- [ ] **Step 1: CHANGELOG entry**

Add at the top of the existing entries (match the file's established format — read it first):

```markdown
- **Help window**: `?` opens a searchable list of every key binding, grouped
  by context. Type to filter; ↑/↓, ctrl+↑/↓, pgup/pgdn, and the mouse wheel
  scroll. Built on a new generic read-only content popup (`contentPopup`)
  reusable for future viewers. Mouse reporting is now enabled while gg runs
  (shift+drag for native terminal text selection).
```

- [ ] **Step 2: README key table**

Add a row to the key table (after the `r` / `q` row):

```markdown
| `?` | help: searchable list of all key bindings (type to filter; `↑`/`↓`, `ctrl+↑`/`ctrl+↓`, `pgup`/`pgdn`, mouse wheel to scroll) |
```

- [ ] **Step 3: Update the adding-tui-windows skill**

In `.claude/skills/adding-tui-windows/SKILL.md`, extend the window-type list (currently items 1-4) with:

```markdown
5. **Read-only scrollable/searchable text → content popup** (exemplar:
   `internal/tui/content_popup.go`). Don't build a new surface: construct
   `newContentPopup(title, []contentLine{...})` and assign it to
   `m.contentPopup`. Filtering, scrolling (keys + wheel), windowing, and
   rendering are free. `heading: true` lines group sections and are skipped
   by the filter. The help window (`help.go`) is the exemplar consumer.
```

Also update the footer-hint pointer in the panel checklist (row 6) and common-mistakes table from `view.go:99` to `the footerText const in view.go`.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs: record the help window and the generic content popup"
```

---

### Final verification (run by the controller after all tasks)

```bash
./test.sh race
```

Expected: gates (vet+gofmt) clean, unit suites pass, e2e pass. The e2e corpus is CLI-only and untouched by this feature.
