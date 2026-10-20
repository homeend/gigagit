package tui

import (
	"context"
	"os/exec"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// walkNoopMsg is a message no handler knows: an Update that only runs the
// tail (the replay, the park's loss check, the history sweep).
type walkNoopMsg struct{}

func walkUpdate(t *testing.T, m Model, msg any) Model {
	t.Helper()
	mm, _ := m.Update(msg)
	return mm.(Model)
}

// park → unpark gives back the very same windows, focus and pin.
func TestCycleParkRoundTrip(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 160, 40
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m.focus = panelFiles
	m.fullMaxed, m.fullMax = true, panelFiles
	m, _ = m.parkCycle()
	if m.topLayer() != nil || m.windowState.cyclePark == nil || m.fullMaxed {
		t.Fatalf("parked: top=%T park=%v pin=%v, want no window, a park, no pin", m.topLayer(), m.windowState.cyclePark != nil, m.fullMaxed)
	}
	m = m.unparkCycle()
	if m.topLayer() != dv || m.focus != panelFiles || !m.fullMaxed || m.fullMax != panelFiles || m.windowState.cyclePark != nil {
		t.Fatalf("unparked: top=%T focus=%v pin=%v/%v park=%v, want the diff, Files, the pin back", m.topLayer(), m.focus, m.fullMaxed, m.fullMax, m.windowState.cyclePark != nil)
	}
}

// Nothing open: nothing to put aside.
func TestCycleParkWithoutWindowsIsNoPark(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m, _ = m.parkCycle()
	if m.windowState.cyclePark != nil {
		t.Fatal("an empty screen was parked")
	}
}

// A parked history keeps its walk: the sweep must see the park.
func TestCycleParkKeepsAHistoryWalk(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, _ = m.parkCycle()
	m.sweepHistoryWalks()
	if stopped {
		t.Fatal("the sweep stopped a walk whose history waits in the cycle park")
	}
}

// A window-addressed result for a parked window waits on the park and is
// applied when the windows come back.
func TestCycleParkQueuesAndReplaysWindowResults(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	bv := &blameView{tag: "t1", loading: true}
	m = m.pushLayer(bv)
	m, _ = m.parkCycle()
	m = walkUpdate(t, m, blameMsg{slotStamp: m.stamp(), tag: "t1", lines: []model.BlameLine{{}}})
	if !bv.loading {
		t.Fatal("the blame took its result while parked (the live group has no blame view: it was handled by luck)")
	}
	if p := m.windowState.cyclePark; p == nil || len(p.queued) != 1 {
		t.Fatalf("park=%+v, want the result queued on it", p)
	}
	m = m.unparkCycle()
	m = walkUpdate(t, m, walkNoopMsg{})
	if bv.loading || len(bv.lines) != 1 {
		t.Fatalf("after unpark: loading=%v lines=%d, want the queued result applied", bv.loading, len(bv.lines))
	}
}

// Opening a window at the plain stop loses the parked windows — closed as
// their esc would (a history walk stops).
func TestOpeningAWindowAtThePlainStopDropsThePark(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, _ = m.parkCycle()
	dv := &diffView{title: "b.go", rev: "def456"}
	m = m.pushLayer(dv)
	m = walkUpdate(t, m, walkNoopMsg{})
	if m.windowState.cyclePark != nil || !stopped || m.topLayer() != dv {
		t.Fatalf("park=%v stopped=%v top=%T, want the park gone, its history stopped, the new diff on top", m.windowState.cyclePark != nil, stopped, m.topLayer())
	}
}

// A popup is not a window: it keeps the park.
func TestAPopupAtThePlainStopKeepsThePark(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	m, _ = m.parkCycle()
	m = m.pushLayer(&gotoCommitPopup{})
	m = walkUpdate(t, m, walkNoopMsg{})
	if m.windowState.cyclePark == nil {
		t.Fatal("a popup over the plain stop dropped the parked windows")
	}
}

// A repository switch drops the park (its windows were the old repo's).
func TestRepoSwitchDropsThePark(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, _ = m.parkCycle()
	mm, _ := m.reRoot(m.currentWorktree)
	m = mm.(Model)
	m.sweepHistoryWalks()
	if m.windowState.cyclePark != nil || !stopped {
		t.Fatalf("park=%v stopped=%v, want both gone", m.windowState.cyclePark != nil, stopped)
	}
}

// Serial: installs a process-global session manager.
//
// alt+a from the plain stop: the windows come back first and the console
// shows over them as alt+a always does — full-screen over a diff, and esc
// returns to it.
func TestConsoleShownAtThePlainStopTakesTheWindows(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	startSessionIn(t, m, m.homeWorktree(), "A")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m, _ = m.parkCycle()
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.consoleFull() || m.windowState.cyclePark != nil {
		t.Fatalf("console=%v full=%v park=%v, want a full-screen console and no park", m.console != nil, m.consoleFull(), m.windowState.cyclePark != nil)
	}
	m = m.closeConsole()
	if m.topLayer() != dv {
		t.Fatalf("top=%T, want the diff back", m.topLayer())
	}
}

// alt+w from the plain stop moves on (the screen IS the plain worktree)
// and the parked windows wait with their worktree, back when it returns.
func TestAltWFromThePlainStopKeepsThePark(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.homeWorktree()
	m, other := addWorktree(t, m, "wt2")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m, _ = m.parkCycle()
	m = m.activateTab(panelBranches).selectWorktreeBranch(home)
	m = pressAlt(t, m, 'w')
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q (%s), want wt2: alt+w must move on from the plain stop", m.viewed, m.statusMsg)
	}
	m, ok := m.switchView(home)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if m.topLayer() != dv || m.windowState.cyclePark != nil {
		t.Fatalf("top=%T park=%v, want home's diff live again", m.topLayer(), m.windowState.cyclePark != nil)
	}
}

