package domain

import (
	"context"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// a.go (5 lines) and b.go (3 lines) on main (permOvRepo), then c.go added on
// HEAD; the overview links a.go (untouched by HEAD) and c.go (added by HEAD).
func viewRepo(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := permOvRepo(t)
	commitFile(t, dir, "c.go", "c1\nc2\n", "add c")
	got, err := saveOv(t, svc, ovDoc("Tour", "[a](a.go:2) [c](c.go:1-2) [file](b.go)",
		`[{"path":"a.go","annotations":[{"newRange":[2,2],"summary":"two"}]},{"path":"c.go","annotations":[{"newRange":[1,1],"summary":"c one"}]}]`))
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, got.ID
}

func TestOverviewDocAtResolvesTheWholeTree(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	od, err := svc.OverviewDocAt(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	var ok []string
	for _, a := range od.Anchors {
		if a.OK {
			ok = append(ok, a.Dest)
		}
	}
	// a.go is NOT changed by the commit, yet its anchor resolves: the whole tree counts.
	if !slices.Equal(ok, []string{"a.go:2", "c.go:1-2", "b.go"}) {
		t.Fatalf("resolved = %v", ok)
	}
}

func TestOverviewFileNotesAndDiff(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	ctx := context.Background()
	ns, err := svc.OverviewFileNotes(ctx, id, "a.go", []string{"1", "2", "3", "4", "5"})
	if err != nil || len(ns) != 1 || ns[0].Note.Summary != "two" || ns[0].Range != [2]int{2, 2} || !model.IsReviewNoteID(ns[0].Note.ID) {
		t.Fatalf("notes = %+v %v", ns, err)
	}
	d, err := svc.OverviewFileDiff(ctx, id, "c.go")
	if err != nil || !d.Changed || d.Status != "A" {
		t.Fatalf("c.go diff = %+v %v", d, err)
	}
	if len(d.Notes) != 1 || d.Notes[0].Note.Summary != "c one" {
		t.Fatalf("c.go diff notes = %+v", d.Notes)
	}
	u, err := svc.OverviewFileDiff(ctx, id, "a.go")
	if err != nil || u.Changed {
		t.Fatalf("a.go (untouched by the commit) = %+v %v", u, err)
	}
}

func TestOverviewsInViewAllNotes(t *testing.T) {
	t.Parallel()
	dir, svc, id := viewRepo(t)
	ov, err := svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	head := revParse(t, dir, "HEAD")
	var found bool
	for _, c := range ov.Commits {
		if c.Hash == head && len(c.Overviews) == 1 && c.Overviews[0].ID == id && len(c.Files) == 0 && len(c.Reviews) == 0 {
			found = true
		}
	}
	if !found {
		t.Fatalf("commits = %+v", ov.Commits)
	}
	if shown := ov.ShownOn([]string{"some-other-branch"}); len(shown.Commits) != 1 {
		t.Fatalf("a commit overview shows from any branch (R5): %+v", shown.Commits)
	}
}

func TestPROverviewsOlderAfterAPush(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, _ := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip, Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if _, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c", Text: ovDoc("How #7 works", "[c](big.go:5)", "")}); err != nil {
		t.Fatal(err)
	}
	hs, err := svc.PROverviews(ctx, set)
	if err != nil || len(hs) != 1 || hs[0].Older {
		t.Fatalf("PROverviews = %+v %v", hs, err)
	}
	moved := set
	moved.Tip = set.Base // the head moved somewhere else
	if hs, _ = svc.PROverviews(ctx, moved); len(hs) != 1 || !hs[0].Older {
		t.Fatalf("after a push = %+v", hs)
	}
}

func TestExportPermanentOverview(t *testing.T) {
	t.Parallel()
	_, svc, id := viewRepo(t)
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{OverviewID: id})
	if err != nil || len(page) == 0 || name != "overview-tour.html" {
		t.Fatalf("export = %d bytes %q %v", len(page), name, err)
	}
}
