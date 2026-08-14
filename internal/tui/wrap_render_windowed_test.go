package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Wrap-mode windowed rendering.
//
// Wrap mode used to fall back to building/wrapping EVERY row each frame ("the
// windowing math needs every row to stay exact"), which made the Commits panel
// O(feed) per keystroke once a few thousand commits were paged in (the "z then
// movement is very slow" bug). The fallback assumption is false: every row
// occupies at least one display line, so an h-line window anchored on row a can
// only ever show rows within h of a, and windowStart's clamps bind only when
// the lines on that side number fewer than h — which implies no rows on that
// side were dropped. Slicing rows to [a-h, a+h] before wrapping is therefore
// output-identical. The tests below pin both halves: the equivalence (against
// a naive full layout) and the O(visible) scaling.
// ---------------------------------------------------------------------------

// naiveWrapWindow is the reference full layout: wrap every row, find the
// anchor's first display line, window with the shared windowStart. This is a
// copy of renderWindow's pre-windowing wrap algorithm; renderWindow must stay
// output-identical to it for every anchor.
func naiveWrapWindow(rows []winRow, o winOpts) []string {
	w, h := o.w, o.h
	if w < 1 {
		w = 1
	}
	if h < 1 {
		h = 1
	}
	pw := o.prefixW
	if pw < 0 {
		pw = 0
	}
	if pw > w-1 {
		pw = w - 1
	}
	bodyW := w - pw
	type dline struct {
		text  string
		style lipgloss.Style
		deco  rowDecorator
		si    int
		row   int
	}
	var dl []dline
	for ri, r := range rows {
		segs := wrapHang(r.text, bodyW, wrapAlignIndent(r.text, bodyW), 1<<20)
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
			dl = append(dl, dline{text: s, style: r.style, deco: r.decorate, si: si, row: ri})
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
			line = dl[idx].deco(line, 0, dl[idx].si)
		}
		out = append(out, dl[idx].style.Render(line))
	}
	return out
}

// wrapTestRows builds n rows with deterministic per-row content of varied
// width (1..4 wrapped lines at w=12), distinct per row so a mis-windowed
// output can never coincide with the correct one. Every 3rd row is styled,
// every 4th carries a width-preserving decorator, and rows alternate leading
// marker glyphs ("", "* ", "● │ ") so the wrap hang-indent exercises
// per-row indents.
func wrapTestRows(n int, withPrefix bool) []winRow {
	rows := make([]winRow, n)
	st := lipgloss.NewStyle().Bold(true)
	markers := []string{"", "* ", "● │ "}
	for i := range rows {
		filler := strings.Repeat(fmt.Sprintf("w%02d ", i), i%4+1)
		r := winRow{text: fmt.Sprintf("%srow%03d %s", markers[i%len(markers)], i, filler)}
		if withPrefix {
			r.prefix = fmt.Sprintf("p%d", i%10)
		}
		if i%3 == 0 {
			r.style = st
		}
		if i%4 == 0 {
			r.decorate = func(visible string, hscroll, visualLine int) string {
				return strings.ToUpper(visible)
			}
		}
		rows[i] = r
	}
	return rows
}

