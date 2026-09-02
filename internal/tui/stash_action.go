package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// stashActionRows offers Apply / Pop / Drop on the selected stash for the
// .-menu while the stash-list side owns the selection (with or without the
// file tree open over it). Apply and Pop start their op directly; Drop
// confirms first via a Drop/Cancel modal.
func (m Model) stashActionRows() []actionRow {
	v := m.stashView
	if v == nil {
		return nil
	}
	e, ok := v.current() // honours the `/` filter: the cursor indexes the visible list
	if !ok {
		return nil
	}
	return []actionRow{
		{id: "stash-apply", label: i18n.T("Apply stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.StashApply{Ref: e.Ref})
		}},
		{id: "stash-pop", label: i18n.T("Pop stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.StashPop{Ref: e.Ref})
		}},
		{id: "stash-drop", label: i18n.T("Drop stash"), run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "stash-drop",
					Prompt:  i18n.T("Drop %s?", e.Ref),
					Options: []string{"Drop", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Drop" {
						return m.startOp(engine.StashDrop{Ref: e.Ref})
					}
					return m, nil
				},
			}
			return m, nil
		}},
	}
}
