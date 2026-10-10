package tui

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
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
		allNotesScopeMsg{}, contentLandedMsg{}, noteLandedMsg{}, versionHintLoadedMsg{}, stashListMsg{}, prRevalidatedMsg{},
		shelfAddedMsg{}, shelfSetAddedMsg{}, // hunt 2: a shelf write consumes the marks of the worktree the files were marked in
	} {
		if _, ok := msg.(slotMsg); !ok {
			t.Errorf("%T does not embed slotStamp", msg)
		}
	}
}

// A full load saves the viewed slot's fields in place; it is not a swap,
// so the `gg session highlight` bands on working files stay live — they
// used to move into the slot and vanish from the screen until the next
// switch, which then overwrote the stashed copy.
func TestFullLoadKeepsTheWorkingFileBandsLive(t *testing.T) {
	m := loadedModel(t)
	k := attentionKey{path: "a.txt", state: "worktree"}
	m.attention = map[attentionKey][]steerMark{k: {{side: "new", start: 1, end: 2}}}
	nm, cmd := m.Update(m.loadCmd()())
	m = settleLoad(t, nm.(Model), cmd)
	if len(m.attention[k]) != 1 {
		t.Fatal("the full load took the working-file bands off the screen")
	}
}

// alt+f / alt+b belong to the console only while it has the keyboard: a
// popup or the . menu layered above it owns every key, these two included.
func TestAltFAndAltBUnderAPopupLeaveTheConsoleAlone(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "A1")
	m, _ = m.showConsole(id, true) // docked, bound
	m = m.pushLayer(&contentPopup{title: "over it"})
	m = pressAlt(t, m, 'f')
	if m.console == nil || m.console.maximized {
		t.Fatalf("alt+f under a popup resized the console: %+v", m.console)
	}
	m = pressAlt(t, m, 'b')
	if m.console == nil || !m.console.focused {
		t.Fatalf("alt+b under a popup unbound the console: %+v", m.console)
	}
}

// alt+b unbinding is the step-out key's unbinding: a docked console the
// user had ctrl+t-maximised docks again (one shown over a full-screen view
// stays full-screen, as there).
func TestAltBUnbindDocksACtrlTMaximisedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "A1")
	m, _ = m.showConsole(id, true) // docked, bound
	m.console.maximized = true     // ctrl+t over the Commits column: the pin (ret.full) stays docked
	m = pressAlt(t, m, 'b')
	if m.console == nil || m.console.focused || m.console.maximized {
		t.Fatalf("after alt+b: %+v, want unbound and docked", m.console)
	}
}

// alt+w during an operation is refused whole: the console stays shown and
// the keyboard focus stays where it was — the refusal comes before the
// hide and the Branches focus, not after them.
func TestAltWDuringAnOpChangesNothing(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.showConsole(id, false)
	m.running = true
	m = pressAlt(t, m, 'w')
	if m.console == nil || m.viewed != model.KeyOf(other) || m.focus != panelCommits {
		t.Fatalf("console=%v viewed=%q focus=%v, want the console still shown and focused", m.console != nil, m.viewed, m.focus)
	}
}

// A maximised console's footer offers alt+f as the way back to the Commits
// column, not another "max".
func TestMaximisedConsoleFooterSaysDock(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	installSessionManager(t)
	id := startSessionIn(t, m, m.currentWorktree, "A1")
	m, _ = m.showConsole(id, true)
	m = pressAlt(t, m, 'f') // maximised, bound
	m.console.focused = false
	if !m.consoleFull() {
		t.Fatalf("precondition: %+v", m.console)
	}
	hint, _ := m.footerOverride()
	if !strings.Contains(hint, "[alt+f] dock") || strings.Contains(hint, "[alt+f] max") {
		t.Fatalf("footer %q, want [alt+f] dock", hint)
	}
}

