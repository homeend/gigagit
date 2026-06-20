package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestRenderWindowCutoff(t *testing.T) {
	rows := []winRow{
		{text: "short"},
		{text: "this row is far too long to fit in the narrow box"},
		{text: "tail"},
	}
	out := renderWindow(rows, winOpts{w: 10, h: 3, mode: modeCutoff, anchor: 0})
	if len(out) != 3 {
		t.Fatalf("want 3 lines, got %d", len(out))
	}
	for i, l := range out {
		if w := ansi.StringWidth(l); w != 10 {
			t.Errorf("line %d width = %d, want 10", i, w)
		}
	}
	if got := ansi.Strip(out[1]); got != "this row …" {
		t.Errorf("row 1 = %q, want %q", got, "this row …")
	}
}

func TestRenderWindowWrap(t *testing.T) {
	rows := []winRow{{text: "aaaaaabbbbbb"}}
	out := renderWindow(rows, winOpts{w: 6, h: 3, mode: modeWrap, anchor: 0})
	if len(out) != 3 {
		t.Fatalf("want 3 lines, got %d", len(out))
	}
	if got := ansi.Strip(out[0]); got != "aaaaaa" {
		t.Errorf("line 0 = %q, want %q", got, "aaaaaa")
	}
	if got := ansi.Strip(out[1]); got != "bbbbbb" {
		t.Errorf("line 1 = %q, want %q", got, "bbbbbb")
	}
	if got := ansi.Strip(out[2]); got != "      " {
		t.Errorf("line 2 = %q, want 6 spaces", got)
	}
}

func TestRenderWindowWrapStylesAllSegments(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	rows := []winRow{{text: "aaaaaabbbbbb", style: selectedRow}}
	out := renderWindow(rows, winOpts{w: 6, h: 2, mode: modeWrap, anchor: 0})
	for i, l := range out {
		if !strings.Contains(l, "\x1b[") {
			t.Errorf("wrapped line %d not styled: %q", i, l)
		}
	}
}

func TestRenderWindowScroll(t *testing.T) {
	rows := []winRow{{text: "0123456789ABCDEF"}}
	out := renderWindow(rows, winOpts{w: 5, h: 1, mode: modeScroll, anchor: 0})
	if got := ansi.Strip(out[0]); got != "01234" {
		t.Errorf("hscroll 0 = %q, want %q", got, "01234")
	}
	out = renderWindow(rows, winOpts{w: 5, h: 1, mode: modeScroll, anchor: 0, hscroll: 5})
	if got := ansi.Strip(out[0]); got != "56789" {
		t.Errorf("hscroll 5 = %q, want %q", got, "56789")
	}
}

func TestRenderWindowAnchorVisible(t *testing.T) {
	// 10 rows, a 3-high window anchored on row 8 must include row 8.
	rows := make([]winRow, 10)
	for i := range rows {
		rows[i] = winRow{text: string(rune('a' + i))}
	}
	out := renderWindow(rows, winOpts{w: 4, h: 3, mode: modeCutoff, anchor: 8})
	joined := strings.Join(out, "\n")
	if !strings.Contains(ansi.Strip(joined), "i") { // 'a'+8 == 'i'
		t.Errorf("anchor row not visible:\n%s", joined)
	}
}

func TestRenderWindowDecorateReceivesGeometryAndPreservesWidth(t *testing.T) {
	var got []struct{ hs, vl int; w int }
	deco := func(visible string, hscroll, visualLine int) string {
		got = append(got, struct{ hs, vl, w int }{hscroll, visualLine, lipgloss.Width(visible)})
		return visible // identity: must not change visible width
	}
	rows := []winRow{{text: "abcdefghij", decorate: deco}}
	// cutoff: hscroll 0, single visual line 0, width == w
	out := renderWindow(rows, winOpts{w: 6, h: 1, mode: modeCutoff})
	if len(got) != 1 || got[0].hs != 0 || got[0].vl != 0 || got[0].w != 6 {
		t.Fatalf("cutoff geometry = %+v", got)
	}
	if lipgloss.Width(out[0]) != 6 {
		t.Fatalf("cutoff line width = %d, want 6", lipgloss.Width(out[0]))
	}
	// scroll: decorator sees the scroll offset.
	got = nil
	renderWindow(rows, winOpts{w: 6, h: 1, mode: modeScroll, hscroll: 3})
	if len(got) != 1 || got[0].hs != 3 {
		t.Fatalf("scroll hscroll = %+v, want hs=3", got)
	}
	// wrap: a long row yields a continuation line with visualLine 1.
	got = nil
	renderWindow(rows, winOpts{w: 6, h: 4, mode: modeWrap})
	if len(got) < 2 || got[0].vl != 0 || got[1].vl != 1 {
		t.Fatalf("wrap visualLine sequence = %+v", got)
	}
}
