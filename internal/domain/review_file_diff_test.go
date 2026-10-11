package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/textdiff"
)

// rowKinds is a diff's rows as "kind:left>right" — enough to read a change.
func rowKinds(d Diff) string {
	var parts []string
	for _, r := range d.Result.Rows {
		parts = append(parts, map[textdiff.Kind]string{textdiff.Same: "same", textdiff.Changed: "change", textdiff.Del: "del", textdiff.Add: "add"}[r.Kind]+":"+r.Left+">"+r.Right)
	}
	return strings.Join(parts, ",")
}

func saveReviewOn(t *testing.T, svc *Service, rng, doc string) string {
	t.Helper()
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: rng, Label: "r"}, Agent: "Claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// Spec 2026-10-11-overview-split-diff §3: a branch review's diff is its
// stored range — the trunk's tip against the branch tip — and a file the
// range leaves alone is unchanged, with no diff read.
func TestReviewFileDiffOfABranchReviewIsTheRangeChange(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\n", "add g")
	ctx := context.Background()
	tg, err := svc.BranchReviewTarget(ctx, "feature")
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: `{"version":1,"summary":"s","files":[]}`})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReviewFileDiff(ctx, id, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !got.Changed || got.Status != "A" || rowKinds(got.Diff) != "add:>x" {
		t.Fatalf("f.txt = %+v rows %q, want an added x", got, rowKinds(got.Diff))
	}
	if main, tip := revParse(t, dir, "main"), revParse(t, dir, "feature"); got.Left != main[:7] || got.Right != tip[:7] {
		t.Fatalf("labels %q → %q, want %q → %q", got.Left, got.Right, main[:7], tip[:7])
	}
	same, err := svc.ReviewFileDiff(ctx, id, "README.md")
	if err != nil || same.Changed || len(same.Diff.Result.Rows) != 0 || same.Right == "" {
		t.Fatalf("README.md = %+v, %v; want unchanged, no rows, labels kept", same, err)
	}
}

// A single-commit review diffs the commit's parent against it, and the
// review's remarks come on BOTH sides (an old-side one on the deleted line).
func TestReviewFileDiffOfACommitCarriesBothSidesRemarks(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	commitFile(t, dir, "g.txt", "one\ntwo\nthree\n", "add g")
	commitFile(t, dir, "g.txt", "one\nTWO\nthree\nfour\n", "edit g")
	tip := revParse(t, dir, "HEAD")
	doc := `{"version":1,"summary":"s","files":[{"path":"g.txt","annotations":[` +
		`{"oldRange":[2,2],"summary":"old two"},{"newRange":[4,4],"summary":"new four"}]}]}`
	id := saveReviewOn(t, svc, tip+"^.."+tip, doc)
	got, err := svc.ReviewFileDiff(context.Background(), id, "g.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "M" || rowKinds(got.Diff) != "same:one>one,change:two>TWO,same:three>three,add:>four" {
		t.Fatalf("g.txt %q rows %q", got.Status, rowKinds(got.Diff))
	}
	if parent := revParse(t, dir, "HEAD^"); got.Left != parent[:7] || got.Right != tip[:7] {
		t.Fatalf("labels %q → %q", got.Left, got.Right)
	}
	sides := map[string]string{}
	for _, n := range got.Notes {
		sides[n.Note.Summary] = string(n.Note.Side)
	}
	if len(got.Notes) != 2 || sides["old two"] != string(model.NoteSideOld) || sides["new four"] != string(model.NoteSideNew) {
		t.Fatalf("notes %+v", got.Notes)
	}
}

// A root commit's review has no parent: every line is added, the left side
// labelled "empty".
func TestReviewFileDiffOfARootCommit(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	root := revParse(t, dir, "main")
	id := saveReviewOn(t, svc, root, `{"version":1,"summary":"s","files":[]}`)
	got, err := svc.ReviewFileDiff(context.Background(), id, "README.md")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "A" || got.Left != "empty" || rowKinds(got.Diff) != "add:>hello" {
		t.Fatalf("root = %+v rows %q", got, rowKinds(got.Diff))
	}
}

// A working review diffs HEAD against the file on disk.
func TestReviewFileDiffOfAWorkingReview(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	files := []model.NoteFile{{Path: "f.txt", Blob: strings.Repeat("a", 40)}}
	id, _, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Agent: "Claude", Text: "x", Files: files})
	if err != nil {
		t.Fatal(err)
	}
	got, err := svc.ReviewFileDiff(ctx, id, "f.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Left != "HEAD" || got.Right != "working tree" || rowKinds(got.Diff) != "change:x>changed" {
		t.Fatalf("working = %+v rows %q", got, rowKinds(got.Diff))
	}
}

// A renamed file diffs against its old path; a deleted one shows every
// line deleted.
func TestReviewFileDiffFollowsRenamesAndDeletes(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	runGitIn(t, dir, "mv", "f.txt", "h.txt")
	runGitIn(t, dir, "commit", "-qm", "rename")
	tip := revParse(t, dir, "HEAD")
	id := saveReviewOn(t, svc, tip+"^.."+tip, `{"version":1,"summary":"s","files":[]}`)
	got, err := svc.ReviewFileDiff(context.Background(), id, "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "R" || got.OldPath != "f.txt" || rowKinds(got.Diff) != "same:x>x" {
		t.Fatalf("rename = %+v rows %q", got, rowKinds(got.Diff))
	}
	runGitIn(t, dir, "rm", "-q", "h.txt")
	runGitIn(t, dir, "commit", "-qm", "delete")
	tip = revParse(t, dir, "HEAD")
	id = saveReviewOn(t, svc, tip+"^.."+tip, `{"version":1,"summary":"s","files":[]}`)
	got, err = svc.ReviewFileDiff(context.Background(), id, "h.txt")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "D" || rowKinds(got.Diff) != "del:x>" {
		t.Fatalf("delete = %+v rows %q", got, rowKinds(got.Diff))
	}
}

func TestReviewFileDiffOfAGoneReview(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	if _, err := svc.ReviewFileDiff(context.Background(), "nope", "f.txt"); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("err = %v, want ErrReviewNotFound", err)
	}
}
