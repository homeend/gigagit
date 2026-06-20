# Commit Lane Color + List Mode Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Color each commit's `●` node by its graph lane (recycled palette), and add a `.`-menu list mode that renders the feed as a flat list with a lane-colored `●` gutter and no connectors.

**Architecture:** Color is applied at render time from separate metadata, never written into the row string (keeps the filter haystack and rune-width truncation correct). A generic `winRow.decorate` hook runs in `renderWindow` after slicing+padding each visual line; a commit-specific closure colors the one `●` rune using lane geometry. The pure `internal/commitgraph` engine is unchanged — color lives entirely in the TUI.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, the existing `renderWindow`/`winRow` windowing primitive.

## Global Constraints

- **Color never enters a row string.** `m.commitRows()` and anything feeding `panelView`/`Row(i)` stay plain text — no ANSI escape (`\x1b`) in any row string. Color is added only at render time by the decorator.
- **v1 colors the node `●` dot only**, not connector lines/vertical bars. (Full lane-line coloring is an explicit out-of-scope follow-up.)
- Lane→color mapping recycles: `laneColor(lane) = lanePalette[lane % len(lanePalette)]`, palette = `{"33","208","40","201","51","220","129"}` (lipgloss 256-colors).
- The `internal/commitgraph` engine stays pure (no lipgloss/color) — unchanged this round.
- The selected, focused commit row is NOT decorated — its `lipgloss.Reverse(true)` highlight wins.
- New `.`-menu action id `commits-viewmode`; labels verbatim `Show as list` (in graph mode) / `Show as graph` (in list mode). No keybinding → advertise in `help.go` only (no footer).
- TUI `Model` is a value receiver; mutate the copy and return it.
- Decorator/prefix interaction is out of scope: the decorator assumes the panel uses no frozen `prefix` gutter (the commits panel does not).
- Run `./test.sh race` before merge.

---

### Task 1: Generic per-row decorator seam in `renderWindow`

**Files:**
- Modify: `internal/tui/window.go` (`winRow` struct ~line 34; `renderWindow` ~lines 53-124)
- Test: `internal/tui/window_test.go`

**Interfaces:**
- Produces: `type rowDecorator func(visible string, hscroll, visualLine int) string`; new optional field `winRow.decorate rowDecorator`. `renderWindow` calls it per visual line, after `padRight`, before `style.Render`; passes `hscroll = o.hscroll` only in `modeScroll` (else 0) and `visualLine = ` the segment index.

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/window_test.go`:

```go
func TestRenderWindowDecorateReceivesGeometryAndPreservesWidth(t *testing.T) {
	var got []struct{ hs, vl int; w int }
	deco := func(visible string, hscroll, visualLine int) string {
		got = append(got, struct{ hs, vl, w int }{hscroll, visualLine, lipgloss.Width(visible)})
		return visible // identity: must not change visible width
	}
	rows := []winRow{{text: "abcdefghij", decorate: deco}}
	// cutoff: hscroll 0, single visual line 0, width == w
	out := renderWindow(rows, winOpts{w: 6, h: 1, mode: modeCutoff})
	if len(got) != 1 || got[0].hs != 0 || got[0].vl != 0 || got[0].w != 6 {
		t.Fatalf("cutoff geometry = %+v", got)
	}
	if lipgloss.Width(out[0]) != 6 {
		t.Fatalf("cutoff line width = %d, want 6", lipgloss.Width(out[0]))
	}
	// scroll: decorator sees the scroll offset.
	got = nil
	renderWindow(rows, winOpts{w: 6, h: 1, mode: modeScroll, hscroll: 3})
	if len(got) != 1 || got[0].hs != 3 {
		t.Fatalf("scroll hscroll = %+v, want hs=3", got)
	}
	// wrap: a long row yields a continuation line with visualLine 1.
	got = nil
	renderWindow(rows, winOpts{w: 6, h: 4, mode: modeWrap})
	if len(got) < 2 || got[0].vl != 0 || got[1].vl != 1 {
		t.Fatalf("wrap visualLine sequence = %+v", got)
	}
}
```

(The cutoff `dispMode` constant is `modeCutoff` in `window.go` — the default/zero value.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestRenderWindowDecorateReceivesGeometryAndPreservesWidth -v`
Expected: FAIL — `winRow` has no field `decorate`.

- [ ] **Step 3: Add the type, field, and the call**

In `internal/tui/window.go`, add above `winRow`:

