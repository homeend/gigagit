package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// rowReview is a current working review that matches a.txt and not b.txt.
func rowReview() []domain.WorkingReview {
	return []domain.WorkingReview{{
		Review: domain.Review{ID: "rv1", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes", Agent: "Claude Code"},
		WorkingReviewMatch: domain.WorkingReviewMatch{Current: true,
			States: map[string]domain.WorkingFileState{"a.txt": domain.WorkingFileMatches, "b.txt": domain.WorkingFileChanged}},
	}}
}

func reviewRowModel(t *testing.T) Model {
	t.Helper()
	m := diffModel()
	m = m.withStatus(model.WorkingTreeStatus{Files: wtFiles("a.txt", "b.txt")})
	m.focus = panelFiles
	return m.withWorkingReviews(rowReview())
}

func TestFilesPanelShowsTheReviewRowFirstAndMarksMatchingFiles(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	idx := m.displayIndices(panelFiles)
	if len(idx) != 3 || idx[0] != reviewRowIdx {
		t.Fatalf("indices = %v, want the review row first", idx)
	}
	l := m.listFor(panelFiles)
	if !strings.Contains(l.Row(idx[0]), i18n.T("Review:")) || !strings.Contains(l.Row(idx[0]), "Claude Code") {
		t.Errorf("row 0 = %q", l.Row(idx[0]))
	}
	if !strings.Contains(l.Row(idx[1]), "a.txt") || !strings.Contains(l.Row(idx[1]), "✎") {
		t.Errorf("a.txt row = %q, want ✎", l.Row(idx[1]))
	}
	if strings.Contains(l.Row(idx[2]), "✎") {
		t.Errorf("b.txt changed since the review: %q must carry no ✎", l.Row(idx[2]))
	}
	if got := m.filesPanelFileCount(); got != 2 {
		t.Errorf("file count = %d, want 2 (the review row is not a file)", got)
	}
}

func TestFilesPanelReviewRowIsNotAFile(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.sel[panelFiles] = 0
	if _, ok := m.backingIndex(panelFiles); ok {
		t.Fatal("backingIndex resolved the review row to a file")
	}
	if m.canShowFileDiff() {
		t.Fatal("the review row offers a file diff")
	}
	if got := m.rowKeyAt(panelFiles, 0); got != "" {
		t.Fatalf("rowKeyAt = %q, want none", got)
	}
	if _, ok := m.selectedKey(panelFiles); ok {
		t.Fatal("selectedKey resolved the review row (a mark would key on it)")
	}
	for _, f := range m.buildStatusStack(false) {
		if f.path == "" {
			t.Fatal("the stacked view gained a pathless entry")
		}
	}
	m.diffNav, m.sel[panelFiles] = diffNavStatus, 2 // on b.txt, stepping back over a.txt to the row
	if paths := m.diffFileSequence(-1); len(paths) != 1 || paths[0] != "a.txt" {
		t.Fatalf("file nav = %v, want only a.txt", paths)
	}
	if _, f, ok := m.nextStatusFile(-1, false); !ok || f.Path != "a.txt" {
		t.Fatalf("nextStatusFile = %+v %v", f, ok)
	}
}

func TestFilesPanelEnterOnTheReviewRowOpensTheReview(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m = m.withStatus(model.WorkingTreeStatus{Files: wtFiles("a.txt", "b.txt")})
	m.focus = panelFiles
	m = m.withWorkingReviews(rowReview())
	m.sel[panelFiles] = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if got := nm.(Model); !got.hasReviewLoading(got.reviewOpenGen) {
		t.Fatal("enter did not open the review")
	}
}

func TestFilesPanelCursorKeepsItsFileWhenTheRowComesAndGoes(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m = m.withStatus(model.WorkingTreeStatus{Files: wtFiles("a.txt", "b.txt")})
	m.focus = panelFiles
	m.sel[panelFiles] = 1 // b.txt
	m = m.withWorkingReviews(rowReview())
	if bi, ok := m.backingIndex(panelFiles); !ok || m.status.Files[bi].Path != "b.txt" {
		t.Fatalf("after the row appeared the cursor is on %v (ok %v)", bi, ok)
	}
	m = m.withWorkingReviews(nil)
	if bi, ok := m.backingIndex(panelFiles); !ok || m.status.Files[bi].Path != "b.txt" {
		t.Fatalf("after the row left the cursor is on %v (ok %v)", bi, ok)
	}
}

func TestFilesPanelHidesTheReviewRowWhileFiltering(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.filterPanel, m.filterQuery = panelFiles, "b"
	for _, u := range m.displayIndices(panelFiles) {
		if u == reviewRowIdx {
			t.Fatal("the review row shows under a / filter")
		}
	}
}

func TestFilesPanelOutdatedReviewHasNoRowAndNoMarks(t *testing.T) {
	t.Parallel()
	m := diffModel()
	m = m.withStatus(model.WorkingTreeStatus{Files: wtFiles("a.txt")})
	rs := rowReview()
	rs[0].Current = false
	m = m.withWorkingReviews(rs)
	if idx := m.displayIndices(panelFiles); len(idx) != 1 || idx[0] == reviewRowIdx {
		t.Fatalf("indices = %v", idx)
	}
	if strings.Contains(m.listFor(panelFiles).Row(0), "✎") {
		t.Fatal("an outdated review marks a file")
	}
}

func TestFilesPanelReviewRowMenuIsOpenCopyLinkDelete(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.sel[panelFiles] = 0
	rows, ok := m.workingReviewRowMenu()
	if !ok || len(rows) != 3 || rows[0].id != "open-review" || rows[1].id != "copy-gg-link" || rows[2].id != "delete-review" {
		t.Fatalf("menu = %+v", rows)
	}
}

// A repo switch drops the old repo's working reviews, and a read still in
// flight from it lands nowhere.
func TestReRootDropsTheWorkingReviews(t *testing.T) {
	t.Parallel()
	m := Model{workingReviews: rowReview()}
	updated, _ := m.reRoot(t.TempDir())
	got := updated.(Model)
	if got.workingReviews != nil {
		t.Fatalf("workingReviews = %+v after reRoot, want nil", got.workingReviews)
	}
	stale := workingReviewsMsg{reviews: rowReview(), gen: m.workingReviewsGen}
	after, _ := got.Update(stale)
	if after.(Model).workingReviews != nil {
		t.Fatal("a read from the old repo installed its reviews")
	}
}
