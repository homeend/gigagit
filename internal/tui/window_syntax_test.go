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
// same rule TestCursorMarkerRowPaintsOnlyCursorRows follows).

// goFuncCls is the class mask for "func main() {": `func` a keyword, `main` a
// function name, everything else plain.
func goFuncCls() []syntax.Class {
	cls := make([]syntax.Class, len([]rune("func main() {")))
	for i := 0; i < 4; i++ {
		cls[i] = syntax.Keyword
	}
	for i := 5; i < 9; i++ {
		cls[i] = syntax.Func
	}
	return cls
}

func kwSeq() string { return "38;5;" + st().syntaxColor(syntax.Keyword) }
func fnSeq() string { return "38;5;" + st().syntaxColor(syntax.Func) }

// An all-Plain mask under the zero style must render byte-identically to the
// nil-cls path: every row whose file has no lexer (or whose line carries no
// runs) goes through the coloured branch and may not change a pixel.
func TestRenderWindowPlainClsIsByteIdenticalToNilCls(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	const text = "    return foo(bar) and a good deal more text besides"
	plain := make([]syntax.Class, len([]rune(text)))
	for _, mode := range []dispMode{modeCutoff, modeWrap, modeScroll} {
		for _, pw := range []int{0, 5} {
			o := winOpts{w: 20, h: 4, mode: mode, anchor: 0, hscroll: 3, prefixW: pw}
			want := renderWindow([]winRow{{text: text, prefix: "gut│"}}, o)
			got := renderWindow([]winRow{{text: text, prefix: "gut│", cls: plain}}, o)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("mode %d prefixW %d: plain mask changed the render\n got %q\nwant %q", mode, pw, got, want)
			}
		}
	}
}

func TestRenderWindowClsColoursCutoff(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out := renderWindow([]winRow{{text: "func main() {", cls: goFuncCls()}}, winOpts{w: 20, h: 1, mode: modeCutoff, anchor: 0})
	if got := ansi.Strip(out[0]); got != "func main() {       " {
		t.Fatalf("text/padding changed: %q", got)
	}
	if w := ansi.StringWidth(out[0]); w != 20 {
		t.Errorf("visible width = %d, want 20", w)
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("`func` should wear the keyword colour: %q", out[0])
	}
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should wear the func colour: %q", out[0])
	}
	// The parenthesis/brace tail is plain: no colour sequence may open on it.
	if strings.Contains(out[0], kwSeq()+"m(") || strings.Contains(out[0], fnSeq()+"m(") {
		t.Errorf("the plain tail must not be coloured: %q", out[0])
	}
}

// truncate keeps a prefix and appends "…" after it (it does not replace a
// kept rune), so the mask's unfixed slice lands the ellipsis on the class
// slot of the first DROPPED rune — the one right after the kept prefix, at
// index len(kept). `func` is Keyword and cut happens inside `main` (Func),
// so the run "func" wears has already ended well before the cut, and this
// test isn't about that run at all: the first-dropped rune ('i' of "main")
// only carries a colour worth catching a bug on because `main` itself is
// coloured too — a mask that only colours "func" could never distinguish
// the bug here, since the first-dropped rune would already read Plain
// either way. With `main` also coloured, an unfixed ellipsis fuses into
// that Func run with no reset before it, while a fixed one always renders
// after a reset, outside any `38;5;` run.
func TestRenderWindowClsCutoffEllipsisIsPlain(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	// bodyW=8 keeps "func ma" and cuts inside "main"; the first-dropped rune
	// ('i') falls inside main's Func-coloured run (positions 5-8).
	out := renderWindow([]winRow{{text: "func main() {", cls: goFuncCls()}}, winOpts{w: 8, h: 1, mode: modeCutoff, anchor: 0})
	if got := ansi.Strip(out[0]); got != "func ma…" {
		t.Fatalf("cutoff = %q, want %q", got, "func ma…")
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("`func` should stay coloured: %q", out[0])
	}
	if !strings.Contains(out[0], fnSeq()+"mma") {
		t.Errorf("the kept part of `main` should stay coloured: %q", out[0])
	}
	if strings.Contains(out[0], fnSeq()+"mma…") {
		t.Errorf("the ellipsis must not fuse into main's colour run: %q", out[0])
	}
	if !strings.Contains(out[0], "\x1b[0m…") {
		t.Errorf("the ellipsis must render right after a reset, outside any coloured run: %q", out[0])
	}
}

func TestRenderWindowClsColoursScrolledSegment(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out := renderWindow([]winRow{{text: "func main() {", cls: goFuncCls()}}, winOpts{w: 8, h: 1, mode: modeScroll, anchor: 0, hscroll: 5})
	if got := ansi.Strip(out[0]); got != "main() {" {
		t.Fatalf("scrolled text = %q, want %q", got, "main() {")
	}
	if !strings.Contains(out[0], fnSeq()+"mmain") {
		t.Errorf("`main` should stay coloured after the scroll: %q", out[0])
	}
	if strings.Contains(out[0], kwSeq()) {
		t.Errorf("the scrolled-off keyword must not colour anything: %q", out[0])
	}
}

