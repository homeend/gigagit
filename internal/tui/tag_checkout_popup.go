package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// tagCheckoutPopup collects a new branch name to create at a tag and switch to.
type tagCheckoutPopup struct {
	tag  string
	name string
}

func (p *tagCheckoutPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if p.name == "" {
			return m, nil
		}
		op := engine.CheckoutTag{Name: p.tag, Branch: p.name}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// Branch names cannot contain spaces.
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}

func (p *tagCheckoutPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *tagCheckoutPopup) box(m Model) string {
	var b strings.Builder
	b.WriteString("New branch at " + p.tag + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] checkout  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
