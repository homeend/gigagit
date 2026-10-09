package tui

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

// A working review's tree marks every file against the review: changed ones
// say so, files the review never read say so, matching ones carry ◆n.
func TestReviewTreeLinesMarksAWorkingReviewsFiles(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{review: domain.Review{Kind: domain.ReviewOnWorktree, Doc: &notebatch.ReviewDoc{}},
		counts: map[string]int{"a.txt": 1},
		states: map[string]domain.WorkingFileState{"a.txt": domain.WorkingFileMatches, "c.txt": domain.WorkingFileChanged}}
	out := reviewTreeLines(st, []contentLine{{text: "M a.txt", path: "a.txt"}, {text: "M c.txt", path: "c.txt"}, {text: "A n.txt", path: "n.txt"}})
	byPath := map[string]string{}
	for _, l := range out {
		byPath[l.path] = l.text
	}
	if !strings.Contains(byPath["a.txt"], noteBadge(1)) || strings.Contains(byPath["a.txt"], i18n.T("changed since the review")) {
		t.Errorf("a.txt = %q", byPath["a.txt"])
	}
	if !strings.Contains(byPath["c.txt"], i18n.T("changed since the review")) {
		t.Errorf("c.txt = %q", byPath["c.txt"])
	}
	if !strings.Contains(byPath["n.txt"], i18n.T("not reviewed")) {
		t.Errorf("n.txt = %q", byPath["n.txt"])
	}
}

// A diff opened from a working review is addressed at the worktree, where the
// review's notes anchor.
func TestStampReviewNotesAddressesAWorkingReviewAtTheWorktree(t *testing.T) {
	t.Parallel()
	m := Model{windowState: windowState{filesReview: &reviewViewState{id: "rv", review: domain.Review{Kind: domain.ReviewOnWorktree, Worktree: "/wt"}}}}
	dv := &diffView{}
	m.stampReviewNotes(dv, "a.txt")
	want := model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a.txt"}
	if dv.noteAddr != want || dv.reviewID != "rv" {
		t.Fatalf("noteAddr %+v reviewID %q, want %+v rv", dv.noteAddr, dv.reviewID, want)
	}
}

// A working review opens as HEAD ↔ the working tree, in review mode.
func TestOpenReviewOpensAWorkingReviewAsAHeadToWorktreeCompare(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	top, err := m.svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "u.txt"), []byte("u1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"summary":"ok","files":[{"path":"u.txt","annotations":[{"newRange":[1,1],"summary":"on u"}]}]}`
	id, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: domain.WorkingReviewTarget(), Agent: "Claude", Text: doc,
		Files: []model.NoteFile{{Path: "u.txt", Blob: blobOfForTest("u1\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	if m.filesReview == nil || m.filesReview.states == nil || m.filesMode != filesModeCompare ||
		m.filesRight.Kind() != model.EndpointWorkTree {
		t.Fatalf("mode %v right %v review %+v (status %q)", m.filesMode, m.filesRight.Kind(), m.filesReview, m.statusMsg)
	}
	if !strings.Contains(m.filesTitle, i18n.T("working changes")) {
		t.Fatalf("title %q", m.filesTitle)
	}
	if l, _ := filesLine(t, m, "u.txt"); !strings.Contains(l.text, noteBadge(1)) {
		t.Fatalf("u.txt row %q, want ◆1", l.text)
	}
}

// View all notes lists every working review under the working tree and
// marks the one no file matches anymore as outdated.
func TestAllNotesListsWorkingReviewsAndMarksTheOutdated(t *testing.T) {
	t.Parallel()
	ov := domain.NotesOverview{WorkingReviews: []domain.WorkingReview{
		{Review: domain.Review{ID: "a", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes"}, WorkingReviewMatch: domain.WorkingReviewMatch{Current: true}},
		{Review: domain.Review{ID: "b", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes"}},
	}}
	rows := buildAllNotesRows(ov, "")
	var group bool
	where := map[string]string{}
	for _, r := range rows {
		if r.kind == anGroup && r.key == "wt" {
			group = true
		}
		if r.kind == anReview {
			_, _, w, _ := anReviewCells(r, time.Now())
			where[r.review.ID] = w
		}
	}
	if !group || len(where) != 2 {
		t.Fatalf("group %v reviews %v", group, where)
	}
	if where["a"] != i18n.T("working changes") || where["b"] != i18n.T("outdated") {
		t.Fatalf("where = %v", where)
	}
}

// blobOfForTest is git's sha1 blob id of s (the tui package cannot import
// internal/git).
func blobOfForTest(s string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(s))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// A working review's file rows copy their review link too (the plain Copy
// link has nothing to name there).
func TestWorkingReviewFileRowCopiesItsReviewLink(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	top, err := m.svc.TopLevel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(top, "u.txt"), []byte("u1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"summary":"ok","files":[{"path":"u.txt","annotations":[{"newRange":[1,1],"summary":"on u"}]}]}`
	id, _, err := m.svc.SaveReview(ctx, domain.SaveReview{Target: domain.WorkingReviewTarget(), Agent: "Claude", Text: doc,
		Files: []model.NoteFile{{Path: "u.txt", Blob: blobOfForTest("u1\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	m, cmd := m.openReview(id, "Review")
	m = drainCmds(t, m, cmd)
	var got string
	m = captureClip(m, &got)
	_, i := filesLine(t, m, "u.txt")
	m.filesView.sel = i
	runMenuRow(t, m, "copy-review-file-link")
	if !strings.HasSuffix(got, "/u.txt?review="+id) {
		t.Fatalf("copied %q", got)
	}
}
