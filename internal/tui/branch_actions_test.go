package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

// branchMergeModel: Branches tab focused, a NON-current branch selected, attached
// HEAD (current = "main"). The selection is "feature" (IsHead=false).
func branchMergeModel() Model {
	m := footerModel()
	m.focus = panelBranches
	m.branches = []model.Branch{
		{Name: "feature", IsHead: false, Hash: "abc1234"},
		{Name: "main", IsHead: true, Hash: "def5678"},
	}
	m.sel[panelBranches] = 0 // "feature"
	m.svc = domain.New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.status.Branch = "main"
	return m
}

func TestBranchMergeRebaseRowsPresent(t *testing.T) {
	m := branchMergeModel()
	got := ids(availableActions(m))
	if !got["branch-merge"] || !got["branch-rebase"] {
		t.Fatalf("expected branch-merge + branch-rebase; got %v", got)
	}
}

// The current (checked-out) branch must NOT offer merge/rebase against itself.
func TestBranchMergeRebaseAbsentOnCurrentBranch(t *testing.T) {
	m := branchMergeModel()
	m.sel[panelBranches] = 1 // "main", IsHead=true
	got := ids(availableActions(m))
	if got["branch-merge"] || got["branch-rebase"] {
		t.Fatalf("merge/rebase must be absent on the current branch; got %v", got)
	}
}

// Tab-scoped: the rows must not leak when another left tab is focused.
func TestBranchMergeRebaseAbsentWhenRemotesTabFocused(t *testing.T) {
	m := branchMergeModel()
	m.focus = panelRemotes
	got := ids(availableActions(m))
	if got["branch-merge"] || got["branch-rebase"] {
		t.Fatalf("branch merge/rebase must be absent off the Branches tab; got %v", got)
	}
}

func TestBranchMergeRebaseHiddenOnDetachedHEAD(t *testing.T) {
	m := branchMergeModel()
	m.status.Branch = "" // detached
	got := ids(availableActions(m))
	if got["branch-merge"] || got["branch-rebase"] {
		t.Fatalf("merge/rebase must be hidden on detached HEAD; got %v", got)
	}
}

func TestBranchMergeRowDispatches(t *testing.T) {
	m := branchMergeModel()
	m.cfg.UI.DisableSlowOpConfirm = true // test op wiring, not confirm UX
	row, ok := m.branchMergeRow()
	if !ok {
		t.Fatal("branchMergeRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("merge row run returned nil cmd")
	}
}

func TestBranchRebaseRowDispatches(t *testing.T) {
	m := branchMergeModel()
	m.cfg.UI.DisableSlowOpConfirm = true // test op wiring, not confirm UX
	row, ok := m.branchRebaseRow()
	if !ok {
		t.Fatal("branchRebaseRow not available")
	}
	if _, cmd := row.run(m); cmd == nil {
		t.Fatal("rebase row run returned nil cmd")
	}
}

// TestBranchVersionsRowDispatches: the "Previous versions…" row (available on
// ANY branch, including the current one — restore/compare have no !IsHead
// gate) pushes the versionsPopup straight into versions mode.
func TestBranchVersionsRowDispatches(t *testing.T) {
	m := branchMergeModel()
	row, ok := m.branchVersionsRow()
	if !ok {
		t.Fatal("branchVersionsRow not available")
	}
	tm, cmd := row.run(m)
	if cmd == nil {
		t.Fatal("branch-versions row run returned nil cmd")
	}
	mm := tm.(Model)
	p := layerOf[*versionsPopup](mm)
	if p == nil {
		t.Fatal("branch-versions row should push the versions popup")
	}
	if p.mode != versionsModeVersions || p.branch != "feature" {
		t.Fatalf("popup = mode %d branch %q, want versionsModeVersions/\"feature\"", p.mode, p.branch)
	}
}

// The current (checked-out) branch DOES offer Previous versions… — unlike
// merge/rebase, restoring/browsing versions of the current branch is a valid
// (and common) recovery flow, so there is no !IsHead gate.
func TestBranchVersionsRowAvailableOnCurrentBranch(t *testing.T) {
	m := branchMergeModel()
	m.sel[panelBranches] = 1 // "main", IsHead=true
	if _, ok := m.branchVersionsRow(); !ok {
		t.Fatal("branch-versions row should be available on the current branch too")
	}
}
