package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// previewSymmetricSideRows are the . menu's two extra rows on a symmetric
// merge preview row: open either side as the ordinary merge preview it is
// made of (A into the base, B into the base).
func (m Model) previewSymmetricSideRows() []actionRow {
	if m.focus != panelPreviews || !m.opsIdle() {
		return nil
	}
	r, ok := m.selectedPreview()
	if !ok || r.sym == nil {
		return nil
	}
	s := *r.sym
	side := func(id, source string) actionRow {
		return actionRow{
			id:    id,
			label: i18n.T("Open merge preview %s → %s", source, s.Base),
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m, m.openPreviewCmd("", source, s.Base, "")
			},
		}
	}
	return []actionRow{side("preview-sym-a", s.A), side("preview-sym-b", s.B)}
}
