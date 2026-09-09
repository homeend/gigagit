package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
	"github.com/homeend/gigagit/internal/textdiff"
)

var errFake = errors.New("boom")

func itoa(i int) string { return strconv.Itoa(i) }

func renderModelWithDiff(v *diffView) Model {
	m := diffModel()
	m.width = 100
	m.height = 20
	m = m.pushLayer(v)
	m.diffTag = "status:x"
	return m
}

func TestRenderDiffViewStates(t *testing.T) {
	cases := []struct {
		name string
		v    *diffView
		want string
	}{
		{"loading", &diffView{title: "f.txt", context: "HEAD → working tree", loading: true}, "(loading…)"},
		{"binary", &diffView{title: "f.bin", binary: true}, "(binary file)"},
		{"too large", &diffView{title: "huge", tooLarge: true}, "(file too large)"},
		{"error", &diffView{title: "f", err: errFake}, "error:"},
	}
	for _, c := range cases {
		m := renderModelWithDiff(c.v)
		out := ansi.Strip(m.render())
		if !strings.Contains(out, c.want) {
			t.Fatalf("%s: rendered view missing %q:\n%s", c.name, c.want, out)
		}
	}
}

func TestRenderDiffViewPanes(t *testing.T) {
	res := textdiff.Compare([]byte("same\nold line\n"), []byte("same\nnew line\n"), textdiff.Options{})
	v := &diffView{title: "f.txt", context: "HEAD → working tree", full: res.Rows, fullBlocks: res.Blocks}
	v.rebuild()
	m := renderModelWithDiff(v)
	// 140 is the diff hint's design budget: the widest (scroll) English
	// variant measures 139 columns, so "[esc] close" survives here and the
	// test fails the moment a new group pushes the line past it.
	m.width = 140
	out := ansi.Strip(m.render())
	lines := strings.Split(out, "\n")
	for i, l := range lines {
		if lipgloss.Width(l) > m.width {
			t.Fatalf("line %d wider than terminal (%d): %q", i, lipgloss.Width(l), l)
		}
	}
	var found bool
	for _, l := range lines {
		if li := strings.Index(l, "│"); li >= 0 {
			left, right := l[:li], l[li:]
			if strings.Contains(left, "old line") && strings.Contains(right, "new line") {
				found = true
			}
			if strings.Contains(left, "new line") || strings.Contains(right, "old line") {
				t.Fatalf("sides swapped: %q", l)
			}
		}
	}
	if !found {
		t.Fatalf("no pane row with old|new pair:\n%s", out)
	}
	if !strings.Contains(out, "f.txt") || !strings.Contains(out, "HEAD → working tree") {
		t.Fatalf("header incomplete:\n%s", lines[0])
	}
	if !strings.Contains(out, "[esc] close") {
		t.Fatalf("hint line missing:\n%s", out)
	}
}

func TestRenderDiffViewTabsStayInPane(t *testing.T) {
	// Tab-indented content (every Go file): tabs must be expanded, never
	// rendered raw — a raw \t would let the terminal push text through the
	// separator.
	res := textdiff.Compare([]byte("\tindented\n"), []byte("\tindented changed\n"), textdiff.Options{})
	v := &diffView{title: "f.go", full: res.Rows, fullBlocks: res.Blocks}
	v.rebuild()
	m := renderModelWithDiff(v)
	out := m.render()
	if strings.Contains(out, "\t") {
		t.Fatal("rendered diff contains a raw tab")
	}
}

func TestRenderDiffViewNoContentDifferenceNote(t *testing.T) {
	res := textdiff.Compare([]byte("a\n"), []byte("a\n"), textdiff.Options{})
	v := &diffView{title: "f", context: "@ abc1234", full: res.Rows, fullBlocks: res.Blocks}
	v.rebuild()
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "(no content difference)") {
		t.Fatalf("empty-blocks diff must explain itself:\n%s", out)
	}
}

func TestRenderDiffViewTruncatedNote(t *testing.T) {
	v := &diffView{title: "f", truncated: true, full: []textdiff.Row{{Kind: textdiff.Del, Left: "x", LeftNo: 1}}, fullBlocks: []int{0}}
	v.rebuild()
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "alignment skipped") {
		t.Fatalf("truncated diff must carry the note:\n%s", out)
	}
}

