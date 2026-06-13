# Diff view word-wrap toggle (Spec B) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `w` toggle to the full-screen diff view that word-wraps long lines across multiple display rows (lockstep across the two panes) instead of truncating with `…`.

**Architecture:** A third, width-keyed display stage in the diff view (`full→lines→disp`). `relayout(width)` expands the logical line stream into a display-row stream (`disp`/`dispBlocks`); `offset` and all navigation index `disp`. Wrapping reuses the emphasis `(disp []rune, emph []bool)` representation so highlights survive a split. Pure TUI — `textdiff`/`domain`/`cache` untouched. Wrap-off renders byte-identical to today.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss (width measurement). All changes in `internal/tui`.

**Reference:** spec `docs/superpowers/specs/2026-06-13-diff-word-wrap-design.md`. Build/test: `go build ./cmd/gg`, `./test.sh` (vet+gofmt → unit → e2e), `./test.sh race` before merge.

**Task order:** 1 (wrapCells splitter) → 2 (display stream: relayout + render + nav retarget) → 3 (activation: `w` key, session flag, resize, hint/help) → 4 (docs).

---

### Task 1: `wrapCells` — the word-wrap splitter

**Files:**
- Modify: `internal/tui/diff_render.go` (add `cellSeg` type + `wrapCells`)
- Test: `internal/tui/diff_render_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_render_test.go`:

```go
func segText(segs []cellSeg) []string {
	out := make([]string, len(segs))
	for i, s := range segs {
		out[i] = string(s.disp)
	}
	return out
}

func runesEmph(s string, on bool) (disp []rune, emph []bool) {
	disp = []rune(s)
	emph = make([]bool, len(disp))
	for i := range emph {
		emph[i] = on
	}
	return disp, emph
}

func TestWrapCellsShortLineOneSegment(t *testing.T) {
	d, e := runesEmph("hello", false)
	segs := wrapCells(d, e, 20)
	if got := segText(segs); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("segs = %q, want [\"hello\"]", got)
	}
}

func TestWrapCellsEmptyIsOneEmptySegment(t *testing.T) {
	segs := wrapCells(nil, nil, 10)
	if len(segs) != 1 || len(segs[0].disp) != 0 {
		t.Fatalf("empty input must yield one empty segment, got %q", segText(segs))
	}
}

func TestWrapCellsBreaksAtWordBoundary(t *testing.T) {
	d, e := runesEmph("foo bar baz", false)
	segs := wrapCells(d, e, 5)
	// greedy fill to 5 cols, break after the last space.
	if got := segText(segs); !reflectEqual(got, []string{"foo ", "bar ", "baz"}) {
		t.Fatalf("segs = %q, want [foo |bar |baz]", got)
	}
}

func TestWrapCellsHardBreaksLongWord(t *testing.T) {
	d, e := runesEmph("abcdefgh", false)
	segs := wrapCells(d, e, 3)
	if got := segText(segs); !reflectEqual(got, []string{"abc", "def", "gh"}) {
		t.Fatalf("segs = %q, want [abc|def|gh]", got)
	}
}

func TestWrapCellsSingleOverWideRuneTakenAlone(t *testing.T) {
	// tw=1, two runes → two single-rune segments (never empty-loop).
	d, e := runesEmph("ab", false)
	segs := wrapCells(d, e, 1)
	if got := segText(segs); !reflectEqual(got, []string{"a", "b"}) {
		t.Fatalf("segs = %q, want [a|b]", got)
	}
}

func TestWrapCellsCarriesEmphMask(t *testing.T) {
	// "ab cd" with emphasis on the whole thing, tw=2 → masks stay aligned.
	d, e := runesEmph("ab cd", true)
	segs := wrapCells(d, e, 2)
	for _, s := range segs {
		if len(s.disp) != len(s.emph) {
			t.Fatalf("seg disp/emph length mismatch: %d vs %d", len(s.disp), len(s.emph))
		}
		for _, on := range s.emph {
			if !on {
				t.Fatal("emphasis mask must carry across the split")
			}
		}
	}
}

func reflectEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestWrapCells`
Expected: FAIL — `wrapCells`/`cellSeg` undefined.

