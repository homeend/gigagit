# Hunk picker display modes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Give the shared `hunkPicker` surface the window display modes — `z` cycles scroll→wrap→cutoff (default scroll), shift+←/→ pans — plus the shared vertical scroll it lacks, keeping the two-column layout with aligned wrapped pairs.

**Architecture:** A new pure `internal/tui/twocol.go` composes the existing leaf transforms (`truncate`/`hslice`/`wrapWidth`/`windowStart`) into a two-column window where each cell is a fixed gutter (cursor + checkbox) plus a mode-transformed body; the picker holds the shared mode+hscroll and builds its rows for it. `renderWindow` and its five consumers are untouched.

**Tech Stack:** Go 1.26, Bubble Tea, lipgloss.

**Spec:** `docs/superpowers/specs/2026-06-16-picker-display-modes-design.md`.

**Key facts (verified on this branch):**
- Leaf helpers: `wrapWidth(s string, w, maxLines int) []string` (tooltip.go), `windowStart(total, n, sel int) int` and `truncate(s string, n int) string` (view.go), `hslice(s string, off, w int) string` (window.go), `padRight(s string, n int) string` (view.go, display-width aware). All stable, pure.
- `dispMode` (window.go): `modeCutoff=0`, `modeWrap=1`, `modeScroll=2`, `dispModeCount=3`; `(dispMode).next()` increments. Styles `pickerDim`/`pickerFocus` and `selectedRow` exist.
- `hunkPicker` (conflict_picker.go) fields `title,leftLabel,rightLabel,requireAll,apply,doc,blocks,bi,side,line`; helpers `cur/sideLen/clampLine/focusFirstUndecided/badge`; free func `cell(...)`. Constructors `newConflictPicker`/`newStagePicker`. It is a view-stack surface: `render(m) string` is clipped to `overlayDims` height by `view.go`; `update(m, msg)` owns all keys.
- `overlayDims()` returns `(m.width, m.height)` (80/24 fallback). Plain `←/→` switch side; `shift+left`/`shift+right` are free in the picker.
- Gate: `./test.sh race`. Commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: `twocol.go` — two-column windowed renderer

**Files:**
- Create: `internal/tui/twocol.go`
- Test: `internal/tui/twocol_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/twocol_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// plain strips ANSI so assertions compare visible text.
func plain(s string) string { return ansi.Strip(s) }

func TestTwoColCutoffTruncates(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "[ ] ", body: "abcdefghijklmnop"},
		right: &winCell{gutter: "[ ] ", body: "right"},
	}}
	out := renderTwoCol(rows, twoColOpts{w: 23, h: 1, sep: " | ", mode: modeCutoff})
	if len(out) != 1 {
		t.Fatalf("want 1 line, got %d", len(out))
	}
	// colW = (23-3)/2 = 10; left cell "[ ] abcde…" fits 10 cols with ellipsis.
	if !strings.Contains(plain(out[0]), "…") {
		t.Fatalf("cutoff should ellipsize long body: %q", plain(out[0]))
	}
	if !strings.Contains(plain(out[0]), "[ ] ") {
		t.Fatalf("gutter must be present: %q", plain(out[0]))
	}
}

func TestTwoColScrollReveals(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "", body: "0123456789ABCDEF"},
		right: &winCell{gutter: "", body: ""},
	}}
	o := twoColOpts{w: 23, h: 1, sep: " | ", mode: modeScroll, hscroll: 5}
	out := renderTwoCol(rows, o)
	// colW=10; scrolled by 5 → left shows "56789ABCDE" (no leading 0).
	if strings.Contains(plain(out[0]), "0123") {
		t.Fatalf("scroll should hide the start: %q", plain(out[0]))
	}
	if !strings.Contains(plain(out[0]), "56789") {
		t.Fatalf("scroll should reveal the offset slice: %q", plain(out[0]))
	}
}

func TestTwoColWrapAlignsPairsAndGutterOnlyFirst(t *testing.T) {
	rows := []colRow{{
		left:  &winCell{gutter: "[x] ", body: "aaa bbb ccc"},
		right: &winCell{gutter: "[ ] ", body: "z"},
	}}
	// colW=10 → bodyW=6 → left wraps "aaa bb"/"b ccc" (2 segs); right is 1 seg.
	out := renderTwoCol(rows, twoColOpts{w: 23, h: 4, sep: " | ", mode: modeWrap})
	if len(out) != 4 {
		t.Fatalf("want h=4 lines, got %d", len(out))
	}
	// First display line has both gutters.
	if !strings.Contains(plain(out[0]), "[x] ") || !strings.Contains(plain(out[0]), "[ ] ") {
		t.Fatalf("first wrap line needs both gutters: %q", plain(out[0]))
	}
	// Second display line: left continues (no gutter), right is blank-padded —
	// the pair stays registered (left's 2nd seg sits beside a blank right cell).
	if strings.Contains(plain(out[1]), "[x]") {
		t.Fatalf("continuation line must not repeat the gutter: %q", plain(out[1]))
	}
}

func TestTwoColVerticalWindowKeepsAnchor(t *testing.T) {
	var rows []colRow
	for i := 0; i < 20; i++ {
		rows = append(rows, colRow{full: &winCell{body: string(rune('A' + i))}})
	}
	out := renderTwoCol(rows, twoColOpts{w: 20, h: 5, sep: " | ", mode: modeCutoff, anchor: 18})
	if len(out) != 5 {
		t.Fatalf("want 5 lines, got %d", len(out))
	}
	joined := plain(strings.Join(out, "\n"))
	if !strings.Contains(joined, "S") { // 'A'+18 == 'S'
		t.Fatalf("anchor row 18 ('S') must be visible: %q", joined)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestTwoCol -v`
