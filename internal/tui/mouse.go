package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// doubleClickWindow is how close two left-clicks on the same target must land
// to count as a double-click. A double-click acts as enter on that control.
const doubleClickWindow = 400 * time.Millisecond

// clickZone names the control a left-click landed on. The zone (plus the
// per-zone fields of clickTarget) is what must match between two presses for
// them to form a double-click.
type clickZone int

const (
	zoneNone clickZone = iota
	zonePanel
	zoneFilesTree
	zoneFilesCommits
	zoneLayer
	zoneActionMenu
)

// clickTarget identifies what a left-click landed on. The zero value never
// matches a real target.
type clickTarget struct {
	zone  clickZone
	panel panel // zonePanel
	row   int   // zonePanel / zoneFilesTree / zoneFilesCommits
	layer layer // zoneLayer: the popup's identity, so a stale record from a
	// closed popup can't pair with a click on the next one
}

// registerClick records a left-click on tgt and reports whether it completed a
// double-click. A completing click clears the record, so a triple-click starts
// a fresh cycle instead of firing enter twice.
func (m Model) registerClick(tgt clickTarget) (Model, bool) {
	if m.lastClick == tgt && time.Since(m.lastClickAt) <= doubleClickWindow {
		m.lastClick, m.lastClickAt = clickTarget{}, time.Time{}
		return m, true
	}
	m.lastClick, m.lastClickAt = tgt, time.Now()
	return m, false
}

// clickEnterLayer reports whether the top layer is a list-style control where a
// double-click may act as enter (enter is the row's primary, deliberate
// action). Text-entry popups stay excluded: enter SUBMITS there, and a stray
// double-click must never submit a form. The decision modal is excluded for the
// same reason — enter commits a decision option.
func clickEnterLayer(l layer) bool {
	switch l.(type) {
	case *repoPopup, *bookmarkPopup, *shelfPopup, *commandPalette, *pairOpPopup, *contentPopup:
		return true
	}
	return false
}

// rightClickMenuLayer reports whether the top layer handles "." itself (the
// full-screen readers open the action menu on it); right-click replays that
// key. Every other layer swallows the right button.
func rightClickMenuLayer(l layer) bool {
	switch l.(type) {
	case *historyView, *blameView, *diffView:
		return true
	}
	return false
}

// handleMouse routes all mouse input. Precedence mirrors the key routing:
// the modal swallows everything; then the full-screen diff view; then
// the help window owns the wheel; under
// any other popup mouse input is ignored entirely (centered overlays —
// hit-testing the background would act on hidden state); then the files
// view's two sides; then the normal panels. Click-to-focus and wheel are
// ungated on running/loading (pure focus/selection movement, like the arrow
// keys). Two additions ride the same precedence: a right-click selects the row
// under the cursor and opens the . menu (a shortcut for ., never the only
// path — terminals differ on forwarding the right button), and a double
// left-click acts as enter on the active control. Both are key-synthesis
// (synthKey), so they can never drift from what the keys do — and they act on
// the control's own selected state, never on hidden background state.
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
	// A process owns the keyboard; mouse is swallowed (v1), just below the modal.
	if m.proc != nil {
		return m, nil
	}
	// The layer stack (surfaces + centered popups) is above any content window;
	// surfaces (history/blame) are keyboard-only (v1) and popups swallow mouse
	// rather than hit-test the hidden background — except the help / `?` cheat-sheet
	// viewer (a contentPopup), which scrolls with the wheel. List-style popups
	// accept a double-click as enter; the full-screen readers accept a
	// right-click as `.`.
	if l := m.topLayer(); l != nil {
		if cp, ok := l.(*contentPopup); ok && wheel != 0 {
			cp.move(wheel)
		}
		if dv, ok := l.(*diffView); ok && wheel != 0 {
			dv.scrollBy(wheel, m.diffBodyRows())
		}
		if msg.Button == tea.MouseButtonLeft && clickEnterLayer(l) {
			var double bool
			if m, double = m.registerClick(clickTarget{zone: zoneLayer, layer: l}); double {
				nm, cmd := l.update(m, synthKey("enter"))
				return nm, cmd
			}
		}
		if msg.Button == tea.MouseButtonRight && rightClickMenuLayer(l) {
			nm, cmd := l.update(m, synthKey("."))
			return nm, cmd
		}
		return m, nil
	}
	if m.actionMenu != nil {
		if wheel != 0 {
			m.actionMenu.move(wheel)
			return m, nil
		}
		if msg.Button == tea.MouseButtonLeft {
			var double bool
			if m, double = m.registerClick(clickTarget{zone: zoneActionMenu}); double {
				return m.updateActionMenuKey(synthKey("enter"))
			}
		}
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
			return m.maybeLoadMoreCommits()
		}
		return m, nil
	}
	// Right-click: select what's under the cursor (same as a left click), then
	// open the . menu on it.
	if msg.Button == tea.MouseButtonRight {
		m.filterTyping = false
		if p != m.focus {
			m = m.rememberLeftFocus()
			m.focus = p
		}
		if idx, ok := m.panelRowAt(p, msg.Y); ok {
			m.sel[p] = idx
		}
		return m.openActionMenu(), nil
	}
	if msg.Button != tea.MouseButtonLeft {
		return m, nil
	}
	// A click commits /-input mode the way enter does (the query stays):
	// otherwise focus would move while typing kept capturing keys for the
	// old panel.
	m.filterTyping = false
	// A click on the tab-bar line of a left-column slot switches to that tab
	// (and focuses it), mirroring ctrl+←/→. Off the tabs, the click falls
	// through to plain focus/selection below.
	if tp, ok := m.tabClickAt(p, msg.X, msg.Y); ok {
		return m.activateTab(tp), nil
	}
	if p != m.focus {
		m = m.rememberLeftFocus()
		m.focus = p
	}
	if idx, ok := m.panelRowAt(p, msg.Y); ok {
		m.sel[p] = idx
		// A second click on the same row within the double-click window acts
		// as enter on it (the first click already selected it).
		var double bool
		if m, double = m.registerClick(clickTarget{zone: zonePanel, panel: p, row: idx}); double {
			return m.Update(synthKey("enter"))
		}
	}
	return m, nil
}

