package domain

import (
	"context"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// A note remembers the scope it was written in: a merge preview by its
// branch NAMES (portable, like its gg:// link), a commit pair by its shas.
func TestPreviewNoteSetPair(t *testing.T) {
	t.Parallel()
	pv := PreviewNoteSet{Source: "feat", Target: "main", Tip: "t", Base: "b"}
	if got := pv.Pair(); got != "main...feat" {
		t.Fatalf("merge preview Pair = %q", got)
	}
	pair := PreviewNoteSet{Tip: "bbbbbbbbbbbb", Base: "aaaaaaaaaaaa"}
	if got := pair.Pair(); got != "aaaaaaa..bbbbbbb" {
		t.Fatalf("commit pair Pair = %q", got)
	}
	if got := (PreviewNoteSet{}).Pair(); got != "" {
		t.Fatalf("zero set Pair = %q", got)
	}
}

// NoteCounts groups a commit's notes by the scope they were written in — the
// commit's Range review rows — and counts apart the ones written in none:
// only those keep a Notes row beside a file the commit does not change.
func TestNoteCountsScopesByCommit(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	tip := revParse(t, dir, "feat")
	add := func(path, preview string) {
		t.Helper()
		if _, err := svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: preview,
			Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: path},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "s",
		}); err != nil {
			t.Fatal(err)
		}
	}
	add("a.txt", "main...feat")
	add("b.txt", "main...feat") // one scope, whichever file
	add("a.txt", "aaaaaaa..bbbbbbb")
	add("b.txt", "") // written outside a preview
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	want := []NoteScopeCount{{Scope: "aaaaaaa..bbbbbbb", N: 1}, {Scope: "main...feat", N: 2}}
	if got := c.ScopesByCommit[tip]; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %+v, want %+v", got, want)
	}
	if got := c.PlainByCommitPath[tip+":b.txt"]; got != 1 {
		t.Fatalf("b.txt plain notes = %d, want 1", got)
	}
	if got := c.PlainByCommitPath[tip+":a.txt"]; got != 0 {
		t.Fatalf("a.txt plain notes = %d, want 0", got)
	}
}

// A scope opens from the commit that holds its notes as a frozen range: a
// commit pair as its own two commits (stored short), a merge preview as where
// the commit left the target .. the commit — whatever the branch did since.
func TestScopeAtCommit(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	base, tip, mid := revParse(t, dir, "main"), revParse(t, dir, "feat"), revParse(t, dir, "feat~1")

	a, b, err := svc.ScopeAtCommit(ctx, "main...feat", mid)
	if err != nil || a != base || b != mid {
		t.Fatalf("merge preview at feat~1 = %s..%s, %v; want %s..%s", a, b, err, base, mid)
	}
	a, b, err = svc.ScopeAtCommit(ctx, mid[:7]+".."+tip[:7], tip)
	if err != nil || a != mid || b != tip {
		t.Fatalf("pair = %s..%s, %v; want %s..%s", a, b, err, mid, tip)
	}
	if _, _, err := svc.ScopeAtCommit(ctx, "gone...feat", tip); err == nil {
		t.Fatal("a target that is not here must be an error")
	}
	// The commit is already in the target (the branch was merged): no range
	// is left to show, and an empty diff must not open in its place.
	if _, _, err := svc.ScopeAtCommit(ctx, "feat...other", mid); err == nil {
		t.Fatal("a commit already in the target must be an error")
	}
	if _, _, err := svc.ScopeAtCommit(ctx, "", tip); err == nil {
		t.Fatal("no scope must be an error")
	}
}
