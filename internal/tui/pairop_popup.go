package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// pairOpPopup offers a panel's two-argument operations on (marked, selected).
type pairOpPopup struct {
	marked, selected string
	ops              []pairOp
	sel              int
}

// updatePairPopupKey handles one key while the pair-op popup is open. The
// popup swallows every key; ctrl+c still quits.
func (m Model) updatePairPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.pairPopup
	switch msg.String() {
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
	var b strings.Builder
	b.WriteString(p.marked + " + " + p.selected + "\n\n")
	for i, op := range p.ops {
		line := op.label(p.marked, p.selected)
		if !op.enabled {
			line += "  (" + op.note + ")"
		}
		if i == p.sel {
			b.WriteString(selectedRow.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[↑/↓] choose  [enter] run  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
