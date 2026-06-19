package tui

import tea "github.com/charmbracelet/bubbletea"

// handleMouse routes all mouse input. Precedence mirrors the key routing:
// the modal swallows everything; then the full-screen diff view; then
// the help window owns the wheel; under
// any other popup mouse input is ignored entirely (centered overlays —
// hit-testing the background would act on hidden state); then the files
// view's two sides; then the normal panels. Click-to-focus and wheel are
// ungated on running/loading (pure focus/selection movement, like the arrow
// keys).
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
	if m.modal != nil {
		return m, nil
	}
	// The global bookmark switcher is a centered overlay above any content
	// window; like the other popups, swallow mouse rather than hit-test the
	// hidden background underneath it.
	if m.bookmarkPopup != nil || m.bookmarkPastePopup != nil {
		return m, nil
	}
	if m.stackTop() != nil {
		return m, nil // history/blame are keyboard-only (v1)
	}
	// Routing invariant: the diff view comes immediately after the modal,
	// matching Update's key routing and render().
	if m.diffView != nil {
		if wheel != 0 {
			m.diffView.scroll(wheel, m.diffBodyRows())
		}
		return m, nil
	}
	if m.actionMenu != nil {
		if wheel != 0 {
			m.actionMenu.move(wheel)
		}
		return m, nil
	}
	if m.contentPopup != nil {
		if wheel != 0 {
			m.contentPopup.move(wheel)
		}
		return m, nil
	}
	if m.popup != nil || m.repoPopup != nil ||
		m.settings != nil || m.branchPopup != nil || m.renameBranchPopup != nil || m.rewordPopup != nil || m.pairPopup != nil {
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
		// Wheeling the commit list toward the end pages in more, like the
		// keyboard movement does.
		if p == panelCommits {
			return m, m.maybeLoadMoreCommits()
		}
		return m, nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// A click commits /-input mode the way enter does (the query stays):
	// otherwise focus would move while typing kept capturing keys for the
	// old panel.
	m.filterTyping = false
	if p != m.focus {
		m = m.rememberLeftFocus()
		m.focus = p
	}
	if idx, ok := m.panelRowAt(p, msg.Y); ok {
		m.sel[p] = idx
	}
	return m, nil
}

// mouseInFilesView routes mouse input while the commit files view is open:
// the left column is the tree box (border y=1, title y=2, windowed rows
// from y=3), the right column the normally-rendered Commits panel. Wheel
// and click both target whatever is under the cursor; commit-side selection
// changes go through the follow-live path (clamped, deduped, one reload).
func (m Model) mouseInFilesView(msg tea.MouseMsg, wheel int) (tea.Model, tea.Cmd) {
	g := m.layout()
	inTree := g.leftW > 0 && msg.X < g.leftW && msg.Y >= 1 && msg.Y < 1+g.bodyH
	inCommits := false
	if p, ok := m.panelAt(msg.X, msg.Y); ok && p == panelCommits {
		inCommits = true
	}
	switch {
	case wheel != 0 && inTree:
		m.filesView.move(wheel)
	case wheel != 0 && inCommits:
		return m.moveCommitUnderFilesView(wheel)
	case msg.Button == tea.MouseButtonLeft && inTree:
		m.filesTreeFocused = true
		i := msg.Y - 3 // box top (y=1) + border + title line
		if i >= 0 && i < m.filesPageRows() {
			vis := m.filesView.visible()
			if idx := windowStart(len(vis), m.filesPageRows(), m.filesView.sel) + i; idx < len(vis) {
				m.filesView.sel = idx
			}
		}
	case msg.Button == tea.MouseButtonLeft && inCommits:
		m.filesTreeFocused = false
		if idx, ok := m.panelRowAt(panelCommits, msg.Y); ok {
			return m.moveCommitUnderFilesView(idx - m.sel[panelCommits])
		}
	}
	return m, nil
}
