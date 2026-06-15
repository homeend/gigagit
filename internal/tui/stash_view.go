package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

// stashView is the stash list, rendered in the right column (over Commits).
type stashView struct {
	entries []model.StashEntry
	sel     int
	loading bool
	err     error
	tag     string // gates stale loads
}

type stashListMsg struct {
	tag     string
	entries []model.StashEntry
	err     error
}

func (m Model) loadStashListCmd(tag string) tea.Cmd {
	svc := m.svc
	return func() tea.Msg {
		es, err := svc.StashList(context.Background())
		return stashListMsg{tag: tag, entries: es, err: err}
	}
}

// renderStashList renders the stash entries as a bordered right-column box of
// boxW×boxH, mirroring renderPanel's framing. It is focused (owns highlight)
// while no file tree is open over it.
func (m Model) renderStashList(boxW, boxH int) string {
	v := m.stashView
	rows := make([]string, len(v.entries))
	for i, e := range v.entries {
		rows[i] = e.Ref + "  " + e.Subject
	}
	switch {
	case v.loading:
		rows = []string{"(loading…)"}
	case v.err != nil:
		rows = []string{"error: " + v.err.Error()}
	case len(v.entries) == 0:
		rows = []string{"(no stashes)"}
	}
	return m.renderListBox("Stashes", rows, v.sel, boxW, boxH, m.filesView == nil)
}

// openStashView opens (or refreshes) the stash list window.
func (m Model) openStashView() (Model, tea.Cmd) {
	m.stashView = &stashView{loading: true, tag: "stash"}
	return m, m.loadStashListCmd(m.stashView.tag)
}
