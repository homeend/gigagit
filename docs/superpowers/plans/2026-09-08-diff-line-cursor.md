# Diff-View Line Cursor Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the TUI's full-screen diff view a current-line cursor (`j`/`k`), a configurable marker, `z` viewport alignment, click-to-place, and `e` = open the editor at that line — the phase-0 foundation review notes anchor on.

**Architecture:** The cursor is a logical-line index (`curLine`) into `diffView.lines`, mapped to display rows through the existing `lineStart` table, so wrap and resize need nothing new. Arrows/wheel keep moving only the viewport; the cursor keys scroll minimally. The marker is painted by `diffPaneLines` for an explicit display-row range so the history view's embedded pane (same renderer) stays unmarked. A single line-aware editor argv builder serves both editor paths.

**Tech Stack:** Go 1.26, Bubble Tea + lipgloss, `internal/textdiff`, `internal/config`, `internal/i18n` (four bundles: `internal/i18n/lang/{ja,ko,zh,ru}.toml`).

**Spec:** `docs/superpowers/specs/2026-09-08-hunk-parity-roadmap.md` §4.1 (design), §4 item 1 (motivation), §6 phase 0.

**Worktree:** `/mnt/t/others/gigagit.worktrees/feat-diff-line-cursor` (branch `feat/diff-line-cursor`, off `main`). Every command below runs there. Commit trailers: `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>` and `Claude-Session: https://claude.ai/code/session_01NDk1DJtgLzZX7hMmxDs9nU`.

## Global Constraints

- `curLine` is an index into `v.lines` (logical lines), never a display row; fold entries (`Line.Fold > 0`) are never a cursor position.
- Arrows and the wheel move ONLY the viewport. `j`/`k`, `pgup`/`pgdown`, `home`/`end`, `n`/`p` (and wraps/file steps) move the cursor and scroll minimally.
- `cur` (focused change block) and `curLine` are independent; `scrollBy`'s `deriveOrdinal` resync is unchanged.
- `diffPaneLines` keeps working for the history view: the cursor display-row range is a parameter; history passes an empty range and renders byte-identical to today.
- `textdiff.Row` gains no fields; cached `domain.Diff` rows stay read-only.
- Every user-visible TUI string goes through `i18n.T` with a literal key present in ALL FOUR bundles (`ja`, `ko`, `zh`, `ru`); the gate tests in `internal/tui` (`i18n_scan_test.go`, `options_vocab_test.go`, `menu_labels_test.go`, `engine_prose_test.go`) must stay green after every task.
- `[ui] diff_cursor` values are exactly `"row"`, `"number"`, `"off"`; default `"row"`; empty = unset in the overlay; a `settingDoc` row + a layers test like `TestUIDiffSyntaxLayers`.
- `internal/tui` never imports `internal/git` (archtest).
- Tests use `t.Parallel()` unless they touch global state; new tests follow the `openedDiffModel` / `diffViewWith` fixtures in `diff_view_test.go`.
- Web: no changes in this phase.
- Run `gofmt -l internal/ && go vet ./internal/tui/ ./internal/config/` before each commit; `./test.sh unit` before the final task's commit.

---

### Task 1: Cursor core on `diffView` (pure, no keys yet)

**Files:**
- Create: `internal/tui/diff_cursor.go`
- Create: `internal/tui/diff_cursor_test.go`
- Modify: `internal/tui/diff_view.go` (struct field, `focusBlock`, `applyDiff`)

**Interfaces:**
- Produces:
  - field `diffView.curLine int`
  - `func (v *diffView) cursorRow() (textdiff.Row, bool)` — the row under the cursor; `false` when the view has no lines or the cursor sits on nothing.
  - `func (v *diffView) cursorDispRange() (start, end int)` — the cursor line's display rows `[start, end)`; `(0, 0)` when there is no cursor.
  - `func (v *diffView) moveCursor(delta, body int)` — step `delta` real lines (folds skipped), clamp, then `ensureCursorVisible`.
  - `func (v *diffView) setCursorLine(li, body int)` — put the cursor on logical line `li` (snapping forward, then backward, off a fold), then `ensureCursorVisible`.
  - `func (v *diffView) setCursorDisp(row int, body int)` — cursor to the line owning display row `row` (a fold row is ignored: no move).
  - `func (v *diffView) ensureCursorVisible(body int)` — minimal scroll so the cursor's FIRST display row is inside `[offset, offset+body)`.
  - `func (v *diffView) alignCursor(mode cursorAlign, body int)` — scroll so the cursor's first display row is at the top / center / bottom of the body.
  - `type cursorAlign int` with `alignCenter`, `alignTop`, `alignBottom` (in that order: the `z` cycle order).
  - `func (v *diffView) reanchorCursor(leftNo, rightNo int)` — after a rebuild, move the cursor to the first line whose row has the same `(LeftNo, RightNo)`; else keep the index clamped.
  - `focusBlock` now also sets `curLine` to the block's first logical line; `applyDiff` seeds the cursor via `focusBlock` or leaves it at line 0.

- [ ] **Step 1: Write the failing tests**

`internal/tui/diff_cursor_test.go`:

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/textdiff"
)

// cursorRows builds n Same rows numbered 1..n with the listed rows Changed —
// the same shape as sameRowsTUI, local so this file reads on its own.
func cursorRows(n int, changed ...int) []textdiff.Row {
	rows := make([]textdiff.Row, n)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: "l", Right: "r", LeftNo: i + 1, RightNo: i + 1}
	}
	for _, c := range changed {
		rows[c] = textdiff.Row{Kind: textdiff.Changed, Left: "x", Right: "y", LeftNo: c + 1, RightNo: c + 1}
	}
	return rows
}

func TestCursorMoveClampsAndScrolls(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40), nil)
	body := 10
	if v.curLine != 0 {
		t.Fatalf("fresh view curLine = %d, want 0", v.curLine)
	}
	v.moveCursor(-1, body)
	if v.curLine != 0 || v.offset != 0 {
		t.Fatalf("k at top: curLine=%d offset=%d, want 0/0", v.curLine, v.offset)
	}
	for i := 0; i < 12; i++ {
		v.moveCursor(1, body)
	}
	// Cursor on line 12; the viewport must have scrolled minimally: last body row.
	if v.curLine != 12 || v.offset != 3 {
		t.Fatalf("12×j: curLine=%d offset=%d, want 12/3", v.curLine, v.offset)
	}
	v.moveCursor(100, body)
	if v.curLine != 39 || v.offset != 30 {
		t.Fatalf("j past end: curLine=%d offset=%d, want 39/30", v.curLine, v.offset)
	}
	r, ok := v.cursorRow()
	if !ok || r.RightNo != 40 {
		t.Fatalf("cursorRow = %+v %v, want RightNo 40", r, ok)
	}
}

func TestCursorSkipsFoldsInPartialMode(t *testing.T) {
	t.Parallel()
	// 40 rows, changes at 10 and 30: partial mode keeps diffContext rows
	// around each and folds the rest, so a fold sits BETWEEN two real lines
	// (the very first line is a fold too; the test needs a middle one).
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.partial = true
	v.rebuild()
	body := 10
	foldAt := -1
	for i, ln := range v.lines {
		if ln.Fold > 0 && i > 0 {
			foldAt = i
			break
		}
	}
	if foldAt < 0 || foldAt+1 >= len(v.lines) {
		t.Fatal("fixture: expected a middle fold line in partial mode")
	}
	v.setCursorLine(foldAt, body)
	if v.lines[v.curLine].Fold > 0 {
		t.Fatalf("setCursorLine on a fold must snap to a real row, got line %d", v.curLine)
	}
	// Step over the fold both ways: the cursor never rests on it.
	v.setCursorLine(foldAt-1, body)
	v.moveCursor(1, body)
	if v.curLine != foldAt+1 {
		t.Fatalf("j over a fold: curLine=%d, want %d", v.curLine, foldAt+1)
	}
	v.moveCursor(-1, body)
	if v.curLine != foldAt-1 {
		t.Fatalf("k over a fold: curLine=%d, want %d", v.curLine, foldAt-1)
	}
}

func TestCursorAlign(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(60), nil)
	body := 10
	v.setCursorLine(30, body)
	v.alignCursor(alignTop, body)
	if v.offset != 30 {
		t.Fatalf("top: offset=%d, want 30", v.offset)
	}
	v.alignCursor(alignCenter, body)
	if v.offset != 25 {
		t.Fatalf("center: offset=%d, want 25", v.offset)
	}
	v.alignCursor(alignBottom, body)
	if v.offset != 21 {
		t.Fatalf("bottom: offset=%d, want 21", v.offset)
	}
	// Clamped at the ends: line 2 can't be centered below offset 0, the last
	// line can't be top-anchored past len-body.
	v.setCursorLine(2, body)
	v.alignCursor(alignCenter, body)
	if v.offset != 0 {
		t.Fatalf("center near top: offset=%d, want 0", v.offset)
	}
	v.setCursorLine(59, body)
	v.alignCursor(alignTop, body)
	if v.offset != 50 {
		t.Fatalf("top near end: offset=%d, want 50", v.offset)
	}
}