Expected: FAIL — `renderTwoCol`/`colRow`/`winCell`/`twoColOpts` undefined.

- [ ] **Step 3: Implement `twocol.go`**

Create `internal/tui/twocol.go`:

```go
package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// winCell is one cell of a two-column window: a fixed gutter (shown verbatim,
// never transformed — the cursor marker + checkbox live here) and a body the
// display mode transforms. style is applied to the whole padded cell after
// slicing, so width math stays ANSI-safe. The zero value is a blank cell.
type winCell struct {
	gutter string
	body   string
	style  lipgloss.Style
}

// colRow is one logical row: a full-width row (full != nil, spanning the whole
// width) OR a paired left/right row (left != nil && right != nil).
type colRow struct {
	full        *winCell
	left, right *winCell
}

// twoColOpts configures renderTwoCol. anchor is the colRow index kept visible.
type twoColOpts struct {
	w, h    int
	sep     string
	mode    dispMode
	hscroll int
	anchor  int
}

// cellSegs lays a cell's body out at width under mode, returning the raw
// (unstyled, unpadded) display segments with the gutter on the first segment
// and a blank indent of the gutter's width on wrap continuations.
func cellSegs(c *winCell, width int, mode dispMode, hscroll int) []string {
	if c == nil {
		return []string{""}
	}
	gw := lipgloss.Width(c.gutter)
	bodyW := width - gw
	if bodyW < 1 {
		bodyW = 1
	}
	switch mode {
	case modeWrap:
		ws := wrapWidth(c.body, bodyW, 1<<20)
		if len(ws) == 0 {
			return []string{c.gutter}
		}
		indent := strings.Repeat(" ", gw)
		out := make([]string, len(ws))
		for i, s := range ws {
			if i == 0 {
				out[i] = c.gutter + s
			} else {
				out[i] = indent + s
			}
		}
		return out
	case modeScroll:
		return []string{c.gutter + hslice(c.body, hscroll, bodyW)}
	default: // modeCutoff
		return []string{c.gutter + truncate(c.body, bodyW)}
	}
}

// segAt returns the kth segment or "" when the cell ran out (the blank-pad that
// keeps a wrapped pair registered).
func segAt(segs []string, k int) string {
	if k < len(segs) {
		return segs[k]
	}
	return ""
}

// renderTwoCol lays rows out under o and returns exactly o.h display lines, each
// padded to o.w columns. The horizontal mode applies per cell body; wrapped
// left/right pairs are aligned by padding the shorter side; the vertical window
// is shared and anchored to o.anchor.
func renderTwoCol(rows []colRow, o twoColOpts) []string {
	w, h := o.w, o.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	colW := (w - lipgloss.Width(o.sep)) / 2
	if colW < 1 {
		colW = 1
	}

	type dline struct {
		text string
		row  int
	}
	var dl []dline
	for ri, r := range rows {
		if r.full != nil {
			segs := cellSegs(r.full, w, o.mode, o.hscroll)
			for _, s := range segs {
				dl = append(dl, dline{text: styleCell(r.full.style, s, w), row: ri})
			}
			continue
		}
		ls := cellSegs(r.left, colW, o.mode, o.hscroll)
		rs := cellSegs(r.right, colW, o.mode, o.hscroll)
		n := len(ls)
		if len(rs) > n {
			n = len(rs)
		}
		for k := 0; k < n; k++ {
			left := styleCell(cellStyle(r.left), segAt(ls, k), colW)
			right := styleCell(cellStyle(r.right), segAt(rs, k), colW)
			dl = append(dl, dline{text: left + o.sep + right, row: ri})
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
		if idx < len(dl) {
			out = append(out, dl[idx].text)
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out
}

// cellStyle returns a cell's style (zero value for a nil/blank cell).
func cellStyle(c *winCell) lipgloss.Style {
	if c == nil {
		return lipgloss.Style{}
	}
	return c.style
}

// styleCell pads raw to w columns then applies style (style after padding keeps
// the width-based slicing ANSI-safe).
func styleCell(style lipgloss.Style, raw string, w int) string {
	return style.Render(padRight(raw, w))
}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run TestTwoCol -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/twocol.go internal/tui/twocol_test.go
git commit -m "feat(tui): twocol — two-column windowed renderer (gutter + display modes)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: wire display modes into `hunkPicker`

**Files:**
- Modify: `internal/tui/conflict_picker.go`
- Test: `internal/tui/conflict_picker_test.go`

- [ ] **Step 1: Add fields, default mode, the cycle helper, and key handling**

In `internal/tui/conflict_picker.go`, add `mode`/`hscroll` to the struct:

```go
type hunkPicker struct {
	title      string
	leftLabel  string
	rightLabel string
	requireAll bool
	apply      func(m Model, content []byte) (Model, tea.Cmd)

	doc    *hunkpick.Doc
	blocks []*hunkpick.Block
	bi     int
	side   hunkpick.Side
	line   int

	mode    dispMode // display mode for candidate lines (default scroll)
	hscroll int      // modeScroll horizontal offset
}
```

In BOTH constructors, set `mode: modeScroll` in the struct literal (e.g. add
`mode: modeScroll,` after `side: hunkpick.Current,`).

Add the step constant + cycle helper (above `update`):

```go
const pickerHScrollStep = 8

