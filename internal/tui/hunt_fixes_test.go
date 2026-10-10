package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// A working-tree stack open in A is A's window: loading B (a clean tree)
// must not reconcile it against B's status — that popped A's diff for good.
func TestSwapDoesNotReconcileTheLeavingStackAgainstTheArrivingStatus(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m = m.withStatus(model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "a.txt", Unstaged: 'M'}, {Path: "b.txt", Unstaged: 'M'}}})
	m = m.pushLayer(&diffView{title: "stack", stk: &diffStack{src: diffNavStatus, files: m.buildStatusStack(false)}})
	if dv := m.diffLayer(); dv == nil || len(dv.stk.files) != 2 {
		t.Fatal("fixture: no two-file stack")
	}
	m = forceSwitch(t, m, other) // B's slot status is empty: a reconcile would pop the stack
	if m.diffLayer() != nil {
		t.Fatal("B shows A's stack")
	}
	m = forceSwitch(t, m, home)
	dv := m.diffLayer()
	if dv == nil || dv.stk == nil {
		t.Fatal("A's stack was popped by the swap: loadView reconciled it against B's status")
	}
	if len(dv.stk.files) != 2 || dv.stk.files[0].path != "a.txt" {
		t.Fatalf("A's stack was rebuilt from another status: %+v", dv.stk.files)
	}
}

// An op finishing with a stash list open and a return queued (a console
// closed while the op ran) must not read the stash list AFTER the return
// swapped the view: the arriving worktree has none, and that was a nil
// dereference. The reload is built for the worktree the op ran in and
// lands there — stamped, so it waits in the slot's queue.
func TestStashOpEndWithAQueuedReturnReloadsTheLeavingList(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m.stashView = &stashView{entries: []model.StashEntry{{Ref: "stash@{0}"}}, tag: "stash"}
	m.pendingReturnView = model.KeyOf(other)
	nm, cmd := m.Update(opFinishedMsg{res: engine.Result{Changed: true}})
	m = nm.(Model)
	if m.viewed != model.KeyOf(other) || m.stashView != nil {
		t.Fatalf("the queued return did not swap the view: viewed=%q stash=%v", m.viewed, m.stashView != nil)
	}
	var list *stashListMsg
	for _, msg := range runCmds(cmd) {
		if l, ok := msg.(stashListMsg); ok {
			list = &l
		}
	}
	if list == nil {
		t.Fatal("no stash list reload was sent")
	}
	nm, _ = m.Update(*list)
	m = nm.(Model)
	if v := m.views[model.KeyOf(home)]; v == nil || len(v.queued) != 1 {
		t.Fatal("the reload did not queue for the worktree the op ran in")
	}
	m = forceSwitch(t, m, home)
	nm, _ = m.Update(heartbeatMsg{})
	m = nm.(Model)
	if m.stashView == nil || m.stashView.loading {
		t.Fatal("A's stash list did not receive its reload on return")
	}
}
