package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// reviewBranchesModel has two branches (main, feature) and one review on
// feature's current tip.
func reviewBranchesModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.loading = false
	now := time.Now().Unix()
	m.branches = []model.Branch{
		{Name: "main", IsHead: true, Hash: strings.Repeat("a", 40), UnixTime: now},
		{Name: "feature", Hash: strings.Repeat("b", 40), UnixTime: now - 60},
	}
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: "r1", Branch: "feature", Commit: strings.Repeat("b", 40),
		Agent: "Claude Code", Summary: "Review: feature", Created: time.Now().Add(-2 * time.Hour)}}
	m.focus = panelBranches
	return m
}

func TestBranchRowsShowReviewSubRows(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	rows, _ := m.panelView(panelBranches)
	if len(rows) != 3 {
		t.Fatalf("rows %q, want main, feature and its review", rows)
	}
	if !strings.Contains(rows[1], "feature ◆1") {
		t.Fatalf("feature row %q lacks the ◆1 marker", rows[1])
	}
	if !strings.Contains(rows[2], "└ ◆") || !strings.Contains(rows[2], "Claude Code") || !strings.Contains(rows[2], "Review: feature") {
		t.Fatalf("review row %q", rows[2])
	}
}

func TestBranchReviewOfAnOlderTipIsNotListed(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.noteCounts.Reviews[0].Commit = strings.Repeat("e", 40)
	rows, _ := m.panelView(panelBranches)
	if len(rows) != 2 || strings.Contains(strings.Join(rows, "\n"), "◆") {
		t.Fatalf("a review of an older tip was listed:\n%s", strings.Join(rows, "\n"))
	}
}

func TestBranchActionOnReviewRowIsRefused(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.sel[panelBranches] = 2 // the review row
	if b, ok := m.selectedBranch(); ok {
		t.Fatalf("a review sub-row resolved to branch %q", b.Name)
	}
	if h, ok := m.selectedBranchReview(); !ok || h.ID != "r1" {
		t.Fatalf("selectedBranchReview = %+v %v", h, ok)
	}
	m.sel[panelBranches] = 1
	if b, ok := m.selectedBranch(); !ok || b.Name != "feature" {
		t.Fatalf("the branch row = %+v %v", b, ok)
	}
}

func TestBranchReviewRowFollowsItsBranchUnderAFilter(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.filterQuery, m.filterPanel = "feat", panelBranches
	rows, _ := m.panelView(panelBranches)
	if len(rows) != 2 || !strings.Contains(rows[1], "Review: feature") {
		t.Fatalf("filtered rows %q, want feature and its review", rows)
	}
}

func TestBranchRowKeysAreDistinct(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	if a, b := m.rowKeyAt(panelBranches, 1), m.rowKeyAt(panelBranches, 2); a == b || a != "feature" {
		t.Fatalf("keys %q and %q", a, b)
	}
}

func TestBranchMenuShowReview(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.sel[panelBranches] = 1
	r, ok := m.showBranchReviewRow()
	if !ok || r.label != "Show review" {
		t.Fatalf("row %+v %v", r, ok)
	}
	m.sel[panelBranches] = 0 // main has no review
	if _, ok := m.showBranchReviewRow(); ok {
		t.Fatal("a branch without reviews offers Show review")
	}
}

// The Branches list carries %(objectname:short); a review stores the full sha.
func TestBranchReviewMatchesAShortBranchHash(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.branches[1].Hash = strings.Repeat("b", 7)
	rows, _ := m.panelView(panelBranches)
	if len(rows) != 3 || !strings.Contains(rows[1], "◆1") {
		t.Fatalf("rows %q, want the review under feature", rows)
	}
}
