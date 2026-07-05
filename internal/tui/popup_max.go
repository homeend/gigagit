package tui

import tea "github.com/charmbracelet/bubbletea"

// popupMax is embedded by any popup that supports T-to-fullscreen. The layer
// stack holds the popup *pointer*, so the flag persists across Model value
// copies (same rationale as the modal/popup pointer fields). maximized is
// transient view state: it resets when the popup instance is dropped.
type popupMax struct{ maximized bool }

// maxed reports whether the popup is currently maximized.
func (p *popupMax) maxed() bool { return p.maximized }

// handleMaxKey toggles maximize on "T" and reports whether it consumed the key.
// Opted-in popups call this at the top of their NAVIGATION branch so that "T"
// stays a literal character while a filter / text field is capturing.
func (p *popupMax) handleMaxKey(msg tea.KeyMsg) bool {
	if msg.String() == "T" {
		p.maximized = !p.maximized
		return true
	}
	return false
}

// popupResolveWidth returns the near-fullscreen inner width when maximized,
// else the popup's normal width. popupFullInnerWidth (w-8) is the same width
// the external-tools wizard renders at permanently.
func popupResolveWidth(w int, maximized bool, normal int) int {
	if maximized {
		return popupFullInnerWidth(w)
	}
	return normal
}

// popupMaxRowCap is the visible-row budget for a maximized list popup whose
// normal budget is a small fixed constant: terminal height minus box chrome,
// floored so a tiny terminal still shows a few rows. Mirrors gitConfigPopup's
// existing capRows (termH - 12).
func popupMaxRowCap(termH int) int {
	n := termH - 12
	if n < 3 {
		n = 3
	}
	return n
}
