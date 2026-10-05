package tui

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/homeend/gigagit/internal/i18n"
)

// reviewTally is a review row's "12 remarks · 4 resolved" — plain words
// (user ruling: no check-mark glyphs) — or "4/12 resolved" when room columns
// cannot hold the long form; "" for a review with no remarks.
func reviewTally(remarks, resolved, room int) string {
	if remarks == 0 {
		return ""
	}
	long := i18n.T("%d remarks · %d resolved", remarks, resolved)
	if remarks == 1 {
		long = i18n.T("1 remark · %d resolved", resolved)
	}
	if lipgloss.Width(long) <= room {
		return long
	}
	return i18n.T("%d/%d resolved", resolved, remarks)
}

// withTally appends a review's tally to its row head, when it has remarks;
// width is the row's room (the short form when the long one does not fit).
func withTally(head string, remarks, resolved, width int) string {
	t := reviewTally(remarks, resolved, width-lipgloss.Width(head)-3)
	if t == "" {
		return head
	}
	return head + " · " + t
}

// Rows laid out without a known width: the Files-view and working-tree
// review rows and View all notes take the long form, the Branches sub-row
// (a narrow column) the short one.
const (
	tallyWide   = 1 << 16
	tallyNarrow = 0
)
