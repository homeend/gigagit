package tui

import (
	"os/exec"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/syntax"
)

// Second post-merge hunt of the fast worktree switch (2026-10-10): the
// operation boundary, the keys as a state machine, the replay queue and
// the shared state.

// An async step between an op key and its startOp (the pre-push remote-tag
// check) is addressed to the worktree it was asked from: landing after a
// swap it must not push the worktree now on screen.
func TestPushTagCheckAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pushCheckGen++
	gen := m.pushCheckGen
	m, _ = viewedOther(t, m)
	mm, cmd := m.Update(pushTagCheckMsg{gen: gen, svc: svcA, remoteSet: map[string]bool{}})
	m = mm.(Model)
	if m.running || cmd != nil || m.modal != nil {
		t.Fatalf("a push check from the worktree that left the screen must not start a push here (running=%v cmd=%v modal=%v)", m.running, cmd, m.modal)
	}
	if m.statusMsg == "" {
		t.Fatal("the dropped push must be said")
	}
}

// Likewise the cherry-pick probe of the bookmark switcher (a parkable
// popup): its confirm must not open over, nor pick into, another worktree.
func TestPickProbeAfterASwapIsDropped(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	svcA := m.svc
	m.pickGen++
	gen := m.pickGen
	m, _ = viewedOther(t, m)
	mm, _ := m.Update(pickProbeMsg{gen: gen, svc: svcA, target: pickTarget{sha: "abc"},
		line: model.LogLine{Hash: "abc1234", Subject: "s"}, found: true})
	m = mm.(Model)
	if m.modal != nil {
		t.Fatal("a probe from the worktree that left the screen must not open its confirm here")
	}
}

// Every message whose handler reaches startOp (or a confirm that starts
// one) after an async step names the worktree it was asked from — the
// Service it was read through, or a slot stamp — and its handler drops a
// mismatch: between the key and the op the panels may have swapped, and
// the op would run in the worktree on screen THEN. The range loads had
// this from the first; the push check and the pick probe did not.
func TestAsyncOpStartersNameTheirWorktree(t *testing.T) {
	t.Parallel()
	for _, msg := range []any{
		pushTagCheckMsg{}, pickProbeMsg{},
		rebaseRangeLoadedMsg{}, squashRangeLoadedMsg{}, dropRangeLoadedMsg{}, irebaseLoadedMsg{},
		amendPrefillMsg{}, conflictFileLoadedMsg{}, stageHunksLoadedMsg{}, unstageHunksLoadedMsg{}, reviewTargetReadyMsg{},
	} {
		if _, ok := msg.(slotMsg); ok {
			continue
		}
		if f, ok := reflect.TypeOf(msg).FieldByName("svc"); !ok || f.Type != reflect.TypeOf((*domain.Service)(nil)) {
			t.Errorf("%T carries neither a slot stamp nor the *domain.Service it was asked through", msg)
		}
	}
}

// The editor's exit re-reads the status with no op of its own; its result
// must not clear the busy flag of an op started meanwhile (a pull pressed
// before the slow read landed): with it cleared the swap refusal lifted
// mid-op, and a second op key overwrote the first op's channel.
func TestEditorStatusReadKeepsARunningOpBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running, m.opName = true, "pull"
	mm, _ := m.Update(m.reloadStatusCmd("")())
	m = mm.(Model)
	if !m.running || m.opName != "pull" {
		t.Fatalf("the editor's status read cleared the running op (running=%v opName=%q)", m.running, m.opName)
	}
	if m.switchRefusalBy(true) == "" {
		t.Fatal("alt+w must stay refused while the op runs")
	}
}

// A staging round's own result does clear it: that round set it.
func TestStagingResultClearsBusy(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	m.running = true
	mm, _ := m.Update(statusRefreshedMsg{svc: m.svc, staging: true, status: m.status})
	if mm.(Model).running {
		t.Fatal("a staging round's result must clear the busy flag it set")
	}
}

