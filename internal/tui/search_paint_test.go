package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
)

// NOTE: the render tests in this file do NOT call t.Parallel() —
// lipgloss.SetColorProfile is process-global, so a parallel sibling's deferred
// reset would land mid-render and drop the ANSI codes they assert on (the same
// rule window_syntax_test.go follows). The pure overlayHits tests do.

func TestOverlayHitsReturnsInputWhenNothingIntersects(t *testing.T) {
	t.Parallel()
	base := []emphLevel{emphWord, emphNone, emphNone}
	got := overlayHits(base, 0, 3, nil)
	if &got[0] != &base[0] {
		t.Fatal("no hits must return the very same slice (the no-search path allocates nothing)")
	}
	// A hit entirely outside the window is not a reason to allocate either.
	got = overlayHits(base, 0, 3, []hitSpan{{start: 10, end: 12}})
	if &got[0] != &base[0] {
		t.Fatal("a hit outside [off, off+n) must return the input untouched")
	}
	if overlayHits(nil, 0, 3, nil) != nil {
		t.Fatal("a nil mask with no hits must stay nil")
	}
}

func TestOverlayHitsPaintsOnACopy(t *testing.T) {
	t.Parallel()
	base := []emphLevel{emphWord, emphWord, emphNone, emphNone}
	got := overlayHits(base, 0, 4, []hitSpan{{start: 1, end: 3, cur: true}})
	want := []emphLevel{emphWord, emphCur, emphCur, emphNone}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("overlay = %v, want %v", got, want)
		}
	}
	if base[1] != emphWord {
		t.Fatal("the input mask must not be written through (it is a shared cache value)")
	}
}

// A window that starts mid-line: the hit's offsets are in the FULL line's index
// space and must be shifted and clipped by off/n.
func TestOverlayHitsShiftsAndClipsToTheWindow(t *testing.T) {
	t.Parallel()
	got := overlayHits(nil, 4, 3, []hitSpan{{start: 2, end: 6}})
	want := []emphLevel{emphHit, emphHit, emphNone}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("overlay = %v, want %v", got, want)
		}
	}
}

func TestStyledRunsPaintsThreeLevels(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	disp := []rune("abcd")
	cls := make([]syntax.Class, 4)
	plain := styledRuns(disp, make([]emphLevel, 4), cls, lipgloss.NewStyle())
	word := styledRuns(disp, []emphLevel{emphWord, emphWord, emphNone, emphNone}, cls, lipgloss.NewStyle())
	hit := styledRuns(disp, []emphLevel{emphHit, emphHit, emphNone, emphNone}, cls, lipgloss.NewStyle())
	cur := styledRuns(disp, []emphLevel{emphCur, emphCur, emphNone, emphNone}, cls, lipgloss.NewStyle())
	if word == plain {
		t.Fatal("word emphasis must change the output")
	}
	if hit != word {
		t.Fatalf("a hit paints exactly like word emphasis (spec §4.3): %q vs %q", hit, word)
	}
	if cur == hit {
		t.Fatal("the current hit must differ from an ordinary hit")
	}
	for _, s := range []string{word, hit, cur} {
		if ansi.Strip(s) != "abcd" || lipgloss.Width(s) != 4 {
			t.Fatalf("emphasis must not change the visible text or width: %q", s)
		}
	}
}

// The cursor row is reverse video, and that is exactly where the current hit
// lands: per-token COLOURS must drop (reverse would turn them into per-token
// backgrounds) but bold/underline emphasis must survive.
func TestRenderPieceReverseKeepsEmphasisDropsClasses(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	rev := st().selectedRow
	body := "func main() {"
	n := len([]rune(body))
	cls := make([]syntax.Class, n)
	cls[0], cls[1], cls[2], cls[3] = syntax.Keyword, syntax.Keyword, syntax.Keyword, syntax.Keyword

	plainPiece := cellPiece{pre: "> ", body: body, mask: runMask{cls: cls, emph: make([]emphLevel, n)}}
	got := renderPiece(rev, plainPiece, 30)
	want := styleCell(rev, "> "+body, 30)
	if got != want {
		t.Fatalf("a reversed cell with no hit must be byte-identical to the plain path:\n got %q\nwant %q", got, want)
	}

	emph := make([]emphLevel, n)
	emph[5], emph[6], emph[7], emph[8] = emphCur, emphCur, emphCur, emphCur
	hitPiece := cellPiece{pre: "> ", body: body, mask: runMask{cls: cls, emph: emph}}
	hitOut := renderPiece(rev, hitPiece, 30)
	if hitOut == want {
		t.Fatal("a hit on the reversed cursor row must still paint")
	}
	if lipgloss.Width(hitOut) != lipgloss.Width(want) {
		t.Fatalf("the hit must not change the cell width: %d vs %d", lipgloss.Width(hitOut), lipgloss.Width(want))
	}
}

