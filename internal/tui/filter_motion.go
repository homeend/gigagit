package tui

import tea "github.com/charmbracelet/bubbletea"

// filterMotion is the shared in-filter navigation contract. While typing a `/`
// filter, plain arrows and pgup/pgdn move the selection through the filtered rows
// WITHOUT resetting the cursor — exactly like the commit/panel filter. It keys
// off msg.Type only, so printable runes (including j/k and z) fall through to the
// caller's query handling and stay query text, never motions.
//
// move is the surface's selection mover (must be pure — no tea.Cmd; use the tree
// mover, not a list-side mover that returns a command). page is the page step for
// pgup/pgdn. Returns true when msg was a motion key and the caller should stop.
func filterMotion(msg tea.KeyMsg, move func(int), page int) bool {
	switch msg.Type {
	case tea.KeyUp:
		move(-1)
	case tea.KeyDown:
		move(1)
	case tea.KeyPgUp:
		move(-page)
	case tea.KeyPgDown:
		move(page)
	default:
		return false
	}
	return true
}

// popupFilterPage is the pgup/pgdn step for the fixed-window quick-switcher
// popups (finder, bookmark, shelf, action menu), whose visible window is ~12-16
// rows. The viewport-backed surfaces (files view, content viewer) page by their
// own viewport-aware row counts instead.
const popupFilterPage = 12