// mouseInFilesView routes mouse input while the commit files view is open:
// the left column is the tree box (border y=1, title y=2, windowed rows
// from y=3), the right column the normally-rendered Commits panel. Wheel
// and click both target whatever is under the cursor; commit-side selection
// changes go through the follow-live path (clamped, deduped, one reload).
// Right-click selects then opens the . menu; a double left-click on the same
// row acts as enter.
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
		// Route through moveListUnderFilesView so the wheel scrolls whatever owns
		// the right column: a live file preview (pager) or a stash list, falling
		// back to the commit list — never reloading a commit under an open preview.
		return m.moveListUnderFilesView(wheel)
	case msg.Button == tea.MouseButtonLeft && inTree:
		m = m.focusTree()
		i := msg.Y - 3 // box top (y=1) + border + title line
		if i >= 0 && i < m.filesPageRows() {
			vis := m.filesView.visible()
			if idx := windowStart(len(vis), m.filesPageRows(), m.filesView.sel) + i; idx < len(vis) {
				m.filesView.sel = idx
			}
		}
		var double bool
		if m, double = m.registerClick(clickTarget{zone: zoneFilesTree, row: m.filesView.sel}); double {
			return m.Update(synthKey("enter"))
		}
	case msg.Button == tea.MouseButtonLeft && inCommits:
		m = m.focusRight()
		if idx, ok := m.panelRowAt(panelCommits, msg.Y); ok {
			var double bool
			if m, double = m.registerClick(clickTarget{zone: zoneFilesCommits, row: idx}); double {
				// Same row twice: the first click already moved the selection,
				// so the delta is 0 and this is a plain enter on it.
				return m.Update(synthKey("enter"))
			}
			return m.moveCommitUnderFilesView(idx - m.sel[panelCommits])
		}
	case msg.Button == tea.MouseButtonRight && inTree:
		m = m.focusTree()
		i := msg.Y - 3
		if i >= 0 && i < m.filesPageRows() {
			vis := m.filesView.visible()
			if idx := windowStart(len(vis), m.filesPageRows(), m.filesView.sel) + i; idx < len(vis) {
				m.filesView.sel = idx
			}
		}
		return m.openActionMenu(), nil
	case msg.Button == tea.MouseButtonRight && inCommits:
		m = m.focusRight()
		if idx, ok := m.panelRowAt(panelCommits, msg.Y); ok {
			nm, cmd := m.moveCommitUnderFilesView(idx - m.sel[panelCommits])
			return nm.(Model).openActionMenu(), cmd
		}
		return m.openActionMenu(), nil
	}
	return m, nil
}
