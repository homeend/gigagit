package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// previewRenamePopup relabels one saved preview. The pair itself is the
// record's identity and never changes here — only the human name does.
type previewRenamePopup struct {
	popupMax
	id    string
	kind  previewRowKind // which store surface renames it: a pair never goes through PreviewRename
	label textfield
}

// update handles one key while the popup is open. It swallows every key;
// ctrl+c still quits so the user is never trapped.
func (p *previewRenamePopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEsc:
		return m.popLayer(), nil
	case tea.KeyEnter:
		v := strings.TrimSpace(p.label.Value())
		m = m.popLayer()
		if v == "" {
			return m, nil // an empty label would erase the row's only name
		}
		return m, m.previewRenameCmd(p.kind, p.id, v)
	default:
		p.label.HandleEditKey(msg)
	}
	return m, nil
}

// render composites the rename dialog over the layer beneath.
func (p *previewRenamePopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	return overlayCenter(clipToHeight(below, h), p.box(m), w, h)
}

// box draws the rename dialog (modal box only).
func (p *previewRenamePopup) box(m Model) string {
	w, _ := m.overlayDims()
	var b strings.Builder
	b.WriteString(i18n.T("Rename preview") + "\n\n")
	b.WriteString(viewField(i18n.T("label: "), p.label, true, popupContentWidth(w)) + "\n\n")
	b.WriteString(i18n.T("[enter] rename  [esc] cancel"))
	return st().modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
}