// cyclePickerMode advances the picker's mode in the requested order
// scroll → wrap → cutoff → scroll. Given the enum (cutoff=0, wrap=1, scroll=2)
// that is a decrement; it is intentionally the reverse of dispMode.next() so
// the scroll default cycles to wrap next.
func cyclePickerMode(d dispMode) dispMode {
	return (d + dispModeCount - 1) % dispModeCount
}
```

In `update`, add three cases (alongside `left`/`right`):

```go
	case "z":
		e.mode = cyclePickerMode(e.mode)
		e.hscroll = 0
	case "shift+left":
		if e.mode == modeScroll {
			if e.hscroll -= pickerHScrollStep; e.hscroll < 0 {
				e.hscroll = 0
			}
		}
	case "shift+right":
		if e.mode == modeScroll {
			e.hscroll += pickerHScrollStep
		}
```

- [ ] **Step 2: Replace `cell` + rewrite `render` to use `renderTwoCol`**

Delete the `cell` free function and replace `render` with:

```go
// pickerCell builds the winCell for one candidate line; r past the side's line
// count yields a blank cell (the gap when sides differ in length). cursor adds
// the "> " marker so the gutter width is constant (focused or not).
func pickerCell(blk *hunkpick.Block, side hunkpick.Side, r int, cursor bool) *winCell {
	var lines []string
	if side == hunkpick.Current {
		lines = blk.Current
	} else {
		lines = blk.Incoming
	}
	if r >= len(lines) {
		return &winCell{}
	}
	cur := "  "
	if cursor {
		cur = "> "
	}
	tick := ""
	if blk.Mode == hunkpick.LineByLine {
		if blk.Picked(side, r) {
			tick = "[x] "
		} else {
			tick = "[ ] "
		}
	}
	c := &winCell{gutter: cur + tick, body: lines[r]}
	if cursor {
		c.style = selectedRow
	}
	return c
}

