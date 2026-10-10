package tui

import "github.com/homeend/gigagit/internal/i18n"

// setReviewStackedPref flips the review split's stacked mode for the session
// and remembers it (machine-local, promptstate). A store that cannot be
// written keeps the session flag and says so in the status line.
func (m Model) setReviewStackedPref(on bool) Model {
	m.reviewStacked = on
	if m.promptStore != nil {
		if err := m.promptStore.SetStackedReviewFiles(on); err != nil {
			m.statusMsg = i18n.T("could not remember the stacked-files choice: %s", err.Error())
		}
	}
	return m
}