// A run that lands on a wrap continuation is coloured there, and the hang
// indent the continuation carries stays plain.
func TestRenderWindowClsColoursWrapContinuation(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	const text = "    return foo(bar)" // leading spaces ⇒ a real hang indent
	cls := make([]syntax.Class, len([]rune(text)))
	for i := 4; i < 10; i++ { // return
		cls[i] = syntax.Keyword
	}
	for i := 11; i < 14; i++ { // foo
		cls[i] = syntax.Func
	}
	// charWrap: this is a CODE line, the column-exact wrap a code view asks for.
	out := renderWindow([]winRow{{text: text, cls: cls}}, winOpts{w: 12, h: 2, mode: modeWrap, anchor: 0, charWrap: true})
	if got := ansi.Strip(out[0]); got != "    return f" {
		t.Fatalf("line 0 = %q", got)
	}
	if got := ansi.Strip(out[1]); got != "    oo(bar) " {
		t.Fatalf("line 1 = %q", got)
	}
	if !strings.Contains(out[0], kwSeq()+"mreturn") {
		t.Errorf("line 0 should colour `return`: %q", out[0])
	}
	if !strings.Contains(out[1], fnSeq()+"moo") {
		t.Errorf("line 1 should colour `oo` (the tail of foo): %q", out[1])
	}
	if !strings.HasPrefix(out[1], "    ") {
		t.Errorf("the hang indent must be plain: %q", out[1])
	}
}

// The blame shape: a frozen gutter prefix stays under the row style while the
// body is coloured, and the gutter is blank (still plain) on continuations.
func TestRenderWindowClsWithPrefixColoursBodyOnly(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out := renderWindow([]winRow{{prefix: "abc│", text: "func main() {", cls: goFuncCls()}},
		winOpts{w: 20, h: 1, mode: modeCutoff, anchor: 0, prefixW: 4})
	if got := ansi.Strip(out[0]); got != "abc│func main() {   " {
		t.Fatalf("prefixed row = %q", got)
	}
	if w := ansi.StringWidth(out[0]); w != 20 {
		t.Errorf("visible width = %d, want 20", w)
	}
	if !strings.HasPrefix(out[0], "abc│") {
		t.Errorf("the gutter must render plain under the row style: %q", out[0])
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("the body should be coloured: %q", out[0])
	}
}

// cls and decorate are mutually exclusive: cls wins and decorate is not called.
func TestRenderWindowClsWinsOverDecorate(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	called := false
	deco := func(visible string, _, _ int) string {
		called = true
		return visible
	}
	out := renderWindow([]winRow{{text: "func main() {", cls: goFuncCls(), decorate: deco}},
		winOpts{w: 20, h: 1, mode: modeCutoff, anchor: 0})
	if called {
		t.Error("decorate must not be called for a row carrying a class mask")
	}
	if !strings.Contains(out[0], kwSeq()+"mfunc") {
		t.Errorf("the row should still be coloured: %q", out[0])
	}
}

// st().selectedRow is Reverse(true), which swaps foreground and background: a
// per-token foreground would become a per-token BACKGROUND (a patchwork of
// coloured blocks). Such a row keeps today's plain reverse-video render.
func TestRenderWindowClsSkippedUnderReverseRowStyle(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	o := winOpts{w: 20, h: 1, mode: modeCutoff, anchor: 0}
	want := renderWindow([]winRow{{text: "func main() {", style: st().selectedRow}}, o)
	got := renderWindow([]winRow{{text: "func main() {", style: st().selectedRow, cls: goFuncCls()}}, o)
	if got[0] != want[0] {
		t.Errorf("a reverse-video row must render exactly as it does today\n got %q\nwant %q", got[0], want[0])
	}
	if !strings.Contains(got[0], "\x1b[7m") {
		t.Errorf("the selected row must keep its reverse-video highlight: %q", got[0])
	}
}

// A background-based row style (the diff cursor's shape) DOES compose: the
// band survives on both sides of a coloured run.
func TestRenderWindowClsKeepsBackgroundRowStyle(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	out := renderWindow([]winRow{{text: "func main() {", style: st().messageBlock, cls: goFuncCls()}},
		winOpts{w: 20, h: 1, mode: modeCutoff, anchor: 0})
	if got := ansi.Strip(out[0]); got != "func main() {       " {
		t.Fatalf("text changed: %q", got)
	}
	bg := "48;5;" + string(st().messageBlock.GetBackground().(lipgloss.Color))
	if n := strings.Count(out[0], bg); n < 2 {
		t.Errorf("the row background must survive across the coloured runs (%d occurrences): %q", n, out[0])
	}
	if !strings.Contains(out[0], kwSeq()) {
		t.Errorf("the keyword colour must still be applied: %q", out[0])
	}
}