// The same ruling for the window primitive: a reversed winRow keeps emph and
// drops cls.
func TestRenderWindowReversedRowKeepsEmphasis(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "func main() {"
	n := len([]rune(text))
	cls := make([]syntax.Class, n)
	cls[0], cls[1], cls[2], cls[3] = syntax.Keyword, syntax.Keyword, syntax.Keyword, syntax.Keyword
	o := winOpts{w: 30, h: 1, mode: modeCutoff}

	withCls := renderWindow([]winRow{{text: text, cls: cls, style: st().selectedRow}}, o)
	noMask := renderWindow([]winRow{{text: text, style: st().selectedRow}}, o)
	if withCls[0] != noMask[0] {
		t.Fatalf("a reversed row must still drop its class mask:\n got %q\nwant %q", withCls[0], noMask[0])
	}

	emph := make([]emphLevel, n)
	emph[5], emph[6] = emphCur, emphCur
	withHit := renderWindow([]winRow{{text: text, cls: cls, emph: emph, style: st().selectedRow}}, o)
	if withHit[0] == noMask[0] {
		t.Fatal("a hit on the reversed selected row must paint")
	}
	if lipgloss.Width(withHit[0]) != lipgloss.Width(noMask[0]) {
		t.Fatalf("the hit must not change the row width: %d vs %d", lipgloss.Width(withHit[0]), lipgloss.Width(noMask[0]))
	}
}

// An unreversed row paints emphasis through colouredLine even with no class
// mask at all (blame with syntax off is exactly that row).
func TestRenderWindowEmphWithoutClasses(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "alpha beta"
	emph := make([]emphLevel, len([]rune(text)))
	emph[6], emph[7], emph[8], emph[9] = emphHit, emphHit, emphHit, emphHit
	o := winOpts{w: 20, h: 1, mode: modeCutoff}
	got := renderWindow([]winRow{{text: text, emph: emph}}, o)
	plain := renderWindow([]winRow{{text: text}}, o)
	if got[0] == plain[0] {
		t.Fatal("an emph-only row must paint (cls stays nil for an unlexed blame)")
	}
	if ansi.Strip(got[0]) != ansi.Strip(plain[0]) {
		t.Fatalf("emphasis changed the visible text: %q vs %q", ansi.Strip(got[0]), ansi.Strip(plain[0]))
	}
}

// emph, like cls, WINS over decorate — a row that sets both takes the painted
// path and decorate is never called (no caller combines them; this is the
// sibling of TestRenderWindowClsWinsOverDecorate).
func TestRenderWindowEmphWinsOverDecorate(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	text := "alpha beta"
	emph := make([]emphLevel, len([]rune(text)))
	emph[0] = emphHit
	deco := func(visible string, hscroll, visualLine int) string { return "DECORATED" }
	out := renderWindow([]winRow{{text: text, emph: emph, decorate: deco}}, winOpts{w: 20, h: 1, mode: modeCutoff})
	if strings.Contains(ansi.Strip(out[0]), "DECORATED") {
		t.Fatalf("an emphasized row must take the painted path, not decorate: %q", out[0])
	}
}

// wrapCells must record where each segment starts, so the renderer can overlay
// whole-line hit offsets onto a wrapped segment without re-laying-out.
func TestWrapCellsRecordsSegmentOffsets(t *testing.T) {
	t.Parallel()
	disp := []rune("aaaa bbbb cccc")
	segs := wrapCells(disp, make([]emphLevel, len(disp)), make([]syntax.Class, len(disp)), 5)
	if len(segs) < 2 {
		t.Fatalf("want several segments, got %d", len(segs))
	}
	off := 0
	for i, s := range segs {
		if s.off != off {
			t.Fatalf("segment %d: off = %d, want %d", i, s.off, off)
		}
		off += len(s.disp)
	}
	if off != len(disp) {
		t.Fatalf("segments cover %d runes, want %d", off, len(disp))
	}
}

// A hit paints in every long-line mode of the diff pane.
func TestDiffCellsPaintHits(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	hits := []hitSpan{{start: 0, end: 3, cur: true}}
	plain := diffCell(1, "foobar", 3, 20, false, false, st().diffDelCell, nil, nil, noMark(), nil)
	hit := diffCell(1, "foobar", 3, 20, false, false, st().diffDelCell, nil, nil, noMark(), hits)
	if hit == plain {
		t.Fatal("diffCell must paint a hit even with no spans and no syntax runs")
	}
	if lipgloss.Width(hit) != lipgloss.Width(plain) {
		t.Fatalf("width changed: %d vs %d", lipgloss.Width(hit), lipgloss.Width(plain))
	}

	sPlain := scrollCell(1, strings.Repeat("x", 40)+"foobar", nil, nil, 30, 3, 20, false, false, st().diffDelCell, noMark(), nil)
	sHit := scrollCell(1, strings.Repeat("x", 40)+"foobar", nil, nil, 30, 3, 20, false, false, st().diffDelCell, noMark(), []hitSpan{{start: 40, end: 46}})
	if sHit == sPlain {
		t.Fatal("scrollCell must paint a hit inside the scrolled window")
	}

	disp, emph, cls := sanitizeCell("aaaa bbbb", nil, nil)
	segs := wrapCells(disp, emph, cls, 5)
	segPlain := segCell(1, segs[1], 3, 20, false, false, st().diffAddCell, noMark(), nil)
	segHit := segCell(1, segs[1], 3, 20, false, false, st().diffAddCell, noMark(), []hitSpan{{start: 5, end: 9, cur: true}})
	if segHit == segPlain {
		t.Fatal("segCell must paint a hit that lands on a wrap continuation")
	}
}
