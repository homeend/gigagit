package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A rebase paused in a worktree lists that tree detached; the Branches tab
// still shows it under the branch the rebase rewrites — its path, the
// viewed-tree marker, its place in the worktree ring and a branch filter's
// exemption — as it did before the rebase started.
func TestBranchesTabKeepsARebasingWorktreeUnderItsBranch(t *testing.T) {
	m := loadedModel(t)
	wt := filepath.Join(t.TempDir(), "feat")
	m.branches = []model.Branch{{Name: "feat"}, {Name: "main", IsHead: true}}
	m.worktrees = []model.Worktree{
		{Path: m.currentWorktree, Branch: "main"},
		{Path: wt, Detached: true, Rebasing: "feat"},
	}

	var featRow string
	for _, row := range m.branchRows() {
		if strings.Contains(row, "feat") {
			featRow = row
		}
	}
	if !strings.Contains(featRow, "("+wt+")") {
		t.Fatalf("feat row = %q, want the rebasing worktree's path", featRow)
	}
	if p, ok := m.worktreePathOf("feat"); !ok || p != wt {
		t.Fatalf("worktreePathOf(feat) = %q, %v; want %q", p, ok, wt)
	}
	if got := m.worktreeOrder(); len(got) != 2 || got[0] != 1 || got[1] != 0 {
		t.Fatalf("worktreeOrder = %v, want [1 0] (feat's tree first, in the Branches tab's order)", got)
	}
	if ex := domain.ExemptBranches(m.branches, m.worktrees); !ex[0] {
		t.Fatalf("ExemptBranches = %v, want feat exempt while its tree rebases", ex)
	}

	// The Worktrees tab names the branch too, not a bare "(detached)".
	ents := m.worktreeEntries()
	if rows := m.worktreeRows(ents); len(rows) < 2 || !strings.Contains(rows[1], "feat (rebasing)  "+wt) {
		t.Fatalf("Worktrees rows = %q, want the tree as \"feat (rebasing)\"", rows)
	}

	// Viewed from the rebasing tree, its branch row carries the marker.
	m.markViewedBranch, m.currentWorktree = true, wt
	for _, row := range m.branchRows() {
		if strings.Contains(row, "feat") && !strings.HasPrefix(strings.TrimLeft(row, " *"), ":") {
			t.Fatalf("feat row = %q, want the viewed-worktree ':' marker", row)
		}
	}
}
