package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// footerModel is an idle fixture: Branches focused (zero value), two branches
// (main is HEAD, selected by default), two worktrees ("/repo" is current,
// selected by default). Every panel except Status/Commits has rows.
func footerModel() Model {
	return Model{
		width:     120,
		height:    40,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Branch: "main"},
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feat/x"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo/wt-x", Branch: "feat/x"},
		},
		currentWorktree: "/repo",
	}
}

// The honesty tests pin the predicate-sharing contract: when a shared
// predicate is false, the key must be a complete no-op (no op spawned, no
// state change) — these three used to start operations git then rejects.

func TestSwitchKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel() // sel 0 = main, the checked-out branch
	u, cmd := m.Update(keyMsg("s"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("s on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel()
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees // sel 0 = /repo = currentWorktree
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the current worktree must be a no-op")
	}
}

func TestEnterNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if cmd != nil || mm.loading {
		t.Fatal("enter on the current worktree must not re-root")
	}
}

func TestMarkKeyNoOpWhileRunning(t *testing.T) {
	m := footerModel()
	m.running = true
	u, _ := m.Update(keyMsg("m"))
	if u.(Model).mark != nil {
		t.Fatal("m while an op runs must not mark")
	}
}

func TestPredicatesOnSelectableRows(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x (not HEAD)
	if !m.canSwitchBranch() || !m.canDeleteBranch() || !m.canOpenBranchPopup() || !m.canOpenWorktreePopup() {
		t.Error("branch predicates must hold on an idle model with a non-HEAD row selected")
	}
	m.sel[panelWorktrees] = 1 // /repo/wt-x (not current)
	if !m.canDeleteWorktree() || !m.canEnterWorktree() {
		t.Error("worktree predicates must hold on a non-current worktree row")
	}
	if !m.canMark() {
		t.Error("canMark must hold when the focused panel has a selected row")
	}
	m.running = true
	if m.opsIdle() || m.canSwitchBranch() || m.canMark() {
		t.Error("all op predicates must be false while running")
	}
}
