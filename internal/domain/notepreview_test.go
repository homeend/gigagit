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
	// A range review records the branch it was written on (main is checked
	// out here), a preview review where its range began.
	want := []NoteScopeCount{{Scope: "aaaaaaa..bbbbbbb", N: 1, Branch: "main"}, {Scope: "main...feat", N: 2, Base: revParse(t, dir, "main")}}
	if got := c.ScopesByCommit[tip]; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %+v, want %+v", got, want)
	}
	if got := c.PlainByCommitPath[tip+":b.txt"]; got != 1 {
		t.Fatalf("b.txt plain notes = %d, want 1", got)
	}
	// The commit's ◆ N counts only the note written in no scope: the other
	// three are its range reviews.
	if all, plain := c.ByCommit[tip], c.PlainCommitNotes(tip); all != 4 || plain != 1 {
		t.Fatalf("commit notes = %d (plain %d), want 4 (plain 1)", all, plain)
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

// A scope shows the notes written IN it alone — not a plain note on one of
// its commits, not another review's — whether it is named by itself (a
// merge preview, a pair) or by Only (a range review opened from its commit);
// a note written in it joins it.
func TestPreviewNoteSetOnly(t *testing.T) {
	t.Parallel()
	svc, dir := newPreviewRepo(t)
	ctx := context.Background()
	base, tip := revParse(t, dir, "main"), revParse(t, dir, "feat")
	ids := map[string]string{}
	for _, pv := range []string{"main...feat", "aaaaaaa..bbbbbbb", ""} {
		n, err := svc.NoteAdd(ctx, model.Note{
			Source: model.NoteSourceAgent, Author: "ada", Preview: pv,
			Address: model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}, Summary: "in " + pv,
		})
		if err != nil {
			t.Fatal(err)
		}
		ids[pv] = n.ID
	}
	// A reply carries no scope of its own: it follows its root.
	for _, pv := range []string{"main...feat", ""} {
		if _, err := svc.NoteReply(ctx, ids[pv], model.Note{Source: model.NoteSourceUser, Author: "bob", Summary: "re " + pv}); err != nil {
			t.Fatal(err)
		}
	}
	set, err := svc.PairNotes(ctx, base, tip)
	if err != nil || !set.OK() {
		t.Fatalf("pair set: %+v, %v", set, err)
	}
	// The pair itself is a scope of its own (base7..tip7): none of the three
	// notes was written in it.
	if _, total, _ := svc.PreviewNoteCounts(ctx, set); total != 0 {
		t.Fatalf("a pair shows the notes written in it alone: %d, want 0", total)
	}
	// …and so is the merge preview, by its own name.
	if pv, err := svc.PreviewNotes(ctx, "feat", "main"); err != nil {
		t.Fatal(err)
	} else if _, total, _ := svc.PreviewNoteCounts(ctx, pv); total != 1 {
		t.Fatalf("the merge preview shows its own note: %d, want 1", total)
	}
	set.Only = "main...feat"
	byPath, total, err := svc.PreviewNoteCounts(ctx, set)
	if err != nil || total != 1 || byPath["a.txt"] != 1 {
		t.Fatalf("one review's notes: %v total %d, %v", byPath, total, err)
	}
	got, err := svc.PreviewNotesAt(ctx, set, "a.txt")
	if err != nil || len(got) != 1 || got[0].Note.Summary != "in main...feat" {
		t.Fatalf("notes = %+v, %v", got, err)
	}
	if rs := got[0].Replies; len(rs) != 1 || rs[0].Note.Summary != "re main...feat" {
		t.Fatalf("the review's thread must keep its reply: %+v", rs)
	}
	// …and the commit's own view keeps the plain thread whole, reply included.
	own, err := svc.NotesAt(ctx, model.FileAddress{State: model.StateCommitted, Commit: tip, Path: "a.txt"})
	if err != nil {
		t.Fatal(err)
	}
	if plain := PlainNotes(own); len(plain) != 1 || plain[0].Note.Summary != "in " || len(plain[0].Replies) != 1 {
		t.Fatalf("the commit's own notes = %+v", plain)
	}
	if set.Pair() != "main...feat" {
		t.Fatalf("a note written in the narrowed set must carry its scope: %q", set.Pair())
	}
}
