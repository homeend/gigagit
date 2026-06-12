package tui

import tea "github.com/charmbracelet/bubbletea"

// handleMouse routes all mouse input. Precedence mirrors the key routing:
// the help window owns the wheel; under any other popup or the modal mouse
// input is ignored entirely (centered overlays — hit-testing the background
// would act on hidden state); then the files view's two sides; then the
// normal panels. Click-to-focus and wheel are ungated on running/loading
// (pure focus/selection movement, like the arrow keys).
func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if msg.Action != tea.MouseActionPress {
		return m, nil
	}
	wheel := 0
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		wheel = -m.wheelStep()
	case tea.MouseButtonWheelDown:
		wheel = m.wheelStep()
	}
	if m.contentPopup != nil {
		if wheel != 0 {
			m.contentPopup.move(wheel)
		}
		return m, nil
	}
	if m.modal != nil || m.popup != nil || m.repoPopup != nil ||
		m.settings != nil || m.branchPopup != nil || m.pairPopup != nil {
		return m, nil
	}
	if m.filesView != nil {
		return m.mouseInFilesView(msg, wheel)
	}

	p, ok := m.panelAt(msg.X, msg.Y)
	if !ok {
		return m, nil
	}
	if wheel != 0 {
		// Position-targeted: scroll the hovered panel, focus untouched.
		if n := m.panelLen(p); n > 0 {
			s := m.sel[p] + wheel
			if s > n-1 {
				s = n - 1
			}
			if s < 0 {
				s = 0
			}
			m.sel[p] = s
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	if p != m.focus {
		m = m.rememberLeftFocus()
		m.focus = p
	}
	if idx, ok := m.panelRowAt(p, msg.Y); ok {
		m.sel[p] = idx
	}
	return m, nil
}

// mouseInFilesView routes mouse input while the commit files view is open.
// Completed in the files-view task; wheel keeps today's behavior until then.
func (m Model) mouseInFilesView(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	if wheel != 0 {
		m.filesView.move(wheel)
	}
	return m, nil
}
