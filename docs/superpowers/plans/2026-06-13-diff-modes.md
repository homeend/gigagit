# Partial/Full Diff Modes + Change Navigation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a GitHub-style partial display mode (toggle with `f`, remembered for the session), `n`/`p` next/previous-change keys, and open-at-first-difference to the existing full-screen diff viewer.

**Architecture:** A pure `textdiff.Collapse` turns aligned rows into a fold-annotated `[]Line` for partial mode (`Expand` wraps 1:1 for full mode). `diffView` renders a `Line` stream that `offset` indexes; one `jumpTo` rule (change lands with 3 context lines above) drives `n`/`p`, `ctrl+↑/↓`, and open. A session `Model.diffPartial` flag remembers the toggle.

**Tech Stack:** Go 1.26, Bubble Tea v1, lipgloss. Tests: standard `testing`, real git repos in `t.TempDir()`.

**Spec:** `docs/superpowers/specs/2026-06-13-diff-modes-design.md` — read before starting.

**File map:**

| File | Status | Responsibility |
|---|---|---|
| `internal/textdiff/textdiff.go` | modify | add `Line`, `Expand`, `Collapse` |
| `internal/textdiff/textdiff_test.go` | modify | table tests for `Collapse`/`Expand` |
| `internal/tui/diff_view.go` | modify | `diffView` line-stream fields, `rebuild`, `jumpTo`/`nextBlock`/`prevBlock`/`currentBlockOrdinal`, `f`/`n`/`p` keys, loaders set mode + open offset |
| `internal/tui/diff_render.go` | modify | iterate `lines`, fold separator, gutter from full rows |
| `internal/tui/diff_view_test.go` | modify | fixture rename helper, updated jump expectations, nav/toggle/open tests |
| `internal/tui/diff_render_test.go` | modify | fixture rename, fold-render test |
| `internal/tui/model.go` | modify | `diffPartial` field, status enter-arm stub mode |
| `internal/tui/files_view.go` | modify | tree enter-arm stub mode |
| `internal/tui/help.go` | modify | `n`/`p`/`f` help rows |
| `CHANGELOG.md`, `README.md` | modify | document the new keys/modes |

---

### Task 1: `textdiff` — `Line`, `Expand`, `Collapse`

**Files:**
- Modify: `internal/textdiff/textdiff.go`
- Test: `internal/textdiff/textdiff_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/textdiff/textdiff_test.go`:

