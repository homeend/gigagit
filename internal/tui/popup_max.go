package tui

// popupMax is embedded by any popup that supports ctrl+t-to-fullscreen. The layer
// stack holds the popup *pointer*, so the flag persists across Model value
// copies (same rationale as the modal/popup pointer fields). maximized is
// transient view state: it resets when the popup instance is dropped.
type popupMax struct{ maximized bool }

// maxed reports whether the popup is currently maximized.
func (p *popupMax) maxed() bool { return p.maximized }

// toggleMaximize flips the maximized flag. The central ctrl+t handler on the
// layer stack (see maximizableLayer / the Update key dispatch) calls this on the
// top popup; a popup never wires the maximize key into its own update.
func (p *popupMax) toggleMaximize() { p.maximized = !p.maximized }

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

// autoMaxForContent reports whether a maximizable centered-box popup whose
// widest content line is contentW columns should OPEN maximized: the content
// exceeds the normal (unmaximized) text width AND maximizing actually buys more
// room (guards a tiny terminal where both widths floor to the same value).
// Popups call this ONCE at creation, so auto-maximizing never fights a later
// manual ctrl+t toggle. This is how "a popup whose essential content would be
// clipped opens full-size by default".
func autoMaxForContent(w, contentW int) bool {
	normal := popupTextWidth(popupInnerWidth(w))
	full := popupTextWidth(popupFullInnerWidth(w))
	return contentW > normal && full > normal
}
