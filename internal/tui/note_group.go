package tui

import "github.com/charmbracelet/lipgloss"

// groupBarStyle is the style a slot's bar (domain.GroupSlot) is painted
// with (false for 0).
func groupBarStyle(slot int) (lipgloss.Style, bool) {
	if slot < 1 || slot > 6 {
		return lipgloss.Style{}, false
	}
	return st().noteGroups[slot-1], true
}