// A parkable popup that arrived UNDER a shown console (B's half-typed
// commit box, displaced when alt+a showed B's agent) is still parkable: the
// user's own swap must judge the same top steerRefusal does — the parked
// one — not the empty live pile, which made alt+w say "cannot switch while
// a window is open" with nothing but the console on screen.
func TestAltWUnderAConsoleOverAParkedPopupHidesIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Agent")
	home := m.home
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("switchView refused: %s", m.statusMsg)
	}
	box := &commitPopup{title: newTextField("half a message")}
	m = m.pushLayer(box)
	m = pressAlt(t, m, 'w') // the box parks with wt2; home shows
	if m.viewed != home || m.topLayer() != nil {
		t.Fatalf("precondition: viewed=%q top=%T", m.viewed, m.topLayer())
	}
	m, _ = m.showConsoleBy(id, true, true) // alt+a onto wt2's agent: wt2 arrives under the console
	if m.viewed != model.KeyOf(other) || m.topLayer() != nil || m.consoleParked == nil || len(m.consoleParked.layers) != 1 {
		t.Fatalf("precondition: viewed=%q top=%T parked=%+v", m.viewed, m.topLayer(), m.consoleParked)
	}
	m, _ = m.cycleWorktrees()
	if m.console != nil {
		t.Fatalf("alt+w must hide the console first (status %q)", m.statusMsg)
	}
	if m.topLayer() != box {
		t.Fatalf("the parked box must be live again, got %T", m.topLayer())
	}
}

// The walk (alt+a) shows the Branches tab for its session row; closing the
// console must put the keyboard on the tab that is SHOWN, not on the tab
// the console was opened from (Worktrees), which is not on screen: no lit
// border, no cursor, a footer for the wrong panel, and enter switching to a
// worktree row the user cannot see.
func TestClosingAConsoleOpenedFromAnotherTabFocusesTheShownTab(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	installSessionManager(t)
	startSessionIn(t, m, m.currentWorktree, "A1")
	m.activeLeftTab, m.focus, m.lastLeftPanel = panelWorktrees, panelWorktrees, panelWorktrees
	m = pressAlt(t, m, 'a')
	if m.console == nil || m.activeLeftTab != panelBranches {
		t.Fatalf("precondition: console=%v tab=%v", m.console != nil, m.activeLeftTab)
	}
	mm, _ := m.Update(ctrlBracket()) // unbind
	m = mm.(Model)
	mm, _ = m.Update(ctrlBracket()) // close
	m = mm.(Model)
	if m.console != nil {
		t.Fatal("precondition: the second ctrl+] closes the console")
	}
	if m.focus != m.activeLeftTab {
		t.Fatalf("focus=%v but the shown tab is %v", m.focus, m.activeLeftTab)
	}
}

// A `t`-maximised Worktrees tab stays maximised as Branches when the walk
// shows the Branches tab (activateTab's own re-pin rule): the pin must
// not name a hidden tab.
func TestTheWalkRepinsAMaximisedLeftTab(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	installSessionManager(t)
	startSessionIn(t, m, m.currentWorktree, "A1")
	m.activeLeftTab, m.focus, m.lastLeftPanel = panelWorktrees, panelWorktrees, panelWorktrees
	m.leftMaxed, m.leftMax = true, panelWorktrees
	m = pressAlt(t, m, 'a')
	if m.leftMax != panelBranches {
		t.Fatalf("leftMax=%v, want the shown Branches tab", m.leftMax)
	}
}

// The HEAD reflog is PER WORKTREE (git keeps it under worktrees/<name>/logs/
// HEAD), not the repository's: the slot keeps it, the kick re-reads it, and
// a read launched through the leaving worktree cannot land on the arriving
// one. The Reflog tab used to keep showing the previous worktree's moves.
func TestReflogFollowsTheWorktree(t *testing.T) {
	m := loadedModel(t)
	homeLog := m.reflog
	if len(homeLog) == 0 {
		t.Fatal("precondition: home has a reflog")
	}
	gen := m.srcGen[srcReflog]
	m, _ = viewedOther(t, m) // the kick lands: wt2's own reflog
	if m.srcGen[srcReflog] == gen {
		t.Fatal("a swap must retire a reflog read in flight through the leaving worktree")
	}
	if len(m.reflog) == 0 || len(m.reflog) == len(homeLog) && m.reflog[0].Subject == homeLog[0].Subject && m.reflog[0].Selector == homeLog[0].Selector {
		t.Fatalf("after the swap the Reflog tab still shows home's log (%d entries)", len(m.reflog))
	}
	m, ok := m.switchView(m.homeWorktree())
	if !ok {
		t.Fatal(m.statusMsg)
	}
	if len(m.reflog) != len(homeLog) {
		t.Fatalf("back home before any read: %d entries, want home's %d from the slot", len(m.reflog), len(homeLog))
	}
}

