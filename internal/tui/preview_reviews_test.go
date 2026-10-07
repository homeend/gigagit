package tui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// reviewPreviewsModel: two saved rows — a merge preview "login" with two
// reviews (a current one and one of an older tip) and a comparison row,
// focused on the Previews tab.
func reviewPreviewsModel(t *testing.T) Model {
	t.Helper()
	m := newTestModel(t)
	m.loading = false
	now := time.Now()
	m.previews = []previewRow{
		{rec: model.MergePreview{ID: "p1", Label: "login", Source: "feat/x", Target: "main", Created: now},
			sum: domain.PreviewSummary{State: domain.PreviewOK, Files: 1, Ahead: 1},
			reviews: []domain.ReviewHead{
				{ID: "r2", Agent: "Claude Code", Created: now.Add(-time.Hour), Remarks: 3, Resolved: 1, Preview: "main...feat/x"},
				{ID: "r1", Agent: "Codex", Created: now.Add(-48 * time.Hour), Preview: "main...feat/x", Older: true},
			}},
		{kind: rowCompare, cmp: domain.SavedCompare{ID: "c1", Label: "cmp", Created: now.Add(-time.Minute)},
			cmpDesc: [2]string{"a", "b"}},
	}
	m = m.activateTab(panelPreviews)
	return m
}

func TestPreviewRowsShowReviewSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 4 {
		t.Fatalf("rows %q, want login, its two reviews, cmp", rows)
	}
	if !strings.Contains(rows[0], "login") || !strings.Contains(rows[3], "cmp") {
		t.Fatalf("parent rows out of place: %q", rows)
	}
	cur := reviewStampAt(m.previews[0].reviews[0].Created, time.Now())
	if !strings.Contains(rows[1], "└ Review: "+cur+" Claude Code") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("current review row %q", rows[1])
	}
	if !strings.Contains(rows[2], "Codex") || !strings.HasSuffix(strings.TrimRight(rows[2], " "), "· older tip") {
		t.Fatalf("older review row %q, want it to end with · older tip", rows[2])
	}
}

func TestPreviewSubRowsDoNotShiftRowActions(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.sel[panelPreviews] = 1 // a review sub-row
	if r, ok := m.selectedPreview(); ok {
		t.Fatalf("a sub-row resolved to preview %q", r.id())
	}
	if h, ok := m.selectedPreviewReview(); !ok || h.ID != "r2" {
		t.Fatalf("selectedPreviewReview = %+v %v", h, ok)
	}
	m.sel[panelPreviews] = 3 // the comparison row, after two sub-rows
	if r, ok := m.selectedPreview(); !ok || r.id() != "c1" {
		t.Fatalf("row after the sub-rows = %+v %v, want c1", r.id(), ok)
	}
	if a, b := m.rowKeyAt(panelPreviews, 0), m.rowKeyAt(panelPreviews, 1); a != "p1" || a == b {
		t.Fatalf("keys %q %q: a preview row keys by its id, a sub-row distinctly", a, b)
	}
	// Marks address preview rows only, by id, in display order.
	m.previewCompareSet = map[string]bool{"c1": true}
	if got := m.previewMarkedSubjects(); got != "cmp" {
		t.Fatalf("marked subjects %q", got)
	}
}

func TestPreviewSubRowsFollowTheirRowUnderAFilter(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	m.filterQuery, m.filterPanel = "login", panelPreviews
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 3 || !strings.Contains(rows[1], "└ Review:") {
		t.Fatalf("filter on the preview: %q, want it and both reviews", rows)
	}
	m.filterQuery = "Codex"
	rows, _ = m.panelView(panelPreviews)
	if len(rows) == 0 || !strings.Contains(rows[0], "login") {
		t.Fatalf("filter on a review's agent: %q, want its preview kept", rows)
	}
}

func TestPreviewSteerLandingSkipsSubRows(t *testing.T) {
	t.Parallel()
	m := reviewPreviewsModel(t)
	if di, ok := m.previewDisplayIndex(1); !ok || di != 3 {
		t.Fatalf("display index of the comparison row = %d %v, want 3", di, ok)
	}
}

// A real store: a review saved on the preview shows as its sub-row after
// a previews read; once the source moves on it is an older tip.
func TestPreviewSubRowFromTheStore(t *testing.T) {
	t.Parallel()
	m, dir := storedPreviewReviewModel(t)
	rows, _ := m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "└ Review:") || strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the preview and its current review", rows)
	}
	runGit(t, dir, "checkout", "-q", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "more")
	runGit(t, dir, "checkout", "-q", "main")
	m = readPreviewsNow(t, m)
	rows, _ = m.panelView(panelPreviews)
	if len(rows) != 2 || !strings.Contains(rows[1], "older tip") {
		t.Fatalf("rows %q, want the review marked older tip", rows)
	}
}

const previewReviewTestDoc = `{"version":1,"summary":"## Summary\nok","files":[{"path":"a.txt","annotations":[{"newRange":[1,1],"summary":"one"}]}]}`

// storedPreviewReviewModel: mergePreviewModel's repo with a note store and
// one review saved on the "login" preview (main...feat/x).
func storedPreviewReviewModel(t *testing.T) (Model, string) {
	t.Helper()
	dir, repo := newRepoDir(t)
	runGit(t, dir, "checkout", "-q", "-b", "feat/x")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "add a")
	runGit(t, dir, "checkout", "-q", "main")
	svc := domain.New(repo)
	svc.UsePreviewsDir(t.TempDir())
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	if _, err := svc.PreviewAdd(ctx, "feat/x", "main", "login"); err != nil {
		t.Fatal(err)
	}
	set, err := svc.PreviewNotes(ctx, "feat/x", "main")
	if err != nil || !set.OK() {
		t.Fatalf("PreviewNotes: %+v %v", set, err)
	}
	if _, _, err := svc.SaveReview(ctx, domain.SaveReview{Target: domain.ScopeReviewTarget(set), Agent: "Claude Code", Text: previewReviewTestDoc}); err != nil {
		t.Fatal(err)
	}
	m := New(svc)
	updated, _ := m.Update(m.loadCmd()())
	m = updated.(Model)
	m = readPreviewsNow(t, m)
	return m.activateTab(panelPreviews), dir
}

// readPreviewsNow runs one srcPreviews read to completion.
func readPreviewsNow(t *testing.T, m Model) Model {
	t.Helper()
	m, cmd := m.reloadSourcesCmd([]sourceKey{srcPreviews}, reloadOpts{manual: true})
	updated, _ := m.Update(cmd())
	return updated.(Model)
}