- [ ] **Step 3: Implement `cellSeg` and `wrapCells`**

In `internal/tui/diff_render.go`, add after the `var (...)` style block (before `sanitizeLine`):

```go
// cellSeg is one pane's text for one display row: the sanitized display runes
// and the parallel emphasis mask, already ≤ the pane's text width. A zero
// cellSeg renders blank.
type cellSeg struct {
	disp []rune
	emph []bool
}

// wrapCells splits a sanitized (disp, emph) line into segments each ≤ tw
// display columns. It greedily fills to tw, then breaks after the last space
// in the segment (word-wrap); a word longer than tw is hard-broken at the fill
// boundary, and a single rune wider than tw is taken alone (never an empty
// loop). The emph mask is sliced alongside so emphasis survives a split. An
// empty input yields one empty segment (so the row still draws a line).
func wrapCells(disp []rune, emph []bool, tw int) []cellSeg {
	if tw < 1 {
		tw = 1
	}
	if len(disp) == 0 {
		return []cellSeg{{}}
	}
	var segs []cellSeg
	start := 0
	for start < len(disp) {
		end, width := start, 0
		for end < len(disp) {
			rw := lipgloss.Width(string(disp[end]))
			if width+rw > tw {
				break
			}
			width += rw
			end++
		}
		if end == start { // a single rune wider than tw
			end = start + 1
		}
		brk := end
		if end < len(disp) { // more to come: prefer a word boundary
			sp := -1
			for j := start; j < end; j++ {
				if disp[j] == ' ' {
					sp = j
				}
			}
			if sp > start {
				brk = sp + 1 // keep the space on this segment
			}
		}
		segs = append(segs, cellSeg{disp: disp[start:brk], emph: emph[start:brk]})
		start = brk
	}
	return segs
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run TestWrapCells` then `gofmt -l internal/tui/diff_render.go`
Expected: PASS; gofmt prints nothing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/diff_render.go internal/tui/diff_render_test.go
git commit -m "feat(tui): word-wrap splitter for diff cells

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: display stream — `relayout`, render, navigation

**Files:**
- Modify: `internal/tui/diff_view.go` (`diffView` fields, `dRow`, `gutterWidth` use, `wrapSide`/`segAt`, `relayout`, `rebuild`, `scroll`, nav retarget, `applyDiff`, `f` toggle)
- Modify: `internal/tui/diff_render.go` (`gutterWidth` extracted, `segCell`, `diffPaneLines` over `disp`, render readout)
- Test: `internal/tui/diff_view_test.go`, `internal/tui/diff_render_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_view_test.go`:

