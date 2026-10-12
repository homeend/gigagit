package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// R9: an overview and its remarks are shown in that overview only.
func TestOverviewInvisibleToNoteReaders(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	if _, err := saveOv(t, svc, ovDoc("T", "[a](a.go:2)",
		`[{"path":"a.go","annotations":[{"newRange":[2,2],"summary":"remark"}]}]`)); err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	// the commit's file: no note, no remark
	got, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"})
	if err != nil || len(got) != 0 {
		t.Fatalf("NotesAt(a.go@HEAD) = %+v %v", got, err)
	}
	// the commit itself (a bare commit address lists entry-level notes)
	got, err = svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: head})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range got {
		if r.Note.IsOverviewNote() {
			t.Fatalf("NotesAt(commit) returned the overview: %+v", r.Note)
		}
	}
	// View all notes: no bucket, no review
	ov, err := svc.NotesOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range ov.Commits {
		if len(c.Files) != 0 || len(c.Reviews) != 0 {
			t.Fatalf("View all notes shows the overview as notes: %+v", c)
		}
	}
	// reviews never include it
	if rs, _ := svc.Reviews(ctx); len(rs) != 0 {
		t.Fatalf("Reviews = %+v", rs)
	}
}

func TestPROverviewInvisibleToThePRNotes(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip,
		Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c",
		Text: ovDoc("How #7 works", "[c](big.go:5)", `[{"path":"big.go","annotations":[{"newRange":[5,5],"summary":"remark"}]}]`)}); err != nil {
		t.Fatal(err)
	}
	all, err := svc.PreviewNotesAll(ctx, set)
	if err != nil {
		t.Fatal(err)
	}
	for path, rs := range all {
		for _, r := range rs {
			if r.Note.IsOverviewNote() {
				t.Fatalf("PR notes list the overview under %q", path)
			}
		}
	}
	c, _ := svc.NoteCounts(ctx)
	if len(c.PreviewReviews) != 0 || len(c.Overviews) != 0 {
		t.Fatalf("a PR overview leaked: reviews %v, commit overviews %v", c.PreviewReviews, c.Overviews)
	}
	if len(c.PreviewOverviews) != 1 {
		t.Fatalf("PreviewOverviews = %v", c.PreviewOverviews)
	}
}