```go
// rowDecorator restyles one already-sliced+padded visual line without changing
// its visible width (e.g. recoloring a single glyph). hscroll is the horizontal
// offset applied to this line (0 unless modeScroll); visualLine is the segment
// index (0 = a row's first line, 1+ = wrap continuations).
type rowDecorator func(visible string, hscroll, visualLine int) string
```

Add the field to `winRow`:

```go
type winRow struct {
	text     string
	prefix   string
	style    lipgloss.Style // zero value renders the text unchanged
	decorate rowDecorator   // optional; applied post-slice, post-pad
}
```

Extend the `dline` struct and its construction to carry the decorator + geometry, and apply it in the output loop. Replace the `dline` type and the two loops (`window.go` ~lines 74-123) with:

```go
	type dline struct {
		text  string
		style lipgloss.Style
		deco  rowDecorator
		hs    int
		si    int
		row   int
	}
	var dl []dline
	for ri, r := range rows {
		var segs []string
		hs := 0
		switch o.mode {
		case modeWrap:
			segs = wrapWidth(r.text, bodyW, 1<<20)
		case modeScroll:
			segs = []string{hslice(r.text, o.hscroll, bodyW)}
			hs = o.hscroll
		default:
			segs = []string{truncate(r.text, bodyW)}
		}
		if len(segs) == 0 {
			segs = []string{""}
		}
		for si, s := range segs {
			if pw > 0 {
				pre := strings.Repeat(" ", pw)
				if si == 0 {
					pre = padRight(truncate(r.prefix, pw), pw)
				}
				s = pre + s
			}
			dl = append(dl, dline{text: s, style: r.style, deco: r.decorate, hs: hs, si: si, row: ri})
		}
	}

	anchorLine := 0
	for i, d := range dl {
		if d.row == o.anchor {
			anchorLine = i
			break
		}
	}
	start := windowStart(len(dl), h, anchorLine)

	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx >= len(dl) {
			out = append(out, padRight("", w))
			continue
		}
		line := padRight(dl[idx].text, w)
		if dl[idx].deco != nil {
			line = dl[idx].deco(line, dl[idx].hs, dl[idx].si)
		}
		out = append(out, dl[idx].style.Render(line))
	}
	return out
```

- [ ] **Step 4: Run it to verify it passes**

Run: `go test ./internal/tui/ -run TestRenderWindowDecorateReceivesGeometryAndPreservesWidth -v`
Expected: PASS.

- [ ] **Step 5: Run the package to confirm no regression**

Run: `go test ./internal/tui/`
Expected: `ok` — existing windowing tests unaffected (decorate defaults nil everywhere).

- [ ] **Step 6: Commit**

```bash
git add internal/tui/window.go internal/tui/window_test.go
git commit -m "feat(tui): add a generic post-slice rowDecorator hook to renderWindow

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: Lane palette + the commit-dot decorator

**Files:**
- Create: `internal/tui/commit_color.go`
- Test: `internal/tui/commit_color_test.go`

**Interfaces:**
- Consumes: `rowDecorator` (Task 1).
- Produces: `lanePalette []lipgloss.Color`; `laneColor(lane int) lipgloss.Color`; `commitDotDecorator(nodeCol int, color lipgloss.Color) rowDecorator` — colors the single `●` rune at visible column `nodeCol - hscroll` on `visualLine 0`, no-op otherwise.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commit_color_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestLaneColorRecycles(t *testing.T) {
	if laneColor(0) != lanePalette[0] {
		t.Fatalf("lane 0 color")
	}
	if laneColor(len(lanePalette)) != lanePalette[0] {
		t.Fatalf("color should recycle modulo palette length")
	}
}

func TestCommitDotDecoratorColorsNodeOnlyOnFirstLine(t *testing.T) {
	deco := commitDotDecorator(2, lipgloss.Color("40")) // ● at column 2
	line := "  ● 1234567 subject"
	out := deco(line, 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected an ANSI color escape around the node: %q", out)
	}
	if lipgloss.Width(out) != lipgloss.Width(line) {
		t.Fatalf("decorator changed visible width: %d vs %d", lipgloss.Width(out), lipgloss.Width(line))
	}
	// wrap continuation line: untouched.
	if got := deco(line, 0, 1); got != line {
		t.Fatalf("continuation line should be untouched")
	}
	// scrolled past the node: untouched.
	if got := deco(line, 5, 0); got != line {
		t.Fatalf("scrolled-off node should be untouched")
	}
}

func TestCommitDotDecoratorIgnoresNonNode(t *testing.T) {
	deco := commitDotDecorator(2, lipgloss.Color("40"))
	line := "  X 1234567 subject" // no ● at column 2
	if got := deco(line, 0, 0); got != line {
		t.Fatalf("a non-● glyph at nodeCol must not be colored: %q", got)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestLaneColor|TestCommitDot' -v`
