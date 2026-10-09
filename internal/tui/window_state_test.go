package tui

import (
	"context"
	"os/exec"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The window fields live in one group: saving the live group into a slot
// and loading an empty one leaves no window on the Model, and loading the
// saved one brings every field back. The test touches one field of each
// family; the compile-time guarantee is the embed itself.
func TestWindowStateRoundTrip(t *testing.T) {
	m := loadedModel(t)
	m = m.pushLayer(&diffView{title: "f"})
	m.filesView = &contentPopup{}
	m.filesMode = filesModeWorktree
	m.wtFiles = &worktreeFiles{query: "q"}
	m.stashView = &stashView{}
	m.diffTag = "tag"
	m.pendingSteer = &pendingSteer{}
	m.versionsGen = 7
	m.tour = "ov-1"
	saved := m.windowState
	m.windowState = windowState{}
	if m.topLayer() != nil || m.filesView != nil || m.wtFiles != nil || m.stashView != nil || m.diffTag != "" || m.pendingSteer != nil || m.versionsGen != 0 || m.tour != "" {
		t.Fatal("a window field survived an empty group")
	}
	m.windowState = saved
	if m.topLayer() == nil || m.filesView == nil || m.wtFiles == nil || m.wtFiles.query != "q" || m.stashView == nil || m.diffTag != "tag" || m.pendingSteer == nil || m.versionsGen != 7 || m.tour != "ov-1" {
		t.Fatal("a window field did not come back")
	}
}

// forceSwitch is the swap without switchView's refusals: what a parkable
// popup gets in phase 5. Until then a popup on top refuses the swap, so
// the gen tests drive the slot mechanics directly.
func forceSwitch(t *testing.T, m Model, path string) Model {
	t.Helper()
	m = m.saveView()
	m = m.sleepView()
	return m.loadView(m.ensureView(path))
}

// A remote-heads popup parked in A is not stuck by the slot-data gen bump
// of the swap: its read, stamped at open, still lands when A returns.
func TestParkedRemoteHeadsPopupStillReceivesItsRead(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openRemoteHeadsBrowser()
	gen := m.remoteHeadsGen
	m = forceSwitch(t, m, other)
	m = forceSwitch(t, m, home)
	nm, _ := m.Update(remoteHeadNamesMsg{names: []string{"origin", "upstream"}, gen: gen})
	m = nm.(Model)
	if p := layerOf[*remoteHeadsPopup](m); p == nil || p.loading || len(p.remotes) != 2 {
		t.Fatal("the parked popup dropped its read: it keyed on loadGen, which the swap bumped")
	}
}

// The all-notes popup likewise.
func TestParkedAllNotesPopupStillReceivesItsRead(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openAllNotes()
	gen := m.allNotesGen
	m = forceSwitch(t, m, other)
	m = forceSwitch(t, m, home)
	nm, _ := m.Update(allNotesMsg{gen: gen})
	m = nm.(Model)
	if p := layerOf[*allNotesPopup](m); p == nil || p.loading {
		t.Fatal("the parked popup dropped its read: it keyed on loadGen, which the swap bumped")
	}
}

// A window generation is the slot's own: B opening and closing its own
// versions popup does not move the gen A's parked popup waits on.
func TestVersionsGenIsTheSlotsOwn(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.openBranchVersions("main", false, false)
	gen := m.versionsGen
	m = forceSwitch(t, m, other)
	m, _ = m.openBranchVersions("main", false, false) // B's own popup, B's own gen
	m = m.popLayer()
	m = forceSwitch(t, m, home)
	nm, _ := m.Update(versionsLoadedMsg{gen: gen, branch: "main"})
	m = nm.(Model)
	if p := layerOf[*versionsPopup](m); p == nil || p.loading {
		t.Fatal("A's versions read was dropped: B's open moved A's generation")
	}
}

// A history parked in a sleeping slot keeps its walk: the sweep leaves it.
func TestParkedHistoryKeepsItsWalk(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, ok := m.switchView(other)
	if !ok {
		t.Fatalf("refused: %s", m.statusMsg)
	}
	m.sweepHistoryWalks()
	if stopped {
		t.Fatal("the sweep stopped a walk whose history waits in its worktree")
	}
}

// A slot dropped with its worktree stops the walks parked in it.
func TestDroppedSlotStopsItsParkedWalks(t *testing.T) {
	m := loadedModel(t)
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	m, _ = m.switchView(other)
	m = landView(t, m)
	h := &historyView{}
	stopped := false
	h.cancel = func() { stopped = true }
	m = m.pushLayer(h)
	m.histWalks = &historyWalks{views: []*historyView{h}}
	m, _ = m.switchView(home)
	if out, err := exec.Command("git", "-C", home, "worktree", "remove", "--force", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree remove: %v\n%s", err, out)
	}
	read := m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})
	nm, _ := m.Update(read())
	m = nm.(Model)
	if !stopped {
		t.Fatal("the gone slot's parked history keeps its git running")
	}
}

// A result asked from A lands while B is shown, with B's own popup of the
// same kind open at the same generation: the gate drops it by its slot
// stamp, so B's popup is never filled with A's answer.
func TestResultForASleepingSlotIsDroppedAtTheGate(t *testing.T) {
	m := loadedModel(t)
	m, other := addWorktree(t, m, "wt2")
	m, cmdA := m.openBranchVersions("main", false, false) // A's popup, A's gen
	if cmdA == nil {
		t.Fatal("precondition: the versions read was not launched")
	}
	m = forceSwitch(t, m, other)
	m, _ = m.openBranchVersions("main", false, false) // B's popup, B's gen (the same number)
	nm, _ := m.Update(cmdA())
	m = nm.(Model)
	if p := layerOf[*versionsPopup](m); p == nil || !p.loading {
		t.Fatal("A's versions answer filled B's popup: the result was not gated by its slot")
	}
}

// A result stamped for a slot that is gone is dropped the same way: it
// must never fall through to a handler acting on the worktree on screen.
func TestResultForAGoneSlotIsDroppedAtTheGate(t *testing.T) {
	m := loadedModel(t)
	m, _ = m.openBranchVersions("main", false, false)
	gen := m.versionsGen
	msg := versionsLoadedMsg{gen: gen, branch: "main"}
	msg.slot = model.KeyOf("/nowhere/gone")
	nm, _ := m.Update(msg)
	m = nm.(Model)
	if p := layerOf[*versionsPopup](m); p == nil || !p.loading {
		t.Fatal("a gone slot's answer filled the popup on screen")
	}
}
