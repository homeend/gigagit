package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/overviewhtml"
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
	d, err := svc.OverviewFileDiff(ctx, id, "c.go", TokInline)
	if err != nil || !d.Changed || d.Status != "A" {
		t.Fatalf("c.go diff = %+v %v", d, err)
	}
	if len(d.Notes) != 1 || d.Notes[0].Note.Summary != "c one" {
		t.Fatalf("c.go diff notes = %+v", d.Notes)
	}
	u, err := svc.OverviewFileDiff(ctx, id, "a.go", TokInline)
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
	dir, svc, id := viewRepo(t)
	// An uncommitted edit after the save: the export shows the commit, never the disk.
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	page, name, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{OverviewID: id})
	if err != nil || name != "overview-tour.html" {
		t.Fatalf("export = %d bytes %q %v", len(page), name, err)
	}
	e := exportBlob(t, page)
	if e.Title != "Overview: Tour" || len(e.Files) != 3 {
		t.Fatalf("title %q, files %+v", e.Title, e.Files)
	}
	byPath := map[string]overviewhtml.File{}
	for _, f := range e.Files {
		byPath[f.Path] = f
	}
	if a := byPath["a.go"]; len(a.Lines) != 5 || a.Changed {
		t.Fatalf("a.go = %d lines, changed %v (want the commit's 5 lines, untouched by it)", len(a.Lines), a.Changed)
	}
	if c := byPath["c.go"]; !c.Changed || c.Status != "A" || len(c.Diff) == 0 {
		t.Fatalf("c.go = %+v (want the commit's own change)", c)
	}
	for _, a := range e.Anchors {
		if a.Missing {
			t.Fatalf("anchor %s missing", a.Dest)
		}
	}
	if strings.Contains(string(page), "c one") {
		t.Fatal("an overview remark reached the page (no notes of any kind)")
	}
}

func TestExportPermanentOverviewUnknown(t *testing.T) {
	t.Parallel()
	_, svc, _ := viewRepo(t)
	if _, _, err := svc.ExportOverviewHTML(context.Background(), OverviewSource{OverviewID: "nope"}); !errors.Is(err, ErrOverviewNotFound) {
		t.Fatalf("err = %v, want ErrOverviewNotFound", err)
	}
}

// A PR overview's diff side is the PR's whole change (merge base..head),
// not its head commit's alone: a.go changes in the PR's first commit.
func TestExportPROverviewDiffsTheWholePR(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "a.go", "a1\n", "a")
	commitFile(t, dir, "b.go", "b1\n", "b")
	runGitIn(t, dir, "checkout", "-q", "-b", "feat")
	commitFile(t, dir, "a.go", "a1 changed\n", "change a")
	commitFile(t, dir, "b.go", "b1 changed\n", "change b")
	head := revParse(t, dir, "HEAD")
	runGitIn(t, dir, "update-ref", git.PRRef(7), head)
	runGitIn(t, dir, "checkout", "-q", "main")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	set, err := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	if err != nil {
		t.Fatal(err)
	}
	tg, err := svc.LinkOverviewTarget(ctx, Resolved{Preview: &set, Commit: set.Tip, Addr: model.FileAddress{State: model.StateCommitted, Commit: set.Tip}})
	if err != nil {
		t.Fatal(err)
	}
	o, _, err := svc.SaveOverview(ctx, SaveOverview{Target: tg, Agent: "c", Text: ovDoc("PR tour", "[a](a.go:1) [b](b.go:1)", "")})
	if err != nil {
		t.Fatal(err)
	}
	page, _, err := svc.ExportOverviewHTML(ctx, OverviewSource{OverviewID: o.ID})
	if err != nil {
		t.Fatal(err)
	}
	files := exportBlob(t, page).Files
	if len(files) != 2 || files[0].Path != "a.go" {
		t.Fatalf("files = %+v, want a.go and b.go", files)
	}
	for _, f := range files {
		if !f.Changed || len(f.Diff) == 0 {
			t.Fatalf("%s unchanged in the export; want the PR's whole change", f.Path)
		}
	}
}

// A stored document that no longer parses fails the export as ErrNoOverview,
// worded for an overview (ErrNoOverview's own text speaks of a review).
func TestExportUnparsedPermanentOverview(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "a.go", "1\n2\n", "a")
	_, svc := newRealRepoAt(t, dir)
	nd := t.TempDir()
	svc.UseNotesDir(nd)
	got, err := saveOv(t, svc, ovDoc("Tour", "[a](a.go:1)", ""))
	if err != nil {
		t.Fatal(err)
	}
	broke := 0
	_ = filepath.WalkDir(nd, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Ext(p) != ".toml" {
			return err
		}
		b, _ := os.ReadFile(p)
		if nb := strings.ReplaceAll(string(b), `\"version\": 1`, `\"version\": 99`); nb != string(b) {
			broke++
			return os.WriteFile(p, []byte(nb), 0o644)
		}
		return nil
	})
	if broke == 0 {
		t.Fatal("setup: found no stored document to break")
	}
	_, _, err = svc.ExportOverviewHTML(context.Background(), OverviewSource{OverviewID: got.ID})
	if !errors.Is(err, ErrNoOverview) || !strings.Contains(err.Error(), "overview "+got.ID+" no longer parses") {
		t.Fatalf("err = %v", err)
	}
}
