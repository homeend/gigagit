package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// handleCommitSpaceKey is the space gesture on the Commits panel: a fast path
// over the same ◉ compare selection the m key toggles (commitCompareSet).
// Space toggles the cursor row's membership, refuses to grow the set past two
// marks, and opens the comparison the moment the second mark lands. WIP
// pseudo-rows (◇ Working tree / ◇ Staged) participate exactly as with m.
func (m Model) handleCommitSpaceKey() (tea.Model, tea.Cmd) {
	if !m.opsIdle() { // match the m key's canMark gate: no set edits mid-op
		return m, nil
	}
	key, ok := m.selectedKey(panelCommits)
	if !ok {
		return m, nil
	}
	if m.commitCompareSet[key] { // marked row: space always toggles off
		delete(m.commitCompareSet, key)
		return m, nil
	}
	// Count only valid marks: the set is stale-tolerant (keys survive scope
	// changes and history rewrites), and a ghost mark must not eat a slot.
	valid := len(m.validCompareKeys())
	if valid >= 2 {
		m.statusMsg = i18n.T("2 commits already marked — space a marked one to unmark, esc to unmark all")
		return m, nil
	}
	if m.commitCompareSet == nil {
		m.commitCompareSet = map[string]bool{}
	}
	m.commitCompareSet[key] = true
	if valid == 0 {
		return m, nil
	}
	// Second mark: open the comparison immediately, resolving endpoints the
	// same way the .-menu Compare row does. Marks persist so esc returns to
	// the Commits panel with both ◉ still set.
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		m.statusMsg = note
		return m, nil
	}
	return m.openCompareFiles(left, right)
}
