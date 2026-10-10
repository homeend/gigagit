package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

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

// A stacked file's diff is re-labelled by stackCmd; the stamp its loader
// carried must survive the wrap, or the answer lands on whatever worktree is
// shown and is dropped — leaving the stack's inflight count stuck.
func TestStackFileResultWaitsForItsWorktree(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	// A COMMIT's stack: the status reconcile that rebuilds a working-tree
	// stack on every status write never touches it, so a lost answer stays
	// lost — the file loading for good, the inflight slot never freed.
	m = m.pushLayer(&diffView{title: "stack", stk: &diffStack{gen: 5, src: diffNavTree, files: []stackFile{{path: "a.txt", load: stackLoading}}}})
	m.diffLayer().stk.inflight = 1
	stamp := m.stamp() // taken when the loader is built, as the real loaders do
	inner := func() tea.Msg { return diffMsg{slotStamp: stamp, view: &diffView{title: "a.txt"}} }
	cmd := stackCmd(5, 0, inner)
	m = forceSwitch(t, m, other)
	nm, _ := m.Update(cmd())
	m = nm.(Model)
	if v := m.views[model.KeyOf(home)]; v == nil || len(v.queued) != 1 {
		t.Fatal("the stack file's answer was not queued for its worktree")
	}
	m = forceSwitch(t, m, home)
	nm, _ = m.Update(heartbeatMsg{})
	m = nm.(Model)
	dv := m.diffLayer()
	if dv == nil || dv.stk == nil || dv.stk.files[0].load != stackLoaded || dv.stk.inflight != 0 {
		t.Fatalf("A's stack did not receive its file on return: %+v", dv.stk)
	}
}

// Every message whose handler writes into a WINDOW (a diff, the files view,
// a popup, the stash list) carries its slot: these were found unstamped
// after the merge, and each one misrouted a worktree's answer.
func TestWindowMessagesEmbedTheirSlotStamp(t *testing.T) {
	t.Parallel()
	for _, msg := range []any{
		stackFileMsg{}, stackNotesMsg{}, stackStatMsg{}, treeFilesMsg{}, notesLoadedMsg{},
		allNotesScopeMsg{}, contentLandedMsg{}, noteLandedMsg{}, versionHintLoadedMsg{}, stashListMsg{},
	} {
		if _, ok := msg.(slotMsg); !ok {
			t.Errorf("%T does not embed slotStamp", msg)
		}
	}
}
