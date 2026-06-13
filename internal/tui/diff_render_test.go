package tui

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/gigagit/gg/internal/textdiff"
)

var errFake = errors.New("boom")

func itoa(i int) string { return strconv.Itoa(i) }

func renderModelWithDiff(v *diffView) Model {
	m := diffModel()
	m.width = 100
	m.height = 20
	m.diffView = v
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

func TestSanitizeSpansMapsThroughTabExpansion(t *testing.T) {
	// "\tx" — a leading tab expands to 4 spaces; the span over 'x' (raw rune
	// index 1) must land on display column 4, not column 1.
	disp, emph := sanitizeSpans("\tx", []textdiff.Span{{Start: 1, End: 2}})
	if string(disp) != "    x" {
		t.Fatalf("disp = %q, want %q", string(disp), "    x")
	}
	want := []bool{false, false, false, false, true}
	for i := range want {
		if emph[i] != want[i] {
			t.Fatalf("emph = %v, want %v", emph, want)
		}
	}
}

func TestSanitizeSpansControlCharBecomesDot(t *testing.T) {
	disp, _ := sanitizeSpans("a\x01b", nil)
	if string(disp) != "a·b" {
		t.Fatalf("disp = %q, want %q", string(disp), "a·b")
	}
}

func TestCoverMaskClampsEnds(t *testing.T) {
	m := coverMask(3, []textdiff.Span{{Start: 1, End: 99}})
	want := []bool{false, true, true}
	for i := range want {
		if m[i] != want[i] {
			t.Fatalf("mask = %v, want %v", m, want)
		}
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
	emph := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, []textdiff.Span{{Start: 0, End: 3}})
	plain := diffCell(1, "foobar", 3, 20, false, true, diffDelCell, nil)
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
	lines := m.diffPaneLines(v, w, 1)
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
	d, e := runesEmph("ab", false)
	segs := wrapCells(d, e, 1)
	if got := segText(segs); !reflectEqual(got, []string{"a", "b"}) {
		t.Fatalf("segs = %q, want [a|b]", got)
	}
}

func TestWrapCellsCarriesEmphMask(t *testing.T) {
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
