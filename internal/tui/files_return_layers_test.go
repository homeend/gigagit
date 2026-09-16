package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A window opened FROM a popup returns to that popup when closed (CLAUDE.md
// convention). The files view is not a layer, so a popup handing off to it
// must park the layer stack rather than clear it; the view's esc/l close
// restores the parked stack, every other teardown drops it.

func versionsPopupOverMain() (Model, *versionsPopup) {
	m := Model{width: 120, height: 40}
	p := &versionsPopup{
		mode:     versionsModeVersions,
		branch:   "main",
		fromList: true, // drilled in from branch mode: a later esc must reach it
		branchRows: []model.VersionedBranch{
			{Branch: "main"}, {Branch: "topic"},
		},
		rows: []model.BranchVersion{
			{Ref: "refs/gg/versions/main/1753100000-rebase", Hash: "5aac0000000000", Subject: "did a rebase", Op: "rebase", Unix: 1753100000, Base: "babe0000000000", Ours: "0ded0000000000"},
			{Ref: "refs/gg/versions/main/1753100001-rebase", Hash: "5aac1111111111", Subject: "did another", Op: "rebase", Unix: 1753100001, Base: "babe1111111111", Ours: "0ded1111111111"},
			{Ref: "refs/gg/versions/main/1753100002-amend", Hash: "a3ed00000000", Subject: "amended", Op: "amend", Unix: 1753100002},
		},
		sel: 1,
	}
	m = m.pushLayer(p)
	return m, p
}

func pressKey(t *testing.T, m Model, key string) Model {
	t.Helper()
	mm, _ := m.Update(keyMsg(key))
	return mm.(Model)
}

// TestFilesViewEscReturnsToVersionsPopup: enter on a version opens the frozen
// preview with the popup gone from the stack; esc on the tree brings back the
// SAME popup instance (branch, row, drilled-from-list flag intact); a second
// esc backs out to branch mode exactly as if the tree had never opened.
func TestFilesViewEscReturnsToVersionsPopup(t *testing.T) {
	t.Parallel()
	m, p := versionsPopupOverMain()

	m = pressKey(t, m, "enter")
	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("enter should open the frozen preview")
	}
	if layerOf[*versionsPopup](m) != nil {
		t.Fatal("the popup must not sit on the stack while the tree is open (it would draw over it)")
	}

	m = pressKey(t, m, "esc")
	if m.filesView != nil {
		t.Fatal("esc should close the files view")
	}
	got := layerOf[*versionsPopup](m)
	if got != p {
		t.Fatalf("esc should restore the SAME versions popup instance, got %v", got)
	}
	if got.mode != versionsModeVersions || got.branch != "main" || got.sel != 1 || !got.fromList {
		t.Fatalf("restored popup lost its state: mode=%d branch=%q sel=%d fromList=%v", got.mode, got.branch, got.sel, got.fromList)
	}

	m = pressKey(t, m, "esc")
	if q := layerOf[*versionsPopup](m); q == nil || q.mode != versionsModeBranches {
		t.Fatal("a second esc should back out to branch mode, as before the tree opened")
	}
}

// TestFilesViewLReturnsToPopupToo: l is the other close key; any close of the
// window returns to the popup that opened it.
func TestFilesViewLReturnsToPopupToo(t *testing.T) {
	t.Parallel()
	m, p := versionsPopupOverMain()
	m = pressKey(t, m, "enter")
	m = pressKey(t, m, "l")
	if m.filesView != nil {
		t.Fatal("l should close the files view")
	}
	if layerOf[*versionsPopup](m) != p {
		t.Fatal("l should restore the versions popup")
	}
}

// TestFieldlessVersionCommitViewReturnsToPopup: the other enter branch (a
// fieldless record opens today's commit view) parks the popup the same way.
func TestFieldlessVersionCommitViewReturnsToPopup(t *testing.T) {
	t.Parallel()
	m, p := versionsPopupOverMain()
	p.sel = 2 // the amend record: no Base/Ours
	m = pressKey(t, m, "enter")
	if m.filesView == nil || m.inCompareMode() {
		t.Fatal("a fieldless record should open the commit view")
	}
	m = pressKey(t, m, "esc")
	if layerOf[*versionsPopup](m) != p {
		t.Fatal("esc should restore the versions popup after the commit view")
	}
}

// TestEntryCompareReturnsToBookmarkSwitcher: the switcher → entry-pair compare
// hand-off (entryCompareMsg) parks the switcher; esc on the tree restores it.
func TestEntryCompareReturnsToBookmarkSwitcher(t *testing.T) {
	t.Parallel()
	m := Model{width: 120, height: 40}
	bp := &bookmarkPopup{}
	m = m.pushLayer(bp)
	mm, _ := m.Update(entryCompareMsg{
		gen:   m.entryCompareGen,
		left:  mustCommitEndpoint("aaaaaaaaaaaaaaaaaaaa"),
		right: mustCommitEndpoint("bbbbbbbbbbbbbbbbbbbb"),
	})
	m = mm.(Model)
	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("the compare should open the files view")
	}
	if m.bookmarkSwitcher() != nil {
		t.Fatal("the switcher must not draw over the files view")
	}
	m = pressKey(t, m, "esc")
	if m.bookmarkSwitcher() != bp {
		t.Fatal("esc should restore the bookmark switcher")
	}
}

// TestReopenInsideLiveViewKeepsParkedPopup: enter on the commit list inside
// an open view re-runs the opener (drill into the tree); the window is still
// the one the popup opened, so the park survives the opener's clean slate.
func TestReopenInsideLiveViewKeepsParkedPopup(t *testing.T) {
	t.Parallel()
	m, p := versionsPopupOverMain()
	p.sel = 2 // commit view
	m = pressKey(t, m, "enter")
	mm, _ := m.openChangedFiles(model.Commit{Hash: "a3ed00000000", Subject: "amended"})
	m = mm
	if m.filesView == nil {
		t.Fatal("re-open should keep a files view open")
	}
	m = pressKey(t, m, "esc")
	if layerOf[*versionsPopup](m) != p {
		t.Fatal("a re-open inside the live view must not drop the parked popup")
	}
}

// TestParkedLayersDropOnOtherTeardowns: only the view's own close returns to
// the popup. A repo switch or a steer navigation tears the view down for a
// reason that makes the popup meaningless, so nothing comes back.
func TestParkedLayersDropOnOtherTeardowns(t *testing.T) {
	t.Parallel()
	t.Run("steerToPanels", func(t *testing.T) {
		t.Parallel()
		m, _ := versionsPopupOverMain()
		m = pressKey(t, m, "enter")
		m = m.steerToPanels()
		if m.filesView != nil || m.topLayer() != nil || len(m.filesReturnLayers) != 0 {
			t.Fatal("steerToPanels should drop the parked popup, not restore it")
		}
	})
	t.Run("fresh open from the panels", func(t *testing.T) {
		t.Parallel()
		m, _ := versionsPopupOverMain()
		m = pressKey(t, m, "enter")
		// A second files view opened while the first is up (e.g. a commit row
		// enter from a panel the tree left focus on) is a clean slate: the
		// stale parked popup must not resurface when THAT view closes.
		m = m.closeFilesView()
		mm, _ := m.openChangedFiles(model.Commit{Hash: "cccccccccccccccccccc", Subject: "x"})
		m = mm
		m = pressKey(t, m, "esc")
		if m.topLayer() != nil {
			t.Fatal("a view opened from the panels must close to the panels")
		}
	})
}
