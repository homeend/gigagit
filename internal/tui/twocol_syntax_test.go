package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
)

// NOTE: none of the tests in this file call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes these assert on (the
// same rule window_syntax_test.go follows).

// goFuncMask is goFuncCls (window_syntax_test.go) as a cell paint mask: the
// class run for "func main() {" with no emphasis.
func goFuncMask() runMask {
	cls := goFuncCls()
	return runMask{cls: cls, emph: make([]bool, len(cls))}
}

// An all-Plain mask must render byte-identically to no mask at all. (A file
// with no lexer takes the empty-mask path instead, so it never reaches here —
// this pins the case where the lexer ran but a given line carries no runs and
// something upstream still hands down a full Plain array.)
func TestTwoColPlainMaskIsByteIdenticalToNoMask(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	const text = "    return foo(bar) and a good deal more text besides"
	n := len([]rune(text))
	pm := runMask{cls: make([]syntax.Class, n), emph: make([]bool, n)}
	for _, mode := range []dispMode{modeCutoff, modeWrap, modeScroll} {
		o := twoColOpts{w: 40, h: 4, sep: " ║ ", mode: mode, hscroll: 3}
		want, _ := renderTwoCol([]colRow{{
			left:  &winCell{gutter: "[ ] ", body: text},
			right: &winCell{gutter: "[ ] ", body: "z"},
		}}, o)
		got, _ := renderTwoCol([]colRow{{
			left:  &winCell{gutter: "[ ] ", body: text, mask: pm},
			right: &winCell{gutter: "[ ] ", body: "z"},
		}}, o)
		if strings.Join(got, "\n") != strings.Join(want, "\n") {
			t.Errorf("mode %d: a plain mask changed the render\n got %q\nwant %q", mode, got, want)
		}
	}
}

func TestTwoColMaskColoursCutoff(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeCutoff})
	// colW = (43-3)/2 = 20; gutter 4 → bodyW 16, so the body fits whole.
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, "[ ] func main() {") {
		t.Fatalf("visible text changed: %q", got)
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("`func` should wear the keyword colour: %q", out[0])
	}
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should wear the func colour: %q", out[0])
	}
	if strings.Contains(out[0], kwSeq()+"m[") || strings.Contains(out[0], fnSeq()+"m[") {
		t.Errorf("the gutter must never be painted: %q", out[0])
	}
}

func TestTwoColMaskColoursScrolledSegment(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeScroll, hscroll: 5})
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should stay coloured after the scroll: %q", out[0])
	}
	if strings.Contains(out[0], kwSeq()) {
		t.Errorf("the scrolled-off keyword must not colour anything: %q", out[0])
	}
}

// A run that lands on a wrap continuation is coloured there.
func TestTwoColMaskColoursWrapContinuation(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	// colW = (25-3)/2 = 11; gutter 4 → bodyW 7, so wrapWidth splits
	// "func main() {" into "func ma" + "in() {" — main's Func run straddles.
	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 25, h: 2, sep: " ║ ", mode: modeWrap})
	if len(out) != 2 {
		t.Fatalf("want 2 lines, got %d", len(out))
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("first line should colour `func`: %q", out[0])
	}
	if !strings.Contains(out[1], fnSeq()+"min") {
		t.Errorf("continuation should colour main's tail `in`: %q", out[1])
	}
}

// truncate keeps a prefix and APPENDS "…", so an unfixed mask lands the
// ellipsis on the class of the first DROPPED rune.
func TestTwoColMaskEllipsisIsPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	// colW = (27-3)/2 = 12; gutter 4 → bodyW 8: keeps "func ma" + "…" and the
	// first dropped rune ('i') sits inside main's Func run.
	out, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask()},
		right: &winCell{},
	}}, twoColOpts{w: 27, h: 1, sep: " ║ ", mode: modeCutoff})
	if got := ansi.Strip(out[0]); !strings.HasPrefix(got, "[ ] func ma…") {
		t.Fatalf("cutoff = %q, want prefix %q", got, "[ ] func ma…")
	}
	if strings.Contains(out[0], fnSeq()+"mma…") {
		t.Errorf("the ellipsis must not fuse into main's colour run: %q", out[0])
	}
}

// A reverse-video cell style (the picker's cursor row) drops the mask —
// reverse swaps fg/bg, so per-token foregrounds would paint per-token
// BACKGROUNDS. This is what keeps the cursor row plain.
func TestTwoColMaskDroppedUnderReverseStyle(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	rev := lipgloss.NewStyle().Reverse(true)
	o := twoColOpts{w: 43, h: 1, sep: " ║ ", mode: modeCutoff}
	got, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", mask: goFuncMask(), style: rev},
		right: &winCell{},
	}}, o)
	want, _ := renderTwoCol([]colRow{{
		left:  &winCell{gutter: "[ ] ", body: "func main() {", style: rev},
		right: &winCell{},
	}}, o)
	if got[0] != want[0] {
		t.Errorf("a reverse-video cell must render exactly as the unmasked one\n got %q\nwant %q", got[0], want[0])
	}
	if strings.Contains(got[0], kwSeq()) {
		t.Errorf("no syntax colour may appear on a reverse-video cell: %q", got[0])
	}
}

// cellSegs keeps its exact contract: pre+body concatenated, one entry per
// display segment (twocol_window_test.go depends on it).
func TestCellSegsStillConcatenatesPieces(t *testing.T) {
	c := &winCell{gutter: "[x] ", body: "aaa bbb ccc", mask: runMask{}}
	segs := cellSegs(c, 10, modeWrap, 0)
	ps := cellPieces(c, 10, modeWrap, 0)
	if len(segs) != len(ps) {
		t.Fatalf("cellSegs %d segments, cellPieces %d", len(segs), len(ps))
	}
	for i := range segs {
		if segs[i] != ps[i].pre+ps[i].body {
			t.Errorf("seg %d = %q, want %q", i, segs[i], ps[i].pre+ps[i].body)
		}
	}
}