func TestRenderDiffViewScrollWindow(t *testing.T) {
	rows := make([]textdiff.Row, 100)
	for i := range rows {
		rows[i] = textdiff.Row{Kind: textdiff.Same, Left: "L" + itoa(i), Right: "L" + itoa(i), LeftNo: i + 1, RightNo: i + 1}
	}
	v := &diffView{title: "f", full: rows}
	v.rebuild()
	v.offset = 50
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "L50") || strings.Contains(out, "L10 ") {
		t.Fatalf("offset window not applied:\n%s", out)
	}
}

func TestRenderDiffViewPartialShowsFold(t *testing.T) {
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
	res := textdiff.Compare([]byte(oldB.String()), []byte(newB.String()), textdiff.Options{})
	v := &diffView{title: "f", full: res.Rows, fullBlocks: res.Blocks, partial: true}
	v.rebuild()
	m := renderModelWithDiff(v)
	out := ansi.Strip(m.render())
	if !strings.Contains(out, "unchanged lines") {
		t.Fatalf("partial mode must render a fold separator:\n%s", out)
	}
	v.partial = false
	v.rebuild()
	out = ansi.Strip(renderModelWithDiff(v).render())
	if strings.Contains(out, "unchanged lines") {
		t.Fatalf("full mode must not fold:\n%s", out)
	}
}

func TestEmphasisActuallyChangesOutput(t *testing.T) {
	// Force TrueColor so lipgloss emits ANSI escape codes in the non-TTY test
	// environment. SetColorProfile is the API lipgloss itself documents for
	// testing. Capture and restore the prior profile (rather than hardcoding
	// Ascii) so this is robust under -shuffle or if a test gains t.Parallel().
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	// A cheap check that the emphasis style lands: the same hot cell rendered
	// with a span differs from the same cell with no span (which takes the
	// original, byte-identical path).
	emph := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, []textdiff.Span{{Start: 0, End: 3}}, nil, noMark)
	plain := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, nil, nil, noMark)
	if emph == plain {
		t.Fatal("an emphasized render must differ from the plain hot render")
	}
	if lipgloss.Width(emph) != lipgloss.Width(plain) {
		t.Fatalf("emphasis must not change visible width: %d vs %d", lipgloss.Width(emph), lipgloss.Width(plain))
	}
}

func TestEnrichedRowRendersWithoutBreakingWidth(t *testing.T) {
	// A Changed row carrying spans must still render as left│right at full
	// width and not panic.
	v := &diffView{
		full: []textdiff.Row{{
			Kind: textdiff.Changed, Left: "foo a", Right: "foo b",
			LeftNo: 1, RightNo: 1,
			LeftSpans: []textdiff.Span{{Start: 4, End: 5}}, RightSpans: []textdiff.Span{{Start: 4, End: 5}},
		}},
		fullBlocks: []int{0},
	}
	v.rebuild()
	m := footerModel()
	// A row renders as left│right = 2*((w-1)/2)+1 columns wide. For w=41 that
	// is 41 (paneW=20 each side + the separator).
	const w = 41
	lines := m.diffPaneLines(v, w, 1, 0, 0, "off")
	if len(lines) != 1 {
		t.Fatalf("want 1 line, got %d", len(lines))
	}
	if lipgloss.Width(lines[0]) != w {
		t.Fatalf("enriched row width = %d, want %d", lipgloss.Width(lines[0]), w)
	}
}

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

func TestWrapCellsShortLineOneSegment(t *testing.T) {
	d, e := runesEmph("hello", false)
	segs := wrapCells(d, e, nil, 20)
	if got := segText(segs); len(got) != 1 || got[0] != "hello" {
		t.Fatalf("segs = %q, want [\"hello\"]", got)
	}
}

func TestWrapCellsEmptyIsOneEmptySegment(t *testing.T) {
	segs := wrapCells(nil, nil, nil, 10)
	if len(segs) != 1 || len(segs[0].disp) != 0 {
		t.Fatalf("empty input must yield one empty segment, got %q", segText(segs))
	}
}

func TestWrapCellsBreaksAtWordBoundary(t *testing.T) {
	d, e := runesEmph("foo bar baz", false)
	segs := wrapCells(d, e, nil, 5)
	if got := segText(segs); !reflectEqual(got, []string{"foo ", "bar ", "baz"}) {
		t.Fatalf("segs = %q, want [foo |bar |baz]", got)
	}
}