Expected: FAIL — `lanePalette`/`laneColor`/`commitDotDecorator` undefined.

- [ ] **Step 3: Implement**

Create `internal/tui/commit_color.go`:

```go
package tui

import "github.com/charmbracelet/lipgloss"

// lanePalette is the recycled set of lane colors (256-color codes), drawn the
// way every git client colors graph lanes. Index by lane % len.
var lanePalette = []lipgloss.Color{"33", "208", "40", "201", "51", "220", "129"}

func laneColor(lane int) lipgloss.Color {
	if lane < 0 {
		lane = 0
	}
	return lanePalette[lane%len(lanePalette)]
}

// commitDotDecorator returns a rowDecorator that recolors the single '●' node
// glyph at text column nodeCol with color. It acts only on a row's first visual
// line (visualLine 0); it is a no-op on wrap continuations, when the node has
// scrolled off the left (nodeCol < hscroll), or when the rune at the target
// column is not '●'. Visible width is preserved (it only restyles one rune).
func commitDotDecorator(nodeCol int, color lipgloss.Color) rowDecorator {
	style := lipgloss.NewStyle().Foreground(color)
	return func(visible string, hscroll, visualLine int) string {
		if visualLine != 0 {
			return visible
		}
		col := nodeCol - hscroll
		if col < 0 {
			return visible
		}
		r := []rune(visible)
		if col >= len(r) || r[col] != '●' {
			return visible
		}
		return string(r[:col]) + style.Render(string(r[col])) + string(r[col+1:])
	}
}
```

- [ ] **Step 4: Run them to verify they pass**

Run: `go test ./internal/tui/ -run 'TestLaneColor|TestCommitDot' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/commit_color.go internal/tui/commit_color_test.go
git commit -m "feat(tui): lane palette + single-node commit-dot decorator

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: Color the node dot in graph mode

**Files:**
- Modify: `internal/tui/model.go` (`rebuildCommitGraph` ~line 1149; add `commitGraphLanes []int` field near `commitGraphRows` ~line 75)
- Modify: `internal/tui/view.go` (`renderPanel` ~line 393 signature + selected-row skip; the two commits call sites ~lines 322-323 and 327/352; add `commitDecorators`)
- Modify the other 3 `renderPanel` call sites (`view.go` lines ~339, 340, 344) to pass `nil`
- Test: `internal/tui/commit_color_test.go`

**Interfaces:**
- Consumes: `commitDotDecorator`, `laneColor` (Task 2); `rowDecorator` (Task 1); `m.panelView(panel) (rows []string, idx []int)`; `m.commitGraphOn() bool`; `commitgraph.Row.Lane`.
- Produces: `Model.commitGraphLanes []int` (parallel to `commitGraphRows`); `func (m Model) commitDecorators(rows []string, idx []int) []rowDecorator`; `renderPanel` gains a `decos []rowDecorator` parameter (parallel to `rows`, may be nil).

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/commit_color_test.go`:

```go
func TestCommitDecoratorsColorGraphNodeNotSelected(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa"}, {Hash: "bbbbbbb"}}
	m = m.rebuildCommitGraph()
	if len(m.commitGraphLanes) != 2 {
		t.Fatalf("rebuildCommitGraph should populate lanes, got %v", m.commitGraphLanes)
	}
	rows, idx := m.panelView(panelCommits)
	decos := m.commitDecorators(rows, idx)
	if decos == nil {
		t.Fatal("graph mode should produce decorators")
	}
	// A linear graph puts both nodes in lane 0 at graph col 0 → text col 2.
	out := decos[0]("  ● aaaaaaa", 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("graph node should be colored: %q", out)
	}
}
```