func (e *hunkPicker) render(m Model) string {
	w, H := m.overlayDims()

	header := fmt.Sprintf("%s    %d hunks", e.title, len(e.blocks))
	if e.requireAll {
		header = fmt.Sprintf("%s    %d regions · %d left", e.title, len(e.blocks), e.doc.Pending())
	}
	hint := fmt.Sprintf("[←/→] side  [shift+←/→] scroll  [z] mode  [↑/↓] line  [space] pick  [c] %s  [i] %s  [C/I] all  [n/p] hunk  [enter] apply  [esc] cancel",
		e.leftLabel, e.rightLabel)

	bodyH := H - 4 // header, separator, blank, hint
	if bodyH < 1 {
		bodyH = 1
	}

	var rows []colRow
	anchor := 0
	blockNo := 0
	for _, it := range e.doc.Items {
		if it.Block == nil {
			for _, l := range it.Literal {
				rows = append(rows, colRow{full: &winCell{body: "  " + l, style: pickerDim}})
			}
			continue
		}
		blk := it.Block
		focused := blockNo == e.bi
		marker, hstyle := "  ", pickerDim
		if focused {
			marker, hstyle = "▶ ", pickerFocus
		}
		rows = append(rows, colRow{full: &winCell{
			body:  fmt.Sprintf("%shunk %d/%d — %s", marker, blockNo+1, len(e.blocks), e.badge(blk)),
			style: hstyle,
		}})
		n := len(blk.Current)
		if len(blk.Incoming) > n {
			n = len(blk.Incoming)
		}
		for r := 0; r < n; r++ {
			lCur := focused && e.side == hunkpick.Current && e.line == r
			rCur := focused && e.side == hunkpick.Incoming && e.line == r
			if lCur || rCur {
				anchor = len(rows)
			}
			rows = append(rows, colRow{
				left:  pickerCell(blk, hunkpick.Current, r, lCur),
				right: pickerCell(blk, hunkpick.Incoming, r, rCur),
			})
		}
		if blk.Mode == hunkpick.LineByLine {
			rows = append(rows, colRow{full: &winCell{body: "  result:", style: pickerDim}})
			tmp := &hunkpick.Doc{Items: []hunkpick.Item{{Block: blk}}}
			if out, ok := tmp.Resolved(); ok {
				for _, l := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
					rows = append(rows, colRow{full: &winCell{body: "    " + l, style: pickerDim}})
				}
			}
		}
		blockNo++
	}

	body := renderTwoCol(rows, twoColOpts{
		w: w, h: bodyH, sep: " ║ ", mode: e.mode, hscroll: e.hscroll, anchor: anchor,
	})

	lines := []string{header, strings.Repeat("─", min(w, 60))}
	lines = append(lines, body...)
	lines = append(lines, "", hint)
	return strings.Join(lines, "\n")
}
```

- [ ] **Step 3: Add the picker behavior tests**

Append to `internal/tui/conflict_picker_test.go`:

```go
func TestHunkPickerDefaultsToScroll(t *testing.T) {
	e := newStagePicker("f.txt", pickerDoc())
	if e.mode != modeScroll {
		t.Fatalf("default mode = %v, want modeScroll", e.mode)
	}
}

