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
	if !strings.Contains(rows[1], "feature ◆ 1") {
		t.Fatalf("feature row %q lacks the ◆ 1 marker", rows[1])
	}
	// "Review: <date> <agent>", indented under the branch name.
	created := reviewStampAt(m.noteCounts.Reviews[0].Created, time.Now())
	if want := "  └ Review: " + created + " Claude Code"; !strings.HasSuffix(strings.TrimRight(rows[2], " "), want) {
		t.Fatalf("review row %q, want it to end %q", rows[2], want)
	}
	if name, sub := strings.Index(rows[1], "feature"), strings.Index(rows[2], "└"); sub <= name {
		t.Fatalf("review row not indented past the branch name:\n%s\n%s", rows[1], rows[2])
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
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") {
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
	if len(rows) != 3 || !strings.Contains(rows[1], "◆ 1") {
		t.Fatalf("rows %q, want the review under feature", rows)
	}
}

// enter on a review sub-row reads the review (openReview's command); nothing
// else — not the branch's own enter action.
func TestBranchReviewRowEnterOpensTheReview(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.sel[panelBranches] = 2
	nm, cmd := updateKey(m, "enter")
	if cmd == nil {
		t.Fatal("enter on a review row started nothing")
	}
	if _, ok := cmd().(reviewViewMsg); !ok {
		t.Fatalf("enter on a review row did not read the review (focus %v)", nm.focus)
	}
}

// A review from this year drops the year, so "└ Review: <date> <agent>" fits
// the Branches tab's default width; an older one keeps it.
func TestReviewStampDropsOnlyTheCurrentYear(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.Local)
	if got := reviewStampAt(time.Date(2026, 9, 28, 23, 37, 0, 0, time.Local), now); got != "09-28 23:37" {
		t.Errorf("this year's stamp = %q, want 09-28 23:37", got)
	}
	if got := reviewStampAt(time.Date(2025, 12, 31, 8, 5, 0, 0, time.Local), now); got != "2025-12-31 08:05" {
		t.Errorf("last year's stamp = %q, want 2025-12-31 08:05", got)
	}
}

// The Commits list marks a reviewed commit with ✎ (its reviews come from the
// note counts the list already holds — no read per row).
func TestCommitRowShowsReviewMarker(t *testing.T) {
	t.Parallel()
	m := reviewBranchesModel(t)
	m.commits = []model.Commit{
		{Hash: strings.Repeat("b", 40), Subject: "reviewed one"},
		{Hash: strings.Repeat("c", 40), Subject: "plain one"},
	}
	rev := m.commitIdentRowAt(m.wipCount(), commitIdentW, false, -1)
	plain := m.commitIdentRowAt(m.wipCount()+1, commitIdentW, false, -1)
	if !strings.Contains(rev, "✎") {
		t.Errorf("reviewed row %q lacks ✎", rev)
	}
	if strings.Contains(plain, "✎") {
		t.Errorf("unreviewed row %q carries ✎", plain)
	}
}