```go
func TestRelayoutWrapOffMirrorsLines(t *testing.T) {
	rows := sameRowsTUI(5, 2) // 5 rows, change block at index 2 (helper already in package)
	v := diffViewWith(rows, []int{2})
	v.relayout(0) // width 0 ⇒ wrap-off 1:1
	if len(v.disp) != len(v.lines) {
		t.Fatalf("wrap-off disp must mirror lines: %d vs %d", len(v.disp), len(v.lines))
	}
	if len(v.dispBlocks) != 1 || v.dispBlocks[0] != v.blocks[0] {
		t.Fatalf("wrap-off dispBlocks must equal blocks: %v vs %v", v.dispBlocks, v.blocks)
	}
	for i, dr := range v.disp {
		if !dr.first || dr.line != i {
			t.Fatalf("dRow %d: first=%v line=%d", i, dr.first, dr.line)
		}
	}
}

func TestRelayoutWrapOnExpandsAndRemapsBlocks(t *testing.T) {
	// One Changed row whose sides are long enough to wrap at a tiny width.
	rows := []textdiff.Row{
		{Kind: textdiff.Same, Left: "a", Right: "a", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Changed, Left: "one two three four", Right: "one two three FOUR", LeftNo: 2, RightNo: 2},
	}
	v := diffViewWith(rows, []int{1})
	v.wrap = true
	v.relayout(40) // paneW≈19, tw≈15 → the long row wraps to several display rows
	// The change block (logical line 1) now starts at its first display row.
	if len(v.dispBlocks) != 1 || v.dispBlocks[0] != v.lineStart[1] {
		t.Fatalf("dispBlocks[0]=%v want lineStart[1]=%d", v.dispBlocks, v.lineStart[1])
	}
	// Line 1 must occupy more than one display row (it wrapped).
	h := 0
	for _, dr := range v.disp {
		if dr.line == 1 {
			h++
		}
	}
	if h < 2 {
		t.Fatalf("the long changed row should wrap to ≥2 display rows, got %d", h)
	}
	// Only the first display row of line 1 carries the gutter.
	firsts := 0
	for _, dr := range v.disp {
		if dr.line == 1 && dr.first {
			firsts++
		}
	}
	if firsts != 1 {
		t.Fatalf("exactly one first-row for the wrapped line, got %d", firsts)
	}
}

func TestRelayoutWrapOnGapSideHasNilSegments(t *testing.T) {
	// An Add row: left is a gap. Its wrapped dRows must carry no left segment.
	rows := []textdiff.Row{{Kind: textdiff.Add, Right: "added text here", RightNo: 1}}
	v := diffViewWith(rows, []int{0})
	v.wrap = true
	v.relayout(40)
	for _, dr := range v.disp {
		if len(dr.left.disp) != 0 {
			t.Fatalf("Add row's left side must be empty (gap), got %q", string(dr.left.disp))
		}
	}
}
```

Add to `internal/tui/diff_render_test.go`:

```go
func TestDiffPaneLinesWrappedRowWidthAndCount(t *testing.T) {
	rows := []textdiff.Row{
		{Kind: textdiff.Changed, Left: "alpha beta gamma delta", Right: "alpha beta gamma DELTA", LeftNo: 1, RightNo: 1},
	}
	v := diffViewWith(rows, []int{0})
	v.wrap = true
	const w = 41 // paneW=20 each → row width 41
	v.relayout(w)
	m := footerModel()
	lines := m.diffPaneLines(v, w, len(v.disp))
	if len(lines) < 2 {
		t.Fatalf("the long row should render as ≥2 display rows, got %d", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != w {
			t.Fatalf("display row %d width = %d, want %d", i, lipgloss.Width(ln), w)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'Relayout|DiffPaneLinesWrapped'`
Expected: FAIL — `relayout`/`v.disp`/`v.dispBlocks`/`v.wrap`/`dRow.line` undefined.

- [ ] **Step 3: Add the `diffView` fields and `dRow` type**

In `internal/tui/diff_view.go`, replace the `diffView` struct (lines ~25-41) with:

```go
// diffView is the open full-screen side-by-side viewer; nil = closed.
// Pure scroll (offset) — there is no cursor row.
type diffView struct {
	title      string          // file path, shown in the header
	context    string          // "HEAD → working tree" or "@ <short-hash> <subject>"
	full       []textdiff.Row  // immutable aligned rows (the comparison result)
	fullBlocks []int           // immutable change-block starts into full
	partial    bool            // mode: collapse unchanged runs (false = full)
	wrap       bool            // mode: word-wrap long lines (false = truncate)
	width      int             // overlay width at last layout (0 = unset → wrap-off)
	lines      []textdiff.Line // logical (mode) stream that relayout consumes
	blocks     []int           // change-block starts into lines
	disp       []dRow          // display rows: what offset indexes and render draws
	dispBlocks []int           // change-block starts as display-row indices (jump targets)
	lineStart  []int           // logical line index → its first display-row index
	offset     int             // top visible display row
	truncated  bool            // alignment skipped (size guard)
	binary     bool
	tooLarge   bool
	loading    bool
	err        error
}

// dRow is one display row: a fold marker, or one (possibly continuation) slice
// of an aligned Row. line is the source index into v.lines (resize re-anchor).
// When wrap is off, a dRow carries the whole row (left/right unset) and the
// renderer draws it through diffCell; when wrap is on, left/right hold this
// row's pre-wrapped slice of each side.
type dRow struct {
	line  int          // source logical-line index
	fold  int          // >0: a fold separator of N unchanged lines (whole width)
	row   textdiff.Row // the source aligned row (Kind / gutter numbers / gap)
	left  cellSeg      // wrap-on: this display row's left slice (zero = blank)
	right cellSeg      // wrap-on: right slice
	first bool         // first display row of the source line (gutter shows here)
}
```

