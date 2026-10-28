package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
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
	leftMaxed     bool // a `t`-maximised top-slot tab: the plain stop re-pins it to Branches
	leftMax       panel
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
		leftMaxed: m.leftMaxed, leftMax: m.leftMax,
	}
	m.windowState = w.fresh()
	m.walkGroupSeq++
	m.windowState.group = m.walkGroupSeq // unique: a stale answer of an earlier plain stop's group is never this one's
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
	// A generation the plain stop's group moved past its fresh start (a
	// popup there asked something) stays: that popup survives on top and
	// its answer must match. One left untouched goes back to the parked
	// value, which the parked windows' own reads carry.
	plain := m.windowState
	pg := p.windows.gens()
	for i, g := range plain.gens() {
		if *g > *pg[i]+1 {
			*pg[i] = *g
		}
	}
	m.windowState = p.windows
	if m.layers == nil {
		m.layers = &layerStack{}
	}
	m.layers.entries = append(m.layers.entries, live...)
	// A PR comments read in flight when they were parked was answered to
	// the plain stop's group (prCommentsMsg is unstamped): the parked view
	// would wait for it forever.
	m.prCommentsInflight = false
	m.replay = append(m.replay, p.queued...)
	if m.console != nil {
		// They come back under a shown console (a slot returning to a
		// console's worktree): the console owns the keyboard and its column
		// — the parked pin and focus are not restored over it.
		return m
	}
	m.focus, m.lastLeftPanel, m.activeLeftTab = p.focus, p.lastLeftPanel, p.activeLeftTab
	m.fullMaxed, m.fullMax = p.fullMaxed, p.fullMax
	m.leftMaxed, m.leftMax = p.leftMaxed, p.leftMax
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

// walkStop is one stop of alt+q's ring over a worktree.
type walkStop struct {
	plain   bool             // the plain worktree: no window, Branches on its row
	session domain.SessionID // a session of the worktree ("" with plain false = your windows)
	info    domain.SessionInfo
}

// walkWorktree is alt+q (back = alt+Q): the viewed worktree's screens in
// one ring in the order of its Branches rows — the worktree's own row (the
// plain worktree, then your windows when any) → its sessions as their
// sub-rows list them, agents and terminals mixed — round and round (spec
// 2026-10-20-alt-q-worktree-cycle; row order: user ruling 2026-10-28).
// From the Branches tab with the keyboard and no window, the cursor's row
// names the worktree (another one is switched to first; a branch no
// worktree has: nothing) — unless a walk is in progress, which wins.
func (m Model) walkWorktree(back bool) (Model, tea.Cmd) {
	if m.altCycleBlocked() {
		return m, nil
	}
	if !m.opsIdle() {
		m.statusMsg = i18n.T("an operation is running — switch once it has finished")
		return m, nil
	}
	parked := m.windowState.cyclePark != nil
	if !parked && m.console == nil && m.topLayer() == nil && !m.windowsOpen() &&
		m.activeLeftTab == panelBranches && m.focus == panelBranches {
		path, named := m.cursorWorktree()
		if !named {
			return m, nil
		}
		if model.KeyOf(path) != m.viewed {
			nm, ok := m.userSwitchView(path)
			if !ok {
				return nm, nil
			}
			nm.pendingReturnView = ""
			m = nm
			parked = m.windowState.cyclePark != nil
		}
	}
	dir := m.viewPath(m.viewed)
	sessions := m.walkSessionsIn(dir)
	if m.console != nil && !walkHas(sessions, m.console.id) {
		m = m.hideConsoleHere() // another worktree's (or an exited) console is no stop of this walk
	}
	stops := []walkStop{{plain: true}}
	if parked || m.windowsOpen() || m.consoleCovers() {
		stops = append(stops, walkStop{})
	}
	for _, info := range sessions {
		stops = append(stops, walkStop{session: info.ID, info: info})
	}
	at := 0 // the plain worktree: parked, or nothing open
	switch {
	case m.console != nil:
		for i, st := range stops {
			if st.session == m.console.id {
				at = i
			}
		}
	case !parked && m.windowsOpen():
		at = 1
	}
	n := len(stops)
	if n == 1 {
		m.statusMsg = i18n.T("no running agent or terminal in this worktree")
		return m, nil
	}
	next := (at + 1) % n
	if back {
		next = (at - 1 + n) % n
	}
	m = m.unparkCycle() // leaving the plain stop: the windows are live again, the step goes from them
	st := stops[next]
	var cmd tea.Cmd
	switch {
	case st.plain:
		m = m.hideConsoleHere()
		m, cmd = m.parkCycle()
		m = m.activateTab(panelBranches).selectWorktreeBranch(dir)
		m.statusMsg = i18n.T("%s — plain worktree, %d of %d", shortWorktreeName(dir), next+1, n)
	case st.session == "":
		m = m.hideConsoleHere()
		m.statusMsg = i18n.T("%s — your windows, %d of %d", shortWorktreeName(dir), next+1, n)
	default:
		if m.console != nil && m.console.id == st.session {
			m.console.focused = true
			m.touchConsole()
			m.focus = panelCommits
			m = m.syncConsoleSize()
		} else {
			m, cmd = m.showConsoleBy(st.session, true, true)
		}
		m = m.selectSessionRow(st.session)
		m.statusMsg = i18n.T("%s in %s — %d of %d", st.info.Title(), shortWorktreeName(dir), next+1, n)
	}
	return m, cmd
}

// walkSessionsIn is alt+q's sessions of the worktree at dir: the running
// ones in the order the Branches tab lists their sub-rows (branchEntries:
// the session list's own order, agents and terminals mixed).
func (m Model) walkSessionsIn(dir string) []domain.SessionInfo {
	var out []domain.SessionInfo
	for _, info := range m.repoSessions(domain.Sessions().List()) {
		if info.State == domain.SessionRunning && model.SamePath(info.Dir, dir) {
			out = append(out, info)
		}
	}
	return out
}

func walkHas(list []domain.SessionInfo, id domain.SessionID) bool {
	for _, info := range list {
		if info.ID == id {
			return true
		}
	}
	return false
}

// consoleCovers reports windows waiting under the shown console (what its
// close brings back): the views it displaced or a files view beside it.
func (m Model) consoleCovers() bool {
	if m.console == nil {
		return false
	}
	cp := m.consoleParked
	return cp != nil && (len(cp.layers) > 0 || cp.stashView != nil || cp.filesPreview != nil)
}

// hideConsoleHere hides the shown console without leaving the viewed
// worktree: its return point is aimed here first, so the windows that come
// back are this worktree's own (another worktree's wait in its slot).
func (m Model) hideConsoleHere() Model {
	if m.console == nil {
		return m
	}
	if m.console.ret != nil {
		m.console.ret.view = m.viewed
	}
	return m.closeConsole()
}

// cursorWorktree is the worktree the Branches cursor's row names (a
// session sub-row names its branch's); false for a branch no worktree has.
func (m Model) cursorWorktree() (string, bool) {
	di := m.sel[panelBranches]
	idx := m.displayIndices(panelBranches)
	ents := m.branchEntries()
	if di < 0 || di >= len(idx) || idx[di] >= len(ents) {
		return "", false
	}
	e := ents[idx[di]]
	mm := m.asMember(e.member) // the row's member: its branches, its worktrees
	if e.heading || e.br < 0 || e.br >= len(mm.branches) {
		return "", false
	}
	return mm.worktreePathOf(mm.branches[e.br].Name)
}
