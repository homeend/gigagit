package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

func TestCarriedNotesFollowIdenticalText(t *testing.T) {
	t.Parallel()
	dir, _ := prPreviewRepo(t) // the PR's a.go ends "var X = 1"
	runGitIn(t, dir, "checkout", "-q", "-b", "other")
	commitFile(t, dir, "a.go", "package a\n\nvar X = 1\nvar Y = 2\n", "same line elsewhere")
	other := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "checkout", "-q", "main")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	add := func(line int, sum string) string {
		n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: sum,
			Address: model.FileAddress{State: model.StateCommitted, Commit: other, Path: "a.go"},
			Side:    model.NoteSideNew, Range: [2]int{line, line}})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	carried := add(3, "X is magic")
	notHere := add(4, "Y is not in the PR")
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil || !set.OK() {
		t.Fatalf("set %+v err %v", set, err)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.go")
	if err != nil {
		t.Fatal(err)
	}
	var hit *ResolvedNote
	for i := range got {
		if got[i].Note.ID == notHere {
			t.Fatal("a note whose text is not in the PR head was carried")
		}
		if got[i].Note.ID == carried {
			hit = &got[i]
		}
	}
	if hit == nil || hit.Range != [2]int{3, 3} || hit.Origin != other[:7] || hit.Sync != model.SyncLocal {
		t.Fatalf("carried = %+v", hit)
	}
	// Only a PR's view carries notes: a branch preview of the same pair does not.
	plain, _ := svc.PreviewNotes(ctx, "other", "main")
	if c := svc.carriedNotes(ctx, plain); c != nil {
		t.Fatalf("a branch preview carried %+v", c)
	}
}
