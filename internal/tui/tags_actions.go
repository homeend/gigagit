package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// tagJumpToCommit moves the Commits cursor to the selected tag's target commit
// (matched by short-hash prefix) and focuses the Commits panel. A target that
// isn't in the loaded commit page leaves a notice (never-trap: no-op + explain).
func (m Model) tagJumpToCommit() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, nil
	}
	target := m.tags[bi].Target
	_, idx := m.panelView(panelCommits)
	for di, ci := range idx {
		if ci >= 0 && ci < len(m.commits) && strings.HasPrefix(m.commits[ci].Hash, target) {
			m.sel[panelCommits] = di
			m.focus = panelCommits
			return m, nil
		}
	}
	m.statusMsg = "tag " + m.tags[bi].Name + " target not in the loaded commits"
	return m, nil
}
