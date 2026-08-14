package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// renderTwoColRef is the pre-optimization algorithm verbatim: build and style
// EVERY display line, then window. The optimized renderTwoCol must produce
// byte-identical output for every mode/anchor/shift combination.
func renderTwoColRef(rows []colRow, o twoColOpts) ([]string, int) {
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
			for _, s := range cellSegs(r.full, w, o.mode, o.hscroll) {
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
			left := styleCell(cellStyle(r.left), segOrBlank(ls, k), colW)
			right := styleCell(cellStyle(r.right), segOrBlank(rs, k), colW)
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
	eff := 0
	if o.vshift != 0 {
		maxStart := len(dl) - h
		if maxStart < 0 {
			maxStart = 0
		}
		s := start + o.vshift
		if s > maxStart {
			s = maxStart
		}
		if s < 0 {
			s = 0
		}
		eff = s - start
		start = s
	}
	out := make([]string, 0, h)
	for i := 0; i < h; i++ {
		idx := start + i
		if idx < len(dl) {
			out = append(out, dl[idx].text)
		} else {
			out = append(out, padRight("", w))
		}
	}
	return out, eff
}

// windowFixtureRows builds a deterministic mixed fixture: full-width literal
// rows, paired rows with uneven side lengths, long lines (wrap/scroll
// exercise), and empty cells.
func windowFixtureRows(n int) []colRow {
	rows := make([]colRow, 0, n)
	for i := 0; i < n; i++ {
		switch i % 4 {
		case 0:
			rows = append(rows, colRow{full: &winCell{body: fmt.Sprintf("literal %d %s", i, strings.Repeat("x", i%97)), style: pickerDim}})
		case 1:
			rows = append(rows, colRow{
				left:  &winCell{gutter: "> ", body: fmt.Sprintf("left %d %s", i, strings.Repeat("l", i%53)), style: selectedRow},
				right: &winCell{gutter: "  ", body: fmt.Sprintf("right %d", i)},
			})
		case 2:
			rows = append(rows, colRow{
				left:  &winCell{gutter: "  ", body: ""},
				right: &winCell{gutter: "  ", body: fmt.Sprintf("only right %d %s", i, strings.Repeat("r", i%71))},
			})
		default:
			rows = append(rows, colRow{
				left:  &winCell{gutter: "[x] ", body: fmt.Sprintf("pair %d", i), style: pickerFocus},
				right: nil,
			})
		}
	}
	return rows
}

func TestRenderTwoColMatchesReference(t *testing.T) {
	rows := windowFixtureRows(120)
	for _, mode := range []dispMode{modeScroll, modeCutoff, modeWrap} {
		for _, anchor := range []int{0, 1, 40, 59, 118, 119} {
			for _, vshift := range []int{0, -5, 3, 500, -500} {
				for _, hscroll := range []int{0, 8} {
					o := twoColOpts{w: 90, h: 20, sep: pickerColSep, mode: mode, hscroll: hscroll, anchor: anchor, vshift: vshift}
					got, geff := renderTwoCol(rows, o)
					want, weff := renderTwoColRef(rows, o)
					if geff != weff {
						t.Fatalf("mode=%v anchor=%d vshift=%d hscroll=%d: eff %d != ref %d", mode, anchor, vshift, hscroll, geff, weff)
					}
					for i := range want {
						if got[i] != want[i] {
							t.Fatalf("mode=%v anchor=%d vshift=%d hscroll=%d line %d:\n got %q\nwant %q", mode, anchor, vshift, hscroll, i, got[i], want[i])
						}
					}
				}
			}
		}
	}
}

func BenchmarkRenderTwoColScrollBig(b *testing.B) {
	rows := windowFixtureRows(5000)
	o := twoColOpts{w: 120, h: 40, sep: pickerColSep, mode: modeScroll, anchor: 2500}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		renderTwoCol(rows, o)
	}
}