```go
func TestExpandWrapsRows1to1(t *testing.T) {
	rows := []Row{{Kind: Same, Left: "a"}, {Kind: Changed, Left: "b", Right: "B"}}
	lines := Expand(rows)
	if len(lines) != 2 {
		t.Fatalf("len = %d, want 2", len(lines))
	}
	for i, l := range lines {
		if l.Fold != 0 || l.Row != rows[i] {
			t.Fatalf("line %d = %+v, want Row %+v Fold 0", i, l, rows[i])
		}
	}
}

// sameRows builds n Same rows then sets the given indices to Changed.
func sameRows(n int, changed ...int) []Row {
	rows := make([]Row, n)
	for i := range rows {
		rows[i] = Row{Kind: Same, LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = Row{Kind: Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

func TestCollapseSingleBlockMidFile(t *testing.T) {
	// 20 rows, change at 10, context 3 → keep [7..13].
	rows := sameRows(20, 10)
	lines, blocks := Collapse(rows, []int{10}, 3)
	if len(lines) != 9 { // Fold(0..6) + 7 kept rows + Fold(14..19)
		t.Fatalf("len(lines) = %d, want 9:\n%+v", len(lines), lines)
	}
	if lines[0].Fold != 7 {
		t.Fatalf("leading fold = %d, want 7", lines[0].Fold)
	}
	if lines[8].Fold != 6 {
		t.Fatalf("trailing fold = %d, want 6", lines[8].Fold)
	}
	if len(blocks) != 1 || blocks[0] != 4 {
		t.Fatalf("blocks = %v, want [4]", blocks)
	}
	if lines[4].Fold != 0 || lines[4].Row.Kind != Changed {
		t.Fatalf("block line = %+v, want the Changed row", lines[4])
	}
}

func TestCollapseChangeAtStartNoLeadingFold(t *testing.T) {
	rows := sameRows(20, 0)
	lines, blocks := Collapse(rows, []int{0}, 3)
	if lines[0].Fold != 0 || lines[0].Row.Kind != Changed {
		t.Fatalf("first line must be the change, got %+v", lines[0])
	}
	if blocks[0] != 0 {
		t.Fatalf("blocks = %v, want [0]", blocks)
	}
	if last := lines[len(lines)-1]; last.Fold != 16 { // rows 4..19
		t.Fatalf("trailing fold = %d, want 16", last.Fold)
	}
}

func TestCollapseChangeAtEndNoTrailingFold(t *testing.T) {
	rows := sameRows(20, 19)
	lines, _ := Collapse(rows, []int{19}, 3)
	if last := lines[len(lines)-1]; last.Fold != 0 || last.Row.Kind != Changed {
		t.Fatalf("last line must be the change, got %+v", last)
	}
}

func TestCollapseTwoBlocksFarApartFoldBetween(t *testing.T) {
	// changes at 5 and 15, context 3 → keep [2..8] and [12..18], gap 9..11.
	rows := sameRows(21, 5, 15)
	lines, blocks := Collapse(rows, []int{5, 15}, 3)
	if len(blocks) != 2 {
		t.Fatalf("blocks = %v, want 2 entries", blocks)
	}
	// There must be a fold strictly between the two block lines.
	var foldBetween bool
	for i := blocks[0] + 1; i < blocks[1]; i++ {
		if lines[i].Fold == 3 {
			foldBetween = true
		}
	}
	if !foldBetween {
		t.Fatalf("expected a Fold:3 between the blocks:\n%+v", lines)
	}
}

func TestCollapseAdjacentBlocksMerge(t *testing.T) {
	// changes at 5 and 9, context 3 → windows [2..8] and [6..12] overlap:
	// no fold between them.
	rows := sameRows(20, 5, 9)
	lines, blocks := Collapse(rows, []int{5, 9}, 3)
	for i := blocks[0]; i <= blocks[1]; i++ {
		if lines[i].Fold != 0 {
			t.Fatalf("merged region must have no fold, found one at %d:\n%+v", i, lines)
		}
	}
}

func TestCollapseContextExceedsGapKeepsAll(t *testing.T) {
	rows := sameRows(10, 5)
	lines, _ := Collapse(rows, []int{5}, 100)
	if len(lines) != 10 {
		t.Fatalf("len = %d, want all 10 rows kept", len(lines))
	}
	for _, l := range lines {
		if l.Fold != 0 {
			t.Fatalf("no fold expected with huge context:\n%+v", lines)
		}
	}
}

func TestCollapseNoBlocksEmpty(t *testing.T) {
	lines, blocks := Collapse(sameRows(10), nil, 3)
	if len(lines) != 0 || len(blocks) != 0 {
		t.Fatalf("no-change collapse must be empty, got %d lines %d blocks", len(lines), len(blocks))
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/textdiff/ -run 'TestExpand|TestCollapse' -v`
Expected: FAIL — undefined `Line`, `Expand`, `Collapse`.

- [ ] **Step 3: Implement**

In `internal/textdiff/textdiff.go`, after the `Result` type, add:

