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
	v := m.views[k]
	if v == nil {
		// A gone slot's message is dropped — all but its process-wide part
		// (a shelf list, a PR row), which is nobody's in particular.
		if sw, ok := msg.(sharedWriter); ok {
			m, _ = sw.applyShared(m)
		}
		return m, true
	}
	if s, ok := msg.(staleFor); ok && s.staleFor(v) {
		return m, true // the slot would drop it on return: not worth keeping
	}
	if sw, ok := msg.(sharedWriter); ok {
		m, msg = sw.applyShared(m) // the process-wide part lands now; the replay skips it
	}
	v.queued = trimQueue(append(v.queued, msg))
	return m, true
}

// trimQueue bounds q to queueCap, oldest first to go, in a FRESH slice: a
// reslice of the grown array would keep the dropped messages — and the
// file lists, diffs and trees they carry — reachable until the slot's
// next trim.
func trimQueue(q []tea.Msg) []tea.Msg {
	if len(q) <= queueCap {
		return q
	}
	kept := make([]tea.Msg, queueCap)
	copy(kept, q[len(q)-queueCap:])
	return kept
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
				v.queued = trimQueue(append(append([]tea.Msg{}, q[i+1:]...), v.queued...))
			}
			break
		}
	}
	return m, tea.Batch(cmds...)
}
