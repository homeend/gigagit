package tui

import "testing"

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
