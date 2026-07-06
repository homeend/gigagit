package tui

// popupMax is embedded by any popup that supports T-to-fullscreen. The layer
// stack holds the popup *pointer*, so the flag persists across Model value
// copies (same rationale as the modal/popup pointer fields). maximized is
// transient view state: it resets when the popup instance is dropped.
type popupMax struct{ maximized bool }

// maxed reports whether the popup is currently maximized.
func (p *popupMax) maxed() bool { return p.maximized }

// toggleMaximize flips the maximized flag. The central "T" handler on the layer
// stack (see maximizableLayer / the Update key dispatch) calls this on the top
// popup; a popup never wires "T" into its own update.
func (p *popupMax) toggleMaximize() { p.maximized = !p.maximized }

// capturingText reports whether the popup is currently capturing text input — a
// /-filter query or a text/number field — so the central "T" handler leaves "T"
// a literal character instead of maximizing. Default false (a popup with no
// input state); a popup with a text-entry state overrides this to return true
// while it is capturing.
func (p *popupMax) capturingText() bool { return false }

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

// popupResolveRowCap returns the visible-row budget for a list popup: when
// maximized, the terminal-derived cap (popupMaxRowCap) but never fewer than
// the popup's normal fixed cap, so maximizing never reduces the visible rows
// even on a short terminal. Not maximized → the normal cap.
func popupResolveRowCap(maximized bool, termH, normal int) int {
	if maximized {
		if c := popupMaxRowCap(termH); c > normal {
			return c
		}
	}
	return normal
}
