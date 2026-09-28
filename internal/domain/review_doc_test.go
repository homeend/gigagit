package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
)

const reviewDocJSON = `{"version":1,"summary":"## Overview\nfine","files":[
 {"path":"g.txt","summary":"adds g","annotations":[{"newRange":[2,2],"summary":"line two","rationale":"why","meta":{"severity":"bug"}}]},
 {"path":"g.txt","annotations":[{"newRange":[99,99],"summary":"past the end"}]},
 {"path":"missing.go","annotations":[{"newRange":[1,1],"summary":"not in the commit"}]}]}`

// structuredReview stores a document review on a commit adding g.txt (3 lines).
func structuredReview(t *testing.T) (*Service, Review) {
	t.Helper()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\ntwo\nthree\n", "add g")
	tip := revParse(t, dir, "HEAD")
	ctx := context.Background()
	tg := ReviewTarget{Kind: ReviewRange, Range: tip + "^.." + tip, Label: "g"}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "Claude", Text: "```json\n" + reviewDocJSON + "\n```"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	return svc, r
}

func TestSaveReviewStoresCanonicalJSON(t *testing.T) {
	t.Parallel()
	_, r := structuredReview(t)
	if r.Doc == nil {
		t.Fatal("a document review must parse on read")
	}
	if strings.Contains(r.Text, "```") || !strings.HasPrefix(r.Text, "{") {
		t.Fatalf("stored text must be the canonical document:\n%s", r.Text)
	}
	if _, err := notebatch.ParseReview([]byte(r.Text)); err != nil {
		t.Fatal(err)
	}
	if r.Doc.Overview != "## Overview\nfine" {
		t.Fatalf("overview %q", r.Doc.Overview)
	}
}

func TestSaveReviewKeepsProseAsText(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "# Prose\nfour findings"})
	r, _ := svc.Review(ctx, id)
	if r.Doc != nil || r.Text != "# Prose\nfour findings" {
		t.Fatalf("%+v", r)
	}
}

func TestReviewNotesForAnchorsOnTheCommitDiff(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	got, err := svc.ReviewNotesFor(context.Background(), r.ID, "g.txt", sideDiff("one", "two", "three"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d notes, want 1 (the one past the end is an other note): %+v", len(got), got)
	}
	n := got[0]
	if n.Note.ID != "review:"+r.ID+":0" || n.Note.Source != model.NoteSourceAgent || n.Note.Author != "Claude" ||
		n.Status != model.NoteActive || n.Range != [2]int{2, 2} || n.Note.Summary != "line two" || n.Note.Rationale != "why" {
		t.Fatalf("%+v", n)
	}
	if len(n.Note.Tags) != 1 || n.Note.Tags[0] != "severity: bug" {
		t.Fatalf("tags %v, want the meta as key: value", n.Note.Tags)
	}
	if n.Note.Address.Commit != r.Commit || n.Note.Address.Path != "g.txt" {
		t.Fatalf("address %+v", n.Note.Address)
	}
	if !model.IsReviewNoteID(n.Note.ID) {
		t.Fatal("review note ids must be read-only ids")
	}
}

func TestReviewNotesOtherSplit(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	ctx := context.Background()
	other, err := svc.ReviewOtherNotes(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 2 || other[0].Path != "g.txt" || other[0].Range != [2]int{99, 99} || other[1].Path != "missing.go" {
		t.Fatalf("other notes %+v", other)
	}
	counts, err := svc.ReviewFileCounts(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 1 || counts["g.txt"] != 1 {
		t.Fatalf("counts %v", counts)
	}
	files, err := svc.ReviewFiles(ctx, r)
	if err != nil || len(files) != 1 || files[0].Path != "g.txt" {
		t.Fatalf("ReviewFiles = %+v, %v", files, err)
	}
}

func TestReviewNotesForAProseReviewIsEmpty(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "prose"})
	if got, err := svc.ReviewNotesFor(ctx, id, "f.txt", sideDiff("x")); err != nil || len(got) != 0 {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestReviewReportReportsStructured(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	commitFile(t, dir, "a.txt", "one\n", "c1")
	commitFile(t, dir, "a.txt", "one\ntwo\n", "c2")
	target := ReviewTarget{Kind: ReviewRange, Range: "HEAD~1..HEAD", Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}
	ctx := context.Background()
	res, err := svc.ReviewReport(ctx, target, "Fake", `printf '{"version":1,"summary":"ok"}'`, nil)
	if err != nil || !res.Structured {
		t.Fatalf("document: structured=%v err=%v", res.Structured, err)
	}
	if r, _ := svc.Review(ctx, res.NoteID); r.Text != res.Content {
		t.Fatalf("Content must be what was stored:\n%q\n%q", res.Content, r.Text)
	}
	res, err = svc.ReviewReport(ctx, target, "Fake", `printf 'just prose'`, nil)
	if err != nil || res.Structured {
		t.Fatalf("prose: structured=%v err=%v", res.Structured, err)
	}
}

// A branch review covers its range: the view lists every file the range
// changes, not only the tip commit's.
func TestReviewFilesOfABranchReviewAreTheRangeFiles(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\n", "add g")
	ctx := context.Background()
	tg, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: `{"version":1,"summary":"s","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"on the first commit's file"}]}]}`})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, id)
	files, err := svc.ReviewFiles(ctx, r)
	if err != nil || len(files) != 2 {
		t.Fatalf("ReviewFiles = %+v, %v; want f.txt and g.txt", files, err)
	}
	if counts, _ := svc.ReviewFileCounts(ctx, id); counts["f.txt"] != 1 {
		t.Fatalf("counts %v", counts)
	}
}
