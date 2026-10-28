package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// sessionWalkStop is one stop of alt+s's ring: a stop of alt+q's walk over
// the worktree at path.
type sessionWalkStop struct {
	path string
	walkStop
}

// sessionWalkRing is what alt+s walks (scoped = alt+S: the live member's
// worktrees only): every worktree with a running session, in the Branches
// tab's order (worktreeRing), each as alt+q walks it — the plain worktree,
// your windows (the viewed worktree's, when there are any), its sessions as
// their sub-rows list them. A worktree without a session is no stop.
func (m Model) sessionWalkRing(scoped bool) []sessionWalkStop {
	var out []sessionWalkStop
	for _, rs := range m.worktreeRing() {
		if scoped && rs.member != m.liveMember() {
			continue
		}
		sessions := m.walkSessionsIn(rs.path)
		if len(sessions) == 0 {
			continue
		}
		out = append(out, sessionWalkStop{path: rs.path, walkStop: walkStop{plain: true}})
		if model.KeyOf(rs.path) == m.viewed && (m.windowState.cyclePark != nil || m.windowsOpen() || m.consoleCovers()) {
			out = append(out, sessionWalkStop{path: rs.path})
		}
		for _, info := range sessions {
			out = append(out, sessionWalkStop{path: rs.path, walkStop: walkStop{session: info.ID, info: info}})
		}
	}
	return out
}

// sessionWalkAt is the stop on screen: the shown console's, else the viewed
// worktree's plain stop (parked, or nothing open) or its windows; -1 when
// the viewed worktree is no stop of the ring.
func (m Model) sessionWalkAt(ring []sessionWalkStop) int {
	if m.console != nil {
		for i, st := range ring {
			if st.session == m.console.id {
				return i
			}
		}
	}
	windows := m.windowState.cyclePark == nil && m.windowsOpen()
	for i, st := range ring {
		if model.KeyOf(st.path) == m.viewed && st.session == "" && st.plain != windows {
			return i
		}
	}
	return -1
}

func sessionWalkHas(ring []sessionWalkStop, id domain.SessionID) bool {
	for _, st := range ring {
		if st.session == id {
			return true
		}
	}
	return false
}

// walkSessions is alt+s (scoped = alt+S): alt+q's walk carried on from
// worktree to worktree — past the shown worktree's last session, the next
// worktree with a running session in the Branches tab's order (in a
// workspace every member's, group by group; alt+S the live member's only),
// at its plain worktree (its windows put aside), around again past the
// last. From a worktree without a session: the next one that has one.
func (m Model) walkSessions(scoped bool) (Model, tea.Cmd) {
	if m.altCycleBlocked() {
		return m, nil
	}
	if !m.opsIdle() {
		m.statusMsg = i18n.T("an operation is running — switch once it has finished")
		return m, nil
	}
	ring := m.sessionWalkRing(scoped)
	if len(ring) == 0 {
		if m.ws != nil && !scoped {
			m.statusMsg = i18n.T("no running agent or terminal in this workspace")
		} else {
			m.statusMsg = i18n.T("no running agent or terminal in this repository")
		}
		return m, nil
	}
	if m.console != nil && !sessionWalkHas(ring, m.console.id) {
		m = m.hideConsoleHere() // an exited (or outside) console is no stop
		ring = m.sessionWalkRing(scoped)
	}
	n := len(ring)
	next := 0
	if at := m.sessionWalkAt(ring); at >= 0 {
		next = (at + 1) % n
	} else {
		vi := m.ringIndex(m.viewPath(m.viewed))
		for i, st := range ring {
			if m.ringIndex(st.path) > vi {
				next = i
				break
			}
		}
	}
	// Another worktree is entered at its plain stop; one that cannot be
	// reached from here (another environment's notation, a directory gone)
	// is skipped for the next.
	for range n {
		st := ring[next]
		if model.KeyOf(st.path) == m.viewed {
			break
		}
		if verdict, _ := checkSwitchTarget(guardStat, guardGOOS, st.path); verdict == switchOK {
			break
		}
		for next = (next + 1) % n; !ring[next].plain; next = (next + 1) % n {
		}
	}
	st := ring[next]
	var cmd tea.Cmd
	switch {
	case model.KeyOf(st.path) != m.viewed:
		nm, ok := m.hideConsoleHere().userSwitchView(st.path) // another member's worktree is the member swap, still a look
		if !ok {
			m.statusMsg = nm.statusMsg // refused: the screen stays as it was, console and all
			return m, nil
		}
		nm.pendingReturnView = ""
		m, cmd = nm.parkCycle()         // the arriving worktree's windows wait aside, its "your windows" stop
		arrived := m.viewPath(m.viewed) // a revived member lands on its main checkout, not st.path
		m = m.activateTab(panelBranches).selectWorktreeBranch(arrived)
		st.path = arrived
	case st.plain:
		m = m.unparkCycle()
		m = m.hideConsoleHere()
		m, cmd = m.parkCycle()
		m = m.activateTab(panelBranches).selectWorktreeBranch(st.path)
	case st.session == "":
		m = m.unparkCycle()
		m = m.hideConsoleHere()
	default:
		m = m.unparkCycle()
		if m.console != nil && m.console.id == st.session {
			m.console.focused = true
			m.touchConsole()
			m.focus = panelCommits
			m = m.syncConsoleSize()
		} else {
			m, cmd = m.showConsoleBy(st.session, true, true)
		}
		m = m.selectSessionRow(st.session)
	}
	// The count is the ring as it stands now: arriving somewhere may give
	// that worktree its "your windows" stop.
	ring = m.sessionWalkRing(scoped)
	pos := m.sessionWalkAt(ring)
	name := shortWorktreeName(st.path)
	switch {
	case pos < 0:
	case st.plain:
		m.statusMsg = i18n.T("%s — plain worktree, %d of %d", name, pos+1, len(ring))
	case st.session == "":
		m.statusMsg = i18n.T("%s — your windows, %d of %d", name, pos+1, len(ring))
	default:
		m.statusMsg = i18n.T("%s in %s — %d of %d", st.info.Title(), name, pos+1, len(ring))
	}
	return m, cmd
}