func TestWrapCellsHardBreaksLongWord(t *testing.T) {
	d, e := runesEmph("abcdefgh", false)
	segs := wrapCells(d, e, nil, 3)
	if got := segText(segs); !reflectEqual(got, []string{"abc", "def", "gh"}) {
		t.Fatalf("segs = %q, want [abc|def|gh]", got)
	}
}

func TestWrapCellsSingleOverWideRuneTakenAlone(t *testing.T) {
	d, e := runesEmph("ab", false)
	segs := wrapCells(d, e, nil, 1)
	if got := segText(segs); !reflectEqual(got, []string{"a", "b"}) {
		t.Fatalf("segs = %q, want [a|b]", got)
	}
}

func TestWrapCellsCarriesEmphMask(t *testing.T) {
	d, e := runesEmph("ab cd", true)
	segs := wrapCells(d, e, nil, 2)
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

func TestDiffPaneLinesWrappedRowWidthAndCount(t *testing.T) {
	rows := []textdiff.Row{
		{Kind: textdiff.Changed, Left: "alpha beta gamma delta", Right: "alpha beta gamma DELTA", LeftNo: 1, RightNo: 1},
	}
	v := diffViewWith(rows, []int{0})
	v.long = longWrap
	const w = 41 // paneW=20 each → row width 41
	v.relayout(w)
	m := footerModel()
	lines := m.diffPaneLines(v, w, len(v.disp), 0, 0, "off")
	if len(lines) < 2 {
		t.Fatalf("the long row should render as ≥2 display rows, got %d", len(lines))
	}
	for i, ln := range lines {
		if lipgloss.Width(ln) != w {
			t.Fatalf("display row %d width = %d, want %d", i, lipgloss.Width(ln), w)
		}
	}
}

func TestScrollCellFitsDelegatesToDiffCell(t *testing.T) {
	got := scrollCell(3, "hello", nil, nil, 0, 3, 20, false, false, diffDelCell, noMark)
	want := diffCell(3, "hello", 3, 20, false, false, diffDelCell, nil, nil, noMark)
	if got != want {
		t.Fatalf("fitting scrollCell must equal diffCell:\n got %q\nwant %q", got, want)
	}
}

func TestScrollCellWidthAlwaysExact(t *testing.T) {
	long := strings.Repeat("abcdefghij ", 8) // ~88 cols
	for _, hOff := range []int{0, 5, 40, 200} {
		cell := scrollCell(1, long, nil, nil, hOff, 3, 20, false, false, diffDelCell, noMark)
		if w := lipgloss.Width(cell); w != 20 {
			t.Fatalf("hOffset %d: cell width %d, want 20", hOff, w)
		}
	}
}

func TestScrollCellRightMarkerWhenMore(t *testing.T) {
	long := strings.Repeat("x", 100)
	cell := ansi.Strip(scrollCell(1, long, nil, nil, 0, 3, 20, false, false, diffDelCell, noMark))
	if !strings.Contains(cell, "›") {
		t.Fatalf("a line past the window must show ›: %q", cell)
	}
	if strings.Contains(cell, "‹") {
		t.Fatalf("at hOffset 0 there is nothing to the left: %q", cell)
	}
}

func TestScrollCellLeftMarkerWhenScrolled(t *testing.T) {
	long := strings.Repeat("x", 100)
	cell := ansi.Strip(scrollCell(1, long, nil, nil, 30, 3, 20, false, false, diffDelCell, noMark))
	if !strings.Contains(cell, "‹") {
		t.Fatalf("scrolled right, ‹ must show on the left: %q", cell)
	}
}

func TestScrollCellGapFiller(t *testing.T) {
	cell := ansi.Strip(scrollCell(0, "", nil, nil, 0, 3, 20, true, false, diffDelCell, noMark))
	if strings.TrimRight(cell, "·") != "" {
		t.Fatalf("gap side must be all · filler: %q", cell)
	}
}

func TestMaxCellWidthIgnoresGapSides(t *testing.T) {
	lines := []textdiff.Line{
		{Row: textdiff.Row{Kind: textdiff.Same, Left: "ab", Right: "ab"}},
		{Row: textdiff.Row{Kind: textdiff.Add, Right: "longer right side here"}},
		{Fold: 4},
	}
	if got := maxCellWidth(lines); got != lipgloss.Width("longer right side here") {
		t.Fatalf("maxCellWidth = %d, want %d", got, lipgloss.Width("longer right side here"))
	}
}

func TestScrollModeRenderShowsMarkers(t *testing.T) {
	long := strings.Repeat("x", 200)
	rows := []textdiff.Row{{Kind: textdiff.Same, Left: long, Right: long, LeftNo: 1, RightNo: 1}}
	v := diffViewWith(rows, nil) // default longScroll
	v.width = 60
	v.rebuild()
	m := footerModel()
	line := ansi.Strip(m.diffPaneLines(v, 60, 1, 0, 0, "off")[0])
	if !strings.Contains(line, "›") || strings.Contains(line, "‹") {
		t.Fatalf("scroll@0 should show › and not ‹: %q", line)
	}
	v.hOffset = 40
	v.clampHOffset()
	line = ansi.Strip(m.diffPaneLines(v, 60, 1, 0, 0, "off")[0])
	if !strings.Contains(line, "‹") {
		t.Fatalf("scrolled right, ‹ must appear: %q", line)
	}
}

func TestDiffHeaderShowsChangeCount(t *testing.T) {
	res := textdiff.Compare([]byte("a\nb\nc\n"), []byte("a\nX\nc\n"), textdiff.Options{})
	v := &diffView{title: "f", context: "ctx", full: res.Rows, fullBlocks: res.Blocks}
	v.rebuild()
	m := renderModelWithDiff(v)
	header := strings.Split(ansi.Strip(m.render()), "\n")[0]
	if !strings.Contains(header, "change 1/1") {
		t.Fatalf("diff header should show the change counter, got:\n%s", header)
	}
}

// NOTE: no t.Parallel() here or in TestDiffPaneLinesUseTokensBySourceLine —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes these assert on.
func TestStyledRunsColoursKeywordAndKeepsEmphasis(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	disp := []rune("if x")
	cls := []syntax.Class{syntax.Keyword, syntax.Keyword, 0, 0}
	// no emphasis: keyword run wears the keyword foreground
	out := styledRuns(disp, []bool{false, false, false, false}, cls, lipgloss.NewStyle())
	if !strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("keyword run should carry its 256-colour foreground: %q", out)
	}
	// emphasis wins over syntax colour (bold + 231), so the diff stays legible
	out = styledRuns(disp, []bool{true, true, false, false}, cls, lipgloss.NewStyle())
	if !strings.Contains(out, "38;5;231") || strings.Contains(out, "38;5;"+syntaxColor(syntax.Keyword)) {
		t.Errorf("emphasised run must use diffEmph, not the syntax colour: %q", out)
	}
	if ansi.Strip(out) != "if x" {
		t.Errorf("text must be unchanged: %q", ansi.Strip(out))
	}
}