func TestCursorDispRangeSpansWrappedLine(t *testing.T) {
	t.Parallel()
	rows := cursorRows(5)
	long := ""
	for i := 0; i < 30; i++ {
		long += "word "
	}
	rows[2].Left, rows[2].Right = long, long // wraps to several display rows in a 40-col view
	v := diffViewWith(rows, nil)
	v.long = longWrap
	v.relayout(40)
	v.setCursorLine(2, 20)
	s, e := v.cursorDispRange()
	if s != v.lineStart[2] || e-s < 2 {
		t.Fatalf("range = [%d,%d) lineStart=%d, want the wrapped line's full span", s, e, v.lineStart[2])
	}
	if v.lineStart[3] != e {
		t.Fatalf("range end %d must equal the next line's first display row %d", e, v.lineStart[3])
	}
}

func TestCursorReanchorsAcrossPartialToggle(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 20, 30), []int{20, 30})
	body := 10
	v.setCursorLine(30, body) // the second change, row RightNo 31
	before, _ := v.cursorRow()
	v.partial = true
	v.rebuild()
	v.reanchorCursor(before.LeftNo, before.RightNo)
	after, ok := v.cursorRow()
	if !ok || after.LeftNo != before.LeftNo || after.RightNo != before.RightNo {
		t.Fatalf("after partial toggle cursorRow = %+v, want %+v", after, before)
	}
	v.partial = false
	v.rebuild()
	v.reanchorCursor(before.LeftNo, before.RightNo)
	if v.curLine != 30 {
		t.Fatalf("back to full: curLine=%d, want 30", v.curLine)
	}
}

func TestCursorSetDispIgnoresFoldRow(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 10, 30), []int{10, 30})
	v.partial = true
	v.rebuild()
	foldRow := -1
	for i, d := range v.disp {
		if d.fold > 0 && i > 0 { // a middle fold (display row 0 is a fold as well)
			foldRow = i
			break
		}
	}
	if foldRow < 0 || foldRow+1 >= len(v.disp) {
		t.Fatal("fixture: expected a middle fold display row")
	}
	v.setCursorLine(1, 10)
	was := v.curLine
	v.setCursorDisp(foldRow, 10)
	if v.curLine != was {
		t.Fatalf("click on a fold row moved the cursor to %d, want %d", v.curLine, was)
	}
	v.setCursorDisp(foldRow+1, 10)
	if v.curLine != v.disp[foldRow+1].line {
		t.Fatalf("click on row %d: curLine=%d, want %d", foldRow+1, v.curLine, v.disp[foldRow+1].line)
	}
}

