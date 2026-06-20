package tui

import tea "github.com/charmbracelet/bubbletea"

// surface is a full-screen view layered on top of the panel interface. The top
// of the stack owns keyboard input and the whole screen; popping it reveals the
// surface beneath (or the panel interface), whose state was never torn down.
type surface interface {
	render(m Model, below string) string
	update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
}

type viewStack struct{ entries []surface }

// stackTop returns the active surface, or nil when the stack is empty.
func (m Model) stackTop() surface {
	if m.stack == nil || len(m.stack.entries) == 0 {
		return nil
	}
	return m.stack.entries[len(m.stack.entries)-1]
}

// pushSurface puts s on top. stack is a pointer field so the push persists
// across Model value copies (same rationale as modal/popup).
func (m Model) pushSurface(s surface) Model {
	if m.stack == nil {
		m.stack = &viewStack{}
	}
	m.stack.entries = append(m.stack.entries, s)
	return m
}

// popSurface drops the top surface; a no-op on an empty stack.
func (m Model) popSurface() Model {
	if m.stack != nil && len(m.stack.entries) > 0 {
		m.stack.entries = m.stack.entries[:len(m.stack.entries)-1]
	}
	return m
}

// clearStack removes every surface. Used when a popup hands off to a
// full-screen diff that must own the screen: the diff view is checked AFTER the
// stack in render/dispatch/mouse, so a lingering surface would hide it.
func (m Model) clearStack() Model {
	if m.stack != nil {
		m.stack.entries = nil
	}
	return m
}