func TestDiffPaneLinesUseTokensBySourceLine(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	v := &diffView{title: "a.go", full: []textdiff.Row{
		{Kind: textdiff.Same, Left: "package a", Right: "package a", LeftNo: 1, RightNo: 1},
		{Kind: textdiff.Add, Right: "var x = 1", RightNo: 2},
	}}
	v.oldTok = [][]syntax.Tok{{{Start: 0, End: 7, Class: syntax.Keyword}}}
	v.newTok = [][]syntax.Tok{
		{{Start: 0, End: 7, Class: syntax.Keyword}},
		{{Start: 0, End: 3, Class: syntax.Keyword}, {Start: 8, End: 9, Class: syntax.Number}},
	}
	v.rebuild()
	m := renderModelWithDiff(v)
	lines := m.diffPaneLines(v, 100, 5, 0, 0, "off")
	if len(lines) != 2 {
		t.Fatalf("lines = %d", len(lines))
	}
	if !strings.Contains(lines[1], "38;5;"+syntaxColor(syntax.Number)) {
		t.Errorf("row 2 right cell (new line 2) should colour the number: %q", lines[1])
	}
	if strings.Contains(lines[1], "38;5;"+syntaxColor(syntax.Keyword)+"m"+"·") {
		t.Errorf("the gap side must stay a plain filler: %q", lines[1])
	}
}

