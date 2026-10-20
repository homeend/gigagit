package tui

import (
	"testing"

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
