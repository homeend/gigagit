package domain

import (
	"context"
	"testing"
)

// Item 13: a PR's note badges re-read on every comment change. Content at a
// commit sha never changes, so a second read — even after a note write
// bumped the notes generation — runs no `git show`.
func TestPRNoteGroupsReadEachFileOnce(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine")
	savePRReview(t, svc, twoRemarks)
	cr := newCountingRunner(svc.repo.Runner)
	svc.repo.Runner = cr
	set := prNoteSetOf(t, svc)
	ctx := context.Background()
	if _, err := svc.PreviewNoteGroups(ctx, set); err != nil {
		t.Fatal(err)
	}
	addPRNote(t, svc, head, "big.go", 25, "another") // bumps the notes generation
	cr.reset()
	if _, err := svc.PreviewNoteGroups(ctx, set); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.PreviewNoteCounts(ctx, set); err != nil {
		t.Fatal(err)
	}
	if n := cr.count("git show"); n != 0 {
		t.Fatalf("the second read ran %d git show", n)
	}
}
