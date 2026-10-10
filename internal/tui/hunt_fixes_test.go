package tui

import (
	"testing"

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
