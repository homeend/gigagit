package tui

// Test helpers for the overlay-stack migration: the bookmark switcher and paste
// popups live on the overlay stack now (no Model field). bookmarkSwitcher() is
// the prod accessor; bookmarkPasteOf returns the paste popup when it is on top.

func bookmarkPasteOf(m Model) *bookmarkPastePopup {
	if p, ok := m.topLayer().(*bookmarkPastePopup); ok {
		return p
	}
	return nil
}

func shelfRestoreOf(m Model) *shelfRestorePopup {
	if p, ok := m.topLayer().(*shelfRestorePopup); ok {
		return p
	}
	return nil
}