- [ ] **Step 4: Replace `rebuild`, `scroll`, and the navigation helpers**

In `internal/tui/diff_view.go`, replace `rebuild` (lines ~43-52) through `currentBlockOrdinal` (ends ~114) with:

```go
// rebuild recomputes the logical (mode) stream, then the display stream.
func (v *diffView) rebuild() {
	if v.partial {
		v.lines, v.blocks = textdiff.Collapse(v.full, v.fullBlocks, diffContext)
	} else {
		v.lines = textdiff.Expand(v.full)
		v.blocks = v.fullBlocks
	}
	v.relayout(v.width)
}

// relayout builds the display-row stream (disp/dispBlocks) from the logical
// lines for the current wrap mode and width. Wrap off (or width unset) is a
// 1:1 mapping — disp mirrors lines, dispBlocks == blocks — so rendering and
// navigation are byte-identical to the pre-wrap view. Wrap on expands each
// aligned row to max(leftSegs, rightSegs) display rows.
func (v *diffView) relayout(width int) {
	v.width = width
	v.disp = v.disp[:0]
	v.dispBlocks = v.dispBlocks[:0]
	if cap(v.lineStart) >= len(v.lines) {
		v.lineStart = v.lineStart[:len(v.lines)]
	} else {
		v.lineStart = make([]int, len(v.lines))
	}

	paneW := (width - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	gut := gutterWidth(v.full)
	tw := paneW - gut - 1
	if tw < 1 {
		tw = 1
	}

	for li := range v.lines {
		v.lineStart[li] = len(v.disp)
		ln := v.lines[li]
		switch {
		case ln.Fold > 0:
			v.disp = append(v.disp, dRow{line: li, fold: ln.Fold, first: true})
		case !v.wrap || width <= 0:
			v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: true})
		default:
			leftSegs := wrapSide(ln.Row.Left, ln.Row.LeftSpans, ln.Row.Kind, false, tw)
			rightSegs := wrapSide(ln.Row.Right, ln.Row.RightSpans, ln.Row.Kind, true, tw)
			h := len(leftSegs)
			if len(rightSegs) > h {
				h = len(rightSegs)
			}
			if h < 1 {
				h = 1
			}
			for k := 0; k < h; k++ {
				v.disp = append(v.disp, dRow{line: li, row: ln.Row, first: k == 0,
					left: segAt(leftSegs, k), right: segAt(rightSegs, k)})
			}
		}
	}

	for _, b := range v.blocks {
		if b >= 0 && b < len(v.lineStart) {
			v.dispBlocks = append(v.dispBlocks, v.lineStart[b])
		}
	}
	v.scroll(0, 1) // clamp offset into the new range (body-agnostic floor)
}

// wrapSide sanitizes+wraps one side of a row into ≤tw segments, or returns nil
// for a gap side (the absent side of an Add/Del) so the renderer draws filler.
func wrapSide(text string, spans []textdiff.Span, kind textdiff.Kind, right bool, tw int) []cellSeg {
	if (!right && kind == textdiff.Add) || (right && kind == textdiff.Del) {
		return nil
	}
	disp, emph := sanitizeSpans(text, spans)
	return wrapCells(disp, emph, tw)
}

// segAt returns the kth segment or a blank cellSeg (a present-but-shorter side
// renders blank past its last segment).
func segAt(segs []cellSeg, k int) cellSeg {
	if k < len(segs) {
		return segs[k]
	}
	return cellSeg{}
}

// scroll moves the viewport by delta, clamped to [0, len(disp)-body].
func (v *diffView) scroll(delta, body int) {
	v.offset += delta
	max := len(v.disp) - body
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

// jumpTo positions block-start display row b with up to diffLead rows above it.
func (v *diffView) jumpTo(b, body int) {
	v.offset = b - diffLead
	if v.offset < 0 {
		v.offset = 0
	}
	v.scroll(0, body)
}

// nextBlock jumps to the first change strictly below the current one; no-op
// past the last. The +diffLead reference neutralizes the lead.
func (v *diffView) nextBlock(body int) {
	for _, b := range v.dispBlocks {
		if b > v.offset+diffLead {
			v.jumpTo(b, body)
			return
		}
	}
}

// prevBlock jumps to the first change strictly above the current one.
func (v *diffView) prevBlock(body int) {
	for i := len(v.dispBlocks) - 1; i >= 0; i-- {
		if v.dispBlocks[i] < v.offset+diffLead {
			v.jumpTo(v.dispBlocks[i], body)
			return
		}
	}
}

// currentBlockOrdinal is the index of the change currently in view (for
// preserving position across a mode toggle).
func (v *diffView) currentBlockOrdinal() int {
	ord := 0
	for _, b := range v.dispBlocks {
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

- [ ] **Step 5: Retarget `applyDiff` and the `f` toggle to `dispBlocks`**

In `internal/tui/diff_view.go`, in `applyDiff` change the jump to use `dispBlocks`:

```go
		v.rebuild()
		if len(v.dispBlocks) > 0 {
			v.jumpTo(v.dispBlocks[0], body)
		}
