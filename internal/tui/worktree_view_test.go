package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestBranchRowsShowWorktreePath(t *testing.T) {
	m := Model{
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feature/x"},
			{Name: "lonely"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo.worktrees/x", Branch: "feature/x"},
		},
		sel: map[panel]int{},
	}
	rows := m.branchRows()
	if !strings.Contains(rows[0], "(/repo)") {
		t.Errorf("main should show its worktree path: %q", rows[0])
	}
	if !strings.Contains(rows[1], "(/repo.worktrees/x)") {
		t.Errorf("feature/x should show its worktree path: %q", rows[1])
	}
	if strings.Contains(rows[2], "(") {
		t.Errorf("lonely is in no worktree, expected no path: %q", rows[2])
	}
	if strings.Contains(strings.Join(rows, "\n"), "◫") {
		t.Errorf("the ◫ glyph should be gone: %v", rows)
	}
}

func TestWorktreeRowsFormatAndCurrentMarker(t *testing.T) {
	m := Model{
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo.worktrees/x", Branch: "feature/x"},
		},
		currentWorktree: "/repo",
		sel:             map[panel]int{},
	}
	rows := m.worktreeRows()
	if len(rows) != 2 {
		t.Fatalf("want 2 worktree rows, got %d: %v", len(rows), rows)
	}
	if !strings.Contains(rows[0], "main") || !strings.Contains(rows[0], "/repo") {
		t.Errorf("row should show branch and path: %q", rows[0])
	}
	if !strings.HasPrefix(rows[0], "* ") {
		t.Errorf("current worktree should be marked: %q", rows[0])
	}
	if strings.HasPrefix(rows[1], "* ") {
		t.Errorf("non-current worktree should not be marked: %q", rows[1])
	}
}

func TestPanelLenWorktrees(t *testing.T) {
	m := Model{worktrees: make([]model.Worktree, 3), sel: map[panel]int{}}
	if n := m.panelLen(panelWorktrees); n != 3 {
		t.Fatalf("panelLen(panelWorktrees) = %d, want 3", n)
	}
}

// Real-repo integration: the loaded model's worktree list contains the repo's
// own root, so the current-worktree marker actually fires (not just on synthetic
// equal strings) and the checked-out branch gets the has-worktree marker.
func TestWorktreeMarkersFireOnRealRepo(t *testing.T) {
	m := loadedModel(t)
	marked := false
	for _, row := range m.worktreeRows() {
		if strings.HasPrefix(row, "* ") {
			marked = true
		}
	}
	if !marked {
		t.Errorf("no worktree row marked current; rows=%v current=%q", m.worktreeRows(), m.currentWorktree)
	}
	// The checked-out branch (main) is in a worktree, so its row shows the path.
	foundPath := false
	for _, row := range m.branchRows() {
		if strings.Contains(row, m.currentWorktree) {
			foundPath = true
		}
	}
	if !foundPath {
		t.Errorf("expected the checked-out branch row to show its worktree path; rows=%v current=%q", m.branchRows(), m.currentWorktree)
	}
}
