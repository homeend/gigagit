package domain

import (
	"context"
	"errors"
	"testing"
)

func TestOverviewsListAndRemove(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	a, err := saveOv(t, svc, ovDoc("A", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	b, err := saveOv(t, svc, ovDoc("B", "[b](b.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	all, err := svc.Overviews(ctx)
	if err != nil || len(all) != 2 || all[0].ID != b.ID || all[1].ID != a.ID {
		t.Fatalf("Overviews = %+v %v, want B then A", all, err)
	}
	if all[0].Doc == nil || all[0].Doc.Overview != "[b](b.go:1)" {
		t.Fatalf("doc = %+v", all[0].Doc)
	}
	head := revParse(t, dir, "HEAD")
	if got, _ := svc.OverviewsForCommit(ctx, head); len(got) != 2 {
		t.Fatalf("OverviewsForCommit = %d", len(got))
	}
	if err := svc.RemoveOverview(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Overview(ctx, a.ID); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("after rm: %v", err)
	}
	if err := svc.RemoveOverview(ctx, "nope"); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("rm unknown = %v", err)
	}
}

func TestOverviewKindFollowsTheBranch(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	if ov, _ := svc.Overview(ctx, got.ID); ov.Kind != ReviewOnBranch {
		t.Fatalf("kind = %v, want on branch", ov.Kind)
	}
	commitFile(t, dir, "c.go", "c\n", "move on")
	if ov, _ := svc.Overview(ctx, got.ID); ov.Kind != ReviewWasTip || ov.Branch != "main" {
		t.Fatalf("after the branch moved: %+v, want was-tip", ov)
	}
}

func TestNoteCountsListOverviewsApart(t *testing.T) {
	t.Parallel()
	dir, svc := permOvRepo(t)
	ctx := context.Background()
	got, err := saveOv(t, svc, ovDoc("T", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	if len(c.Overviews) != 1 || c.Overviews[0].ID != got.ID || c.Overviews[0].Title != "T" || c.Overviews[0].Branch != "main" {
		t.Fatalf("Overviews = %+v", c.Overviews)
	}
	if c.ByCommit[head] != 0 || len(c.Reviews) != 0 || len(c.PreviewReviews) != 0 {
		t.Fatalf("an overview leaked into the note badges / reviews: %+v", c)
	}
}