```go
// Line is one display line of a diff view: either an aligned Row (Fold == 0)
// or a fold marker standing in for Fold elided equal rows (Row is zero). The
// partial (GitHub-style) view is a slice of these; the full view is the rows
// wrapped 1:1.
type Line struct {
	Row  Row
	Fold int // > 0: this line hides Fold unchanged rows
}

// Expand wraps every row as a Line 1:1 (the full-file view): no folds.
func Expand(rows []Row) []Line {
	lines := make([]Line, len(rows))
	for i, r := range rows {
		lines[i] = Line{Row: r}
	}
	return lines
}

// Collapse produces the partial view: every change block plus up to `context`
// equal rows on each side is kept; each remaining run of equal rows becomes a
// single fold Line. blocks holds the row index of each change-block start (as
// in Result.Blocks); the returned blockIdx remaps those starts into the kept
// Line slice (jump targets). No blocks → empty (the caller shows a "no
// difference" note). context < 0 is treated as 0.
func Collapse(rows []Row, blocks []int, context int) (lines []Line, blockIdx []int) {
	if len(blocks) == 0 {
		return nil, nil
	}
	if context < 0 {
		context = 0
	}
	n := len(rows)
	keep := make([]bool, n)
	for i := 0; i < n; i++ {
		if rows[i].Kind != Same {
			lo, hi := i-context, i+context
			if lo < 0 {
				lo = 0
			}
			if hi >= n {
				hi = n - 1
			}
			for j := lo; j <= hi; j++ {
				keep[j] = true
			}
		}
	}
	rowLine := make([]int, n) // original row index -> line index (kept rows)
	for i := range rowLine {
		rowLine[i] = -1
	}
	for i := 0; i < n; {
		if keep[i] {
			rowLine[i] = len(lines)
			lines = append(lines, Line{Row: rows[i]})
			i++
			continue
		}
		start := i
		for i < n && !keep[i] {
			i++
		}
		lines = append(lines, Line{Fold: i - start})
	}
	for _, b := range blocks {
		if b >= 0 && b < n && rowLine[b] >= 0 {
			blockIdx = append(blockIdx, rowLine[b])
		}
	}
	return lines, blockIdx
}
```

- [ ] **Step 4: Run to verify pass**

Run: `go test ./internal/textdiff/ -run 'TestExpand|TestCollapse' -v` → PASS.
Then the whole package: `go test ./internal/textdiff/ -race` → PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -l internal/textdiff/ && go vet ./internal/textdiff/
git add internal/textdiff/
git commit -m "feat(textdiff): Line + Expand/Collapse for partial diff views"
```

---

### Task 2: `diffView` renders a Line stream + new jump rule

This task makes `diffView` hold a `textdiff.Line` stream instead of raw rows, renders fold separators, and rewires `ctrl+↑/↓` onto a shared `jumpTo` rule (change lands with 3 context lines above). The package must compile and all existing diff tests must pass at the end. No partial mode is *triggered* yet (that's Task 3) — but `rebuild` supports it.

**Files:**
- Modify: `internal/tui/diff_view.go`
- Modify: `internal/tui/diff_render.go`
- Modify: `internal/tui/diff_view_test.go`, `internal/tui/diff_render_test.go`

- [ ] **Step 1: Replace the `diffView` struct + `scroll` + add rebuild/nav helpers**

In `internal/tui/diff_view.go`, replace the `diffView` type and `scroll` (lines ~21-49) with:

```go
// diffContext is the equal lines kept on each side of a change in partial
// mode; diffLead is the context kept above a change a jump lands on.
const (
	diffContext = 3
	diffLead    = 3
)

// diffView is the open full-screen side-by-side viewer; nil = closed.
// Pure scroll (offset) — there is no cursor row.
type diffView struct {
	title      string // file path, shown in the header
	context    string // "HEAD → working tree" or "@ <short-hash> <subject>"
	full       []textdiff.Row // immutable aligned rows (the comparison result)
	fullBlocks []int          // immutable change-block starts into full
	partial    bool           // current display mode (false = full)
	lines      []textdiff.Line // displayed sequence for the current mode
	blocks     []int           // block-start indices into lines (jump targets)
	offset     int             // top visible line
	truncated  bool            // alignment skipped (size guard)
	binary     bool
	tooLarge   bool
	loading    bool
	err        error
}

// rebuild recomputes the displayed lines/blocks from the immutable rows for
// the current mode. Full mode wraps 1:1; partial mode collapses unchanged runs.
func (v *diffView) rebuild() {
	if v.partial {
		v.lines, v.blocks = textdiff.Collapse(v.full, v.fullBlocks, diffContext)
	} else {
		v.lines = textdiff.Expand(v.full)
		v.blocks = v.fullBlocks
	}
}

// scroll moves the viewport by delta, clamped to [0, len(lines)-body].
func (v *diffView) scroll(delta, body int) {
	v.offset += delta
	max := len(v.lines) - body
	if max < 0 {
		max = 0
	}
	if v.offset > max {
		v.offset = max
	}
	if v.offset < 0 {
		v.offset = 0
	}
}