// walkSessions starts agent A1 then terminal T1 in dir.
func walkSessions(t *testing.T, m Model, dir string) (a1, t1 domain.SessionID) {
	t.Helper()
	return startSessionIn(t, m, dir, "A1"), startTerminalIn(t, m, dir, "T1")
}

// walkSelectBranch puts the Branches tab, with the keyboard, on the row of
// branch name.
func walkSelectBranch(t *testing.T, m Model, name string) Model {
	t.Helper()
	m = m.activateTab(panelBranches)
	ents := m.branchEntries()
	for di, u := range m.displayIndices(panelBranches) {
		if u < len(ents) && ents[u].sess == "" && m.branches[ents[u].br].Name == name {
			m.sel[panelBranches] = di
			return m
		}
	}
	t.Fatalf("no Branches row for %q", name)
	return m
}

// addLonelyBranch creates a branch no worktree has checked out.
func addLonelyBranch(t *testing.T, m Model, name string) Model {
	t.Helper()
	if out, err := exec.Command("git", "-C", m.currentWorktree, "branch", name).CombinedOutput(); err != nil {
		t.Fatalf("branch: %v\n%s", err, out)
	}
	nm, _ := m.Update(m.readSourceCmd(context.Background(), srcBranches, reloadOpts{manual: true})())
	return nm.(Model)
}

func pressWalk(t *testing.T, m Model, back bool) Model {
	t.Helper()
	if back {
		return pressAlt(t, m, 'Q')
	}
	return pressAlt(t, m, 'q')
}

func wantSession(t *testing.T, m Model, step string, id domain.SessionID, full bool) {
	t.Helper()
	if m.console == nil || m.console.id != id || !m.console.focused || m.consoleFull() != full {
		t.Fatalf("%s: console=%+v full=%v (%s), want %s bound, full=%v", step, m.console, m.consoleFull(), m.statusMsg, id, full)
	}
}

func wantPlain(t *testing.T, m Model, step string) {
	t.Helper()
	if m.console != nil || m.windowsOpen() || m.focus != panelBranches || m.activeLeftTab != panelBranches {
		t.Fatalf("%s: console=%v windows=%v focus=%v tab=%v (%s), want the plain worktree", step, m.console != nil, m.windowsOpen(), m.focus, m.activeLeftTab, m.statusMsg)
	}
}

func wantWindows(t *testing.T, m Model, step string, dv *diffView) {
	t.Helper()
	if m.console != nil || m.topLayer() != dv || m.windowState.cyclePark != nil {
		t.Fatalf("%s: console=%v top=%T park=%v (%s), want the diff back", step, m.console != nil, m.topLayer(), m.windowState.cyclePark != nil, m.statusMsg)
	}
}

// Serial (session manager). alt+q: your windows → agents → terminals →
// plain → your windows.
func TestAltQWalksWindowsAgentsTerminalsPlain(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	a1, t1 := walkSessions(t, m, m.homeWorktree())
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m.focus = panelFiles
	m = pressWalk(t, m, false)
	wantSession(t, m, "1st", a1, true)
	m = pressWalk(t, m, false)
	wantSession(t, m, "2nd", t1, true)
	m = pressWalk(t, m, false)
	wantPlain(t, m, "3rd")
	if m.windowState.cyclePark == nil {
		t.Fatal("3rd: the diff was not put aside")
	}
	m = pressWalk(t, m, false)
	wantWindows(t, m, "4th", dv)
	if m.focus != panelFiles {
		t.Fatalf("4th: focus=%v, want Files back", m.focus)
	}
}

// Serial. alt+Q walks the same ring back.
func TestAltShiftQWalksBack(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	a1, t1 := walkSessions(t, m, m.homeWorktree())
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressWalk(t, m, true)
	wantPlain(t, m, "1st")
	m = pressWalk(t, m, true)
	wantSession(t, m, "2nd", t1, true)
	m = pressWalk(t, m, true)
	wantSession(t, m, "3rd", a1, true)
	m = pressWalk(t, m, true)
	wantWindows(t, m, "4th", dv)
}