```

In `updateDiffViewKey`, the `case "f":` block becomes (only `v.blocks` → `v.dispBlocks`):

```go
	case "f":
		ord := v.currentBlockOrdinal()
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
```

- [ ] **Step 6: Extract `gutterWidth`, add `segCell`, render `disp`**

In `internal/tui/diff_render.go`, add `gutterWidth` (used by both `relayout` and `diffPaneLines`):

```go
// gutterWidth is the line-number column width, derived from the full rows so it
// is stable across mode/wrap toggles. Minimum 3.
func gutterWidth(full []textdiff.Row) int {
	maxNo := 0
	for _, r := range full {
		if r.LeftNo > maxNo {
			maxNo = r.LeftNo
		}
		if r.RightNo > maxNo {
			maxNo = r.RightNo
		}
	}
	g := len(fmt.Sprint(maxNo))
	if g < 3 {
		g = 3
	}
	return g
}
```

Replace `diffPaneLines` (lines ~104-142) with the `disp`-based version that branches on `v.wrap`:

```go
// diffPaneLines renders the visible window of display rows. A fold dRow is a
// full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell.
func (m Model) diffPaneLines(v *diffView, w, body int) []string {
	paneW := (w - 1) / 2
	if paneW < 4 {
		paneW = 4
	}
	gut := gutterWidth(v.full)

	out := make([]string, 0, body)
	for i := v.offset; i < v.offset+body && i < len(v.disp); i++ {
		dr := v.disp[i]
		if dr.fold > 0 {
			out = append(out, foldSeparator(dr.fold, w))
			continue
		}
		r := dr.row
		if !v.wrap {
			left := diffCell(r.LeftNo, r.Left, gut, paneW,
				r.Kind == textdiff.Add,
				r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell, r.LeftSpans)
			right := diffCell(r.RightNo, r.Right, gut, paneW,
				r.Kind == textdiff.Del,
				r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell, r.RightSpans)
			out = append(out, left+"│"+right)
			continue
		}
		leftGap := r.Kind == textdiff.Add
		rightGap := r.Kind == textdiff.Del
		leftNo, rightNo := 0, 0
		if dr.first && !leftGap {
			leftNo = r.LeftNo
		}
		if dr.first && !rightGap {
			rightNo = r.RightNo
		}
		left := segCell(leftNo, dr.left, gut, paneW, leftGap,
			r.Kind == textdiff.Del || r.Kind == textdiff.Changed, diffDelCell)
		right := segCell(rightNo, dr.right, gut, paneW, rightGap,
			r.Kind == textdiff.Add || r.Kind == textdiff.Changed, diffAddCell)
		out = append(out, left+"│"+right)
	}
	return out
}

