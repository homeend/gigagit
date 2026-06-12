package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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

// contentFastStep is the ctrl+↑/↓ jump; contentWheelStep the mouse-wheel tick.
const (
	contentFastStep  = 5
	contentWheelStep = 3
)

// contentPageRows is the popup's visible row capacity: terminal height minus
// chrome (double border 2, vertical padding 2, title 1, blank after title 1,
// hint line 1), floored so tiny terminals still show a few rows.
func (m Model) contentPageRows() int {
	_, h := m.overlayDims()
	n := h - 7
	if n < 3 {
		n = 3
	}
	return n
}

// updateContentPopupKey handles all keys while the viewer is open. It swallows
// everything (no fallthrough to global handlers).
func (m Model) updateContentPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := m.contentPopup
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		if p.query != "" { // first esc clears the filter, second closes
			p.query = ""
			p.sel = 0
			return m, nil
		}
		m.contentPopup = nil
		return m, nil
	case tea.KeyEnter:
		m.contentPopup = nil
		return m, nil
	case tea.KeyUp:
		p.move(-1)
	case tea.KeyDown:
		p.move(1)
	case tea.KeyCtrlUp:
		p.move(-contentFastStep)
	case tea.KeyCtrlDown:
		p.move(contentFastStep)
	case tea.KeyPgUp:
		p.move(-m.contentPageRows())
	case tea.KeyPgDown:
		p.move(m.contentPageRows())
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.query); len(r) > 0 {
			p.query = string(r[:len(r)-1])
		}
		p.sel = 0
	case tea.KeySpace:
		p.query += " "
		p.sel = 0
	case tea.KeyRunes:
		p.query += string(msg.Runes)
		p.sel = 0
	}
	return m, nil
}

// renderContentPopup draws the viewer box (composited by render via
// overlayCenter). Headings render bold, the cursor row reversed; the window
// follows the cursor via the same windowRows helper the panels use.
func (m Model) renderContentPopup() string {
	p := m.contentPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)

	vis := p.visible()
	rows := make([]string, len(vis))
	for i, l := range vis {
		switch {
		case l.heading:
			rows[i] = titleStyle.Render(truncate(l.text, inner))
		case i == p.sel:
			rows[i] = selectedRow.Render(truncate("> "+l.text, inner))
		default:
			rows[i] = truncate("  "+l.text, inner)
		}
	}
	capRows := m.contentPageRows()
	win, _ := windowRows(rows, capRows, p.sel)

	title := p.title
	if p.query != "" {
		title += "  /" + p.query
	}
	var b strings.Builder
	b.WriteString(truncate(title, inner) + "\n\n")
	if len(win) == 0 {
		b.WriteString("  (no match)\n")
	}
	for _, r := range win {
		b.WriteString(r + "\n")
	}
	hint := "[esc] close"
	if len(vis) > capRows {
		hint = fmt.Sprintf("%d/%d  %s", p.sel+1, len(vis), hint)
	}
	b.WriteString(truncate(hint, inner))
	return modalStyle.Width(inner).Render(b.String()) + "\n"
}