// A branches read launched through the leaving worktree lands after the
// swap with ITS `%(HEAD)`: the list is the repository's and stays, but the
// `*` mark is the viewed worktree's and is re-marked on every arrival, not
// only on the swap.
func TestBranchesArrivalKeepsTheViewedHeadMark(t *testing.T) {
	m := loadedModel(t)
	m, _ = viewedOther(t, m)
	headOf := func(bs []model.Branch) string {
		for _, b := range bs {
			if b.IsHead {
				return b.Name
			}
		}
		return ""
	}
	if headOf(m.branches) != "wt2" {
		t.Fatalf("precondition: * on %q, want wt2", headOf(m.branches))
	}
	stale := slices.Clone(m.branches)
	for i := range stale {
		stale[i].IsHead = stale[i].Name == "main" // read through home's service
	}
	mm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches], value: stale})
	m = mm.(Model)
	if headOf(m.branches) != "wt2" {
		t.Fatalf("after the stale arrival * is on %q, want the viewed wt2", headOf(m.branches))
	}
}

// A sharedWriter addressed to a slot that is GONE (pruned while its read
// was in flight) still lands its repository-wide part: the shelf list and
// a PR row are nobody's in particular.
func TestSharedPartOfAMessageForAGoneSlotStillLands(t *testing.T) {
	t.Parallel()
	m := loadedModel(t)
	entries := []model.ShelfEntry{{ID: "s1"}}
	msg := shelfLoadedMsg{slotStamp: slotStamp{slot: model.KeyOf("/gone/worktree")}, entries: entries}
	m, took := m.routeSlotMsg(msg)
	if !took {
		t.Fatal("the gate must take a message for a gone slot")
	}
	if len(m.shelfEntries) != 1 || m.shelfEntries[0].ID != "s1" {
		t.Fatalf("the shelf list (shared) was lost with the gone slot: %+v", m.shelfEntries)
	}
}

// A click on another left panel moves the focus without unbinding the
// console (the next key does): the ghost cursor is the BOUND console's
// stand-in for the missing Commits focus, so it must not be drawn beside
// the clicked panel's real cursor.
func TestGhostCursorNeedsTheCommitsFocus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	startSessionIn(t, m, other, "Shell")
	m = pressAlt(t, m, 'a')
	if m.console == nil || !m.console.focused {
		t.Fatal("precondition: the console is bound")
	}
	m.focus = panelFiles // a click on the Files panel
	out := ansi.Strip(m.renderPanel(panelBranches, "Branches", m.branchRows(), nil, 60, 12))
	if strings.Contains(out, "> ") {
		t.Fatalf("the ghost cursor is drawn while another panel has the focus:\n%s", out)
	}
}

// The file-path popup's list is read off-thread and lands unstamped on the
// live pile: parked while loading, it would never fill ("(loading…)" for
// good) — like the other popups with work in flight, it parks only once
// the list landed.
func TestFilePathPopupIsNotParkableWhileLoading(t *testing.T) {
	t.Parallel()
	if parkableLayer(&filePathPopup{loading: true}) {
		t.Fatal("a loading file-path popup must refuse the swap")
	}
	if !parkableLayer(&filePathPopup{}) {
		t.Fatal("a loaded file-path popup parks")
	}
}

// A shelf write's result consumes the MARKS of the worktree the files were
// marked in: stamped, it waits for that slot instead of deleting the same
// paths from whatever worktree is on screen when the tar lands.
func TestShelfResultConsumesTheMarksOfItsOwnWorktree(t *testing.T) {
	m := loadedModel(t)
	home := m.home
	m, other := viewedOther(t, m)
	m.fileMarks = map[string]bool{"a.go": true} // wt2's own marks on the same paths
	mm, _ := m.Update(shelfSetAddedMsg{slotStamp: slotStamp{slot: home}, paths: []string{"a.go"}})
	m = mm.(Model)
	if !m.fileMarks["a.go"] {
		t.Fatal("home's shelf result consumed wt2's mark")
	}
	if q := m.views[home].queued; len(q) != 1 {
		t.Fatalf("home's slot holds %d queued, want its shelf result", len(q))
	}
	mm, _ = m.Update(shelfAddedMsg{slotStamp: slotStamp{slot: home}, unmark: "a.go"})
	if m = mm.(Model); !m.fileMarks["a.go"] {
		t.Fatal("home's single-file shelf result consumed wt2's mark")
	}
	_ = other
}

