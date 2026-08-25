package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func localRef(name string, head bool) model.Ref {
	return model.Ref{Name: name, Kind: model.RefLocal, Head: head}
}

func commitBranchModel(refs []model.Ref) Model {
	m := New(nil)
	m.width, m.height = 80, 30
	m.loading = false
	if m.sel == nil {
		m.sel = map[panel]int{}
	}
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Subject: "x", Refs: refs}}
	m.sel[panelCommits] = 0
	return m
}

func branchRowIDs(rows []actionRow) []string {
	var out []string
	for _, r := range rows {
		out = append(out, r.id)
	}
	return out
}

func TestCommitBranchRowsNonHeadTip(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	rows := m.commitBranchRows()
	if len(rows) != 2 || rows[0].id != "rename-branch" || rows[1].id != "delete-branch" {
		t.Fatalf("ids = %v", branchRowIDs(rows))
	}
	if rows[0].label != "Rename branch feature" || rows[1].label != "Delete branch feature" {
		t.Fatalf("labels = %q, %q", rows[0].label, rows[1].label)
	}
}

func TestCommitBranchRowsHeadTipNoDelete(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("main", true)})
	ids := branchRowIDs(m.commitBranchRows())
	if len(ids) != 1 || ids[0] != "rename-branch" {
		t.Fatalf("head tip should offer rename only, got %v", ids)
	}
}

func TestCommitBranchRowsOtherWorktreeNoDelete(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("topic", false)})
	m.worktrees = []model.Worktree{{Branch: "topic", Path: "/elsewhere"}}
	ids := branchRowIDs(m.commitBranchRows())
	if len(ids) != 1 || ids[0] != "rename-branch" {
		t.Fatalf("branch checked out elsewhere should offer rename only, got %v", ids)
	}
}

func TestCommitBranchRowsTwoTips(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("main", true), localRef("topic", false)})
	var rename, del int
	for _, id := range branchRowIDs(m.commitBranchRows()) {
		switch id {
		case "rename-branch":
			rename++
		case "delete-branch":
			del++
		}
	}
	if rename != 2 || del != 1 {
		t.Fatalf("want 2 rename + 1 delete, got rename=%d del=%d", rename, del)
	}
}

func TestCommitBranchRowsNonTip(t *testing.T) {
	t.Parallel()
	if rows := commitBranchModel(nil).commitBranchRows(); rows != nil {
		t.Fatalf("no rows for a non-tip commit, got %v", branchRowIDs(rows))
	}
	m := commitBranchModel([]model.Ref{{Name: "origin/x", Kind: model.RefRemote}})
	if rows := m.commitBranchRows(); rows != nil {
		t.Fatalf("remote ref should yield no rows, got %v", branchRowIDs(rows))
	}
}

func TestCommitBranchRowsGating(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	m.focus = panelBranches
	if m.commitBranchRows() != nil {
		t.Fatal("no rows off the Commits panel")
	}
	m.focus = panelCommits
	m.running = true
	if m.commitBranchRows() != nil {
		t.Fatal("no rows while running")
	}
}

func TestAvailableActionsIncludesCommitBranchRows(t *testing.T) {
	t.Parallel()
	m := commitBranchModel([]model.Ref{localRef("feature", false)})
	if _, ok := findRow(availableActions(m), "rename-branch"); !ok {
		t.Fatal("availableActions missing rename-branch on a tip commit")
	}
	if _, ok := findRow(availableActions(m), "delete-branch"); !ok {
		t.Fatal("availableActions missing delete-branch on a non-head tip commit")
	}
}
