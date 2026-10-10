package tui

import (
	"context"
	"os/exec"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Follow-ups of the fast worktree switch post-merge hunt: what it reported
// and deferred (docs/CLAUDE-details.md, "Post-merge hunt").

// The slot queue's trim releases what it dropped: a reslice of the grown
// array keeps the dropped messages (and their payloads) reachable.
func TestSlotQueueTrimReleasesTheDroppedMessages(t *testing.T) {
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
	if len(q) != queueCap || cap(q) != queueCap {
		t.Fatalf("queue len %d cap %d; want both %d (a fresh slice, the dropped messages released)", len(q), cap(q), queueCap)
	}
}

// The viewed slot holds no copy of its windows: the live group is the
// Model's, and a copy left in the slot would keep every window closed
// since the swap alive until the next one.
func TestTheViewedSlotKeepsNoWindowCopy(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m = forceSwitch(t, m, other)
	if v := m.views[m.viewed]; v.windows.layers != nil {
		t.Fatal("the viewed slot keeps a copy of its windows after loadView")
	}
}

// A worktree removed under gg takes its open files with it when its slot
// is pruned: the documents cannot be reloaded from a tree that is gone,
// and the registry would hold them (their lines, their notes) for the
// rest of the session.
func TestAGoneWorktreeReleasesItsOpenFiles(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m.ensureView(other)
	m.openFiles.touch(other, wtDoc("a.txt"), noneShown)
	m.openFiles.touch(home, wtDoc("b.txt"), noneShown)
	if out, err := exec.Command("git", "-C", home, "worktree", "remove", "--force", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %v\n%s", err, out)
	}
	nm, _ := m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	m = nm.(Model)
	if m.views[model.KeyOf(other)] != nil {
		t.Fatal("precondition: the slot was not pruned")
	}
	if n := len(m.openFiles.list(other)); n != 0 {
		t.Fatalf("the gone worktree still has %d open files", n)
	}
	if n := len(m.openFiles.list(home)); n != 1 {
		t.Fatalf("home's open files were touched: %d", n)
	}
}
