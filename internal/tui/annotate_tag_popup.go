package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// annotateTagPopup edits the message for an existing tag, force-recreating it
// as annotated at its current target. Prefilled with the tag's subject.
type annotateTagPopup struct {
	popupMax
	tag     string    // the tag being annotated (fixed)
	target  string    // its current commit, preserved
	message textfield // prefilled with the tag's current subject
}

// openAnnotateTagPopup opens the dialog for the selected Tags-panel row.
func (m Model) openAnnotateTagPopup() (Model, bool) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, false
	}
	t := m.tags[bi]
	m = m.pushLayer(&annotateTagPopup{tag: t.Name, target: t.Target, message: newTextField(t.Subject)})
	return m, true
}

func (p *annotateTagPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		if p.message.Value() == "" { // annotate requires a message
			return m, nil // keep the popup open
		}
		op := engine.CreateTag{Name: p.tag, Commit: p.target, Message: p.message.Value(), Force: true}
		m = m.popLayer()
		return m.startOp(op)
	default:
		p.message.HandleEditKey(msg) // the message allows spaces
	}
	return m, nil
}

func (p *annotateTagPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

func (p *annotateTagPopup) box(m Model) string {
	var b strings.Builder
	b.WriteString(i18n.T("Annotate tag %s", p.tag) + "\n\n")
	w, _ := m.overlayDims()
	b.WriteString(viewField(i18n.T("message: "), p.message, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[type] message  [enter] annotate  [esc] cancel"))
	return modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
