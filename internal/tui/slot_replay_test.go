package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// A versions read asked from A lands while B is shown: nothing changes on
// screen; back in A the popup shows loaded.
func TestResultForASleepingSlotReplaysOnReturn(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, cmdA := m.openBranchVersions("main", false, false)
	m = forceSwitch(t, m, other)
	nm, _ := m.Update(cmdA())
	m = nm.(Model)
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("A's popup shows over B")
	}
	m = forceSwitch(t, m, home)
	m, _ = m.replayQueued()
	if p := layerOf[*versionsPopup](m); p == nil || p.loading {
		t.Fatal("A's result was not replayed on return")
	}
}

// A result for a window closed before the swap is dropped at replay by its
// own generation: nothing opens, nothing is said.
func TestReplayDropsAResultForAClosedWindow(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, cmdA := m.openBranchVersions("main", false, false)
	m = m.popLayer()
	m = forceSwitch(t, m, other)
	nm, _ := m.Update(cmdA())
	m = nm.(Model)
	m = forceSwitch(t, m, home)
	m.statusMsg = ""
	m, _ = m.replayQueued()
	if layerOf[*versionsPopup](m) != nil || m.statusMsg != "" {
		t.Fatalf("a closed window's result acted on return: top=%T msg=%q", m.topLayer(), m.statusMsg)
	}
}

// The queue is bounded: the oldest goes.
func TestSlotQueueIsBounded(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m = forceSwitch(t, m, other)
	for i := 1; i <= queueCap+6; i++ {
		msg := versionsLoadedMsg{gen: i}
		msg.slot = model.KeyOf(home)
		nm, _ := m.Update(msg)
		m = nm.(Model)
	}
	q := m.views[model.KeyOf(home)].queued
	if len(q) != queueCap || q[0].(versionsLoadedMsg).gen != 7 {
		t.Fatalf("queue = %d entries, first gen %v; want %d entries starting at gen 7", len(q), q[0], queueCap)
	}
}

// A message for a slot that is gone is dropped, never queued.
func TestResultForAGoneSlotIsNotQueued(t *testing.T) {
	m := loadedModel(t)
	msg := versionsLoadedMsg{gen: 1}
	msg.slot = model.KeyOf("/nowhere/gone")
	nm, _ := m.Update(msg)
	m = nm.(Model)
	for k, v := range m.views {
		if len(v.queued) != 0 {
			t.Fatalf("slot %s queued a gone slot's message", k)
		}
	}
	if len(m.replay) != 0 {
		t.Fatal("a gone slot's message is waiting to replay")
	}
}

// A replayed message that moves the view leaves the rest on the slot that
// left: they are not applied to the new worktree. (replayQueued stops at
// the move; the slot gate would route a leftover there too — the test
// pins the outcome, not the mechanism.) The mover is a repo path resolve
// naming another worktree of this repository (an in-repo switch).
func TestReplayStopsWhenTheViewMoves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m = m.pushLayer(&repoPathPopup{input: newTextField(other)})
	later := versionsLoadedMsg{gen: 1, branch: "main"}
	later.slot = model.KeyOf(home)
	m.replay = []tea.Msg{repoResolvedMsg{path: other, top: other}, later}
	m, _ = m.replayQueued()
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q, want the resolve's target", m.viewed)
	}
	q := m.views[model.KeyOf(home)].queued
	if len(q) != 1 {
		t.Fatalf("the leaving slot's queue = %d entries, want the one left over", len(q))
	}
	if len(m.replay) != 0 {
		t.Fatal("the leftover is still waiting to replay against the new worktree")
	}
}
