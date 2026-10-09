package tui

import tea "github.com/charmbracelet/bubbletea"

// queueCap bounds what a sleeping slot keeps: a slot nobody returns to
// cannot grow without end. The oldest goes; a window whose answer was
// dropped keeps its loading state, which its own close discards.
const queueCap = 64

// routeSlotMsg sends a message addressed to a worktree not on screen where
// it belongs: a sleeping slot keeps it (queued, applied on return by
// replayQueued), a gone slot's is dropped. Reports whether it took the
// message.
func (m Model) routeSlotMsg(msg tea.Msg) (Model, bool) {
	sm, ok := msg.(slotMsg)
	if !ok {
		return m, false
	}
	k := sm.slotKey()
	if k == "" || k == m.viewed {
		return m, false
	}
	if v := m.views[k]; v != nil {
		v.queued = append(v.queued, msg)
		if len(v.queued) > queueCap {
			v.queued = v.queued[len(v.queued)-queueCap:]
		}
	}
	return m, true
}

// replayQueued applies what landed for the viewed worktree while it slept
// (loadView moved the slot's queue to m.replay): each message goes through
// Update itself, so the slot gate, the console's parked-aware routing and
// the tail's invariants hold for it as for a live one, and the window gens
// still drop an answer for a window closed before the swap. The queue is
// taken off the Model first: a nested Update finds it empty. A replayed
// message that moves the view (a link landing elsewhere, a console show)
// ends the replay; what is left belongs to the slot that just left and
// goes back on ITS queue.
func (m Model) replayQueued() (Model, tea.Cmd) {
	q := m.replay
	m.replay = nil
	var cmds []tea.Cmd
	for i, msg := range q {
		was := m.viewed
		nm, cmd := m.Update(msg)
		m = nm.(Model)
		cmds = append(cmds, cmd)
		if m.viewed != was {
			if v := m.views[was]; v != nil {
				v.queued = append(append([]tea.Msg{}, q[i+1:]...), v.queued...)
			}
			break
		}
	}
	return m, tea.Batch(cmds...)
}