(`footerModel` and `model.Commit` are already imported by the package's tests; add the `model` import to this test file if needed.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run TestCommitDecoratorsColorGraphNode -v`
Expected: FAIL — `m.commitGraphLanes` / `m.commitDecorators` undefined.

- [ ] **Step 3: Store lanes in `rebuildCommitGraph`**

In `internal/tui/model.go`, add the field beside `commitGraphRows`:

```go
	commitGraphRows  []string // cached single-line graph cells, parallel to commits; empty = none
	commitGraphLanes []int    // cached node lane per commit, parallel to commits
```

Populate it in `rebuildCommitGraph`:

```go
func (m Model) rebuildCommitGraph() Model {
	cs := make([]commitgraph.Commit, len(m.commits))
	for i, c := range m.commits {
		cs[i] = commitgraph.Commit{Hash: c.Hash, Parents: c.Parents}
	}
	rows, _ := commitgraph.Lay(cs)
	m.commitGraphRows = make([]string, len(rows))
	m.commitGraphLanes = make([]int, len(rows))
	for i, r := range rows {
		m.commitGraphRows[i] = r.Cells
		m.commitGraphLanes[i] = r.Lane
	}
	return m
}
```

- [ ] **Step 4: Add `commitDecorators`**

Add to `internal/tui/view.go` (near `commitRows`):

```go
// commitDecorators returns a per-display-row decorator slice (parallel to rows)
// that colors each commit's '●' node by its lane, or nil when coloring does not
// apply. idx maps display row → backing commit index (from panelView).
func (m Model) commitDecorators(rows []string, idx []int) []rowDecorator {
	if len(m.commitGraphLanes) != len(m.commits) {
		return nil
	}
	// Graph mode only this task; list mode is added in Task 4.
	if !m.commitGraphOn() {
		return nil
	}
	decos := make([]rowDecorator, len(rows))
	for j := range rows {
		ci := j
		if j < len(idx) {
			ci = idx[j]
		}
		if ci < 0 || ci >= len(m.commitGraphLanes) {
			continue
		}
		lane := m.commitGraphLanes[ci]
		decos[j] = commitDotDecorator(2+2*lane, laneColor(lane)) // 2 = renderPanel marker prefix
	}
	return decos
}
```

- [ ] **Step 5: Thread `decos` through `renderPanel` and skip the selected row**

In `internal/tui/view.go`, change the signature and the winRow build:

```go
func (m Model) renderPanel(p panel, label string, rows []string, decos []rowDecorator, boxW, boxH int) string {
```

In the row loop (where `wr[i] = winRow{...}` is built), attach the decorator for non-selected rows:

```go
		var deco rowDecorator
		if i != sel || !isFocused {
			if i < len(decos) {
				deco = decos[i]
			}
		}
		wr[i] = winRow{text: prefix + row, style: st, decorate: deco}
```

Update the two commits call sites to compute and pass `decos`:

```go
		cmRows, cmIdx := m.panelView(panelCommits)
		decos := m.commitDecorators(cmRows, cmIdx)
		body := m.renderPanel(panelCommits, m.panelLabel(panelCommits, "Commits ("+m.commitScopeLabel()+")"), cmRows, decos, g.w, g.boxH[panelCommits])
```

and the right-hand commits panel similarly (line ~352), reusing a freshly computed `cmRows, cmIdx := m.panelView(panelCommits)` + `decos`. Update the other three `renderPanel` calls (active/files/staged, lines ~339-344) to pass `nil` for the new `decos` argument.

- [ ] **Step 6: Run the new test + the package**

Run: `go test ./internal/tui/ -run TestCommitDecoratorsColorGraphNode -v && go test ./internal/tui/`
Expected: the targeted test PASSES; the full package is `ok` (all `renderPanel` call sites updated, so it compiles).

- [ ] **Step 7: Add the filter-haystack invariant test**

Add to `internal/tui/commit_color_test.go`:

```go
func TestCommitRowsNeverContainAnsi(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "x"}, {Hash: "bbbbbbb", Subject: "y"}}
	m = m.rebuildCommitGraph()
	for _, row := range m.commitRows() {
		if strings.ContainsRune(row, '\x1b') {
			t.Fatalf("commitRows must stay plain (no ANSI): %q", row)
		}
	}
}
```

Run: `go test ./internal/tui/ -run TestCommitRowsNeverContainAnsi -v`
Expected: PASS (color is render-time only).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/model.go internal/tui/view.go internal/tui/commit_color_test.go
git commit -m "feat(tui): color the commit graph node dot by lane

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: List mode

**Files:**
- Modify: `internal/tui/model.go` (`commitListMode bool` field)
- Modify: `internal/tui/view.go` (`commitRows` list branch; `commitDecorators` list branch)
- Modify: `internal/tui/commit_scope.go` (`commitViewModeRow`)
- Modify: `internal/tui/action_menu.go` (wire `commitViewModeRow`)
- Test: `internal/tui/commit_color_test.go`

**Interfaces:**
- Consumes: `commitDotDecorator`, `laneColor`; `m.commitGraphLanes`; `actionRow`.
- Produces: `Model.commitListMode bool`; `func (m Model) commitViewModeRow() (actionRow, bool)` (id `commits-viewmode`); list-mode branches in `commitRows` and `commitDecorators`.

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/commit_color_test.go`:

```go
func TestCommitViewModeToggle(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	r, ok := findRow(availableActions(m), "commits-viewmode")
	if !ok {
		t.Fatal("view-mode toggle missing on Commits panel")
	}
	if r.label != "Show as list" {
		t.Fatalf("graph-mode label = %q, want Show as list", r.label)
	}
	mm, _ := r.run(m)
	m = mm.(Model)
	if !m.commitListMode {
		t.Fatal("run should switch to list mode")
	}
	r2, _ := findRow(availableActions(m), "commits-viewmode")
	if r2.label != "Show as graph" {
		t.Fatalf("list-mode label = %q, want Show as graph", r2.label)
	}
}

func TestListModeRowsHaveDotGutterAndColorUnderFilter(t *testing.T) {
	m := footerModel()
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaa", Subject: "x"}, {Hash: "bbbbbbb", Subject: "y"}}
	m = m.rebuildCommitGraph()
	m.commitListMode = true
	rows := m.commitRows()
	if len(rows) != 2 || !strings.HasPrefix(rows[0], "● ") {
		t.Fatalf("list rows should start with a ● gutter: %q", rows)
	}
	if strings.ContainsRune(rows[0], '\x1b') {
		t.Fatalf("list rows must stay plain: %q", rows[0])
	}
	// List mode colors even when the graph would be suppressed (simulate filter
	// by asserting commitDecorators returns non-nil while commitGraphOn is false).
	m.sortModes[panelCommits] = sortDateDesc // non-default → forces commitGraphOn() false
	decos := m.commitDecorators(rows, []int{0, 1})
	if decos == nil {
		t.Fatal("list mode should color regardless of graph suppression")
	}
	out := decos[0]("  ● aaaaaaa x", 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("list dot should be colored: %q", out)
	}
}
```

(`sortDateDesc` is a real non-`sortDefault` constant in `viewstate.go`; any non-default value makes `commitGraphOn()` false.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCommitViewMode|TestListMode' -v`
Expected: FAIL — `commitListMode` / `commitViewModeRow` undefined; list branches absent.

- [ ] **Step 3: Add the field, the row branches, and the toggle**

In `internal/tui/model.go`, add near `commitListMode`'s siblings:

```go
	commitListMode bool // Commits feed rendered as a flat ●-gutter list, not a graph
```

In `internal/tui/view.go`, give `commitRows` a list branch at the top:

```go
func (m Model) commitRows() []string {
	if m.commitListMode {
		out := make([]string, 0, len(m.commits))
		for _, c := range m.commits {
			h := c.Hash
			if len(h) > 7 {
				h = h[:7]
			}
			out = append(out, "● "+h+" "+commitRefLabels(c.Refs)+c.Subject)
		}
		return out
	}
	graph := m.commitGraphOn() && len(m.commitGraphRows) == len(m.commits)
	// ... existing body unchanged ...
}
```

Replace the early-return guard in `commitDecorators` so list mode colors regardless of `commitGraphOn`, with the node column depending on mode:

```go
func (m Model) commitDecorators(rows []string, idx []int) []rowDecorator {
	if len(m.commitGraphLanes) != len(m.commits) {
		return nil
	}
	if !m.commitListMode && !m.commitGraphOn() {
		return nil // graph mode but graph suppressed → plain rows, no color
	}
	decos := make([]rowDecorator, len(rows))
	for j := range rows {
		ci := j
		if j < len(idx) {
			ci = idx[j]
		}
		if ci < 0 || ci >= len(m.commitGraphLanes) {
			continue
		}
		lane := m.commitGraphLanes[ci]
		nodeCol := 2 // list mode: ● at row col 0 → text col 2 (marker prefix)
		if !m.commitListMode {
			nodeCol = 2 + 2*lane // graph mode: node at graph col 2*lane
		}
		decos[j] = commitDotDecorator(nodeCol, laneColor(lane))
	}
	return decos
}
```

Add `commitViewModeRow` to `internal/tui/commit_scope.go`:

```go
// commitViewModeRow toggles the Commits feed between the lane graph and a flat
// ●-gutter list. Offered from the Branches or Commits panel.
func (m Model) commitViewModeRow() (actionRow, bool) {
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	label := "Show as list"
	if m.commitListMode {
		label = "Show as graph"
	}
	return actionRow{
		id:    "commits-viewmode",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitListMode = !m.commitListMode
			return m, nil
		},
	}, true
}
```

- [ ] **Step 4: Wire into `availableActions`**

In `internal/tui/action_menu.go`, after the `commitShowAllRow` block:

```go
	if r, ok := m.commitViewModeRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 5: Run the tests + package**

Run: `go test ./internal/tui/ -run 'TestCommitViewMode|TestListMode' -v && go test ./internal/tui/`
Expected: the targeted tests PASS; full package `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/model.go internal/tui/view.go internal/tui/commit_scope.go internal/tui/action_menu.go internal/tui/commit_color_test.go
git commit -m "feat(tui): list mode — flat ●-gutter commit list toggle

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Docs + help + full suite

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `internal/tui/help.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: no new symbols.

- [ ] **Step 1: Update the CHANGELOG**

Add to the top section of `CHANGELOG.md`:

```markdown
- **Commits panel — lane color + list mode.** Each commit's `●` node is colored
  by its graph lane (recycled palette, the standard git-client convention). A new
  `.`-menu toggle **Show as list / Show as graph** renders the feed as a flat
  `●`-gutter list (no connectors) that keeps its per-commit lane color even when
  filtered or re-sorted (where the connected graph is suppressed).
```

- [ ] **Step 2: Advertise in help (help.go only — no footer keybind)**

In `internal/tui/help.go`, find the Commits-panel `.`-menu description rows (the ones mentioning "Show all branches when soloed" / Solo). Add the view-mode action to the `.`-summary row and a description line, mirroring the existing `r(".", ...)` / `r("", ...)` style, e.g.:

```go
		r("", "Show as list / Show as graph (.-menu): flat ●-gutter list vs the lane graph; lane color marks each commit's line of development"),
```

Match the surrounding copy density. No footer change (the action has no keybinding).

- [ ] **Step 3: Run the full race suite**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit packages `ok`, e2e `ok`.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md internal/tui/help.go
git commit -m "docs(tui): changelog + help for commit lane color & list mode

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Self-Review

**Spec coverage:**
- Lane-based color, engine unchanged, color in TUID — Tasks 2-3. ✓
- Color never in the row string (invariant + test) — Global Constraints + Task 3 Step 7. ✓
- Generic post-slice decorator with hscroll/visualLine geometry — Task 1. ✓
- What each mode renders (cutoff/scroll/wrap) — Task 2 decorator logic + Task 1 geometry test. ✓
- Selected row not decorated — Task 3 Step 5. ✓
- Node-dot-only (no connector coloring) — decorator colors a single `●`. ✓
- List mode toggle (`commits-viewmode`, label copy), `●` gutter, colors under filter — Task 4. ✓
- List mode works where graph is suppressed — Task 4 `commitDecorators` branch + test. ✓
- Width invariant test — Task 1 + Task 2. ✓
- help.go-only advertising, CHANGELOG — Task 5. ✓
- Out of scope (lane lines, symbols, monochrome fallback) — nothing implements them. ✓

**Placeholder scan:** No TBD/TODO; all code steps show full code. Two steps ask the implementer to confirm a constant name against the codebase (`modeCutoff` in Task 1, `sortNewest` in Task 4) — these are real lookups with a named fallback instruction, not placeholders. The help.go row (Task 5 Step 2) is exact copy with the file location and pattern named.

**Type consistency:** `rowDecorator func(visible string, hscroll, visualLine int) string` defined in Task 1, consumed verbatim in Tasks 2-4. `commitDotDecorator(nodeCol int, color lipgloss.Color) rowDecorator`, `laneColor(int) lipgloss.Color`, `commitGraphLanes []int`, `commitDecorators(rows []string, idx []int) []rowDecorator`, `commitListMode bool`, action id `commits-viewmode` — all consistent across tasks. `renderPanel`'s new `decos []rowDecorator` parameter is added in Task 3 and all 5 call sites updated in the same task.
