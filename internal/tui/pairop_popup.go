package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pairOpPopup offers a panel's two-argument operations on (marked, selected).
type pairOpPopup struct {
	marked, selected string
	ops              []pairOp
	sel              int
	mode             dispMode // text display mode; z cycles (cutoff default)
	hscroll          int      // modeScroll horizontal offset
}

// updatePairPopupKey handles one key while the pair-op popup is open. The
// popup swallows every key; ctrl+c still quits.
func (m Model) updatePairPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.pairPopup
	switch msg.String() {
	case "z": // cycle the text display mode (cutoff / wrap / scroll)
		p.mode = p.mode.next()
		p.hscroll = 0
	case "shift+left":
		if p.mode == modeScroll && p.hscroll > 0 {
			if p.hscroll -= m.hscrollStep(); p.hscroll < 0 {
				p.hscroll = 0
			}
		}
	case "shift+right":
		if p.mode == modeScroll {
			p.hscroll += m.hscrollStep()
		}
	case "esc":
		m.pairPopup = nil // the mark survives: the user may pick another row
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.ops)-1 {
			p.sel++
		}
	case "enter":
		op := p.ops[p.sel]
		if !op.enabled {
			m.statusMsg = op.label(p.marked, p.selected) + ": " + op.note
			return m, nil
		}
		marked, selected := p.marked, p.selected
		m.pairPopup = nil
		m.mark = nil
		if op.open != nil {
			return op.open(m, marked, selected)
		}
		return m.startOp(op.build(marked, selected))
	}
	return m, nil
}

// renderPairOpPopup draws the operation picker.
func (m Model) renderPairOpPopup() string {
	p := m.pairPopup
	w, _ := m.overlayDims()
	inner := popupInnerWidth(w)
	wr := make([]winRow, len(p.ops))
	for i, op := range p.ops {
		line := op.label(p.marked, p.selected)
		if !op.enabled {
			line += "  (" + op.note + ")"
		}
		prefix := "  "
		var st lipgloss.Style
		if i == p.sel {
			prefix, st = "> ", selectedRow
		}
		wr[i] = winRow{text: prefix + line, style: st}
	}
	body := renderWindow(wr, winOpts{w: inner, h: len(p.ops), mode: p.mode, anchor: p.sel, hscroll: p.hscroll})
	parts := []string{p.marked + " + " + p.selected, ""}
	parts = append(parts, body...)
	parts = append(parts, "", "[↑/↓] choose  [enter] run  [z] mode  [esc] cancel")
	return modalStyle.Width(inner).Render(strings.Join(parts, "\n")) + "\n"
}
