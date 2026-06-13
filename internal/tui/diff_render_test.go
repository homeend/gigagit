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
	// testing. Restore Ascii (no color) after the test to leave other tests
	// unaffected.
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

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
