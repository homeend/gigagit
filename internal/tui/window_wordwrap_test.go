package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/homeend/gigagit/internal/syntax"
)

func TestWrapHangWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, s string
		w, ind  int
		want    []string
	}{
		{"fits", "short line", 20, 0, []string{"short line"}},
		{"breaks at spaces", "the quick brown fox jumps", 10, 0, []string{"the quick ", "brown fox ", "jumps"}},
		{"long word is split", "see https://example.com/a/very/long/path ok", 16, 0, []string{"see ", "https://example.", "com/a/very/long/", "path ok"}},
		{"hang under a bullet", "  • alpha beta gamma delta", 14, 4, []string{"  • alpha ", "    beta gamma ", "    delta"}[:0]},
		{"no continuation starts with a space", "ab cde fghij kl", 6, 0, []string{"ab ", "cde ", "fghij ", "kl"}},
	}
	for _, c := range cases {
		got := wrapHangWords(c.s, c.w, c.ind)
		// Invariants for every case: segments rebuild the text verbatim
		// (continuations behind their pad) and none is wider than the line.
		var joined strings.Builder
		for i, seg := range got {
			if lipgloss.Width(seg) > c.w {
				t.Errorf("%s: segment %q wider than %d", c.name, seg, c.w)
			}
			if i > 0 {
				if !strings.HasPrefix(seg, strings.Repeat(" ", c.ind)) {
					t.Errorf("%s: continuation %q lost its hang", c.name, seg)
				}
				seg = seg[c.ind:]
			}
			joined.WriteString(seg)
		}
		if joined.String() != c.s {
			t.Errorf("%s: segments rebuild %q, want %q", c.name, joined.String(), c.s)
		}
		if len(c.want) > 0 && strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
	// A break never strands the marker on a line of its own.
	for _, seg := range wrapHangWords("  • supercalifragilistic word", 12, 4)[:1] {
		if strings.TrimSpace(seg) == "•" {
			t.Errorf("marker stranded: %q", seg)
		}
	}
	bullet := wrapHangWords("  • alpha beta gamma delta", 14, 4)
	if strings.Join(bullet, "|") != "  • alpha |    beta |    gamma |    delta" {
		t.Errorf("bullet wrap = %q", bullet)
	}
}

func TestWrapAlignIndentProse(t *testing.T) {
	t.Parallel()
	for s, want := range map[string]int{
		"1. first item":       3,
		"    12) twelfth":     8,
		"  • bullet":          4,
		"│ quoted":            2,
		"2024-01-02 a date":   0,
		"1.2 a version":       0,
		"3 files changed":     0,
		"plain prose":         0,
		"7. ":                 0,
		"  ☑ done and dusted": 4,
	} {
		if got := wrapAlignIndentProse(s, 80); got != want {
			t.Errorf("wrapAlignIndentProse(%q) = %d, want %d", s, got, want)
		}
	}
}

// The window: prose wraps on words by default, a code view (charWrap) keeps
// the column-exact wrap, and a class mask still lands on the right runes.
func TestRenderWindowWordWrap(t *testing.T) {
	t.Parallel()
	text := "1. see https://example.com/spec and then more"
	cls := make([]syntax.Class, len([]rune(text)))
	rows := []winRow{{text: text, cls: cls}}
	prose := renderWindow(rows, winOpts{w: 20, h: 4, mode: modeWrap})
	code := renderWindow(rows, winOpts{w: 20, h: 4, mode: modeWrap, charWrap: true})
	strip := func(ls []string) []string {
		out := make([]string, len(ls))
		for i, l := range ls {
			out[i] = strings.TrimRight(ansi.Strip(l), " ")
		}
		return out
	}
	p, c := strip(prose), strip(code)
	if p[0] != "1. see" || !strings.HasPrefix(p[1], "   https://example.c") || !strings.HasPrefix(p[2], "   om/spec and then") {
		t.Errorf("prose wrap = %q", p)
	}
	if c[0] != "1. see https://examp" {
		t.Errorf("a code view keeps the column wrap: %q", c)
	}
	for _, l := range prose {
		if lipgloss.Width(l) != 20 {
			t.Errorf("line %q is not padded to the window", l)
		}
	}
	o := winOpts{w: 20, h: 99, mode: modeWrap}
	if n := wrapContentLines(rows, o, 99); n != len(wrapHangWords(text, 20, 3)) {
		t.Errorf("line count %d disagrees with the layout", n)
	}
}

// A class run that lands on a WORD-wrapped continuation is coloured there, and
// a preformatted row (noWrap) is cut in wrap mode instead of reflowed.
func TestRenderWindowWordWrapMaskAndNoWrap(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.ANSI256)
	defer lipgloss.SetColorProfile(prev)

	const text = "• alpha beta gamma"
	cls := make([]syntax.Class, len([]rune(text)))
	for i := len([]rune("• alpha beta ")); i < len(cls); i++ {
		cls[i] = syntax.Keyword
	}
	out := renderWindow([]winRow{{text: text, cls: cls}}, winOpts{w: 14, h: 2, mode: modeWrap})
	if got := ansi.Strip(out[0]); got != "• alpha beta  " {
		t.Fatalf("line 0 = %q", got)
	}
	if got := ansi.Strip(out[1]); got != "  gamma       " {
		t.Fatalf("line 1 = %q", got)
	}
	if !strings.Contains(out[1], kwSeq()) || strings.Contains(out[0], kwSeq()) {
		t.Errorf("the keyword colour belongs on the continuation only:\n%q\n%q", out[0], out[1])
	}

	table := "Name  │ Count │ a rather long last column"
	rows := []winRow{{text: table, noWrap: true}, {text: "prose that is long enough to wrap around"}}
	o := winOpts{w: 20, h: 4, mode: modeWrap}
	lines := renderWindow(rows, o)
	if got := ansi.Strip(lines[0]); lipgloss.Width(got) != 20 || !strings.HasSuffix(strings.TrimRight(got, " "), "…") {
		t.Errorf("a noWrap row is cut to one line: %q", got)
	}
	if got := ansi.Strip(lines[1]); !strings.HasPrefix(got, "prose that is long") {
		t.Errorf("the row after it starts on the next line: %q", got)
	}
	if n := wrapContentLines(rows, o, 99); n != 1+len(wrapHangWords(rows[1].text, 20, 0)) {
		t.Errorf("line count %d: a noWrap row is one line", n)
	}
}
