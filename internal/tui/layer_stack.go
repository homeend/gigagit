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

type layerStack struct{ entries []layer }

// maximizableLayer is a centered-box popup whose size can be toggled to
// near-fullscreen with ctrl+t. A popup opts in by embedding popupMax (which
// supplies toggleMaximize) and rendering wider — and taller where it caps rows —
// when maxed(). The central handler in Update toggles the top layer on ctrl+t;
// ctrl+t never collides with typed text, so every popup (including text editors)
// can maximize. Full-screen surfaces (see isFullScreenLayer) do not embed
// popupMax and are never maximized — they already own the screen.
type maximizableLayer interface {
	layer
	toggleMaximize()
}

// isFullScreenLayer reports whether l owns the whole screen (a surface: history,
// blame, the rebase/conflict/stage editors) rather than compositing a centered
// box over a backdrop (a popup). render uses this to build a popup's backdrop
// from the surfaces beneath it. Keep in sync when adding a full-screen surface.
func isFullScreenLayer(l layer) bool {
	switch l.(type) {
	case *historyView, *blameView, *irebaseEditor, *hunkPicker, *diffView, *reviewView:
		return true
	}
	return false
}

// topLayer returns the active (topmost) layer, or nil when the stack is empty.
func (m Model) topLayer() layer {
	if m.layers == nil || len(m.layers.entries) == 0 {
		return nil
	}
	return m.layers.entries[len(m.layers.entries)-1]
}

// hasLayer reports whether l is anywhere on the stack — on top or covered by
// popups pushed over it. Async results addressed to a specific surface use
// this rather than topLayer: a surface the user merely covered is still live
// and must still receive them.
func (m Model) hasLayer(l layer) bool {
	if m.layers == nil || l == nil {
		return false
	}
	for _, e := range m.layers.entries {
		if e == l {
			return true
		}
	}
	return false
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

// clearLayers removes every layer FOR GOOD. Used when a popup hands off to an
// operation (the identity apply-op, Reset branch, cherry-pick, apply patch):
// the op changes what the popup listed, so there is nothing to return to and
// the op must land in the main panels. A popup handing off to a WINDOW never
// uses this — see handOffToFilesView. (The full-screen diff is a stack layer
// and is pushed/popped like any other, preserving the surface it was opened
// over.)
func (m Model) clearLayers() Model {
	if m.layers != nil {
		m.layers.entries = nil
	}
	return m
}

// handOffToFilesView opens a files view (tree / compare / shelf files) FROM a
// popup: the convention is that a window opened from a popup returns to that
// popup when closed, so the layer stack is parked rather than cleared. The
// files view is not a layer, so the stack must be empty while it is open (a
// popup left on the stack would draw over the tree and keep the keyboard);
// the view's esc/l close restores the parked stack (restoreParkedLayers) and
// every other teardown drops it (closeFilesView zeroes filesReturnLayers).
//
// The park is armed AFTER open runs: every opener starts with closeFilesView
// as its clean slate, which would zero a park armed before it. A popup
// opened OVER an already-open files view replaces whatever that view had
// parked — the latest opener wins.
func (m Model) handOffToFilesView(open func(Model) (Model, tea.Cmd)) (Model, tea.Cmd) {
	var parked []layer
	if m.layers != nil {
		parked = m.layers.entries
		m.layers.entries = nil
	}
	m, cmd := open(m)
	m.filesReturnLayers = parked
	return m, cmd
}

// restoreParkedLayers puts the stack a hand-off parked back as the live
// stack. Called only from the files view's own close paths, which read the
// field before closeFilesView zeroes it; the stack is empty at that point
// (the view only receives keys when nothing sits above it).
func (m Model) restoreParkedLayers(parked []layer) Model {
	if len(parked) == 0 {
		return m
	}
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(m.layers.entries, parked...)
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

// diffLayer returns the open standalone diff on the layer stack, else nil.
func (m Model) diffLayer() *diffView { return layerOf[*diffView](m) }

// removeLayer drops the first matching entry wherever it sits (not only the top).
// Used to close a window that may have a popup above it (e.g. the diff on resize).
func (m Model) removeLayer(target layer) Model {
	if m.layers == nil {
		return m
	}
	for i, l := range m.layers.entries {
		if l == target {
			m.layers.entries = append(m.layers.entries[:i], m.layers.entries[i+1:]...)
			break
		}
	}
	return m
}
