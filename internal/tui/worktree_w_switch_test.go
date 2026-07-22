package tui

import (
	"strings"
	"testing"
)

// Model-level W = "w + create & switch": the popup opens for the selected
// EXISTING branch (no templated new branch, so the dir carries no b/from-
// prefix or random suffix) with create-and-switch as enter's default. The old
// new-templated-branch flow lives in the Branches `.` menu.

func TestShiftWOpensExistingModeWithSwitchDefault(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>-<random-alpha:4>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	p := layerOf[*worktreePopup](updated.(Model))
	if p == nil {
		t.Fatal("W should open the worktree popup")
	}
	if !p.existing {
		t.Fatal("W must open in existing-branch mode (same popup as w)")
	}
	if !p.switchOnCreate {
		t.Fatal("W must default enter to create & switch")
	}
	if p.previewBranch != p.startPoint {
		t.Fatalf("previewBranch = %q, want the fixed selection %q", p.previewBranch, p.startPoint)
	}
	if strings.Contains(p.previewPath, "b-from-") || strings.Contains(p.previewPath, "b/from-") {
		t.Fatalf("previewPath = %q must not carry the branch-template prefix/random suffix", p.previewPath)
	}
}

func TestSmallWHasNoSwitchDefault(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p == nil || p.switchOnCreate {
		t.Fatal("w must open the popup without the switch default")
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.pendingSwitch {
		t.Fatal("enter in a w-opened popup must create WITHOUT switching")
	}
	if !m.running {
		t.Fatal("enter should still start the create op")
	}
}

func TestShiftWEnterCreatesAndSwitches(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	if layerOf[*worktreePopup](m) != nil {
		t.Error("popup should close on create-and-switch")
	}
	if !m.running {
		t.Error("enter should start the create op")
	}
	if !m.pendingSwitch {
		t.Error("enter in a W-opened popup must mark pendingSwitch (create & switch)")
	}
}

func TestShiftWPopupPlainCreateStillOffered(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.pendingSwitch {
		t.Fatal("w inside a W-opened popup must create WITHOUT switching")
	}
	if !m.running {
		t.Fatal("w should still start the create op")
	}
}

func TestShiftWSwitchHintRendered(t *testing.T) {
	m := modelWithConfig(t, "b/auto", "../<repo>.worktrees/<branch>")
	m.width, m.height = 100, 30
	updated, _ := m.Update(keyMsg("W"))
	out := updated.(Model).View()
	if !strings.Contains(out, "[enter/W] create & switch") {
		t.Errorf("W-opened popup must advertise enter as create & switch:\n%s", out)
	}
}

// The new-templated-branch flow moved to the Branches `.` menu.

func TestWorktreeNewBranchRowGating(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	m.focus = panelBranches
	r, ok := m.worktreeNewBranchRow()
	if !ok {
		t.Fatal("new-branch worktree row must be present on the Branches panel")
	}
	if r.run == nil {
		t.Fatal("new-branch worktree row must have a run handler")
	}
	m.focus = panelCommits
	if _, ok := m.worktreeNewBranchRow(); ok {
		t.Fatal("new-branch worktree row must be Branches-panel only")
	}
}

func TestWorktreeNewBranchRowOpensTemplatedPopup(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	m.focus = panelBranches
	r, ok := m.worktreeNewBranchRow()
	if !ok {
		t.Fatal("row absent")
	}
	tm, _ := r.run(m)
	p := layerOf[*worktreePopup](tm.(Model))
	if p == nil {
		t.Fatal("row must open the worktree popup")
	}
	if p.existing || p.switchOnCreate {
		t.Fatalf("row must open new-branch mode without a switch default, got existing=%v switchOnCreate=%v", p.existing, p.switchOnCreate)
	}
	if !strings.HasPrefix(p.previewBranch, "b/from-") {
		t.Fatalf("previewBranch = %q, want it resolved from the branch template", p.previewBranch)
	}
}
