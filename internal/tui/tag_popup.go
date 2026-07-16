package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// tagPopup is the create-tag dialog at a commit. An empty message creates a
// lightweight tag; a non-empty one an annotated tag. tab toggles the field.
type tagPopup struct {
	popupMax
	commit  string // full SHA the tag points at
	name    textfield
	message textfield
	onMsg   bool // false = editing name, true = editing message
}

func (p *tagPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyTab:
		p.onMsg = !p.onMsg
		return m, nil
	case tea.KeyEnter:
		if p.name.Value() == "" {
			return m, nil
		}
		op := engine.CreateTag{Name: p.name.Value(), Commit: p.commit, Message: p.message.Value()}
		m = m.popLayer()
		return m.startOp(op)
	default:
		if p.onMsg {
			p.message.HandleEditKey(msg) // the annotated message allows spaces
		} else if msg.Type != tea.KeySpace {
			p.name.HandleEditKey(msg) // tag names cannot contain spaces
		}
	}
	return m, nil
}

func (p *tagPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *tagPopup) box(m Model) string {
	nameMark, msgMark := "> ", "  "
	if p.onMsg {
		nameMark, msgMark = "  ", "> "
	}
	w, _ := m.overlayDims()
	cw := popupContentWidth(w)
	var b strings.Builder
	b.WriteString(i18n.T("Create tag at %s", displayStart(p.commit)) + "\n\n")
	b.WriteString(viewField(nameMark+i18n.T("name:    "), p.name, !p.onMsg, cw) + "\n")
	b.WriteString(viewField(msgMark+i18n.T("message: "), p.message, p.onMsg, cw) + "\n")
	b.WriteString(strings.Repeat(" ", 11) + i18n.T("(empty message = lightweight tag)") + "\n\n")
	b.WriteString(i18n.T("[tab] field  [enter] create  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
