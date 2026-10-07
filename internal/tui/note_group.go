package tui

import (
	"hash/fnv"

	"github.com/charmbracelet/lipgloss"
)

// groupSlot is a note group's colour slot (spec 2026-10-07 §1.3): 0 for no
// group, else 1–6 from the group id's FNV-1a hash — stable across runs and
// processes, so a review and the GitHub review it became share a colour.
func groupSlot(group string) int {
	if group == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(group))
	return int(h.Sum32()%6) + 1
}

// groupSlotMine pins "my draft review"'s slot: a hash change would recolour
// every user's groups, and the test catches it.
const groupSlotMine = 5

// groupBarStyle is the style a slot's bar is painted with (false for 0).
func groupBarStyle(slot int) (lipgloss.Style, bool) {
	if slot < 1 || slot > 6 {
		return lipgloss.Style{}, false
	}
	return st().noteGroups[slot-1], true
}
