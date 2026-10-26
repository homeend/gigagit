package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// panBy moves a scroll-mode horizontal offset one step toward dir (−1 left,
// +1 right): never below column 0 and, for a limit ≥ 0, never past limit —
// so ← always gets back to the first column, and → never pans into blank
// columns that would take as many ← presses to come back from. It reports
// whether the offset moved.
func panBy(hs *int, dir, step, limit int) bool {
	old := *hs
	n := old + dir*step
	if limit >= 0 && n > limit {
		n = limit
	}
	n = max(n, 0)
	*hs = n
	return n != old
}

// panPreview pans a file's preview or viewer one step in scroll mode (the
// only mode with columns off screen to reach) and reports whether it moved.
// The stop is the widest line's end at the right edge of the text the last
// frame drew.
func (m Model) panPreview(p *contentPopup, dir int) bool {
	if p.mode != modeScroll {
		return false
	}
	limit := -1
	if p.panW > 0 && dir > 0 {
		w := p.widest.of(len(p.lines), func(i int) string { return p.lines[i].text }, func() any { return &p.lines[0] })
		limit = max(w-p.panW, 0)
	}
	return panBy(&p.hscroll, dir, m.hscrollStep(), limit)
}

// widestCache is a document's widest line width, measured once: re-measured
// on every → it cost a frame per press on a big file. It is valid while the
// document is the same slice (same length, same first element).
type widestCache struct {
	n     int
	first any
	w     int
}

// of returns the widest of n lines (line(i) is the i-th's text), measuring
// only when the slice changed; first returns the slice's first element's
// address (called only when n > 0).
func (c *widestCache) of(n int, line func(int) string, first func() any) int {
	if n == 0 {
		return 0
	}
	if f := first(); c.n != n || c.first != f {
		w := 0
		for i := 0; i < n; i++ {
			w = max(w, lipgloss.Width(line(i)))
		}
		*c = widestCache{n: n, first: f, w: w}
	}
	return c.w
}

// pan pans the blame's code column one step in scroll mode (the gutter stays
// put) and reports whether it moved; the stop is the widest line's end at
// the right edge.
func (b *blameView) pan(m Model, dir int) bool {
	if b.mode != modeScroll {
		return false
	}
	limit := -1
	if dir > 0 {
		b.ensureSan()
		w := b.widest.of(len(b.san), func(i int) string { return b.san[i] }, func() any { return &b.san[0] })
		limit = max(w-b.codeWidth(m), 0)
	}
	return panBy(&b.hscroll, dir, m.hscrollStep(), limit)
}

// panHint ends hint with the ←/→ pan key when the whole line still fits in
// w columns: last, and only then, so it never cuts a key the hint already
// teaches (the line is truncated to the window as one string).
func panHint(hint string, w int) string {
	if t := hint + "  " + i18n.T("[←/→] pan"); lipgloss.Width(t) <= w {
		return t
	}
	return hint
}