// TestRenderWindowWrapMatchesFullLayout is the equivalence oracle: for every
// anchor (including out-of-range ones) and several window heights, wrap-mode
// renderWindow must produce byte-identical output to the naive full layout.
func TestRenderWindowWrapMatchesFullLayout(t *testing.T) {
	for _, withPrefix := range []bool{false, true} {
		for _, n := range []int{0, 1, 5, 23, 60} {
			rows := wrapTestRows(n, withPrefix)
			for _, h := range []int{1, 3, 7, 12} {
				for anchor := -2; anchor <= n+2; anchor++ {
					o := winOpts{w: 12, h: h, mode: modeWrap, anchor: anchor}
					if withPrefix {
						o.prefixW = 3
					}
					got := renderWindow(rows, o)
					want := naiveWrapWindow(rows, o)
					if len(got) != len(want) {
						t.Fatalf("prefix=%v n=%d h=%d anchor=%d: line count %d != %d",
							withPrefix, n, h, anchor, len(got), len(want))
					}
					for i := range got {
						if got[i] != want[i] {
							t.Fatalf("prefix=%v n=%d h=%d anchor=%d line %d:\n got %q\nwant %q",
								withPrefix, n, h, anchor, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}

// TestWrapHang pins the hanging-indent wrap: first line at full width,
// continuations indented and wrapped in the remaining width.
func TestWrapHang(t *testing.T) {
	cases := []struct {
		name   string
		s      string
		w      int
		indent int
		want   []string
	}{
		{"no wrap needed", "abc", 8, 2, []string{"abc"}},
		{"basic hang", "abcdefgh", 4, 2, []string{"abcd", "  ef", "  gh"}},
		{"zero indent = wrapWidth", "abcdefgh", 4, 0, []string{"abcd", "efgh"}},
		{"indent clamped to w-1", "abcdefgh", 3, 9, []string{"abc", "  d", "  e", "  f", "  g", "  h"}},
		{"wide glyphs stay unsplit", "日本語です", 4, 2, []string{"日本", "  語", "  で", "  す"}},
	}
	for _, c := range cases {
		got := wrapHang(c.s, c.w, c.indent, 1<<20)
		if len(got) != len(c.want) {
			t.Fatalf("%s: got %q want %q", c.name, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("%s: line %d: got %q want %q", c.name, i, got[i], c.want[i])
			}
		}
	}
}

// TestWrapContentLines pins the popup-height measurement: wrapped lines
// capped with early exit, one line per row in other modes.
func TestWrapContentLines(t *testing.T) {
	rows := []winRow{
		{text: "* abcdef"}, // aligns at 2 → "* ab"/"  cd"/"  ef" = 3 lines at w=4
		{text: "ab"},       // 1 line
		{text: ""},         // empty row still renders one blank line
	}
	o := winOpts{w: 4, mode: modeWrap}
	if got := wrapContentLines(rows, o, 100); got != 5 {
		t.Fatalf("wrap content lines = %d, want 5", got)
	}
	if got := wrapContentLines(rows, o, 4); got != 4 {
		t.Fatalf("capped content lines = %d, want 4", got)
	}
	if got := wrapContentLines(rows, winOpts{w: 4, mode: modeCutoff}, 100); got != 3 {
		t.Fatalf("cutoff content lines = %d, want 3 (one per row)", got)
	}
}

// TestWrapAlignIndent pins the per-row auto indent: the width of everything
// before the row's first letter/digit, capped at half the body width.
func TestWrapAlignIndent(t *testing.T) {
	cases := []struct {
		s     string
		bodyW int
		want  int
	}{
		{"name", 20, 0},              // text at column 0
		{"> name", 20, 2},            // cursor prefix
		{"> ● name", 20, 4},          // cursor + marker
		{"  * feat/x (path)", 20, 4}, // panel row with baked-in branch marker
		{"● │ ─ deep", 8, 4},         // cap at bodyW/2
		{"----", 20, 0},              // no letter/digit: nothing to align under
		{"→ 日本語", 20, 2},             // CJK letter stops the scan
	}
	for _, c := range cases {
		if got := wrapAlignIndent(c.s, c.bodyW); got != c.want {
			t.Errorf("wrapAlignIndent(%q, %d) = %d, want %d", c.s, c.bodyW, got, c.want)
		}
	}
}

// ansiStrip removes ANSI escape sequences for line-content assertions.
func ansiStrip(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		switch {
		case inEsc:
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				inEsc = false
			}
		case r == '\x1b':
			inEsc = true
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestRenderPanelWrapIndentsContinuations pins the panel-level hang indent:
// in wrap mode a panel row's continuation lines start 2 columns (the cursor/
// mark prefix) past the row's first line.
func TestRenderPanelWrapIndentsContinuations(t *testing.T) {
	m := benchModel(2, 20, 8)
	m.dispModes[panelCommits] = modeWrap
	m.sel[panelCommits] = 0
	rows := []string{"alpha-" + strings.Repeat("x", 40) + "-tail", "beta"}
	out := m.renderPanel(panelCommits, "Commits", rows, nil, 24, 10)
	lines := strings.Split(out, "\n")
	lead := func(s string) int {
		n := 0
		for _, r := range s {
			if r != ' ' {
				break
			}
			n++
		}
		return n
	}
	inner := func(s string) string { // content between the border glyphs
		r := []rune(ansiStrip(s))
		if len(r) < 2 {
			return ""
		}
		return string(r[1 : len(r)-1])
	}
	first := -1
	for i, l := range lines {
		if strings.Contains(ansiStrip(l), "> alpha") {
			first = i
			break
		}
	}
	if first == -1 || first+1 >= len(lines) {
		t.Fatalf("selected wrapped row not found:\n%s", out)
	}
	if got, want := lead(inner(lines[first+1])), lead(inner(lines[first]))+2; got != want {
		t.Fatalf("panel continuation indent = %d, want %d:\n%s", got, want, out)
	}
}

// TestRenderWindowWrapAllocScaleFlat pins the O(visible) property at the
// renderWindow layer: quadrupling the row count must not grow the wrap-mode
// allocations (the window is the same size either way). Pre-fix this fails
// (allocations scale ~linearly with len(rows)).
func TestRenderWindowWrapAllocScaleFlat(t *testing.T) {
	measure := func(n int) float64 {
		rows := wrapTestRows(n, false)
		o := winOpts{w: 12, h: 10, mode: modeWrap, anchor: n / 2}
		return testing.AllocsPerRun(3, func() {
			_ = renderWindow(rows, o)
		})
	}
	a := measure(1000)
	b := measure(4000)
	if ratio := b / a; ratio > 2.0 { // flat ≈1.0; O(n) would be ≈4.0
		t.Fatalf("wrap renderWindow allocs grew %.2fx for 4x rows (want ~flat, O(n) fallback present)", ratio)
	}
}

// TestCommitBodyWrapAllocScaleFlat pins O(visible) for the Commits-panel row
// build in wrap mode: commitBody must style only the rows the window can show,
// exactly as it already does in cutoff/scroll.
func TestCommitBodyWrapAllocScaleFlat(t *testing.T) {
	measure := func(n int) float64 {
		m := benchModel(n, 20, 8)
		m.sel[panelCommits] = n / 2
		m.dispModes[panelCommits] = modeWrap
		// Warm the identity-width cache the way rebuildCommitGraph maintains it at
		// runtime; benchModel assigns m.commits directly, and the cold-cache
		// fallback is a deliberate O(n) scan that would drown the measurement.
		m.identWCache = m.scanCommitIdentWidth(m.commits)
		m.identWValid = true
		return testing.AllocsPerRun(3, func() {
			_, _, _ = m.commitBody(80, 40)
		})
	}
	a := measure(1000)
	b := measure(4000)
	if ratio := b / a; ratio > 2.0 {
		t.Fatalf("wrap commitBody allocs grew %.2fx for 4x feed (want ~flat, O(feed) fallback present)", ratio)
	}
}

// TestViewWrapAllocScaleFlat is the integration pin for the reported bug: the
// ENTIRE per-frame View() with the Commits panel in wrap mode must not scale
// with the number of loaded commits.
func TestViewWrapAllocScaleFlat(t *testing.T) {
	measure := func(n int) float64 {
		m := benchModel(n, 20, 8)
		m.ready = true
		m.sel[panelCommits] = n / 2
		m.dispModes[panelCommits] = modeWrap
		m = m.syncCommitsIdx()
		m.identWCache = m.scanCommitIdentWidth(m.commits)
		m.identWValid = true
		return testing.AllocsPerRun(3, func() {
			_ = m.View()
		})
	}
	a := measure(1000)
	b := measure(4000)
	if ratio := b / a; ratio > 2.0 {
		t.Fatalf("wrap-mode View allocs grew %.2fx for 4x feed (want ~flat, O(feed) render present)", ratio)
	}
}

// BenchmarkWrapViewScale measures the ENTIRE per-frame View() with the Commits
// panel in wrap mode across feed sizes — the per-keystroke cost after `z` on a
// deeply paged feed. Pre-windowing this grew linearly (≈47ms at 5k, ≈84ms at
// 10k commits — the "movement becomes very slow" bug); windowed it is ~flat.
func BenchmarkWrapViewScale(b *testing.B) {
	for _, n := range []int{1000, 10000, 50000} {
		m := benchModel(n, 20, 8)
		m.ready = true
		m.sel[panelCommits] = n / 2
		m.dispModes[panelCommits] = modeWrap
		m = m.syncCommitsIdx()
		m.identWCache = m.scanCommitIdentWidth(m.commits)
		m.identWValid = true
		b.Run(fmt.Sprintf("n=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_ = m.View()
			}
		})
	}
}

// TestPanelViewWindowedWrapWindowsRows guards that wrap mode materializes only
// the [sel-rowsCap, sel+rowsCap] span (plus keeps idx full-length), mirroring
// the cutoff/scroll contract.
func TestPanelViewWindowedWrapWindowsRows(t *testing.T) {
	const n = 500
	m := benchModel(n, 20, 8)
	m.sel[panelCommits] = n / 2
	m.dispModes[panelCommits] = modeWrap
	const boxH = 40
	rowsCap := boxH - 3
	rows, idx := m.panelViewWindowed(panelCommits, boxH)
	if len(rows) != n || len(idx) != n {
		t.Fatalf("full-length slices expected: len(rows)=%d len(idx)=%d want %d", len(rows), len(idx), n)
	}
	sel := n / 2
	for i := range rows {
		inWin := i >= sel-rowsCap && i <= sel+rowsCap
		if inWin && rows[i] == "" {
			t.Fatalf("row %d inside the wrap window [%d,%d] not materialized", i, sel-rowsCap, sel+rowsCap)
		}
		if !inWin && rows[i] != "" {
			t.Fatalf("row %d outside the wrap window [%d,%d] was materialized (O(feed) build)", i, sel-rowsCap, sel+rowsCap)
		}
	}
}

// TestRenderPanelWrapBlankedOffWindowEquivalence pins the contract between the
// upstream builders (commitBody/panelViewWindowed leave off-window rows "")
// and renderPanel's wrap windowing: blanking rows outside [sel-rowsCap,
// sel+rowsCap] must not change the rendered panel, because renderPanel never
// lets them reach the layout.
func TestRenderPanelWrapBlankedOffWindowEquivalence(t *testing.T) {
	const n = 200
	const boxW, boxH = 40, 20
	rowsCap := boxH - 3
	for _, sel := range []int{0, 3, n / 2, n - 1} {
		m := benchModel(n, 20, 8)
		m.dispModes[panelCommits] = modeWrap
		m.sel[panelCommits] = sel
		full := make([]string, n)
		for i := range full {
			full[i] = fmt.Sprintf("commit %03d %s", i, strings.Repeat("x", i%50))
		}
		blanked := make([]string, n)
		for i := range blanked {
			if i >= sel-rowsCap && i <= sel+rowsCap {
				blanked[i] = full[i]
			}
		}
		a := m.renderPanel(panelCommits, "Commits", full, nil, boxW, boxH)
		b := m.renderPanel(panelCommits, "Commits", blanked, nil, boxW, boxH)
		if a != b {
			t.Fatalf("sel=%d: blanking off-window rows changed wrap output\nfull:\n%s\nblanked:\n%s", sel, a, b)
		}
	}
}