// TestWrapCellsSlicesClassMaskAlongside pins the class mask to the display
// runes across a wrap: a segment whose cls is shorter than its disp would
// panic in styledRuns, and one sliced at a different offset would paint the
// wrong runes. Concatenating the segments must reproduce the input mask.
func TestWrapCellsSlicesClassMaskAlongside(t *testing.T) {
	t.Parallel()
	disp := []rune("if x { return y }")
	emph := make([]bool, len(disp))
	cls := make([]syntax.Class, len(disp))
	for i := 0; i < 2; i++ { // "if"
		cls[i] = syntax.Keyword
	}
	for i := 7; i < 13; i++ { // "return"
		cls[i] = syntax.Keyword
	}
	segs := wrapCells(disp, emph, cls, 8)
	if len(segs) < 2 {
		t.Fatalf("a 17-rune line at width 8 must wrap, got %d segment(s)", len(segs))
	}
	var joined []syntax.Class
	for i, s := range segs {
		if len(s.cls) != len(s.disp) {
			t.Fatalf("seg %d: cls len %d != disp len %d", i, len(s.cls), len(s.disp))
		}
		joined = append(joined, s.cls...)
	}
	if len(joined) != len(cls) {
		t.Fatalf("segments cover %d runes, want %d", len(joined), len(cls))
	}
	for i := range cls {
		if joined[i] != cls[i] {
			t.Fatalf("class mask desynced at rune %d: %v, want %v (%v)", i, joined[i], cls[i], joined)
		}
	}
}

// TestScrollCellWindowKeepsClassesAligned pins the panned-window slice: the
// window's wcls must follow the same runes as wdisp, so only the part of the
// keyword still visible after the pan is coloured — never the plain run that
// follows it. Serial: lipgloss.SetColorProfile is process-global.
func TestScrollCellWindowKeepsClassesAligned(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	text := "aaaaaaaaaa" + "keyword" + strings.Repeat("b", 23)
	toks := []syntax.Tok{{Start: 10, End: 17, Class: syntax.Keyword}}
	// tw = 20-3-1 = 16; hOffset 12 with more text to the right leaves both
	// markers, so the content window is runes [13,27): "word" + ten 'b'.
	raw := scrollCell(1, text, nil, toks, 12, 3, 20, false, false, lipgloss.NewStyle(), noMark)
	if got, want := ansi.Strip(raw), "  1 ‹wordbbbbbbbbbb›"; got != want {
		t.Fatalf("visible window = %q, want %q", got, want)
	}
	kw := "38;5;" + syntaxColor(syntax.Keyword)
	if n := strings.Count(raw, kw); n != 1 {
		t.Fatalf("keyword colour appears %d times, want exactly 1 (the `word` remnant): %q", n, raw)
	}
	iKw, iWord, iB := strings.Index(raw, kw), strings.Index(raw, "word"), strings.Index(raw, "bbbbbbbbbb")
	if iKw < 0 || iWord < 0 || iB < 0 {
		t.Fatalf("missing colour or text in %q", raw)
	}
	if !(iKw < iWord && iWord < iB) {
		t.Errorf("the keyword colour must open immediately before the visible `word`, not the plain 'b' run: kw@%d word@%d b@%d in %q", iKw, iWord, iB, raw)
	}
}

// benchDiffView builds a diffView over a synthetic ~3000-line Go file with
// real syntax runs and real intraline spans, so the benchmark below walks the
// same enriched render path a highlighted file takes in the app.
func benchDiffView(b *testing.B) *diffView {
	b.Helper()
	var oldSrc, newSrc strings.Builder
	oldSrc.WriteString("package bench\n")
	newSrc.WriteString("package bench\n")
	for i := 0; i < 1000; i++ {
		n := strconv.Itoa(i)
		oldSrc.WriteString("// helper " + n + " keeps the file realistic\n")
		newSrc.WriteString("// helper " + n + " keeps the file realistic\n")
		oldSrc.WriteString("func helper" + n + "(a int, b string) (int, error) {\n")
		newSrc.WriteString("func helper" + n + "(a int, b string) (int, error) {\n")
		// Perturb every fifth line so a fifth of the rows are Changed and
		// carry word-diff spans on top of the syntax runs.
		if i%5 == 0 {
			oldSrc.WriteString("\treturn a + " + n + ", nil\n}\n")
			newSrc.WriteString("\treturn a - " + n + ", errors.New(b)\n}\n")
		} else {
			oldSrc.WriteString("\treturn a + " + n + ", nil\n}\n")
			newSrc.WriteString("\treturn a + " + n + ", nil\n}\n")
		}
	}
	oldB, newB := []byte(oldSrc.String()), []byte(newSrc.String())
	res := textdiff.Compare(oldB, newB, textdiff.Options{Enhanced: true})
	v := &diffView{title: "bench.go", full: res.Rows, fullBlocks: res.Blocks}
	lang := syntax.Detect("bench.go")
	v.oldTok = syntax.Lex(lang, oldB)
	v.newTok = syntax.Lex(lang, newB)
	if len(v.oldTok) == 0 || len(v.newTok) == 0 {
		b.Fatal("the synthetic file must lex, else the benchmark measures the plain path")
	}
	v.rebuild()
	return v
}

