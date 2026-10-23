package tui

import (
	"os/exec"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/gitwatch"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// openTwo opens a two-member workspace from the loaded model and settles
// the member loads; returns the model and member B.
func openTwo(t *testing.T, m Model) (Model, *repoView) {
	t.Helper()
	home := m.homeRepo()
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "shop", Repos: []string{home.mainPath, secondRepo(t)}})
	m = settleWorkspace(t, nm, cmd)
	b := m.ws.members[1]
	if !b.loaded {
		t.Fatal("B did not load")
	}
	return m, b
}

// A read addressed to a sleeping member lands in its slot, never in the
// live lists (Review Focus 3).
func TestSleepingMemberBranchesRouteToSlot(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	live := append([]model.Branch(nil), m.branches...)
	nm, _ := m.Update(memberLoadedMsg{svc: b.svc, branches: []model.Branch{{Name: "routed"}}, wts: worktreesPayload{worktrees: b.worktrees}})
	m = nm.(Model)
	if len(b.branches) != 1 || b.branches[0].Name != "routed" {
		t.Fatalf("B.branches = %+v", b.branches)
	}
	if len(m.branches) != len(live) || m.branches[0].Name != live[0].Name {
		t.Fatalf("the live lists changed: %+v", m.branches)
	}
}

// A sleeping member's watcher sees a branch created in its repository
// while another member is on screen; it closes when the member becomes
// live and is wanted again when it goes back to sleep.
func TestMemberWatcherSurvivesSwitch(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	// The settle started a watcher whose listener (left behind by
	// drainTimed) would eat the event: close it and start one this test
	// alone reads.
	b.closeWatch()
	ready := startMemberWatchCmd(b)().(memberWatchReadyMsg)
	if !ready.supported {
		t.Skip("file watching unsupported on this filesystem")
	}
	nm, listen := m.Update(ready)
	m = nm.(Model)
	if b.watcher == nil || listen == nil {
		t.Fatal("B's watcher was not kept")
	}
	if out, err := exec.Command("git", "-C", b.mainPath, "branch", "created-while-asleep").CombinedOutput(); err != nil {
		t.Fatalf("git branch: %v\n%s", err, out)
	}
	select {
	case s := <-b.watcher.Events():
		if s != gitwatch.Branches && s != gitwatch.Worktrees {
			t.Fatalf("event = %v", s)
		}
		nm, cmd := m.Update(memberWatchEventMsg{svc: b.svc, gen: b.watchGen, source: watchSourceKey(s)})
		m = nm.(Model)
		for _, msg := range drainTimed(cmd) { // the re-armed listener is left behind
			if ml, ok := msg.(memberLoadedMsg); ok {
				nm, _ = m.Update(ml)
				m = nm.(Model)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no watch event for the new branch")
	}
	found := false
	for _, br := range b.branches {
		if br.Name == "created-while-asleep" {
			found = true
		}
	}
	if !found {
		t.Fatalf("B's group does not show the new branch: %+v", b.branches)
	}
	// Into B: its own watcher closes (the live member is watched by the kick).
	a := m.homeRepo()
	nm2, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm2.statusMsg)
	}
	m = nm2
	if b.watcher != nil || b.polling {
		t.Fatal("B's watcher stayed open while B is live")
	}
	// Back to A: B sleeps again and a watcher is wanted for it.
	nm2, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm2.statusMsg)
	}
	m = nm2
	if !m.memberWatchKick || m.memberWatchStarts() == nil || !b.watchStarting {
		t.Fatalf("no watcher start wanted for B: kick=%v starting=%v", m.memberWatchKick, b.watchStarting)
	}
}

// Leaving the workspace closes every member watcher.
func TestLeaveClosesMemberWatchers(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	if b.watcher == nil {
		ready := startMemberWatchCmd(b)().(memberWatchReadyMsg)
		if !ready.supported {
			t.Skip("file watching unsupported on this filesystem")
		}
		nm, _ := m.Update(ready)
		m = nm.(Model)
	}
	gen := b.watchGen
	nm2, _ := m.leaveWorkspace()
	if b.watcher != nil || b.watchGen == gen || nm2.inWorkspace() {
		t.Fatalf("watcher=%v gen moved=%v ws=%v", b.watcher != nil, b.watchGen != gen, nm2.inWorkspace())
	}
}

// A member whose git dir cannot be watched polls its two lists instead.
func TestUnsupportedMemberPolls(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	nm, cmd := m.Update(memberWatchReadyMsg{svc: b.svc, gen: b.watchGen, supported: false})
	m = nm.(Model)
	if !b.polling || cmd == nil {
		t.Fatalf("polling=%v cmd=%v", b.polling, cmd != nil)
	}
	nm, cmd = m.Update(memberPollTickMsg{svc: b.svc, gen: b.watchGen})
	if cmd == nil {
		t.Fatal("a poll tick must read and re-arm")
	}
	_ = nm
}