// The PR fetch's follow-up (open the PR's diff) is built BEFORE the queued
// return swaps the panels — as the stash arm's list reload is — so it is
// stamped for the worktree the fetch ran in, not opened in the one swapped
// to.
func TestPRFetchFollowUpIsBuiltBeforeTheQueuedReturn(t *testing.T) {
	m := loadedModel(t)
	home := m.home
	m, other := viewedOther(t, m)
	m.pendingReturnView = home // a console closed while the fetch ran
	m.pendingPROpen = &model.PullRequest{Number: 7}
	m.running = true
	mm, cmd := m.Update(opFinishedMsg{})
	m = mm.(Model)
	if m.viewed != home {
		t.Fatalf("precondition: the queued return went (viewed=%q)", m.viewed)
	}
	var stamp model.CheckoutKey
	for _, msg := range runCmds(cmd) {
		if po, ok := msg.(previewOpenMsg); ok && po.prNumber == 7 {
			stamp = po.slotKey()
		}
	}
	if stamp != model.KeyOf(other) {
		t.Fatalf("the PR open is stamped %q, want the fetch's worktree %q", stamp, model.KeyOf(other))
	}
}

// The remote's tag set is the repository's: a read in flight survives an
// in-repo swap (only a repository switch retires it).
func TestRemoteTagsReadSurvivesASwap(t *testing.T) {
	m := loadedModel(t)
	msg := m.remoteTagsCmd(t.Context(), true)().(remoteTagsMsg) // no origin here: the names come from the test
	msg.err, msg.names = nil, map[string]bool{"v1": true}
	m, _ = viewedOther(t, m)
	mm, _ := m.Update(msg)
	if m = mm.(Model); !m.remoteTagNames["v1"] {
		t.Fatal("the remote tags read was dropped by the in-repo swap")
	}
}

// A commit-message task that ends while its worktree sleeps leaves the
// message as THAT worktree's pending one (c there opens it), with the
// notice saying where; "not here" used to mean another repository.
func TestCommitMessageForASleepingWorktreeIsKeptPending(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m.pendingCommitMsg = map[string]pendingMessage{}
	m, _ = m.applyCommitMessage(domain.TaskInfo{Key: "k", Agent: "claude", Worktree: other, Result: "feat: x"})
	if pm, ok := m.pendingCommitMsg[pendingKey(other)]; !ok || pm.text != "feat: x" {
		t.Fatalf("pending for wt2 = %+v, %v", pm, ok)
	}
	if pendingKey(other) != string(model.KeyOf(other)) {
		t.Fatal("pendingKey must be the checkout key every other per-worktree map uses")
	}
}

// A hunk picker's syntax runs land through the picker's own pointer: a
// picker parked with its worktree (alt+w during the lex) is still live
// — nothing recomputes the runs, so a dropped lex left it plain for good.
func TestParkedHunkPickerStillGetsItsSyntax(t *testing.T) {
	m := loadedModel(t)
	p := &hunkPicker{}
	m = m.pushLayer(p)
	m, other := addWorktree(t, m, "wt2")
	m = forceSwitch(t, m, other)
	if m.topLayer() != nil {
		t.Fatal("precondition: the picker parked with home")
	}
	runs := [][]syntax.Tok{{{Start: 0, End: 1}}}
	mm, _ := m.Update(pickerLexedMsg{picker: p, cur: runs, inc: runs})
	_ = mm
	if p.curTok == nil {
		t.Fatal("the parked picker's lex was dropped")
	}
}

// A worktree arriving under a shown console has its stash list displaced
// (consoleParked); the list's own read, replayed then, must still fill it:
// the console-parked dispatch restores the stash list and the preview
// beneath the handler as it does the layers.
func TestParkedStashListUnderAConsoleStillFills(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.stashView = &stashView{tag: "t1", loading: true}
	m, _ = m.openConsole(s.Info().ID) // captureReturn parks the stash list under the console
	if m.stashView != nil || m.consoleParked == nil || m.consoleParked.stashView == nil {
		t.Fatalf("precondition: stashView=%v parked=%+v", m.stashView, m.consoleParked)
	}
	mm, _ := m.Update(stashListMsg{slotStamp: m.stamp(), tag: "t1", entries: []model.StashEntry{{Ref: "stash@{0}"}}})
	m = mm.(Model)
	sv := m.consoleParked.stashView
	if sv == nil || sv.loading || len(sv.entries) != 1 {
		t.Fatalf("the parked stash list did not take its read: %+v", sv)
	}
	if m.stashView != nil {
		t.Fatal("the stash list must be parked again after the dispatch")
	}
}

