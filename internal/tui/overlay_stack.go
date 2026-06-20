package tui

import tea "github.com/charmbracelet/bubbletea"

// overlay is a centered popup layered above the panel interface (and any
// full-screen surface). It owns its own state and is the single source of
// truth for what it shows; the overlayStack orchestrates which overlay is on
// top, compositing, and key routing. Mirrors the full-screen `surface`
// interface — the only difference is render takes `below` (the screen beneath)
// and composites onto it instead of owning the whole screen. The two stacks
// merge into one compositor when overlays and surfaces unify.
type overlay interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
}

type overlayStack struct{ entries []overlay }

// overlayTop returns the active overlay, or nil when the stack is empty.
func (m Model) overlayTop() overlay {
	if m.overlays == nil || len(m.overlays.entries) == 0 {
		return nil
	}
	return m.overlays.entries[len(m.overlays.entries)-1]
}

// pushOverlay puts o on top. overlays is a pointer field so the push persists
// across Model value copies (same rationale as modal/popup/stack).
func (m Model) pushOverlay(o overlay) Model {
	if m.overlays == nil {
		m.overlays = &overlayStack{}
	}
	m.overlays.entries = append(m.overlays.entries, o)
	return m
}

// popOverlay drops the top overlay; a no-op on an empty stack. Popping reveals
// the overlay beneath (or, when empty, the surface/panel layers), whose state
// was never torn down.
func (m Model) popOverlay() Model {
	if m.overlays != nil && len(m.overlays.entries) > 0 {
		m.overlays.entries = m.overlays.entries[:len(m.overlays.entries)-1]
	}
	return m
}

// clearOverlays removes every overlay. Used when a popup hands off to a
// full-screen diff that must own the screen: the diff view is checked AFTER the
// overlay layer in render/dispatch/mouse, so a lingering overlay would hide it.
func (m Model) clearOverlays() Model {
	if m.overlays != nil {
		m.overlays.entries = nil
	}
	return m
}

// bookmarkSwitcher returns the topmost bookmark switcher on the overlay stack,
// or nil when none is open. Lets code and tests reach the live switcher without
// a Model field.
func (m Model) bookmarkSwitcher() *bookmarkPopup {
	if m.overlays == nil {
		return nil
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(*bookmarkPopup); ok {
			return p
		}
	}
	return nil
}

// shelfSwitcher returns the topmost shelf switcher on the overlay stack, else nil.
func (m Model) shelfSwitcher() *shelfPopup {
	if m.overlays == nil {
		return nil
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(*shelfPopup); ok {
			return p
		}
	}
	return nil
}

// overlayOf returns the topmost overlay of concrete type T on the stack, or the
// zero value (nil for a pointer type) when none is present. Lets production code
// and tests reach a live popup by type without a dedicated Model field.
func overlayOf[T overlay](m Model) T {
	var zero T
	if m.overlays == nil {
		return zero
	}
	for i := len(m.overlays.entries) - 1; i >= 0; i-- {
		if p, ok := m.overlays.entries[i].(T); ok {
			return p
		}
	}
	return zero
}
