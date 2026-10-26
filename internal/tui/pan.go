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
		w := 0
		for _, l := range p.lines {
			w = max(w, lipgloss.Width(l.text))
		}
		limit = max(w-p.panW, 0)
	}
	return panBy(&p.hscroll, dir, m.hscrollStep(), limit)
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
		w := 0
		for _, s := range b.san {
			w = max(w, lipgloss.Width(s))
		}
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