// A queued return taken at an op's end swaps BEFORE the op's reload: the
// leaving slot recorded the pre-op list's branch, and a checkout op's own
// worktree was then "recycled" by the list arrival (its windows dropped).
// After a HEAD-moving op the leaving slot takes its baseline from the next
// list instead.
func TestQueuedReturnAfterACheckoutDoesNotRecycleTheOpsWorktree(t *testing.T) {
	m := loadedModel(t)
	home := m.home
	m, other := addWorktree(t, m, "wt2")
	dv := &diffView{title: "a.go", rev: "abc123"}
	m = m.pushLayer(dv)                      // a window open in home over the op
	m.pendingReturnView = model.KeyOf(other) // a console show queued the swap while the op ran
	m.running = true
	m.pendingSources = nil // a checkout: every source, the worktree list included
	if out, err := exec.Command("git", "-C", m.currentWorktree, "checkout", "-q", "-b", "feat/x").CombinedOutput(); err != nil {
		t.Fatalf("checkout: %v\n%s", err, out)
	}
	mm, cmd := m.Update(opFinishedMsg{})
	m = mm.(Model)
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("precondition: the queued return went (viewed=%q)", m.viewed)
	}
	for _, msg := range runCmds(cmd) {
		if da, ok := msg.(dataAvailableMsg); ok && da.source == srcWorktrees {
			mm, _ = m.Update(da)
			m = mm.(Model)
		}
	}
	if v := m.views[home]; v == nil || !v.windows.holds(dv) {
		t.Fatal("the checkout's own worktree was treated as recycled: its window is gone")
	}
	if v := m.views[home]; v.branch != "feat/x" || !v.branchKnown {
		t.Fatalf("home's record = %q known=%v, want the post-op branch", v.branch, v.branchKnown)
	}
}

// With one worktree (the bare entry does not count) the footer does not
// advertise alt+w, and the hide-first step says there is no next one.
func TestAltWHintsWithOneWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.worktrees = append(m.worktrees, model.Worktree{Path: "/bare.git", Bare: true})
	b, _ := bindingByID("next-worktree")
	if b.when(m) {
		t.Fatal("the footer advertises alt+w with one worktree beside the bare entry")
	}
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m, _ = m.cycleWorktrees()
	if m.console != nil {
		t.Fatal("the press hides the console")
	}
	if m.statusMsg == i18n.T("console hidden — alt+w again for the next worktree") {
		t.Fatalf("the hint promises a next worktree there is none: %q", m.statusMsg)
	}
}

// On the startup fan-out the branches may land BEFORE the worktree list:
// with no list to say which branch the viewed worktree has, the read's own
// `%(HEAD)` stands (re-marking from an empty list cleared every `*`).
func TestBranchesArrivalBeforeTheWorktreeListKeepsTheReadsHead(t *testing.T) {
	m := loadedModel(t)
	m.worktrees = nil
	bs := slices.Clone(m.branches)
	for i := range bs {
		bs[i].IsHead = bs[i].Name == "main"
	}
	mm, _ := m.Update(dataAvailableMsg{source: srcBranches, gen: m.srcGen[srcBranches], value: bs})
	m = mm.(Model)
	for _, b := range m.branches {
		if b.Name == "main" && !b.IsHead {
			t.Fatal("the read's own head mark was cleared with no worktree list to re-mark from")
		}
	}
}

// Only an op whose reload re-reads the worktree LIST re-baselines the
// leaving slot: after a commit (no list read) the record stays known, or
// the slot would sit un-baselined and a recycle meanwhile would be adopted
// instead of caught.
func TestQueuedReturnAfterACommitKeepsTheBranchRecord(t *testing.T) {
	m := loadedModel(t)
	home := m.home
	m, other := addWorktree(t, m, "wt2")
	m.pendingReturnView = model.KeyOf(other)
	m.running = true
	m.pendingSources = opAffectedSources(engine.Commit{}) // status, feed, branches, reflog — no worktree list
	mm, _ := m.Update(opFinishedMsg{})
	m = mm.(Model)
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("precondition: the queued return went (viewed=%q)", m.viewed)
	}
	if v := m.views[home]; !v.branchKnown || v.branch != "main" {
		t.Fatalf("home's record = %q known=%v, want main kept", v.branch, v.branchKnown)
	}
}

// Every op that can change the checked-out branch re-reads the worktree
// list, so the re-baseline above fires for it (the unmapped switch/checkout
// ops read every source).
func TestBranchChangingOpsReloadTheWorktreeList(t *testing.T) {
	t.Parallel()
	for _, op := range []engine.Operation{engine.CheckoutRemoteBranch{}, engine.SmartSwitch{}, engine.SmartCheckout{}, engine.Checkout{}, engine.RenameBranch{}, engine.RestoreBranchVersion{}} {
		srcs := opAffectedSources(op)
		if srcs != nil && !slices.Contains(srcs, srcWorktrees) {
			t.Errorf("%T reloads %v without the worktree list", op, srcs)
		}
	}
}