// BenchmarkDiffPaneLinesScrollHighlighted measures one rendered frame (50
// visible rows) of a highlighted file in scroll mode — the mode that
// re-sanitizes and re-styles every visible cell on every frame, unlike wrap
// mode which precomputes segments in relayout.
func BenchmarkDiffPaneLinesScrollHighlighted(b *testing.B) {
	// Under `go test` there is no TTY, so lipgloss would fall back to the
	// Ascii profile and Render would emit no escape sequences at all —
	// undercounting the real per-frame cost.
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)
	v := benchDiffView(b)
	v.long = longScroll
	v.relayout(200)
	m := footerModel()
	m.width, m.height = 200, 60
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Alternate the pan so both scrollCell paths are measured: hOffset 0
		// delegates to diffCell for lines that fit, >0 takes the windowed
		// slice.
		v.hOffset = (i % 2) * 8
		if got := m.diffPaneLines(v, 200, 50, 0, 0, "off"); len(got) != 50 {
			b.Fatalf("frame = %d lines, want 50", len(got))
		}
	}
}

// NOTE: serial (no t.Parallel) — lipgloss.SetColorProfile is process-global.
// The "row" cursor mark must stay visible on EVERY row kind: a hot add/del
// cell (whose own background used to win over the band, hiding the cursor on
// exactly the rows a reviewer stops on), the dotted gap side, and a plain
// cell. Each is compared against the same cell rendered without the mark.
func TestCursorMarkVisibleOnHotAndGapCells(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	mk := cursorMark("row")
	cases := []struct {
		name       string
		plain, cur string
	}{
		{"add cell", diffCell(41, "object CacheConfig {", 3, 30, false, true, diffAddCell, nil, nil, noMark),
			diffCell(41, "object CacheConfig {", 3, 30, false, true, diffAddCell, nil, nil, mk)},
		{"del cell", diffCell(27, "x = 3600", 3, 30, false, true, diffDelCell, nil, nil, noMark),
			diffCell(27, "x = 3600", 3, 30, false, true, diffDelCell, nil, nil, mk)},
		{"gap cell", diffCell(0, "", 3, 30, true, false, diffAddCell, nil, nil, noMark),
			diffCell(0, "", 3, 30, true, false, diffAddCell, nil, nil, mk)},
		{"wrapped add seg", segCell(41, cellSeg{disp: []rune("abc"), emph: make([]bool, 3), cls: make([]syntax.Class, 3)}, 3, 30, false, true, diffAddCell, noMark),
			segCell(41, cellSeg{disp: []rune("abc"), emph: make([]bool, 3), cls: make([]syntax.Class, 3)}, 3, 30, false, true, diffAddCell, mk)},
		{"wrapped gap seg", segCell(0, cellSeg{}, 3, 30, true, false, diffAddCell, noMark),
			segCell(0, cellSeg{}, 3, 30, true, false, diffAddCell, mk)},
		{"scroll add cell", scrollCell(41, "abc", nil, nil, 0, 3, 30, false, true, diffAddCell, noMark),
			scrollCell(41, "abc", nil, nil, 0, 3, 30, false, true, diffAddCell, mk)},
		{"scroll gap cell", scrollCell(0, "", nil, nil, 0, 3, 30, true, false, diffAddCell, noMark),
			scrollCell(0, "", nil, nil, 0, 3, 30, true, false, diffAddCell, mk)},
	}
	for _, c := range cases {
		if c.plain == c.cur {
			t.Errorf("%s: cursor row renders byte-identical to the unmarked cell: %q", c.name, c.cur)
		}
		if lipgloss.Width(c.plain) != lipgloss.Width(c.cur) {
			t.Errorf("%s: cursor mark changed the cell width %d → %d", c.name, lipgloss.Width(c.plain), lipgloss.Width(c.cur))
		}
	}
}