// segCell renders one pane's pre-wrapped segment into a width-col cell: gutter
// (number when no>0, blank on a continuation) + the styled, padded body. gap
// draws the · filler (absent side). hot applies the add/del background;
// emphasis rides in seg.emph.
func segCell(no int, seg cellSeg, gut, width int, gap, hot bool, hotStyle lipgloss.Style) string {
	if gap {
		return diffGapCell.Render(strings.Repeat("·", width))
	}
	if gut > width-2 { // degenerate pane: keep the cell inside its width
		gut = width - 2
		if gut < 1 {
			gut = 1
		}
	}
	num := strings.Repeat(" ", gut+1)
	if no > 0 {
		num = fmt.Sprintf("%*d ", gut, no)
	}
	tw := width - gut - 1
	if tw < 1 {
		tw = 1
	}
	base := lipgloss.NewStyle()
	if hot {
		base = hotStyle
	}
	body := styledRuns(seg.disp, seg.emph, base)
	if pad := tw - lipgloss.Width(string(seg.disp)); pad > 0 {
		body += base.Render(strings.Repeat(" ", pad))
	}
	return diffGutter.Render(truncate(num, gut+1)) + body
}
```

In `renderDiffView`, change the `rows X–Y/N` readout to count display rows — replace `if n := len(v.lines); n > 0 {` with `if n := len(v.disp); n > 0 {`.

- [ ] **Step 7: Run tests**

Run: `go test ./internal/tui/` then `go build ./cmd/gg` and `gofmt -l internal/tui/diff_view.go internal/tui/diff_render.go`
Expected: PASS — the new relayout/render tests pass AND **every pre-existing `diff_view_test.go` / `diff_render_test.go` test passes unchanged** (wrap defaults off → `disp` mirrors `lines`, so navigation and rendering are byte-identical). Build clean; gofmt silent.

> If a pre-existing test fails, the wrap-off path is NOT byte-identical or navigation drifted — STOP and report; do not edit the golden/expectation to match.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/diff_view_test.go internal/tui/diff_render_test.go
git commit -m "feat(tui): display-row stream + lockstep wrap render for the diff view

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: activation — `w` toggle, session flag, resize, hint/help

**Files:**
- Modify: `internal/tui/model.go` (`Model.diffWrap`; `WindowSizeMsg` re-anchor; status enter-arm sets wrap/width)
- Modify: `internal/tui/diff_view.go` (loaders set `wrap`/`width`; `w` key case)
- Modify: `internal/tui/files_view.go` (tree enter-arm sets wrap/width)
- Modify: `internal/tui/diff_render.go` (`diffHint` += `[w] wrap`)
- Modify: `internal/tui/help.go` ("Diff view" `w` row)
- Test: `internal/tui/diff_view_test.go`

- [ ] **Step 1: Write the failing tests**

Add to `internal/tui/diff_view_test.go`:

```go
func TestDiffWToggleFlipsWrapAndSession(t *testing.T) {
	rows := sameRowsTUI(40, 20)
	m := diffModel()
	m.width, m.height = 80, 24
	m.diffView = diffViewWith(rows, []int{20})
	m.diffView.width = 80
	m.diffView.rebuild()
	m.diffTag = "status:x"

	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	mm := u.(Model)
	if !mm.diffView.wrap {
		t.Fatal("w must turn wrap on")
	}
	if !mm.diffWrap {
		t.Fatal("w must record the session wrap flag")
	}
	u2, _ := mm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("w")})
	if u2.(Model).diffView.wrap {
		t.Fatal("a second w must turn wrap off")
	}
}

func TestDiffOpenInheritsSessionWrap(t *testing.T) {
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
	m.diffWrap = true
	msg := m.loadStatusDiffCmd(model.FileStatus{Path: "f.txt", Staged: '.', Unstaged: 'M'})().(diffMsg)
	if !msg.view.wrap {
		t.Fatal("a new diff must inherit the session's wrap mode")
	}
}