// jumpTo positions block-start line b with up to diffLead lines above it,
// clamped to the scroll range (scroll's clamp).
func (v *diffView) jumpTo(b, body int) {
	v.offset = b - diffLead
	if v.offset < 0 {
		v.offset = 0
	}
	v.scroll(0, body)
}

// nextBlock jumps to the first change strictly below the current one; no-op
// past the last. The +diffLead reference neutralizes the lead so the current
// change isn't re-selected.
func (v *diffView) nextBlock(body int) {
	for _, b := range v.blocks {
		if b > v.offset+diffLead {
			v.jumpTo(b, body)
			return
		}
	}
}

// prevBlock jumps to the first change strictly above the current one.
func (v *diffView) prevBlock(body int) {
	for i := len(v.blocks) - 1; i >= 0; i-- {
		if v.blocks[i] < v.offset+diffLead {
			v.jumpTo(v.blocks[i], body)
			return
		}
	}
}

// currentBlockOrdinal is the index of the change currently in view (for
// preserving position across a mode toggle).
func (v *diffView) currentBlockOrdinal() int {
	ord := 0
	for _, b := range v.blocks {
		if b <= v.offset+diffLead {
			ord++
		}
	}
	if ord > 0 {
		ord--
	}
	return ord
}
```

- [ ] **Step 2: Point `fillDiff` at the new fields**

In `internal/tui/diff_view.go`, replace the body of `fillDiff` after the binary guard:

```go
	res := textdiff.Compare(oldB, newB)
	v.full = res.Rows
	v.fullBlocks = res.Blocks
	v.truncated = res.Truncated
	v.rebuild()
```

- [ ] **Step 3: Rewire `ctrl+↑/↓` onto the helpers**

In `updateDiffViewKey`, replace the `case "ctrl+down":` and `case "ctrl+up":` blocks (the loops referencing `v.rows`) with:

```go
	case "ctrl+down":
		v.nextBlock(m.diffBodyRows())
	case "ctrl+up":
		v.prevBlock(m.diffBodyRows())
```

- [ ] **Step 4: Render the Line stream + fold separators**

In `internal/tui/diff_render.go`:

4a. Add a fold style to the `var (...)` block:

```go
	diffFold = lipgloss.NewStyle().Foreground(lipgloss.Color("240")) // dim fold rule