func TestFocusBlockSeedsCursor(t *testing.T) {
	t.Parallel()
	v := diffViewWith(cursorRows(40, 20, 30), []int{20, 30})
	v.focusBlock(1, 10)
	if v.curLine != 30 {
		t.Fatalf("focusBlock(1): curLine=%d, want 30", v.curLine)
	}
	v.focusBlock(0, 10)
	if v.curLine != 20 {
		t.Fatalf("focusBlock(0): curLine=%d, want 20", v.curLine)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestCursor|TestFocusBlockSeedsCursor' 2>&1 | head -20`
Expected: compile errors — `v.curLine undefined`, `v.moveCursor undefined`, …

- [ ] **Step 3: Add the field and the cursor helpers**

In `internal/tui/diff_view.go`, add to the `diffView` struct right after `cur int …`:

```go
	curLine int // cursor: index into lines (never a display row; never a fold) — see diff_cursor.go
```

Create `internal/tui/diff_cursor.go`:

```go
package tui

import "github.com/homeend/gigagit/internal/textdiff"

// The diff view's line cursor. curLine indexes v.lines (the logical stream:
// one entry per aligned row, plus fold separators in partial mode), never
// v.disp — a wrapped line owns several display rows, and lineStart maps the
// cursor to its first one, so wrap toggles and resizes need no bookkeeping.
// The cursor never rests on a fold; every mover snaps off one. The cursor
// and the focused change block (cur) are independent: n/p seed the cursor
// through focusBlock, but a free scroll (arrows, wheel) moves neither.
//
// cursorRow is the contract later phases build on: review notes anchor on
// the row under the cursor (RightNo on the new side, LeftNo on a Del row).

// cursorAlign is a z-cycle position for the cursor line in the viewport.
// Declaration order is the cycle order (Emacs recenter: center, top, bottom).
type cursorAlign int

const (
	alignCenter cursorAlign = iota
	alignTop
	alignBottom
)

// cursorRow returns the aligned row under the cursor, or false when the view
// has no lines or the cursor index is out of range.
func (v *diffView) cursorRow() (textdiff.Row, bool) {
	if v.curLine < 0 || v.curLine >= len(v.lines) || v.lines[v.curLine].Fold > 0 {
		return textdiff.Row{}, false
	}
	return v.lines[v.curLine].Row, true
}

// cursorDispRange is the half-open display-row range the cursor line owns
// (one row, or several when wrapped). (0,0) when there is no cursor.
func (v *diffView) cursorDispRange() (start, end int) {
	if v.curLine < 0 || v.curLine >= len(v.lineStart) {
		return 0, 0
	}
	start = v.lineStart[v.curLine]
	end = len(v.disp)
	if v.curLine+1 < len(v.lineStart) {
		end = v.lineStart[v.curLine+1]
	}
	return start, end
}

// snapOffFold moves li forward, then backward, to the nearest non-fold line.
// Returns -1 when the stream has no real line at all.
func (v *diffView) snapOffFold(li int) int {
	n := len(v.lines)
	if n == 0 {
		return -1
	}
	if li < 0 {
		li = 0
	}
	if li > n-1 {
		li = n - 1
	}
	for j := li; j < n; j++ {
		if v.lines[j].Fold == 0 {
			return j
		}
	}
	for j := li - 1; j >= 0; j-- {
		if v.lines[j].Fold == 0 {
			return j
		}
	}
	return -1
}

// setCursorLine puts the cursor on logical line li (snapped off a fold) and
// scrolls minimally so it is visible.
func (v *diffView) setCursorLine(li, body int) {
	if j := v.snapOffFold(li); j >= 0 {
		v.curLine = j
	} else {
		v.curLine = 0
	}
	v.ensureCursorVisible(body)
}

// moveCursor steps delta real lines (a fold counts as no line) and clamps at
// the ends, then scrolls minimally.
func (v *diffView) moveCursor(delta, body int) {
	li := v.snapOffFold(v.curLine)
	if li < 0 {
		return
	}
	step := 1
	if delta < 0 {
		step, delta = -1, -delta
	}
	for ; delta > 0; delta-- {
		j := li + step
		for j >= 0 && j < len(v.lines) && v.lines[j].Fold > 0 {
			j += step
		}
		if j < 0 || j >= len(v.lines) {
			break
		}
		li = j
	}
	v.curLine = li
	v.ensureCursorVisible(body)
}

// setCursorDisp moves the cursor to the line that owns display row `row`
// (a click). A fold row or an out-of-range row leaves the cursor alone.
func (v *diffView) setCursorDisp(row, body int) {
	if row < 0 || row >= len(v.disp) || v.disp[row].fold > 0 {
		return
	}
	v.curLine = v.disp[row].line
	v.ensureCursorVisible(body)
}

// ensureCursorVisible scrolls the least amount that brings the cursor line's
// first display row inside [offset, offset+body).
func (v *diffView) ensureCursorVisible(body int) {
	start, _ := v.cursorDispRange()
	if len(v.disp) == 0 {
		return
	}
	switch {
	case start < v.offset:
		v.offset = start
	case start >= v.offset+body:
		v.offset = start - body + 1
	}
	v.scroll(0, body) // clamp
}

// alignCursor places the cursor line's first display row at the top, the
// centre, or the bottom of the body (clamped into the scroll range).
func (v *diffView) alignCursor(mode cursorAlign, body int) {
	start, _ := v.cursorDispRange()
	switch mode {
	case alignTop:
		v.offset = start
	case alignBottom:
		v.offset = start - body + 1
	default:
		v.offset = start - body/2
	}
	v.scroll(0, body) // clamp
}

// reanchorCursor re-finds the cursor after a rebuild (f / ctrl+w) by the
// row's source numbers: the first line with the same (LeftNo, RightNo). A row
// hidden by a fold, or gone, leaves the index clamped and snapped off a fold.
func (v *diffView) reanchorCursor(leftNo, rightNo int) {
	for i, ln := range v.lines {
		if ln.Fold == 0 && ln.Row.LeftNo == leftNo && ln.Row.RightNo == rightNo {
			v.curLine = i
			return
		}
	}
	if j := v.snapOffFold(v.curLine); j >= 0 {
		v.curLine = j
	} else {
		v.curLine = 0
	}
}
```

- [ ] **Step 4: Seed the cursor from `focusBlock` and `applyDiff`**

In `internal/tui/diff_view.go`, `focusBlock` — after `v.cur = i` and before `v.jumpTo(...)`:

```go
	if i < len(v.blocks) {
		v.curLine = v.blocks[i] // blocks index v.lines; the block's first row
	}
```

(`v.blocks` and `v.dispBlocks` are the same length: `relayout` derives one from the other.)

In `applyDiff`, after `v.rebuild()`, replace the `if len(v.dispBlocks) > 0 { v.focusBlock(0, body) }` block with:

```go
		if len(v.dispBlocks) > 0 {
			v.focusBlock(0, body) // open on the first change (cur = 0, cursor on its first row)
		} else {
			v.setCursorLine(0, body)
		}
```

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -run 'TestCursor|TestFocusBlockSeedsCursor|TestDiffView' -count=1`
Expected: PASS (existing `TestDiffView*` still green: `focusBlock` only gained a cursor write).

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/tui/ && go vet ./internal/tui/
git add internal/tui/diff_cursor.go internal/tui/diff_cursor_test.go internal/tui/diff_view.go
git commit -m "feat(tui): diff view line cursor core — curLine, movers, align, re-anchor"
```

---

### Task 2: Cursor keys, `z` align cycle, click-to-place, header `line N`

**Files:**
- Modify: `internal/tui/diff_view.go:610-770` (`updateDiffViewKey`)
- Modify: `internal/tui/diff_render.go:165-230` (`renderDiffView` header)
- Modify: `internal/tui/mouse.go:112-116` (left click on the diff layer)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (two keys)
- Test: `internal/tui/diff_cursor_test.go` (append), `internal/tui/diff_view_test.go` (one assertion)

**Interfaces:**
- Consumes (Task 1): `moveCursor`, `setCursorLine`, `setCursorDisp`, `alignCursor`, `reanchorCursor`, `cursorRow`, `cursorAlign` constants.
- Produces: field `diffView.zCycle cursorAlign` (the NEXT alignment `z` applies; reset to `alignCenter` by any other key, like `wrapArm`); header text `line %d` / `old line %d`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/tui/diff_cursor_test.go`:

```go
func TestDiffKeysJKMoveCursorArrowsScroll(t *testing.T) {
	t.Parallel()
	// 40 rows, changes at 20 and 30, body 10 (height 12). Opens with the
	// cursor on line 20 and offset 17.
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	v := m.diffLayer()
	if v.curLine != 20 || v.offset != 17 {
		t.Fatalf("open: curLine=%d offset=%d, want 20/17", v.curLine, v.offset)
	}
	u, _ := m.Update(keyMsg("j"))
	v = u.(Model).diffLayer()
	if v.curLine != 21 || v.offset != 17 {
		t.Fatalf("j: curLine=%d offset=%d, want 21/17 (no scroll needed)", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("k"))
	u, _ = u.(Model).Update(keyMsg("k"))
	v = u.(Model).diffLayer()
	if v.curLine != 19 {
		t.Fatalf("k k: curLine=%d, want 19", v.curLine)
	}
	// Arrows move the viewport only: the cursor stays on 19 even off-screen.
	for i := 0; i < 20; i++ {
		u, _ = u.(Model).Update(keyMsg("down"))
	}
	v = u.(Model).diffLayer()
	if v.curLine != 19 || v.offset != 30 {
		t.Fatalf("20×down: curLine=%d offset=%d, want 19/30", v.curLine, v.offset)
	}
	// The next j pulls the cursor back into view minimally (offset = 20).
	u, _ = u.(Model).Update(keyMsg("j"))
	v = u.(Model).diffLayer()
	if v.curLine != 20 || v.offset != 20 {
		t.Fatalf("j from off-screen: curLine=%d offset=%d, want 20/20", v.curLine, v.offset)
	}
}

func TestDiffKeysPageHomeEndMoveCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	u, _ := m.Update(keyMsg("pgdown"))
	v := u.(Model).diffLayer()
	if v.curLine != 30 || v.offset != 27 {
		t.Fatalf("pgdown: curLine=%d offset=%d, want 30/27", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("home"))
	v = u.(Model).diffLayer()
	if v.curLine != 0 || v.offset != 0 {
		t.Fatalf("home: curLine=%d offset=%d, want 0/0", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("end"))
	v = u.(Model).diffLayer()
	if v.curLine != 39 || v.offset != 30 {
		t.Fatalf("end: curLine=%d offset=%d, want 39/30", v.curLine, v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("pgup"))
	v = u.(Model).diffLayer()
	if v.curLine != 29 || v.offset != 20 {
		t.Fatalf("pgup: curLine=%d offset=%d, want 29/20", v.curLine, v.offset)
	}
}

func TestDiffKeyNPSeedCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	u, _ := m.Update(keyMsg("n"))
	if v := u.(Model).diffLayer(); v.curLine != 30 {
		t.Fatalf("n: curLine=%d, want 30", v.curLine)
	}
	u, _ = u.(Model).Update(keyMsg("p"))
	if v := u.(Model).diffLayer(); v.curLine != 20 {
		t.Fatalf("p: curLine=%d, want 20", v.curLine)
	}
}

func TestDiffKeyZCyclesAlignment(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(60), nil)
	m.diffLayer().setCursorLine(30, m.diffBodyRows())
	u, _ := m.Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 25 {
		t.Fatalf("z (center): offset=%d, want 25", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 30 {
		t.Fatalf("z z (top): offset=%d, want 30", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 21 {
		t.Fatalf("z z z (bottom): offset=%d, want 21", v.offset)
	}
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 25 {
		t.Fatalf("fourth z wraps to center: offset=%d, want 25", v.offset)
	}
	// Any other key resets the cycle: j then z centers again.
	u, _ = u.(Model).Update(keyMsg("z")) // top
	u, _ = u.(Model).Update(keyMsg("j"))
	u, _ = u.(Model).Update(keyMsg("z"))
	if v := u.(Model).diffLayer(); v.offset != 26 {
		t.Fatalf("j resets the cycle, z centers line 31: offset=%d, want 26", v.offset)
	}
}

func TestDiffToggleKeepsCursorRow(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	m.diffLayer().setCursorLine(31, m.diffBodyRows())
	u, _ := m.Update(keyMsg("f")) // partial
	r, ok := u.(Model).diffLayer().cursorRow()
	if !ok || r.RightNo != 32 {
		t.Fatalf("after f: cursorRow=%+v ok=%v, want RightNo 32", r, ok)
	}
	u, _ = u.(Model).Update(keyMsg("ctrl+w")) // wrap
	r, ok = u.(Model).diffLayer().cursorRow()
	if !ok || r.RightNo != 32 {
		t.Fatalf("after ctrl+w: cursorRow=%+v ok=%v, want RightNo 32", r, ok)
	}
}

func TestDiffHeaderNamesCursorLine(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	m.width = 100
	head := strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "line 21") {
		t.Fatalf("header %q must name the cursor line (line 21)", head)
	}
	rows := cursorRows(5)
	rows[2] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 3}
	for i := 3; i < 5; i++ {
		rows[i].RightNo--
	}
	m = openedDiffModel(12, rows, []int{2})
	m.width = 100
	head = strings.SplitN(m.renderDiffView(), "\n", 2)[0]
	if !strings.Contains(head, "old line 3") {
		t.Fatalf("header %q must say old line 3 on a Del row", head)
	}
}

func TestDiffLeftClickPlacesCursor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20, 30), []int{20, 30})
	// Body row 5 (y = 6: header is y 0, body starts at y 1) at offset 17 → display row 22.
	u, _ := m.Update(mouseMsg(10, 6, tea.MouseButtonLeft))
	if v := u.(Model).diffLayer(); v.curLine != 22 {
		t.Fatalf("click y=6: curLine=%d, want 22", v.curLine)
	}
	// A click on the header row (y 0) is inert.
	u, _ = u.(Model).Update(mouseMsg(10, 0, tea.MouseButtonLeft))
	if v := u.(Model).diffLayer(); v.curLine != 22 {
		t.Fatalf("click on the header moved the cursor to %d", v.curLine)
	}
}
```

Add the imports `"strings"` and `tea "github.com/charmbracelet/bubbletea"` to the test file.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestDiffKeys|TestDiffKey|TestDiffToggleKeepsCursorRow|TestDiffHeaderNamesCursorLine|TestDiffLeftClickPlacesCursor' -count=1 2>&1 | head -30`
Expected: FAIL — `j` still scrolls (curLine stays 20 / offset 18), no `line 21` in the header, click inert.

- [ ] **Step 3: Rewire the keys**

In `internal/tui/diff_view.go`:

Add to the `diffView` struct after `fileArm`:

```go
	zCycle  cursorAlign // the alignment the NEXT z applies (center → top → bottom); reset by any other key
```

In `updateDiffViewKey`, right after `m.diffNotice = ""` add:

```go
	zc := v.zCycle
	v.zCycle = alignCenter
	body := m.diffBodyRows()
```

Then change these cases (replace the existing bodies):

```go
	case "up":
		v.scrollBy(-1, body)
	case "down":
		v.scrollBy(1, body)
	case "k":
		v.moveCursor(-1, body)
	case "j":
		v.moveCursor(1, body)
	case "pgup":
		v.scrollBy(-body, body)
		v.moveCursor(-body, body)
	case "pgdown":
		v.scrollBy(body, body)
		v.moveCursor(body, body)
	case "z":
		v.alignCursor(zc, body)
		v.zCycle = (zc + 1) % 3
```

`home`: in the `if v.offset > 0` branch, after `v.scrollBy(-len(v.disp), body)` add `v.setCursorLine(0, body)`; ALSO add the same `v.setCursorLine(0, body)` as a new first statement of the case so `home` at the top still parks the cursor on line 0 (keep the arm logic untouched).

`end`: symmetric — first statement `v.setCursorLine(len(v.lines)-1, body)`, and the existing scroll stays.

`f` and `ctrl+w`: capture the row before the rebuild and re-anchor after:

```go
	case "f":
		ord := v.currentBlockOrdinal()
		cr, hadRow := v.cursorRow()
		v.partial = !v.partial
		v.rebuild()
		m.diffPartial = v.partial
		if len(v.dispBlocks) > 0 {
			v.focusBlock(ord, body)
		} else {
			v.cur, v.offset = 0, 0
		}
		if hadRow {
			v.reanchorCursor(cr.LeftNo, cr.RightNo)
			v.ensureCursorVisible(body)
		}
```

and the same `cr, hadRow := v.cursorRow()` … `reanchorCursor` + `ensureCursorVisible` pair around the `ctrl+w` body (after `v.relayout(v.width)` and the focusBlock/else).

Replace every remaining `m.diffBodyRows()` inside the function with `body`.

`n`/`p`/`N`/`P` need no change: `focusBlock` seeds the cursor (Task 1).

- [ ] **Step 4: Header `line N`**

In `internal/tui/diff_render.go` `renderDiffView`, right after the `if len(v.blocks) > 0 { right = i18n.T("change %d/%d", …) }` block, add:

```go
	if r, ok := v.cursorRow(); ok {
		ln := ""
		if r.RightNo > 0 {
			ln = i18n.T("line %d", r.RightNo)
		} else if r.LeftNo > 0 {
			ln = i18n.T("old line %d", r.LeftNo)
		}
		if ln != "" {
			if right != "" {
				right = ln + "  " + right
			} else {
				right = ln
			}
		}
	}
```

- [ ] **Step 5: Click places the cursor**

In `internal/tui/mouse.go`, inside the `if l := m.topLayer(); l != nil {` block, extend the diff-view line:

```go
		if dv, ok := l.(*diffView); ok {
			if wheel != 0 {
				dv.scrollBy(wheel, m.diffBodyRows())
			}
			// Left click on a body row places the cursor there (y 0 is the
			// header; the body starts at y 1). Fold rows are ignored.
			if msg.Button == tea.MouseButtonLeft && msg.Y >= 1 && msg.Y <= m.diffBodyRows() {
				dv.setCursorDisp(dv.offset+msg.Y-1, m.diffBodyRows())
			}
		}
```

(This replaces the existing two-line `if dv, ok := l.(*diffView); ok && wheel != 0 { … }`.)

- [ ] **Step 6: Add the i18n keys to all four bundles**

Append to each of `internal/i18n/lang/ja.toml`, `ko.toml`, `zh.toml`, `ru.toml` (keep the file's existing `"key" = "value"` form; place near the `"change %d/%d"` key):

```toml
"line %d" = "行 %d"
"old line %d" = "旧 %d 行"
```
(ja). ko: `"line %d" = "줄 %d"`, `"old line %d" = "이전 줄 %d"`. zh: `"line %d" = "第 %d 行"`, `"old line %d" = "旧第 %d 行"`. ru: `"line %d" = "строка %d"`, `"old line %d" = "старая строка %d"`.

- [ ] **Step 7: Fix the one existing assertion that pressed j/k as scroll**

Run `grep -n 'keyMsg("j")\|keyMsg("k")' internal/tui/*_test.go` — for any diff-view test that expects `j`/`k` to change `offset`, switch the key to `"down"`/`"up"` (the arrows keep the old semantics). Leave non-diff tests alone.

- [ ] **Step 8: Run the tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS, including the i18n gate tests.

- [ ] **Step 9: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/diff_view.go internal/tui/diff_render.go internal/tui/mouse.go internal/tui/diff_cursor_test.go internal/i18n/lang/
git commit -m "feat(tui): diff view — j/k move the line cursor, z cycles align, click places it, header names the line"
```

---

### Task 3: Cursor marker — `[ui] diff_cursor` config + rendering + session toggle

**Files:**
- Modify: `internal/config/config.go` (field, default, overlay, accessor), `internal/config/template.go` (settingDoc), `internal/config/config_test.go`
- Modify: `internal/tui/model.go` (session override field), `internal/tui/diff_render.go` (`diffPaneLines` signature + styles), `internal/tui/history_view.go:256` (caller)
- Modify: `internal/tui/action_menu.go` (menu row), `internal/i18n/lang/*.toml`
- Test: `internal/tui/diff_cursor_test.go` (append), `internal/config/config_test.go`

**Interfaces:**
- Consumes (Task 1): `cursorDispRange`.
- Produces:
  - `config.UIConfig.DiffCursor string` (`toml:"diff_cursor"`), default `"row"`; `func (c UIConfig) CursorStyle() string` → `"row"|"number"|"off"` (unknown → `"row"`).
  - `Model.diffCursor string` — session override (`""` = follow config); `func (m Model) cursorStyle() string`.
  - `func (m Model) diffPaneLines(v *diffView, w, body int, curStart, curEnd int, style string) []string` — marks display rows in `[curStart, curEnd)` per `style`; `curStart == curEnd` = no marker.
  - styles `diffCursorRow` (background `237`), `diffCursorNo` (bold, foreground `231`).
  - `.`-menu row id `diff-cursor-style`, label `Cursor marker: %s` cycling row → number → off → row.

- [ ] **Step 1: Config — failing test**

Append to `internal/config/config_test.go`:

```go
func TestUIDiffCursorLayers(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.toml")

	cfg, err := Load(missing, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.DiffCursor != "row" || cfg.UI.CursorStyle() != "row" {
		t.Errorf("default diff_cursor = %q (style %q), want row", cfg.UI.DiffCursor, cfg.UI.CursorStyle())
	}

	g := filepath.Join(dir, "global.toml")
	writeFile(t, g, "[ui]\ndiff_cursor = \"off\"\n")
	cfg, err = Load(g, missing)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CursorStyle() != "off" {
		t.Errorf("global off: style = %q", cfg.UI.CursorStyle())
	}

	r := filepath.Join(dir, "repo.toml")
	writeFile(t, r, "[ui]\ndiff_cursor = \"number\"\n")
	cfg, err = Load(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.CursorStyle() != "number" {
		t.Errorf("repo number must win over global off, got %q", cfg.UI.CursorStyle())
	}
	if (UIConfig{DiffCursor: "bogus"}).CursorStyle() != "row" {
		t.Error("unknown value must fall back to row")
	}
}
```

Run: `go test ./internal/config/ -run TestUIDiffCursorLayers` → FAIL (`DiffCursor undefined`).

- [ ] **Step 2: Config — implement**

`internal/config/config.go`, after the `DiffSyntax` field:

```go
	// DiffCursor selects how the diff view marks its current line:
	//   "row"    — a background under the cursor row on both panes. THE DEFAULT.
	//   "number" — only the gutter line numbers are highlighted.
	//   "off"    — no marker (the cursor still drives e / notes).
	// Empty = unset (zero-is-unset overlay rule); resolved to the default.
	DiffCursor string `toml:"diff_cursor"`
```

After `SyntaxOn`:

```go
// CursorStyle returns the diff-view cursor marker: "row", "number" or "off".
// Anything else (including unset) is "row".
func (c UIConfig) CursorStyle() string {
	switch c.DiffCursor {
	case "number", "off":
		return c.DiffCursor
	}
	return "row"
}
```

Defaults literal (line ~164): add `DiffCursor: "row"` right after `DiffSyntax: "auto"`. Overlay (line ~292): add

```go
	if src.DiffCursor != "" {
		dst.DiffCursor = src.DiffCursor
	}
```

`internal/config/template.go`, after the `diff_syntax` settingDoc row:

```go
	{"ui", "diff_cursor", "row", "diff-view current-line marker: row (background), number (gutter only) or off; the . menu's Cursor marker row switches it for the session"},
```

Run: `go test ./internal/config/ -count=1` → PASS (the template/registry tests check every field has a doc; if one names the field set, add `DiffCursor` where `DiffSyntax` is listed).

- [ ] **Step 3: Rendering — failing tests**

Append to `internal/tui/diff_cursor_test.go`:

```go
func TestCursorMarkerRowPaintsOnlyCursorRows(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m.width = 80
	v := m.diffLayer()
	v.setCursorLine(5, m.diffBodyRows())
	s, e := v.cursorDispRange()
	marked := m.diffPaneLines(v, 80, 10, s, e, "row")
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "row")
	if len(marked) != len(plain) {
		t.Fatal("row counts differ")
	}
	for i := range marked {
		isCur := i+v.offset >= s && i+v.offset < e
		if isCur && marked[i] == plain[i] {
			t.Fatalf("row %d is the cursor row but renders unchanged", i)
		}
		if !isCur && marked[i] != plain[i] {
			t.Fatalf("row %d is not the cursor row but renders differently", i)
		}
	}
	// "off" is byte-identical to no marker; "number" changes the row but not the way "row" does.
	if off := m.diffPaneLines(v, 80, 10, s, e, "off"); !reflect.DeepEqual(off, plain) {
		t.Fatal("off must render exactly like no marker")
	}
	num := m.diffPaneLines(v, 80, 10, s, e, "number")
	if num[s-v.offset] == plain[s-v.offset] || num[s-v.offset] == marked[s-v.offset] {
		t.Fatal("number style must differ from both plain and row")
	}
}

func TestCursorMarkerSkipsFoldRow(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40, 20), []int{20})
	m.width = 80
	v := m.diffLayer()
	v.partial = true
	v.rebuild()
	foldRow := -1
	for i, d := range v.disp {
		if d.fold > 0 {
			foldRow = i
			break
		}
	}
	v.offset = 0
	// Force the "cursor" range onto the fold row: the renderer must not paint it.
	got := m.diffPaneLines(v, 80, 10, foldRow, foldRow+1, "row")
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "row")
	if got[foldRow] != plain[foldRow] {
		t.Fatal("a fold separator must never carry the cursor marker")
	}
}

func TestCursorStyleSessionOverrideAndCycle(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	if m.cursorStyle() != "row" {
		t.Fatalf("default style = %q, want row", m.cursorStyle())
	}
	m.cfg.UI.DiffCursor = "number"
	if m.cursorStyle() != "number" {
		t.Fatalf("config number: style = %q", m.cursorStyle())
	}
	m.diffCursor = "off"
	if m.cursorStyle() != "off" {
		t.Fatalf("session off must win: %q", m.cursorStyle())
	}
	if nextCursorStyle("row") != "number" || nextCursorStyle("number") != "off" || nextCursorStyle("off") != "row" {
		t.Fatal("cycle must be row → number → off → row")
	}
}

func TestHistoryPaneHasNoCursorMarker(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	m.width = 80
	v := m.diffLayer()
	v.setCursorLine(5, m.diffBodyRows())
	if got := m.diffPaneLines(v, 80, 10, 0, 0, m.cursorStyle()); len(got) != 10 {
		t.Fatalf("rows = %d", len(got))
	}
	// The full-screen render marks the cursor; strip the marker style and it must
	// equal the unmarked render (proves the marker is the only difference).
	full := strings.Split(m.renderDiffView(), "\n")[1:11]
	plain := m.diffPaneLines(v, 80, 10, 0, 0, "off")
	diffRows := 0
	for i := range plain {
		if full[i] != plain[i] {
			diffRows++
		}
	}
	if diffRows != 1 {
		t.Fatalf("full-screen render differs from the unmarked pane on %d rows, want exactly 1 (the cursor)", diffRows)
	}
}
```

Add `"reflect"` to the imports. Run: `go test ./internal/tui/ -run 'TestCursorMarker|TestCursorStyle|TestHistoryPaneHasNoCursorMarker'` → FAIL (signature / undefined).

- [ ] **Step 4: Rendering — implement**

`internal/tui/diff_render.go`:

Add styles to the `var (` block:

```go
	diffCursorRow = lipgloss.NewStyle().Background(lipgloss.Color("237")) // cursor line: subtle grey under both panes
	diffCursorNo  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("231"))
```

Change the signature and body of `diffPaneLines`. The marker must be laid
under the text AT THE SOURCE (the cell renderers' base style): wrapping a
finished line in a background style does not work, because every hot/emph
run inside the line ends with an ANSI reset that would cancel the
background for the plain text after it.

```go
// cellMark is the cursor marker for one rendered cell: when row is set, base
// is laid under every non-hot run and the padding (hot add/del backgrounds
// win — they stay as they are); gut styles the gutter number. noMark is the
// unmarked default.
type cellMark struct {
	row  bool
	base lipgloss.Style
	gut  lipgloss.Style
}

var noMark = cellMark{gut: diffGutter}

// cursorMark is the cellMark for a cursor row under the given style.
func cursorMark(style string) cellMark {
	switch style {
	case "row":
		return cellMark{row: true, base: diffCursorRow, gut: diffGutter}
	case "number":
		return cellMark{gut: diffCursorNo}
	}
	return noMark
}
```

```go
// diffPaneLines renders the visible window of display rows. A fold dRow is a
// full-width separator. Otherwise: wrap off draws the row via diffCell (raw
// text, truncated — byte-identical to before); wrap on draws each side's
// pre-wrapped segment via segCell. Display rows in [curStart, curEnd) carry
// the cursor marker per style ("row" | "number" | "off"); curStart == curEnd
// (the history pane) draws no marker. A fold row is never marked.
func (m Model) diffPaneLines(v *diffView, w, body int, curStart, curEnd int, style string) []string {
```

Inside the loop, right after `dr := v.disp[i]` and the fold `continue`, compute

```go
		mk := noMark
		if i >= curStart && i < curEnd {
			mk = cursorMark(style)
		}
```

and pass `mk` as a new trailing parameter to every `segCell`, `diffCell` and `scrollCell` call in the three cases.

Add the trailing parameter `mk cellMark` to `segCell`, `diffCell` and `scrollCell`:

- `segCell`: `base := mk.base` when `mk.row`, else `lipgloss.NewStyle()`; `if hot { base = hotStyle }` unchanged; gutter `mk.gut.Render(truncate(num, gut+1))`. The padding already renders through `base`, so the marker covers the whole cell.
- `diffCell`: enriched path — same base rule; plain path — after `bodyTxt = padRight(...)`: `switch { case hot: bodyTxt = hotStyle.Render(bodyTxt); case mk.row: bodyTxt = mk.base.Render(bodyTxt) }`; gutter through `mk.gut`.
- `scrollCell`: its `diffCell` delegation passes `mk` through; its own `base := lipgloss.NewStyle()` follows the same rule; the ‹/› marker columns and the gutter through `mk.gut`; padding through `base`.
- Gap cells (`diffGapCell` filler) stay exactly as they are (never marked).

Update every caller: `grep -n 'segCell(\|diffCell(\|scrollCell(' internal/tui/*.go` — production callers are all in `diff_render.go`; test callers in `diff_render_test.go` / `diff_syntax_test.go` get `noMark` appended.

`renderDiffView` (same file): replace `m.diffPaneLines(v, w, body)` with

```go
		s, e := v.cursorDispRange()
		lines = append(lines, m.diffPaneLines(v, w, body, s, e, m.cursorStyle())...)
```

`internal/tui/history_view.go:256`: `lines := m.diffPaneLines(v, w, body, 0, 0, "off")`.

`internal/tui/model.go`, next to `diffLong`:

```go
	diffCursor  string      // session override of [ui] diff_cursor ("" = follow config); the . menu's Cursor marker row cycles it
```

Add to `internal/tui/diff_cursor.go`:

```go
// cursorStyle is the effective marker style: the session override, else config.
func (m Model) cursorStyle() string {
	if m.diffCursor != "" {
		return m.diffCursor
	}
	return m.cfg.UI.CursorStyle()
}

// nextCursorStyle is the . menu's cycle: row → number → off → row.
func nextCursorStyle(s string) string {
	switch s {
	case "row":
		return "number"
	case "number":
		return "off"
	}
	return "row"
}

// cursorStyleLabel is the human name of a marker style for the menu row.
func cursorStyleLabel(s string) string {
	switch s {
	case "number":
		return i18n.T("number")
	case "off":
		return i18n.T("off")
	}
	return i18n.T("row")
}

// diffCursorStyleRow is the . menu row that cycles the marker style for the
// session. Only while a diff view is on top.
func (m Model) diffCursorStyleRow() (actionRow, bool) {
	if _, ok := m.topLayer().(*diffView); !ok {
		return actionRow{}, false
	}
	cur := m.cursorStyle()
	return actionRow{
		id:    "diff-cursor-style",
		label: i18n.T("Cursor marker: %s", cursorStyleLabel(cur)),
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.diffCursor = nextCursorStyle(cur)
			return m, nil
		},
	}, true
}
```

(add the `tea` and `i18n` imports to `diff_cursor.go`).

`internal/tui/action_menu.go`: in the content-window branch, right after the `exportFilePatchRow` append, add

```go
		if r, ok := m.diffCursorStyleRow(); ok {
			rows = append(rows, r)
		}
```

i18n keys (all four bundles): `"Cursor marker: %s"`, `"row"`, `"number"`, `"off"` — check first with `grep -n '^"row"\|^"number"\|^"off"' internal/i18n/lang/ja.toml`; add only the missing ones. ja: `"Cursor marker: %s" = "カーソル表示: %s"`, `"row" = "行"`, `"number" = "番号"`, `"off" = "オフ"`; ko: `"커서 표시: %s"`, `"행"`, `"번호"`, `"끔"`; zh: `"光标标记：%s"`, `"整行"`, `"行号"`, `"关闭"`; ru: `"Маркер курсора: %s"`, `"строка"`, `"номер"`, `"выкл"`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ ./internal/config/ -count=1`
Expected: PASS (including `menu_labels_test.go` — the row is a direct-run row with a literal label, so no `actionMenuLabel` case is needed; if that gate insists on a case for every id, add `case "diff-cursor-style": return i18n.T("Cursor marker"), true` and the key to the four bundles).

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/ ./internal/config/
git add internal/config/ internal/tui/ internal/i18n/lang/
git commit -m "feat(tui): diff cursor marker — [ui] diff_cursor row|number|off, session cycle in the . menu"
```

---

### Task 4: `e` opens the editor at the cursor line

**Files:**
- Modify: `internal/tui/edit_actions.go:36-47` (`editorCommandAt`), `internal/tui/open_external.go` (line in `editorViewMsg`, `openInEditorAtCmd`, `viewExternalCmd`), `internal/tui/model.go:2587-2592` (handler passes the line)
- Modify: `internal/tui/diff_view.go` (`e` key), `internal/tui/action_menu.go` (row), `internal/i18n/lang/*.toml`
- Create: `internal/tui/diff_edit.go`
- Test: `internal/tui/edit_actions_test.go` (append), `internal/tui/diff_cursor_test.go` (append)

**Interfaces:**
- Consumes (Task 1): `cursorRow`; `diffView.rev`, `.title`, `.compare`, `.lines`, `.curLine`.
- Produces:
  - `func editorCommandAt(editor, absPath string, line int) *exec.Cmd`; `editorCommand(editor, absPath)` becomes `editorCommandAt(editor, absPath, 0)`.
  - `func editorProgram(editor string) string` — basename of the first field, `.exe`/`.cmd` stripped, lower-cased.
  - `func (m Model) editFileAtCmd(rel string, line int) tea.Cmd`; `editFileCmd(rel)` = `editFileAtCmd(rel, 0)`.
  - `func (m Model) openInEditorAtCmd(name string, line int, resolve func(context.Context) ([]byte, error)) tea.Cmd`; `openInEditorCmd(name, resolve)` = `openInEditorAtCmd(name, 0, resolve)`. `editorViewMsg.line int`; `viewExternalCmd(path, name string, line int)`.
  - `func (v *diffView) editLine() int` — the new-side line for `e` (see rule).
  - `func (m Model) diffEditRow() (actionRow, bool)` — id `diff-edit-at-line`, label `Open in editor at line %d`; also the `e` key.

- [ ] **Step 1: Failing tests**

Append to `internal/tui/edit_actions_test.go`:

```go
func TestEditorCommandAtByProgram(t *testing.T) {
	t.Parallel()
	cases := []struct {
		editor string
		want   []string
	}{
		{"vim", []string{"vim", "+12", "/wt/a.go"}},
		{"nvim -u NONE", []string{"nvim", "-u", "NONE", "+12", "/wt/a.go"}},
		{"nano", []string{"nano", "+12", "/wt/a.go"}},
		{"emacs -nw", []string{"emacs", "-nw", "+12", "/wt/a.go"}},
		{"micro", []string{"micro", "+12", "/wt/a.go"}},
		{"kak", []string{"kak", "+12", "/wt/a.go"}},
		{"code -w", []string{"code", "-w", "--goto", "/wt/a.go:12"}},
		{"codium", []string{"codium", "--goto", "/wt/a.go:12"}},
		{"cursor", []string{"cursor", "--goto", "/wt/a.go:12"}},
		{"code-insiders", []string{"code-insiders", "--goto", "/wt/a.go:12"}},
		{"hx", []string{"hx", "/wt/a.go:12"}},
		{"subl -w", []string{"subl", "-w", "/wt/a.go:12"}},
		{"zed", []string{"zed", "/wt/a.go:12"}},
		{"/usr/local/bin/Vim.EXE", []string{"/usr/local/bin/Vim.EXE", "+12", "/wt/a.go"}},
		{`C:\tools\code.cmd`, []string{`C:\tools\code.cmd`, "--goto", "/wt/a.go:12"}},
		{"gedit", []string{"gedit", "/wt/a.go"}},
	}
	for _, c := range cases {
		got := editorCommandAt(c.editor, "/wt/a.go", 12).Args
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q: args = %v, want %v", c.editor, got, c.want)
		}
	}
	// line <= 0 is the plain invocation for every program.
	if got := editorCommandAt("code -w", "/wt/a.go", 0).Args; !reflect.DeepEqual(got, []string{"code", "-w", "/wt/a.go"}) {
		t.Errorf("line 0: %v", got)
	}
}
```

(`editorProgram` strips the directory on both `/` and `\\`, so the `.cmd` and `.EXE` cases prove suffix stripping; a path with spaces is still split by `strings.Fields` — the documented v1 limitation, unchanged here.)

Append to `internal/tui/diff_cursor_test.go`:

```go
func TestDiffEditLineRule(t *testing.T) {
	t.Parallel()
	rows := cursorRows(6)
	rows[2] = textdiff.Row{Kind: textdiff.Del, Left: "gone", LeftNo: 3}
	for i := 3; i < 6; i++ {
		rows[i].RightNo-- // new side: 1,2,_,3,4,5
	}
	v := diffViewWith(rows, []int{2})
	v.setCursorLine(1, 10)
	if got := v.editLine(); got != 2 {
		t.Fatalf("Same row: editLine=%d, want 2", got)
	}
	v.setCursorLine(2, 10) // the Del row: next row with a new-side number → 3
	if got := v.editLine(); got != 3 {
		t.Fatalf("Del row: editLine=%d, want 3", got)
	}
	rows2 := cursorRows(3)
	rows2[2] = textdiff.Row{Kind: textdiff.Del, Left: "tail", LeftNo: 3}
	v2 := diffViewWith(rows2, []int{2})
	v2.setCursorLine(2, 10) // trailing deletion: no following new line → the last one (2)
	if got := v2.editLine(); got != 2 {
		t.Fatalf("trailing Del: editLine=%d, want 2", got)
	}
}

func TestDiffEditRowGating(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(10), nil)
	if _, ok := m.diffEditRow(); !ok {
		t.Fatal("working-tree diff must offer the edit row")
	}
	m.diffLayer().compare = true
	if _, ok := m.diffEditRow(); ok {
		t.Fatal("a compare view has no single file: no edit row")
	}
	m.diffLayer().compare = false
	m.diffLayer().rev = "abc123"
	r, ok := m.diffEditRow()
	if !ok || !strings.Contains(r.label, "line 1") {
		t.Fatalf("commit diff row = %+v ok=%v, want an 'at line 1' row", r, ok)
	}
}

func TestDiffEKeyWorkingTreeUsesLiveEditor(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(10), nil)
	m.currentWorktree = "/wt"
	m.diffLayer().setCursorLine(4, m.diffBodyRows())
	_, cmd := m.Update(keyMsg("e"))
	if cmd == nil {
		t.Fatal("e on a working-tree diff must return an editor command")
	}
}
```

Run: `go test ./internal/tui/ -run 'TestEditorCommandAtByProgram|TestDiffEdit|TestDiffEKey'` → FAIL (undefined).

- [ ] **Step 2: Implement the argv builder**

Replace `editorCommand` in `internal/tui/edit_actions.go` with:

```go
// editorCommand builds the plain editor invocation (no line): see editorCommandAt.
func editorCommand(editor, absPath string) *exec.Cmd { return editorCommandAt(editor, absPath, 0) }

// editorProgram is the editor's program name for the line-flag table: the
// basename of the first field, a Windows .exe/.cmd suffix stripped, lower-cased.
func editorProgram(editor string) string {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		return ""
	}
	p := fields[0]
	if i := strings.LastIndexAny(p, `/\`); i >= 0 {
		p = p[i+1:]
	}
	p = strings.ToLower(p)
	p = strings.TrimSuffix(strings.TrimSuffix(p, ".exe"), ".cmd")
	return p
}

// editorCommandAt builds the editor invocation, opening absPath at line when
// line > 0 and the program's goto syntax is known: the editor string is split
// on whitespace (binary + leading flags; no shell-quote parsing — v1), then
// vi-style editors get `+N path`, VS Code-style editors `--goto path:N`,
// helix/sublime/zed `path:N`, and anything else the plain path (the line is
// dropped, never guessed). Guards the empty-fields case as belt-and-braces.
func editorCommandAt(editor, absPath string, line int) *exec.Cmd {
	fields := strings.Fields(editor)
	if len(fields) == 0 {
		fields = []string{defaultEditor()}
	}
	args := append([]string{}, fields[1:]...)
	if line > 0 {
		switch editorProgram(editor) {
		case "vim", "nvim", "vi", "nano", "emacs", "micro", "kak":
			args = append(args, fmt.Sprintf("+%d", line), absPath)
		case "code", "code-insiders", "codium", "cursor":
			args = append(args, "--goto", fmt.Sprintf("%s:%d", absPath, line))
		case "hx", "subl", "zed":
			args = append(args, fmt.Sprintf("%s:%d", absPath, line))
		default:
			args = append(args, absPath)
		}
	} else {
		args = append(args, absPath)
	}
	return exec.Command(fields[0], args...)
}
```

(add `"fmt"` to the imports if missing). Change `editFileCmd`:

```go
// editFileCmd suspends the TUI and opens rel (repo-relative) in the user's
// editor; on exit it yields an editorFinishedMsg. See editFileAtCmd.
func (m Model) editFileCmd(rel string) tea.Cmd { return m.editFileAtCmd(rel, 0) }

// editFileAtCmd is editFileCmd positioned on line (0 = no position). Bubble
// Tea's ExecProcess owns the terminal release/restore and the cmd's stdio —
// do not set them here.
func (m Model) editFileAtCmd(rel string, line int) tea.Cmd {
	abs := filepath.Join(m.currentWorktree, rel)
	cmd := editorCommandAt(resolveEditor(), abs, line)
	cmd.Dir = m.currentWorktree
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorFinishedMsg{path: rel, err: err}
	})
}
```

- [ ] **Step 3: Thread the line through the read-only path**

`internal/tui/open_external.go`: add `line int` to `editorViewMsg` (comment: `// 1-based line to open at; 0 = none`). Change:

```go
func (m Model) openInEditorCmd(name string, resolve func(context.Context) ([]byte, error)) tea.Cmd {
	return m.openInEditorAtCmd(name, 0, resolve)
}

// openInEditorAtCmd is openInEditorCmd positioned on line (0 = none).
func (m Model) openInEditorAtCmd(name string, line int, resolve func(context.Context) ([]byte, error)) tea.Cmd {
	return func() tea.Msg {
		data, err := resolve(context.Background())
		if err != nil {
			return editorViewMsg{name: name, err: err}
		}
		if len(data) > domain.MaxDiffBytes {
			return editorViewMsg{name: name, err: fmt.Errorf("%s", i18n.T("file too large to open (%d bytes)", len(data)))}
		}
		path, err := writeReadOnlyTempFile(name, data)
		return editorViewMsg{path: path, name: name, line: line, err: err}
	}
}
```

`viewExternalCmd(path, name string, line int)` uses `editorCommandAt(resolveEditor(), path, line)`. In `internal/tui/model.go` the `editorViewMsg` case becomes `return m, viewExternalCmd(msg.path, msg.name, msg.line)`. Fix any other `viewExternalCmd(` caller (`grep -n 'viewExternalCmd(' internal/tui/`) by passing `0`.

- [ ] **Step 4: The diff view's `e` and menu row**

Create `internal/tui/diff_edit.go`:

```go
package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// editLine is the NEW-side line `e` opens: the cursor row's RightNo, or on a
// Del row (no new-side number) the next row that has one, else the last row
// that has one. 0 when the view has no numbered row at all.
func (v *diffView) editLine() int {
	r, ok := v.cursorRow()
	if ok && r.RightNo > 0 {
		return r.RightNo
	}
	for i := v.curLine + 1; i < len(v.lines); i++ {
		if ln := v.lines[i]; ln.Fold == 0 && ln.Row.RightNo > 0 {
			return ln.Row.RightNo
		}
	}
	for i := len(v.lines) - 1; i >= 0; i-- {
		if ln := v.lines[i]; ln.Fold == 0 && ln.Row.RightNo > 0 {
			return ln.Row.RightNo
		}
	}
	return 0
}

// diffEditRow is "Open in editor at line N" for the diff view on top: a
// working-tree diff (rev == "") opens the real file for live editing — the
// staged diff's new side is the index, so its line is an approximation of the
// working file; a commit diff opens a read-only temp copy of rev:path at the
// line. Absent on a two-sided compare (no single file) and while an op runs.
func (m Model) diffEditRow() (actionRow, bool) {
	v, ok := m.topLayer().(*diffView)
	if !ok || v.compare || v.loading || v.err != nil || v.binary || v.tooLarge || !m.opsIdle() {
		return actionRow{}, false
	}
	line := v.editLine()
	path, rev, svc := v.title, v.rev, m.svc
	return actionRow{
		id:    "diff-edit-at-line",
		label: i18n.T("Open in editor at line %d", line),
		run: func(m Model) (tea.Model, tea.Cmd) {
			if rev == "" {
				return m, m.editFileAtCmd(path, line)
			}
			return m, m.openInEditorAtCmd(path, line, func(ctx context.Context) ([]byte, error) {
				return svc.ShowFile(ctx, rev, path)
			})
		},
	}, true
}
```

`internal/tui/diff_view.go` `updateDiffViewKey`, add a case next to `"h"`/`"b"`:

```go
	case "e":
		if r, ok := m.diffEditRow(); ok {
			nm, cmd := r.run(m)
			return nm.(Model), cmd
		}
```

`internal/tui/action_menu.go`: after the `diffCursorStyleRow` append (Task 3) add

```go
		if r, ok := m.diffEditRow(); ok {
			rows = append(rows, r)
		}
```

Check `m.svc` may be nil in the minimal test fixture (`openedDiffModel` → `New(nil)`?): the `run` closure only dereferences `svc` inside the resolve function, which runs off-thread later, so the row itself is safe; `TestDiffEKeyWorkingTreeUsesLiveEditor` uses `rev == ""` and never touches `svc`.

i18n key (four bundles): `"Open in editor at line %d"` — ja `"エディタで %d 行目を開く"`, ko `"편집기에서 %d번째 줄 열기"`, zh `"在编辑器中打开第 %d 行"`, ru `"Открыть в редакторе на строке %d"`.

- [ ] **Step 5: Run the tests**

Run: `go test ./internal/tui/ -count=1`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
gofmt -l internal/ && go vet ./internal/tui/
git add internal/tui/ internal/i18n/lang/
git commit -m "feat(tui): e opens the editor at the diff cursor line (vi/code/helix goto syntax)"
```

---

### Task 5: Footer hint, help, `.`-menu align rows, docs, headless check

**Files:**
- Modify: `internal/tui/diff_render.go:27-40` (`diffHintFor`), `internal/tui/help.go:246-260` (Diff view rows), `internal/tui/diff_cursor.go` (align rows), `internal/tui/action_menu.go`, `internal/i18n/lang/*.toml`
- Modify: `CHANGELOG.md`, `README.md` (row `enter`, `## Configuration`), `docs/CLAUDE-details.md`
- Test: `internal/tui/diff_cursor_test.go` (append)

**Interfaces:**
- Consumes: `alignCursor`, `cursorAlign` (Task 1); `diffEditRow`, `diffCursorStyleRow` (Tasks 3–4).
- Produces: `.`-menu rows `diff-align-top`, `diff-align-center`, `diff-align-bottom`; the new hint line.

- [ ] **Step 1: Failing test**

Append to `internal/tui/diff_cursor_test.go`:

```go
func TestDiffMenuOffersCursorRows(t *testing.T) {
	t.Parallel()
	m := openedDiffModel(12, cursorRows(40), nil)
	ids := map[string]bool{}
	for _, r := range availableActions(m) {
		ids[r.id] = true
	}
	for _, want := range []string{"diff-edit-at-line", "diff-cursor-style", "diff-align-top", "diff-align-center", "diff-align-bottom"} {
		if !ids[want] {
			t.Errorf("diff . menu lacks %s (have %v)", want, ids)
		}
	}
}

func TestDiffHintAdvertisesCursorKeys(t *testing.T) {
	t.Parallel()
	h := diffHintFor(longScroll)
	for _, k := range []string{"[j/k]", "[z]", "[e]"} {
		if !strings.Contains(h, k) {
			t.Errorf("hint %q lacks %s", h, k)
		}
	}
}
```

(`availableActions(m Model) []actionRow` is the row-assembly function in `action_menu.go`.) Run → FAIL.

- [ ] **Step 2: Align rows + hint + help**

`internal/tui/diff_cursor.go` — add:

```go
// diffAlignRows are the . menu's three viewport alignments for the cursor
// line (the z key cycles them).
func (m Model) diffAlignRows() []actionRow {
	if _, ok := m.topLayer().(*diffView); !ok {
		return nil
	}
	mk := func(id, label string, mode cursorAlign) actionRow {
		return actionRow{id: id, label: label, run: func(m Model) (tea.Model, tea.Cmd) {
			if v := m.diffLayer(); v != nil {
				v.alignCursor(mode, m.diffBodyRows())
			}
			return m, nil
		}}
	}
	return []actionRow{
		mk("diff-align-top", i18n.T("Align cursor line: top"), alignTop),
		mk("diff-align-center", i18n.T("Align cursor line: center"), alignCenter),
		mk("diff-align-bottom", i18n.T("Align cursor line: bottom"), alignBottom),
	}
}
```

`internal/tui/action_menu.go`: after the `diffEditRow` append, `rows = append(rows, m.diffAlignRows()...)`.

`internal/tui/diff_render.go` `diffHintFor` — replace the return with:

```go
	return i18n.T("[↑↓] scroll  [j/k] line  [z] align  [e] edit  [n/p] change  [f] part  [ctrl+w] %s", mode) + pan + i18n.T("  [h] hist  [b] blame  [esc] close")
```

and add that new key to the four bundles (copy each language's existing `"[↑↓] scroll  [n/p] change  [f] part  [ctrl+w] %s"` value and insert the three new bracket groups: ja `[j/k] 行  [z] 位置  [e] 編集`, ko `[j/k] 줄  [z] 정렬  [e] 편집`, zh `[j/k] 行  [z] 对齐  [e] 编辑`, ru `[j/k] строка  [z] выровнять  [e] правка`). Remove the old key from the bundles only if `i18n_scan_test.go` complains about unused keys (check its rules first; otherwise leave it).

`internal/tui/help.go` Diff view section — change the first row and add three:

```go
		r("↑/↓", i18n.T("scroll one line (the cursor stays where it is)")),
		r("j/k", i18n.T("move the line cursor (the view scrolls only as needed); pgup/pgdn, home/end and n/p move it too; a click places it")),
		r("z", i18n.T("cycle the cursor line's position in the view: center → top → bottom (also three . menu rows)")),
		r("e", i18n.T("open the file in your editor at the cursor line — the working-tree file for a working-tree diff (for the staged diff the index is the new side, so the line is approximate), a read-only copy of the shown revision for a commit diff; not offered on a two-sided compare. The marker style is [ui] diff_cursor = row | number | off, and the . menu's Cursor marker row switches it for the session")),
```

Keys to the four bundles (translate faithfully; the `adding-translations` skill in `.claude/skills/` has the house style).

- [ ] **Step 3: Docs**

`CHANGELOG.md` under `## [Unreleased]`, first bullet:

```markdown
- **Diff view line cursor.** The full-screen diff now has a current line:
  `j`/`k` move it (the view scrolls only as needed), `↑`/`↓` and the wheel
  keep scrolling the viewport without moving it, `pgup`/`pgdn`, `home`/`end`
  and `n`/`p` move it too, and a left click places it. `z` cycles the line's
  position in the view (center → top → bottom; also `.`-menu rows). `e`
  opens the file in your editor at that line — live for a working-tree diff,
  a read-only copy of the revision for a commit diff — with the right goto
  syntax for vi-style editors, VS Code-style editors, helix, sublime and
  zed. The marker is `[ui] diff_cursor = "row"` (background), `"number"`
  (gutter only) or `"off"`; the `.` menu's **Cursor marker** row switches
  it for the session. The header names the cursor line. This is the anchor
  review notes will use.
```

`README.md` row `enter` (line 56): after "`←`/`→`/`0` pan in scroll mode," insert "`j`/`k` move the **line cursor** (`↑`/`↓` scroll without moving it; `z` cycles its position center/top/bottom; a click places it), `e` opens the file in your editor at that line,". In `## Configuration` after the `diff_syntax` paragraph (line ~334) add:

```markdown
`[ui] diff_cursor` (default `"row"`) picks the diff view's current-line
marker: `"row"` paints a background under the cursor line, `"number"`
highlights only its gutter numbers, `"off"` hides it (the cursor still
drives `e`). The `.` menu's **Cursor marker** row switches it for the session.
```

`docs/CLAUDE-details.md`: after the diff syntax paragraph (line ~93) add one paragraph: cursor = `curLine` into `diffView.lines` (`diff_cursor.go`), independent of `cur`; `cursorRow()` is the notes anchor; `diffPaneLines(v, w, body, curStart, curEnd, style)` — the history pane passes `0, 0, "off"`; `editorCommandAt` goto table; `e` gating (`compare`, `rev`).

- [ ] **Step 4: Full gates + headless smoke**

```bash
gofmt -l internal/ && go vet ./...
./test.sh unit 2>&1 | tail -5
go build -o /tmp/claude-1000/-mnt-t-others-gigagit/a145f667-53f5-4103-95d2-a26fb6444dcf/scratchpad/gg-cursor ./cmd/gg
```

Then drive the TUI headlessly (`driving-tui-headless` skill): `./tui-capture.sh` with a keyscript that opens a Files-panel diff, presses `j` three times, `z`, `z`, then `.`; eyeball the snapshots for the marker row, the `line N` header, the hint, and the menu rows. Fix anything visibly wrong before committing.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/ internal/i18n/lang/ CHANGELOG.md README.md docs/CLAUDE-details.md
git commit -m "feat(tui): diff cursor — footer hint, help, align menu rows, docs"
```

---

## Self-review

- **Spec coverage (§4.1):** cursor model + fold skip + re-anchor → Task 1; keys rule, `z` cycle, click, header → Task 2; marker config/styles/session row/history pane unmarked → Task 3; `e` + argv table + line rule + gating → Task 4; hint/help/menu align rows/docs → Task 5. "Not in this phase" items untouched. `cursorRow()` contract → Task 1.
- **Placeholders:** none; every step carries code.
- **Type consistency:** `cursorAlign` (`alignCenter`/`alignTop`/`alignBottom`) used in Tasks 1, 2, 5; `diffPaneLines(v, w, body, curStart, curEnd, style)` in Tasks 3 and 5's history caller; `editorCommandAt(editor, absPath, line)`, `editFileAtCmd(rel, line)`, `openInEditorAtCmd(name, line, resolve)`, `viewExternalCmd(path, name, line)` consistent across Task 4; row ids `diff-edit-at-line`, `diff-cursor-style`, `diff-align-{top,center,bottom}` consistent between Tasks 3–5.