// A navigate parked in a worktree that is then removed is answered when
// its slot goes — the sender would otherwise wait out its whole timeout
// for a view that never opens. (A local navigate's refusal is the status
// line's startAtFailMsg; a `gg session navigate --wait` gets the reply.)
func TestPrunedSlotAnswersItsParkedNavigate(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(home, "only-home.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ = m.switchView(other)
	m, _ = m.steerNavigateStatusFile(steer.Command{Cmd: "navigate", File: "only-home.txt"}, false)
	if m.pendingSteer == nil {
		t.Fatal("precondition: parked in wt2")
	}
	m, _ = m.switchView(home) // wt2 sleeps with the navigate parked
	if out, err := exec.Command("git", "-C", home, "worktree", "remove", "--force", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %v\n%s", err, out)
	}
	nm, cmd := m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	m = nm.(Model)
	if m.views[model.KeyOf(other)] != nil {
		t.Fatal("precondition: the slot was not pruned")
	}
	var failed bool
	for _, msg := range runCmds(cmd) {
		if _, ok := msg.(startAtFailMsg); ok {
			failed = true
		}
	}
	if !failed {
		t.Fatal("the parked navigate was dropped with its slot, unanswered")
	}
}

// A console whose show was queued (its worktree could not be shown during
// an operation) and that then steps aside for another right-column owner
// takes its queued show with it: the panels must not jump to its worktree
// once the operation ends.
func TestDroppedConsoleCancelsItsQueuedShow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m.running = true
	m, _ = m.showConsole(id, false)
	if m.console == nil || m.pendingReturnView != model.KeyOf(other) {
		t.Fatalf("precondition: console=%v pending=%q", m.console != nil, m.pendingReturnView)
	}
	m = m.dropConsole()
	if m.pendingReturnView != "" {
		t.Fatalf("pendingReturnView=%q after the console was dropped, want none", m.pendingReturnView)
	}
}

// A rebase-range read started in one worktree must not start the rebase
// through another worktree's service after a switch (as stageHunksLoadedMsg
// already refuses).
func TestRangeLoadsRefuseAnotherWorktreesService(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	aSvc := m.svc
	m, _ = m.switchView(other)
	for name, msg := range map[string]tea.Msg{
		"rebase":  rebaseRangeLoadedMsg{svc: aSvc, branch: "main", onto: "HEAD~1", commits: []model.RangeCommit{{Hash: "a"}}},
		"squash":  squashRangeLoadedMsg{svc: aSvc, branch: "main", onto: "HEAD~1", commits: []model.RangeCommit{{Hash: "a"}}},
		"drop":    dropRangeLoadedMsg{svc: aSvc, branch: "main", onto: "HEAD~1", commits: []model.RangeCommit{{Hash: "a"}}},
		"irebase": irebaseLoadedMsg{svc: aSvc, branch: "main", onto: "HEAD~1", commits: []model.RangeCommit{{Hash: "a"}}},
	} {
		nm, _ := m.Update(msg)
		got := nm.(Model)
		if got.running || got.topLayer() != nil || got.statusMsg != "" {
			t.Errorf("%s: a stale range load acted on the switched-to worktree: running=%v top=%T status=%q", name, got.running, got.topLayer(), got.statusMsg)
		}
	}
}

// The plain editors the spec promised would wait with their worktree
// (add-worktree, stash, tag: no result of their own in flight) are
// parkable; the user's alt+w over them switches instead of typing into them.
func TestPlainFormPopupsArePark(t *testing.T) {
	t.Parallel()
	for name, l := range map[string]layer{
		"stash":        &stashPopup{},
		"add worktree": &worktreePopup{},
		"tag":          &tagPopup{},
	} {
		if !parkableLayer(l) {
			t.Errorf("%s: not parkable, alt+w types into it", name)
		}
	}
}

// A text field never takes an alt+letter as text: alt+w, alt+a and the
// other alt keys are gg's, and a field that inserted the letter made the
// key both switch and type.
func TestTextfieldIgnoresAltLetters(t *testing.T) {
	t.Parallel()
	var f textfield
	if f.HandleEditKey(altKey('w')) || string(f.runes) != "" {
		t.Fatalf("alt+w inserted %q", string(f.runes))
	}
	f.HandleEditKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	if string(f.runes) != "w" {
		t.Fatalf("a plain w did not insert: %q", string(f.runes))
	}
}

// A file list the sleeping slot will certainly discard (its F window was
// put to sleep: the generation moved on) is dropped at the gate instead of
// waiting in the queue — on a large tree that is every path of the
// worktree, kept for as long as nobody returns.
func TestStaleFileListIsNotQueuedForASleepingSlot(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	v := m.ensureView(other)
	v.windows.wtFiles = &worktreeFiles{gen: 3}
	key := model.KeyOf(other)
	nm, _ := m.Update(lsFilesMsg{slotStamp: slotStamp{slot: key}, gen: 2, paths: []string{"a"}})
	if n := len(nm.(Model).views[key].queued); n != 0 {
		t.Fatalf("a stale file list was queued (%d)", n)
	}
	nm, _ = m.Update(lsFilesMsg{slotStamp: slotStamp{slot: key}, gen: 3, paths: []string{"a"}})
	if n := len(nm.(Model).views[key].queued); n != 1 {
		t.Fatalf("the current file list was not queued (%d)", n)
	}
}
