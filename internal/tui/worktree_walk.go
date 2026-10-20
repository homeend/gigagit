package tui

import (
	tea "github.com/charmbracelet/bubbletea"
)

// cyclePark is a worktree's whole window group put aside by alt+q's
// plain-worktree stop (spec 2026-10-20-alt-q-worktree-cycle): the pile,
// the files view and its cluster, the stash list, the preview, the window
// generations — with the panel state the windows were shown in, which is
// process-wide on Model. It hangs on the live (fresh) group, so a slot
// swap carries it with its worktree.
type cyclePark struct {
	windows windowState
	// queued holds the window-addressed results (slotMsg) that landed for
	// the parked windows; they are replayed when the windows come back.
	queued        []tea.Msg
	focus         panel
	lastLeftPanel panel
	activeLeftTab panel
	fullMaxed     bool
	fullMax       panel
}

// windowsOpen reports a window on the live group — what the plain stop
// puts aside, and what, opened while it is put aside, loses it: a diff,
// history, blame, viewer or split on the pile, the files view, the stash
// list, a preview. A popup is no window.
func (m Model) windowsOpen() bool {
	if m.filesView != nil || m.stashView != nil || m.filesPreview != nil {
		return true
	}
	if m.layers == nil {
		return false
	}
	for _, l := range m.layers.entries {
		switch l.(type) {
		case *diffView, *historyView, *blameView, *fileViewer, *splitView:
			return true
		}
	}
	return false
}

// parkCycle puts the viewed worktree's windows aside for the plain stop:
// the live group becomes a fresh one holding the park. A steer request
// waiting in the group is answered now — its view is out of reach. With
// no window open there is nothing to put aside: no park.
func (m Model) parkCycle() (Model, tea.Cmd) {
	if !m.windowsOpen() {
		return m, nil
	}
	w := m.windowState
	cmd := m.failParkedSteer(&w, "the view was put aside (alt+q)")
	p := &cyclePark{
		windows: w,
		focus:   m.focus, lastLeftPanel: m.lastLeftPanel, activeLeftTab: m.activeLeftTab,
		fullMaxed: m.fullMaxed, fullMax: m.fullMax,
	}
	m.windowState = w.fresh()
	m.windowState.cyclePark = p
	m.fullMaxed = false
	return m, cmd
}

// unparkCycle brings the parked windows back exactly as they were, with
// their focus, tab and pin; a popup opened over the plain stop stays on
// top. What landed for them meanwhile is replayed by the Update tail.
func (m Model) unparkCycle() Model {
	p := m.windowState.cyclePark
	if p == nil {
		return m
	}
	var live []layer
	if m.layers != nil {
		live = m.layers.entries
	}
	m.windowState = p.windows
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(m.layers.entries, live...)
	m.focus, m.lastLeftPanel, m.activeLeftTab = p.focus, p.lastLeftPanel, p.activeLeftTab
	m.fullMaxed, m.fullMax = p.fullMaxed, p.fullMax
	m.replay = append(m.replay, p.queued...)
	return m.reconcileFullscreenFocus()
}

// dropCyclePark loses the parked windows (a window opened at the plain
// stop, a repository switch): each goes as its esc would — a history walk
// stops, a split retires, an open file stays open in the background — and
// what was queued for them goes too.
func (m Model) dropCyclePark() Model {
	p := m.windowState.cyclePark
	if p == nil {
		return m
	}
	m.windowState.cyclePark = nil
	var parked []layer
	if p.windows.layers != nil {
		parked = p.windows.layers.entries
	}
	for _, l := range append(parked, p.windows.filesReturnLayers...) {
		switch v := l.(type) {
		case *historyView:
			v.stop()
		case *splitView:
			v.leaveChars()
			v.retire()
		case *fileViewer:
			if v.openFile != nil {
				v.openFile.backgrounded = true
			}
		}
	}
	return m
}