```

4b. Replace `diffPaneLines` (the gutter scan + the render loop) with:

```go
// diffPaneLines renders the visible window of display lines: a row Line as
// left│right, a fold Line as a full-width dim separator.
func (m Model) diffPaneLines(v *diffView, w, body int) []string {
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	maxNo := 0
	for _, r := range v.full { // gutter width from the full rows: stable across toggle
		if r.LeftNo > maxNo {
			maxNo = r.LeftNo
		}
		if r.RightNo > maxNo {
			maxNo = r.RightNo
		}
	}
	gut := len(fmt.Sprint(maxNo))
	if gut < 3 {
		gut = 3
	}

	out := make([]string, 0, body)
	for i := v.offset; i < v.offset+body && i < len(v.lines); i++ {
		ln := v.lines[i]
		if ln.Fold > 0 {
			out = append(out, foldSeparator(ln.Fold, w))
			continue
		}
		r := ln.Row
		left := diffCell(r.LeftNo, r.Left, gut, paneW,
			r.Kind == textdiff.Add, // gap on the left when the line exists only on the right
			r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
		right := diffCell(r.RightNo, r.Right, gut, paneW,
			r.Kind == textdiff.Del,
			r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
		out = append(out, left+"│"+right)
	}
	return out
}

// foldSeparator renders a fold marker as a centered label on a dim rule
// spanning the full width.
func foldSeparator(n, w int) string {
	label := fmt.Sprintf(" ⤬ %d unchanged lines ", n)
	if n == 1 {
		label = " ⤬ 1 unchanged line "
	}
	lw := lipgloss.Width(label)
	if lw >= w {
		return diffFold.Render(truncate(label, w))
	}
	left := (w - lw) / 2
	right := w - lw - left
	return diffFold.Render(strings.Repeat("─", left) + label + strings.Repeat("─", right))
}
```

4c. In `renderDiffView`, change the header range denominator from rows to lines: replace `if n := len(v.rows); n > 0 {` with `if n := len(v.lines); n > 0 {`.

- [ ] **Step 5: Update existing test fixtures (rename + rebuild) and the two jump expectations**

The struct no longer has a `rows` field. Add a fixture helper at the top of `internal/tui/diff_view_test.go` (after the imports):

```go
// diffViewWith builds a full-mode view over the given rows and block starts.
func diffViewWith(rows []textdiff.Row, blocks []int) *diffView {
	v := &diffView{full: rows, fullBlocks: blocks}
	v.rebuild()
	return v
}
```

Then update fixtures:

- `TestDiffViewKeysScrollAndJump`: replace `m.diffView = &diffView{rows: rows, blocks: []int{20, 30}}` with `m.diffView = diffViewWith(rows, []int{20, 30})`. Update the two jump expectations for the new lead rule:
  - `ctrl+down` from offset 0 now lands at `17` (block 20 − 3): change `!= 20` / `want 20` to `!= 17` / `want 17`.
  - `ctrl+up` from offset 35 now lands at `27` (block 30 − 3): change `!= 30` / `want 30` to `!= 27` / `want 27`.
- `TestDiffViewEscClosesAndQQuits`, `TestDiffViewSwallowsActionKeys`, `TestDiffViewEscReturnsToFilesView`, `TestDiffViewClosedOnNarrowResize`, `TestReRootClearsDiffView`, `TestDiffMsgStaleTagDropped`: these use `&diffView{}` (empty) — leave as-is (no `rows` field referenced).
- `TestDiffViewWheelScrolls`: replace `m.diffView = &diffView{rows: make([]textdiff.Row, 80)}` with `m.diffView = diffViewWith(make([]textdiff.Row, 80), nil)`.
- `TestDiffViewJumpAtMaxScrollIsNoOp`: replace `m.diffView = &diffView{rows: rows, blocks: []int{20, 30}, offset: 20}` with:
  ```go
  m.diffView = diffViewWith(rows, []int{20, 30})
  m.diffView.offset = 20
  ```
  The expectations stay `25` and `25` (the last block clamps to max 25, and a second press holds — verified: `nextBlock` at offset 25 finds block 30 > 28, `jumpTo(30)` clamps to 25 again).

In `internal/tui/diff_render_test.go`, every `&diffView{... rows: X, blocks: Y ...}` becomes a build-then-rebuild. Concretely:
- `TestRenderDiffViewPanes`: `v := &diffView{title: "f.txt", context: "HEAD → working tree", full: res.Rows, fullBlocks: res.Blocks}; v.rebuild()`.
- `TestRenderDiffViewTabsStayInPane`: `v := &diffView{title: "f.go", full: res.Rows, fullBlocks: res.Blocks}; v.rebuild()`.
- `TestRenderDiffViewNoContentDifferenceNote`: `v := &diffView{title: "f", context: "@ abc1234", full: res.Rows, fullBlocks: res.Blocks}; v.rebuild()`.
- `TestRenderDiffViewTruncatedNote`: `v := &diffView{title: "f", truncated: true, full: []textdiff.Row{{Kind: textdiff.Del, Left: "x", LeftNo: 1}}, fullBlocks: []int{0}}; v.rebuild()`.
- `TestRenderDiffViewScrollWindow`: `v := &diffView{title: "f", full: rows}; v.rebuild(); v.offset = 50`.

- [ ] **Step 6: Add a fold-render test**

Append to `internal/tui/diff_render_test.go`:

```go
func TestRenderDiffViewPartialShowsFold(t *testing.T) {
	// A long unchanged run around a single change: partial mode folds it.
	var oldB, newB strings.Builder
	for i := 0; i < 40; i++ {
		if i == 20 {
			oldB.WriteString("OLD\n")
			newB.WriteString("NEW\n")
		} else {
			oldB.WriteString(itoa(i) + "\n")
			newB.WriteString(itoa(i) + "\n")
		}
	}
	res := textdiff.Compare([]byte(oldB.String()), []byte(newB.String()))
	v := &diffView{title: "f", full: res.Rows, fullBlocks: res.Blocks, partial: true}
	v.rebuild()
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "unchanged lines") {
		t.Fatalf("partial mode must render a fold separator:\n%s", out)
	}
	// Full mode of the same diff has no fold.
	v.partial = false
	v.rebuild()
	out = ansi.Strip(renderModelWithDiff(v).render())
	if strings.Contains(out, "unchanged lines") {
		t.Fatalf("full mode must not fold:\n%s", out)
	}
}
```

- [ ] **Step 7: Run the package**

Run: `go test ./internal/tui/ -race -count=1`
Expected: PASS — all existing diff tests (with updated fixtures/expectations) plus the new fold test.

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/diff_view_test.go internal/tui/diff_render_test.go
git commit -m "refactor(tui): diff view renders a Line stream; context-aware jumps"
```

---

### Task 3: partial mode wiring — `Model.diffPartial`, `f`/`n`/`p`, open-at-first-difference

**Files:**
- Modify: `internal/tui/model.go`, `internal/tui/files_view.go`, `internal/tui/diff_view.go`
- Test: `internal/tui/diff_view_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/diff_view_test.go`:

```go
func TestDiffNPAliasCtrlJumps(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	rows := sameRowsTUI(40, 20, 30)
	mk := func() Model {
		mm := m
		mm.diffView = diffViewWith(rows, []int{20, 30})
		mm.diffTag = "status:x"
		return mm
	}
	for _, pair := range [][2]string{{"n", "ctrl+down"}, {"p", "ctrl+up"}} {
		a := mk()
		a.diffView.offset = 15
		b := mk()
		b.diffView.offset = 15
		ua, _ := a.Update(keyMsg(pair[0]))
		ub, _ := b.Update(keyMsg(pair[1]))
		if ua.(Model).diffView.offset != ub.(Model).diffView.offset {
			t.Fatalf("%s (%d) != %s (%d)", pair[0], ua.(Model).diffView.offset,
				pair[1], ub.(Model).diffView.offset)
		}
	}
}

func TestDiffJumpLandsWithContextAbove(t *testing.T) {
	m := diffModel()
	m.height = 12
	m.diffView = diffViewWith(sameRowsTUI(40, 20), []int{20})
	m.diffTag = "status:x"
	u, _ := m.Update(keyMsg("n"))
	if got := u.(Model).diffView.offset; got != 17 { // 20 - diffLead(3)
		t.Fatalf("offset = %d, want 17 (change with 3 lines above)", got)
	}
}

func TestDiffToggleFlipsModeAndSession(t *testing.T) {
	m := diffModel()
	m.height = 12
	m.diffView = diffViewWith(sameRowsTUI(40, 20), []int{20})
	m.diffTag = "status:x"
	if m.diffView.partial {
		t.Fatal("default mode is full")
	}
	u, _ := m.Update(keyMsg("f"))
	mm := u.(Model)
	if !mm.diffView.partial {
		t.Fatal("f must switch the open view to partial")
	}
	if !mm.diffPartial {
		t.Fatal("f must remember partial as the session default")
	}
	// Fold present now.
	if len(mm.diffView.lines) >= len(mm.diffView.full) {
		t.Fatal("partial mode must collapse unchanged runs")
	}
	u2, _ := mm.Update(keyMsg("f"))
	if u2.(Model).diffView.partial || u2.(Model).diffPartial {
		t.Fatal("a second f must switch back to full")
	}
}

func TestDiffTogglePreservesBlock(t *testing.T) {
	m := diffModel()
	m.height = 12 // body = 10
	m.diffView = diffViewWith(sameRowsTUI(60, 10, 50), []int{10, 50})
	m.diffTag = "status:x"
	// Jump to the second change, then toggle: that same change (by ordinal)
	// must still be on screen in the new mode. (currentBlockOrdinal can't be
	// compared directly across modes — clamping a short partial view shifts
	// the measured ordinal; what matters is the block stays visible.)
	u, _ := m.Update(keyMsg("n"))        // first change
	u, _ = u.(Model).Update(keyMsg("n")) // second change
	mm := u.(Model)
	ord := mm.diffView.currentBlockOrdinal()
	u, _ = mm.Update(keyMsg("f"))
	v := u.(Model).diffView
	body := mm.diffBodyRows()
	target := v.blocks[ord] // the same change, remapped into the new line stream
	if target < v.offset || target >= v.offset+body {
		t.Fatalf("toggle lost the focused change: line %d not in view [%d,%d)",
			target, v.offset, v.offset+body)
	}
}

func TestStatusLoaderOpensAtFirstDifference(t *testing.T) {
	dir, repo := newRepoDir(t)
	var base, work strings.Builder
	for i := 0; i < 60; i++ {
		base.WriteString(itoa(i) + "\n")
		if i == 40 {
			work.WriteString("CHANGED\n")
		} else {
			work.WriteString(itoa(i) + "\n")
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(base.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(work.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.height = 12
	m.repo = repo
	m.currentWorktree = dir
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if msg.view.err != nil {
		t.Fatal(msg.view.err)
	}
	if msg.view.offset == 0 {
		t.Fatal("a diff with a change far down must open scrolled to it, not at the top")
	}
	// The change block is visible from the open offset.
	if msg.view.offset > msg.view.blocks[0] || msg.view.blocks[0] >= msg.view.offset+m.diffBodyRows() {
		t.Fatalf("first block %d not in view [%d,%d)", msg.view.blocks[0],
			msg.view.offset, msg.view.offset+m.diffBodyRows())
	}
}

func TestDiffOpenInheritsSessionMode(t *testing.T) {
	dir, repo := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nb\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", "f.txt")
	gitIn(t, dir, "commit", "-m", "base")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("a\nB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := diffModel()
	m.repo = repo
	m.currentWorktree = dir
	m.diffPartial = true // session default set by a prior toggle
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if !msg.view.partial {
		t.Fatal("a new diff must inherit the session's partial mode")
	}
}
```

Add this helper near `diffViewWith` in `internal/tui/diff_view_test.go` (the tui-package mirror of textdiff's `sameRows`):

```go
// sameRowsTUI builds n Same rows then marks the given indices Changed.
func sameRowsTUI(n int, changed ...int) []textdiff.Row {
	rows := make([]textdiff.Row, n)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = textdiff.Row{Kind: textdiff.Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/tui/ -run 'TestDiffNP|TestDiffJump|TestDiffToggle|TestStatusLoaderOpensAt|TestDiffOpenInherits' -v`
Expected: FAIL — `m.diffPartial` undefined; `f`/`n`/`p` do nothing; loaders don't set offset/partial.

- [ ] **Step 3: Add the `Model.diffPartial` field**

In `internal/tui/model.go`, after the `diffTag string` field (line ~54), add:

```go
	diffPartial bool // session default for new diffs (false = full); the f key toggles it
```

- [ ] **Step 4: Add `f`/`n`/`p` to `updateDiffViewKey`**

In `internal/tui/diff_view.go` `updateDiffViewKey`, add these cases (alongside the existing `ctrl+down`/`ctrl+up`):

```go
	case "n":
		v.nextBlock(m.diffBodyRows())
	case "p":
		v.prevBlock(m.diffBodyRows())
	case "f":
		ord := v.currentBlockOrdinal()
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
		if len(v.blocks) > 0 {
			if ord >= len(v.blocks) {
				ord = len(v.blocks) - 1
			}
			v.jumpTo(v.blocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
```

- [ ] **Step 5: Loaders set mode + open offset**

In `internal/tui/diff_view.go`:

Make exactly three edits to **each** loader (`loadStatusDiffCmd` and
`loadCommitDiffCmd`); the fetch/guard body of each closure is otherwise
unchanged.

5a. `loadStatusDiffCmd`:
- After `root := m.currentWorktree`, add: `body := m.diffBodyRows()`
- Change the view literal to carry the session mode:
  `v := &diffView{title: f.Path, context: "HEAD → working tree", partial: m.diffPartial}`
- Replace the final `fillDiff(v, oldB, newB); return diffMsg{tag: tag, view: v}`
  (the last two statements of the closure) with:
  ```go
  fillDiff(v, oldB, newB)
  if len(v.blocks) > 0 {
      v.jumpTo(v.blocks[0], body)
  }
  return diffMsg{tag: tag, view: v}
  ```

5b. `loadCommitDiffCmd` — the same three edits:
- After `repo := m.repo`, add: `body := m.diffBodyRows()`
- Change the literal to:
  `v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), partial: m.diffPartial}`
- Replace the closure's final `fillDiff(v, oldB, newB); return diffMsg{tag: tag, view: v}` with the same `fillDiff` + `if len(v.blocks) > 0 { v.jumpTo(v.blocks[0], body) }` + `return` shown in 5a.

- [ ] **Step 6: Loading stubs carry the mode**

6a. `internal/tui/model.go` status enter arm (line ~309): add `partial: m.diffPartial`:

```go
				m.diffView = &diffView{title: f.Path, context: "HEAD → working tree", loading: true, partial: m.diffPartial}
```

6b. `internal/tui/files_view.go` tree enter arm (line ~161): add `partial: m.diffPartial`:

```go
		m.diffView = &diffView{
			title:   l.path,
			context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "),
			loading: true,
			partial: m.diffPartial,
		}
```

- [ ] **Step 7: Run the tests**

Run: `go test ./internal/tui/ -run 'TestDiffNP|TestDiffJump|TestDiffToggle|TestStatusLoaderOpensAt|TestDiffOpenInherits' -v` → PASS.
Then the whole package: `go test ./internal/tui/ -race -count=1` → PASS.

- [ ] **Step 8: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/diff_view.go internal/tui/model.go internal/tui/files_view.go internal/tui/diff_view_test.go
git commit -m "feat(tui): partial diff mode (f), n/p jumps, open at first difference"
```

---

### Task 4: hint line, help rows, docs, full gate

**Files:**
- Modify: `internal/tui/diff_render.go` (hint), `internal/tui/help.go`, `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Update the hint line**

In `internal/tui/diff_render.go`, replace the `diffHint` const:

```go
const diffHint = "[↑↓] scroll  [n/p] prev/next change  [f] full/partial  [esc] close  [q] quit"
```

- [ ] **Step 2: Update the help "Diff view" section**

In `internal/tui/help.go`, replace the `h("Diff view (enter)")` block (the rows for scroll/page/ctrl/esc/quit) with:

```go
		h("Diff view (enter)"),
		r("↑/k ↓/j", "scroll one line"),
		r("pgup/pgdn", "scroll one screen"),
		r("n/p", "next / previous change (also ctrl+↓/↑)"),
		r("f", "toggle full file ↔ changed lines only"),
		r("esc", "close"),
		r("q/ctrl+c", "quit"),
```

- [ ] **Step 3: Verify help/render tests still pass**

Run: `go test ./internal/tui/ -run 'TestHelp|TestRenderDiffView' -count=1` → PASS.

- [ ] **Step 4: CHANGELOG**

In `CHANGELOG.md`, under `## [Unreleased]` → `### Added`, add a block above the most recent feature entry:

```markdown
#### Diff view: partial mode + change navigation
- The full-screen diff view gains a **partial mode** (`f` toggles): show only
  changed lines plus 3 lines of context, collapsing long unchanged runs into a
  fold marker — GitHub's split-diff style. The choice is remembered for the
  session.
- `n` / `p` jump to the next / previous change (aliases of `ctrl+↓` / `ctrl+↑`).
- A diff now opens scrolled to the first change instead of the top.
```

- [ ] **Step 5: README**

In `README.md`, find the `| `enter` |` row describing the diff view and extend its inside-the-diff parenthetical to mention the new keys, e.g. change the trailing `…`ctrl+↑`/`ctrl+↓` jump between changes, `esc` closes)` to:

```markdown
`n`/`p` (or `ctrl+↑`/`ctrl+↓`) jump between changes, `f` toggles full file ↔ changed-lines-only, `esc` closes)
```

- [ ] **Step 6: Full race gate**

Run: `./test.sh race`
Expected: `all green`.

- [ ] **Step 7: Commit**

```bash
git add internal/tui/diff_render.go internal/tui/help.go CHANGELOG.md README.md
git commit -m "docs(tui): hint/help/docs for diff modes + n/p navigation"
```
