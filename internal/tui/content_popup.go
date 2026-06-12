package tui

import (
	"strings"
)

// contentLine is one display line of a contentPopup. heading lines are section
// headers: the filter never matches them, and they survive filtering only
// while at least one non-heading line beneath them (before the next heading)
// matches.
type contentLine struct {
	text    string
	heading bool
}

// contentPopup is a generic read-only viewer popup: any list of lines with
// repo-popup-style type-to-filter search and cursor-driven scrolling. The
// help window is its first consumer.
type contentPopup struct {
	title string
	lines []contentLine // full, unfiltered content
	query string        // case-insensitive substring over non-heading lines
	sel   int           // cursor index into the FILTERED view
}

func newContentPopup(title string, lines []contentLine) *contentPopup {
	return &contentPopup{title: title, lines: lines}
}

// visible returns the filtered lines in display order: non-heading lines
// matching query, plus each heading that still has a matching line.
func (p *contentPopup) visible() []contentLine {
	if p.query == "" {
		return p.lines
	}
	q := strings.ToLower(p.query)
	out := make([]contentLine, 0, len(p.lines))
	var pending *contentLine // last heading not yet emitted
	for i := range p.lines {
		l := p.lines[i]
		if l.heading {
			pending = &p.lines[i]
			continue
		}
		if strings.Contains(strings.ToLower(l.text), q) {
			if pending != nil {
				out = append(out, *pending)
				pending = nil
			}
			out = append(out, l)
		}
	}
	return out
}

// move shifts the cursor by delta, clamped to the visible range.
func (p *contentPopup) move(delta int) {
	n := len(p.visible())
	p.sel += delta
	if p.sel > n-1 {
		p.sel = n - 1
	}
	if p.sel < 0 {
		p.sel = 0
	}
}