// Serial. The plain stop has no windows on screen, yet a session reached
// from it backwards shows full-screen over the diff, as forwards.
func TestAltQAndAltShiftQShowTheSameScreenPerStop(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	_, t1 := walkSessions(t, m, m.homeWorktree())
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressWalk(t, m, true)
	m = pressWalk(t, m, true)
	wantSession(t, m, "back to T1", t1, true)
	mm, _ := m.Update(ctrlBracket()) // unbind; esc then closes the console
	m = mm.(Model)
	m = m.closeConsole()
	if m.topLayer() != dv {
		t.Fatalf("top=%T, want the diff under the console", m.topLayer())
	}
}

// Serial. No session: alt+q toggles your windows ↔ plain.
func TestAltQWithoutSessionsTogglesWindowsAndPlain(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressWalk(t, m, false)
	wantPlain(t, m, "1st")
	m = pressWalk(t, m, false)
	wantWindows(t, m, "2nd", dv)
	m = pressWalk(t, m, false)
	wantPlain(t, m, "3rd")
}

// Serial. No session, no window: one stop — a status line, nothing moves.
func TestAltQWithNothingToWalkSaysSo(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	m = walkSelectBranch(t, m, m.worktreeBranch(m.homeWorktree()))
	m.statusMsg = ""
	m = pressWalk(t, m, false)
	if m.console != nil || m.statusMsg == "" {
		t.Fatalf("console=%v msg=%q, want nothing but a status line", m.console != nil, m.statusMsg)
	}
}

// Serial. From the plain panels with nothing open there is no "your
// windows" stop: agent ↔ plain.
func TestAltQFromPlainPanelsHasNoWindowsStop(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	a1 := startSessionIn(t, m, m.homeWorktree(), "A1")
	m = walkSelectBranch(t, m, m.worktreeBranch(m.homeWorktree()))
	m = pressWalk(t, m, false)
	wantSession(t, m, "1st", a1, false)
	m = pressWalk(t, m, false)
	wantPlain(t, m, "2nd")
	m = pressWalk(t, m, false)
	wantSession(t, m, "3rd", a1, false)
}

// Serial. The cursor on a branch no worktree has: nothing at all.
func TestAltQOnABranchWithoutWorktreeDoesNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	startSessionIn(t, m, m.homeWorktree(), "A1")
	m = addLonelyBranch(t, m, "lonely")
	m = walkSelectBranch(t, m, "lonely")
	m.statusMsg = ""
	m = pressWalk(t, m, false)
	if m.console != nil || m.focus != panelBranches || m.statusMsg != "" {
		t.Fatalf("console=%v focus=%v msg=%q, want nothing at all", m.console != nil, m.focus, m.statusMsg)
	}
}

// Serial. The cursor on another worktree's row: that worktree is walked.
func TestAltQOnAnotherWorktreesRowWalksThatWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, m.homeWorktree(), "Home")
	id := startSessionIn(t, m, other, "Other")
	m = walkSelectBranch(t, m, "wt2")
	m = pressWalk(t, m, false)
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q (%s), want wt2", m.viewed, m.statusMsg)
	}
	wantSession(t, m, "1st", id, false)
}

// Serial. A walk in progress wins over the cursor: at the plain stop,
// with the cursor on a branch without a worktree, alt+q brings the
// windows back.
func TestAltQWalkInProgressWinsOverTheCursor(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	m = addLonelyBranch(t, m, "lonely")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m = pressWalk(t, m, false)
	wantPlain(t, m, "1st")
	m = walkSelectBranch(t, m, "lonely")
	m = pressWalk(t, m, false)
	wantWindows(t, m, "2nd", dv)
}

// Serial. A docked console of another worktree while the panels show
// home: the walk is home's — the console is no stop of it, and home's
// diff (displaced under it) is home's windows, so the first press goes to
// home's agent, full-screen over the diff.
func TestAltQFromAForeignDockedConsoleWalksTheViewedWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.homeWorktree()
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	foreign := startSessionIn(t, m, other, "Other")
	a1 := startSessionIn(t, m, home, "Home")
	m = m.pushLayer(&diffView{title: "a.go", rev: "abc123"})
	m, _ = m.showConsole(foreign, false)
	m, ok := m.switchView(home)
	if !ok || m.console == nil || m.console.id != foreign {
		t.Fatalf("precondition: ok=%v console=%+v", ok, m.console)
	}
	m.focus = panelFiles
	m = pressWalk(t, m, false)
	if m.viewed != model.KeyOf(home) {
		t.Fatalf("viewed=%q, want home", m.viewed)
	}
	wantSession(t, m, "1st", a1, true)
}

// Serial. An operation running: nothing moves, the status line says why.
func TestAltQIsRefusedDuringAnOperation(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	startSessionIn(t, m, m.homeWorktree(), "A1")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)
	m.running = true
	m.statusMsg = ""
	m = pressWalk(t, m, false)
	if m.console != nil || m.topLayer() != dv || m.statusMsg == "" {
		t.Fatalf("console=%v top=%T msg=%q, want nothing moved and a status line", m.console != nil, m.topLayer(), m.statusMsg)
	}
}
