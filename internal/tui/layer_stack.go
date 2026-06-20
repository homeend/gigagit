package tui

import tea "github.com/charmbracelet/bubbletea"

// layer is a window on the layer stack: a full-screen surface (history, blame,
// rebase/conflict/stage editors) or a centered popup (bookmark/shelf switchers,
// content/help, reword, …). The top owns the keyboard; popping reveals the layer
// beneath, whose state was never torn down. render composites onto `below` (the
// accumulated render of everything beneath): a surface ignores `below` and owns
// the screen; a popup composites its centered box onto `below`.
type layer interface {
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
	render(m Model, below string) string
}

// surface and overlay are retained as aliases during migration; Task 3 removes them.
type surface = layer
type overlay = layer

type layerStack struct{ entries []layer }

// topLayer returns the active (topmost) layer, or nil when the stack is empty.
func (m Model) topLayer() layer {
	if m.layers == nil || len(m.layers.entries) == 0 {
		return nil
	}
	return m.layers.entries[len(m.layers.entries)-1]
}

// pushLayer puts l on top. layers is a pointer field so the push persists across
// Model value copies (same rationale as modal/proc).
func (m Model) pushLayer(l layer) Model {
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(m.layers.entries, l)
	return m
}

// popLayer drops the top layer; a no-op on an empty stack. Popping reveals the
// layer beneath (or the panels), whose state was never torn down.
func (m Model) popLayer() Model {
	if m.layers != nil && len(m.layers.entries) > 0 {
		m.layers.entries = m.layers.entries[:len(m.layers.entries)-1]
	}
	return m
}

// clearLayers removes every layer. Used when a popup hands off to a full-screen
// diff that must own the screen: the diff view is the render base the stack walks
// over, so a lingering layer would composite on top and hide nothing — clearing
// makes the diff the sole visible surface.
func (m Model) clearLayers() Model {
	if m.layers != nil {
		m.layers.entries = nil
	}
	return m
}

// layerOf returns the topmost layer of concrete type T on the stack, or the zero
// value (nil for a pointer type) when none is present. Lets production code and
// tests reach a live window by type without a dedicated Model field.
func layerOf[T layer](m Model) T {
	var zero T
	if m.layers == nil {
		return zero
	}
	for i := len(m.layers.entries) - 1; i >= 0; i-- {
		if p, ok := m.layers.entries[i].(T); ok {
			return p
		}
	}
	return zero
}

// bookmarkSwitcher returns the topmost bookmark switcher on the stack, else nil.
func (m Model) bookmarkSwitcher() *bookmarkPopup { return layerOf[*bookmarkPopup](m) }

// shelfSwitcher returns the topmost shelf switcher on the stack, else nil.
func (m Model) shelfSwitcher() *shelfPopup { return layerOf[*shelfPopup](m) }

// --- migration shims: old overlay/surface names delegating to the one stack.
// Task 3 renames call sites and deletes these. ---

func (m Model) pushOverlay(o layer) Model { return m.pushLayer(o) }
func (m Model) pushSurface(s layer) Model { return m.pushLayer(s) }
func (m Model) popOverlay() Model         { return m.popLayer() }
func (m Model) popSurface() Model         { return m.popLayer() }
func (m Model) clearOverlays() Model      { return m.clearLayers() }
func (m Model) clearStack() Model         { return m.clearLayers() }
func (m Model) overlayTop() layer         { return m.topLayer() }
func (m Model) stackTop() layer           { return m.topLayer() }
func overlayOf[T layer](m Model) T        { return layerOf[T](m) }
