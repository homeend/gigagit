package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A console shown for a session in B parks home's working diff; the user
// keeps B when the console closes (alt+w's first hit). B shows nothing of
// home; back home the diff is there.
func TestConsoleReturnToAnotherWorktreeShowsNothingOfTheFirst(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m = m.pushLayer(&diffView{title: "f", rev: ""}) // home's working diff
	m, _ = m.showConsole(id, false)                 // parks it, shows other
	m.console.ret.view = model.KeyOf(other)         // the panels stay on other
	m = m.closeConsole()
	if m.viewed != model.KeyOf(other) || m.topLayer() != nil {
		t.Fatalf("viewed=%q top=%T: home's parked diff came back over %s", m.viewed, m.topLayer(), other)
	}
	m, ok := m.switchView(home)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	if d, _ := m.topLayer().(*diffView); d == nil || d.title != "f" {
		t.Fatalf("home's diff is not back: top=%T", m.topLayer())
	}
}

// A console closed while an op runs queues the return home: B shows
// nothing of home meanwhile, and when the op ends and the return happens,
// home's displaced diff comes back with home.
func TestParkedCopyFollowsAQueuedReturn(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m = m.pushLayer(&diffView{title: "f", rev: "abc1"})
	m, _ = m.showConsole(id, false)
	m.running = true
	m = m.closeConsole()
	if m.viewed != model.KeyOf(other) || m.pendingReturnView != m.home || m.topLayer() != nil {
		t.Fatalf("viewed=%q pending=%q top=%T", m.viewed, m.pendingReturnView, m.topLayer())
	}
	nm, _ := m.Update(opFinishedMsg{})
	m = nm.(Model)
	if m.viewed != m.home {
		t.Fatalf("after the op: viewed=%q", m.viewed)
	}
	if d, _ := m.topLayer().(*diffView); d == nil || d.title != "f" {
		t.Fatalf("home's displaced diff did not come back with the queued return: top=%T", m.topLayer())
	}
}

// A non-key message handled with the console's parked views put back
// beneath the stack swaps the view: what was parked belongs to the
// worktree that left and goes back on ITS slot, not on the new one's pile.
func TestDispatchParkedAwareReparksOntoTheSlotThatLeft(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "Shell") // a console in home: no swap on show
	was := m.viewed
	m = m.pushLayer(&diffView{title: "f", rev: "abc1"})
	m, _ = m.showConsole(id, false)
	if m.consoleParked == nil || len(m.consoleParked.layers) != 1 {
		t.Fatalf("precondition: the diff is parked on the slot: %+v", m.consoleParked)
	}
	m = m.pushLayer(&repoPathPopup{input: newTextField(other)}) // over the console
	nm, _ := m.Update(repoResolvedMsg{path: other, top: other}) // an in-repo switch
	m = nm.(Model)
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q, want the resolve's target", m.viewed)
	}
	if m.topLayer() != nil {
		t.Fatalf("home's parked diff is live over %s: %T", other, m.topLayer())
	}
	left := m.views[was].windows
	if left.consoleParked == nil || len(left.consoleParked.layers) != 1 || (left.layers != nil && len(left.layers.entries) != 0) {
		t.Fatalf("the parked diff did not go back on the slot that left: parked=%+v pile=%+v", left.consoleParked, left.layers)
	}
}

// alt+a from a worktree showing a full-screen diff, into an agent that
// runs in another worktree with nothing full-screen: the console docks —
// its size is the agent's worktree's business, not the diff's, which waits
// in the worktree it was opened in.
func TestConsoleSizeFollowsTheWorktreeItShowsIn(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, other, "Shell")
	m = m.pushLayer(&diffView{title: "f", rev: "abc1"}) // home's full-screen diff
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.viewed != model.KeyOf(other) {
		t.Fatalf("console=%v viewed=%q", m.console != nil, m.viewed)
	}
	if m.console.maximized || m.consoleFull() {
		t.Fatal("the console is maximised because of a diff that waits in another worktree")
	}
	m = m.closeConsole()
	m, _ = m.switchView(home)
	if d, _ := m.topLayer().(*diffView); d == nil || d.title != "f" {
		t.Fatalf("home's diff did not come back: %T", m.topLayer())
	}
}