func TestDiffResizeReanchorsToTopLine(t *testing.T) {
	// A wrapped view scrolled to a known logical line stays anchored to it
	// after a width change.
	rows := make([]textdiff.Row, 30)
	wide := "this is a fairly long line that will wrap at a narrow pane width"
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: wide, Right: wide, LeftNo: i + 1, RightNo: i + 1}
	}
	m := diffModel()
	m.width, m.height = 80, 24
	v := diffViewWith(rows, nil)
	v.wrap = true
	v.width = 80
	v.rebuild()
	v.offset = v.lineStart[10] // top is logical line 10
	m.diffView = v
	m.diffTag = "status:x"

	u, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	mm := u.(Model)
	if mm.diffView == nil {
		t.Fatal("a width≥60 resize must not close the diff")
	}
	// After re-wrap at the new width, the top display row still belongs to
	// logical line 10.
	if got := mm.diffView.disp[mm.diffView.offset].line; got != 10 {
		t.Fatalf("after resize the top line should still be 10, got %d", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'DiffWToggle|DiffOpenInheritsSessionWrap|DiffResizeReanchors'`
Expected: FAIL — `m.diffWrap` undefined; `w` key unhandled; resize doesn't re-anchor.

- [ ] **Step 3: Add `Model.diffWrap`**

In `internal/tui/model.go`, add the field next to `diffPartial` (search for `diffPartial bool` and add below it):

```go
	diffWrap bool // session: word-wrap long lines in the diff view (w toggles)
```

- [ ] **Step 4: Set `wrap`/`width` at the diff creation sites**

In `internal/tui/diff_view.go`, in `loadStatusDiffCmd`, after `body := m.diffBodyRows()` add `width, _ := m.overlayDims()`, and change the `v := &diffView{...}` to:

```go
	v := &diffView{title: f.Path, context: "HEAD → working tree", partial: m.diffPartial, wrap: m.diffWrap, width: width}
```

In `loadCommitDiffCmd`, likewise add `width, _ := m.overlayDims()` after `body := m.diffBodyRows()` and change its `v := &diffView{...}` to include `wrap: m.diffWrap, width: width`:

```go
	v := &diffView{title: line.path, context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "), partial: m.diffPartial, wrap: m.diffWrap, width: width}
```

In `internal/tui/model.go`, the status enter-arm loading stub (search `loading: true, partial: m.diffPartial`) gains `wrap: m.diffWrap`:

```go
				m.diffView = &diffView{title: f.Path, context: "HEAD → working tree", loading: true, partial: m.diffPartial, wrap: m.diffWrap}
```

In `internal/tui/files_view.go`, the tree enter-arm stub gains `wrap: m.diffWrap` (in the multi-line struct literal):

```go
		m.diffView = &diffView{
			title:   l.path,
			context: "@ " + strings.TrimPrefix(m.filesTitle, "Files "),
			loading: true,
			partial: m.diffPartial,
			wrap:    m.diffWrap,
		}
```

- [ ] **Step 5: Add the `w` key case**

In `internal/tui/diff_view.go`, in `updateDiffViewKey`, add a `case "w":` after the `case "f":` block (before the closing `}` of the switch):

```go
	case "w":
		ord := v.currentBlockOrdinal()
		v.wrap = !v.wrap
		v.relayout(v.width)
		m.diffWrap = v.wrap
		if len(v.dispBlocks) > 0 {
			if ord >= len(v.dispBlocks) {
				ord = len(v.dispBlocks) - 1
			}
			v.jumpTo(v.dispBlocks[ord], m.diffBodyRows())
		} else {
			v.offset = 0
		}
```

- [ ] **Step 6: Re-anchor on resize**

In `internal/tui/model.go`, in the `tea.WindowSizeMsg` case, the diff guard currently closes the view when too narrow. Replace that `if` block (search `m.diffView != nil && msg.Width > 0 && msg.Width < 60`) with a close-or-relayout:

```go
		if m.diffView != nil && msg.Width > 0 {
			if msg.Width < 60 {
				m.diffView = nil
				m.diffTag = ""
				m.statusMsg = "diff closed: terminal too narrow"
			} else {
				v := m.diffView
				topLine := 0
				if v.offset < len(v.disp) {
					topLine = v.disp[v.offset].line
				}
				w, _ := m.overlayDims()
				v.relayout(w)
				if topLine < len(v.lineStart) {
					v.offset = v.lineStart[topLine]
				}
				v.scroll(0, m.diffBodyRows())
			}
		}
```

> Note: `m.width` is already set to `msg.Width` earlier in the `WindowSizeMsg` case (the line `m.width = msg.Width`), so `m.overlayDims()` here reflects the NEW width.

- [ ] **Step 7: Hint + help rows**

In `internal/tui/diff_render.go`, extend `diffHint` to include `[w] wrap` after `[f] toggle partial`:

```go
const diffHint = "[↑↓] scroll  [pgup/pgdn] page  [n/p] next/prev change  [f] toggle partial  [w] wrap  [esc] close  [q] quit"
```

In `internal/tui/help.go`, add a `w` row to the Diff view section, right after the `f` row (`r("f", "toggle full file ↔ changed lines only")`):

```go
		r("w", "wrap long lines on/off"),
```

- [ ] **Step 8: Run tests**

Run: `go test ./internal/tui/` then `go build ./cmd/gg` and `gofmt -l internal/tui/`
Expected: PASS (new activation tests + all existing). Build clean; gofmt silent.

- [ ] **Step 9: Commit**

```bash
git add internal/tui/model.go internal/tui/diff_view.go internal/tui/files_view.go internal/tui/diff_render.go internal/tui/help.go internal/tui/diff_view_test.go
git commit -m "feat(tui): w toggles diff word-wrap; session-remembered, re-wraps on resize

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: Docs

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`

- [ ] **Step 1: CHANGELOG**

In `CHANGELOG.md`, under the current `### Added` section (alongside the other diff-view entries), add:

```markdown
#### Diff view: word-wrap toggle
- `w` in the diff view word-wraps long lines across multiple rows (the two
  panes stay aligned) instead of truncating them with `…`. Default off,
  remembered for the session, and the view re-wraps when the terminal resizes.
```

- [ ] **Step 2: README**

In `README.md`, the diff keymap row (the `enter` row listing the diff keys) — extend the in-diff key list to mention `w`. Find `` `f` toggles full file ↔ changed-lines-only `` and change the tail to:

```
`f` toggles full file ↔ changed-lines-only, `w` wraps long lines,
```

(keep the rest of the sentence — `esc` closes, etc. — intact).

- [ ] **Step 3: Verify build & commit**

Run: `go build ./cmd/gg`
Expected: clean.

```bash
git add CHANGELOG.md README.md
git commit -m "docs: diff view word-wrap toggle (changelog, readme)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `./test.sh race` — all stages green.
- [ ] Manual smoke (optional): `go build ./cmd/gg && ./gg`, open a diff with a long line, press `w` — the line wraps across rows with the panes aligned; `w` again restores truncation; resize the terminal and confirm the viewport keeps its place; `n`/`p` still jump between changes.
- [ ] Dispatch the final holistic code reviewer over the whole branch.

## Notes for the executor

- **No CLI surface changed** ⇒ do NOT bump `agentskill.Version`. **No footer-registry key** ⇒ `footer.go`/`avail.go`/`TestHelpFooterCoverage` are untouched (the diff view draws its own hint line; the new key needs only the `diffHint` string + a `help.go` row). **No new window kind** ⇒ adding-tui-windows skill unchanged.
- Keep `textdiff`/`domain`/`cache` untouched — wrap is a pure TUI layout transform.
- Wrap-off MUST stay byte-identical: the renderer branches on `v.wrap`, and the wrap-off `diffCell(raw)` path is unchanged. If an existing render/nav test breaks, that's a regression — fix the code, not the test.
- Commit after every task; never commit in the shared checkout (work stays in this worktree on `feat/diff-wrap`).
```