func TestHunkPickerZCyclesScrollWrapCutoff(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	if e.mode != modeScroll {
		t.Fatalf("start = %v", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeWrap {
		t.Fatalf("after 1st z = %v, want wrap", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeCutoff {
		t.Fatalf("after 2nd z = %v, want cutoff", e.mode)
	}
	m, _ = e.update(m, key("z"))
	if e.mode != modeScroll {
		t.Fatalf("after 3rd z = %v, want scroll", e.mode)
	}
}

func TestHunkPickerShiftPansOnlyInScroll(t *testing.T) {
	e := newStagePicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 24}
	m, _ = e.update(m, keyMsg("shift+right"))
	if e.hscroll != pickerHScrollStep {
		t.Fatalf("shift+right in scroll → hscroll=%d, want %d", e.hscroll, pickerHScrollStep)
	}
	m, _ = e.update(m, keyMsg("shift+left"))
	if e.hscroll != 0 {
		t.Fatalf("shift+left → hscroll=%d, want 0", e.hscroll)
	}
	// switching to wrap disables panning and z resets hscroll
	m, _ = e.update(m, keyMsg("shift+right")) // hscroll = step
	m, _ = e.update(m, key("z"))              // → wrap, hscroll reset
	if e.hscroll != 0 {
		t.Fatalf("z must reset hscroll, got %d", e.hscroll)
	}
	m, _ = e.update(m, keyMsg("shift+right")) // wrap mode: no-op
	if e.hscroll != 0 {
		t.Fatalf("shift+right in wrap must not pan, got %d", e.hscroll)
	}
}

func TestHunkPickerRenderFitsHeight(t *testing.T) {
	e := newConflictPicker("f.txt", pickerDoc())
	m := Model{stack: &viewStack{entries: []surface{e}}, width: 80, height: 12}
	out := e.render(m)
	if got := len(splitLinesTest(out)); got != 12 {
		t.Fatalf("render produced %d lines, want 12 (the overlay height)", got)
	}
}

func splitLinesTest(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
```

> `pickerDoc()` already exists in this test file (from the conflict-picker tests). Add `"strings"` to the test file's imports if it is not already there.

- [ ] **Step 4: Run the tui package**

Run: `go test ./internal/tui/`
Expected: PASS — the new picker tests, the existing conflict/staging tests (unchanged behavior besides the scroll default), and `TestHelpFooterCoverage`.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/conflict_picker.go internal/tui/conflict_picker_test.go
git commit -m "feat(tui): hunk picker gains z display modes + shift-pan + vertical scroll

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: docs

**Files:**
- Modify: `README.md`, `CHANGELOG.md`

- [ ] **Step 1: README** — in the `H` (Files panel) and `x` (conflicts) rows that describe the picker, note that long lines are readable: `z` cycles display mode (scroll default / wrap / cutoff) and `shift+←/→` pans in scroll mode; the picker scrolls vertically to keep the cursor in view.

- [ ] **Step 2: CHANGELOG** — under `## [Unreleased]` → `### Added` (or `### Changed`):

```markdown
- TUI: the hunk picker (conflict resolve / `H` staging) now reads long lines —
  **`z`** cycles the display mode (**scroll** default → wrap → cutoff) and
  **shift+←/→** pans in scroll mode, matching the rest of the app's windows. The
  picker also scrolls vertically now, so large hunks no longer run off-screen.
```

- [ ] **Step 3: Commit**

```bash
git add README.md CHANGELOG.md
git commit -m "docs: hunk picker display modes in README/CHANGELOG

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] Manual smoke (REQUIRED — no e2e for TUI): build, make a file with long lines, `H` → confirm default is scroll (long lines panable with shift+←/→), `z` cycles scroll→wrap→cutoff, and a tall hunk scrolls vertically with the cursor staying visible. Repeat via the `x` conflict resolver.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] After merge, RE-RUN `./test.sh race` on merged `main`.

---

## Self-Review

**1. Spec coverage:**
- `z` cutoff/wrap/scroll, default scroll, scroll→wrap→cutoff order → Task 2 (`cyclePickerMode`, `mode: modeScroll`). ✓
- shift+←/→ pan in scroll only → Task 2 `update`. ✓
- Two-column with aligned wrapped pairs + fixed gutter → Task 1 (`renderTwoCol`/`cellSegs`/`segAt`). ✓
- Shared vertical scroll anchored to cursor (fixes overflow) → Task 1 (`windowStart` over display lines, `anchor`) + Task 2 (`anchor` = cursor row, `bodyH`). ✓
- `renderWindow` + its consumers untouched → Task 1 is a sibling file reusing only leaf helpers. ✓
- Both consumers default scroll → Task 2 (both constructors). ✓
- Plain `←/→` still switch side → unchanged in `update`. ✓
- Docs → Task 3. ✓

**2. Placeholder scan:** complete code throughout; `pickerHScrollStep = 8` concrete; the one note (add `"strings"` import if absent) is a concrete conditional.

**3. Type consistency:** `winCell{gutter,body,style}`, `colRow{full,left,right}`, `twoColOpts{w,h,sep,mode,hscroll,anchor}`, `renderTwoCol`, `cellSegs`, `segAt`, `styleCell`, `cellStyle` consistent between Task 1 and Task 2. `cyclePickerMode`/`pickerHScrollStep`/`pickerCell` consistent within Task 2. `dispMode`/`modeScroll`/`modeWrap`/`modeCutoff`/`dispModeCount` match window.go. `m.overlayDims()` returns `(w, h)` as used.

**Out of scope:** refactoring the diff view onto `twocol`; `pgup`/`pgdn` in the picker; per-column independent mode/hscroll.
