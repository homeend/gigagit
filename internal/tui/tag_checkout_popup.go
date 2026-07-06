package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// tagCheckoutPopup collects a new branch name to create at a tag and switch to.
type tagCheckoutPopup struct {
	popupMax
	tag  string
	name textfield
}

func (p *tagCheckoutPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.Checkout{Ref: p.tag, Branch: p.name.Value()}
		m = m.popLayer()
		return m.startOp(op)
	case tea.KeySpace:
		// Branch names cannot contain spaces — drop it.
	default:
		p.name.HandleEditKey(msg)
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
	w, _ := m.overlayDims()
	b.WriteString(viewField("name: ", p.name, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] name  [enter] checkout  [esc] cancel")
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
