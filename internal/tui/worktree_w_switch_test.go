package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
)

// One popup for w and W: the branch starts as the SELECTED branch (no
// templated name, so the dir carries no b/from- prefix or random suffix).
// W makes create-and-switch enter's default; e/p rename the branch into a
// NEW branch cut from the selection.

func TestShiftWOpensExistingModeWithSwitchDefault(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>-<random-alpha:4>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("W"))
	p := layerOf[*worktreePopup](updated.(Model))
	if p == nil {
		t.Fatal("W should open the worktree popup")
	}
	if !p.existing {
		t.Fatal("W must open on the selected existing branch (same popup as w)")
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

// ---- e/p fold: renaming inside the w/W popup creates a NEW branch ----

func TestEditRenamesIntoNewBranchFromSelection(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	start := layerOf[*worktreePopup](m).startPoint

	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.state != stEdit {
		t.Fatal("e must open branch-name editing in the w/W popup")
	}
	if p.editBuf.Value() != start {
		t.Fatalf("editBuf seeded with %q, want the selection %q (not a template)", p.editBuf.Value(), start)
	}

	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(keyMsg("backspace"))
		m = updated.(Model)
	}
	for _, ch := range []string{"f", "x"} {
		updated, _ = m.Update(keyMsg(ch))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter"))
	m = updated.(Model)
	p = layerOf[*worktreePopup](m)
	if p.previewBranch != "fx" {
		t.Fatalf("previewBranch = %q, want fx", p.previewBranch)
	}
	op, ok := p.createOp("").(engine.CreateWorktree)
	if !ok {
		t.Fatalf("createOp = %T, want engine.CreateWorktree (new branch)", p.createOp(""))
	}
	if op.StartPoint != start || op.Branch != "fx" {
		t.Fatalf("op = {StartPoint:%q Branch:%q}, want {%q fx}", op.StartPoint, op.Branch, start)
	}
}

func TestEmptyEditFallsBackToSelection(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	start := layerOf[*worktreePopup](m).startPoint

	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	for len([]rune(layerOf[*worktreePopup](m).editBuf.Value())) > 0 {
		updated, _ = m.Update(keyMsg("backspace"))
		m = updated.(Model)
	}
	updated, _ = m.Update(keyMsg("enter")) // confirm the emptied name
	m = updated.(Model)
	p := layerOf[*worktreePopup](m)
	if p.previewBranch != start {
		t.Fatalf("previewBranch = %q, want the selection %q after an empty confirm", p.previewBranch, start)
	}
	if _, ok := p.createOp("").(engine.CreateWorktreeForBranch); !ok {
		t.Fatalf("createOp = %T, want CreateWorktreeForBranch (still the existing branch)", p.createOp(""))
	}
}

func TestPrefixKeyActiveInBranchPopup(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	_, cmd := m.Update(keyMsg("p"))
	if cmd == nil {
		t.Fatal("p must open the prefix picker in the w/W popup (no longer inert)")
	}
}
