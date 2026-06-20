package tui

// Test helpers for the overlay-stack migration: the bookmark switcher and paste
// popups live on the overlay stack now (no Model field). bookmarkSwitcher() is
// the prod accessor; bookmarkPasteOf returns the paste popup when it is on top.

func bookmarkPasteOf(m Model) *bookmarkPastePopup {
	if p, ok := m.overlayTop().(*bookmarkPastePopup); ok {
		return p
	}
	return nil
}
