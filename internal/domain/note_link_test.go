package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestNoteLinkTextCommittedNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	id := addPRNote(t, svc, head, "big.go", 5, "here")
	link, err := svc.NoteLinkText(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	// A note written for a PR is addressed at the PR's scope, where its
	// view shows it — never at the bare tip commit, whose own diff hides
	// scoped notes.
	if !strings.HasSuffix(link, "/big.go@main...refs/gg/pr/7:5?note="+id) {
		t.Fatalf("link = %q (head %s)", link, head[:7])
	}
	// A reply links its own id at the thread's anchor.
	rep, err := svc.NoteReply(context.Background(), id, model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "and"})
	if err != nil {
		t.Fatal(err)
	}
	if rl, err := svc.NoteLinkText(context.Background(), rep.ID); err != nil || !strings.HasSuffix(rl, "/big.go@main...refs/gg/pr/7:5?note="+rep.ID) {
		t.Fatalf("reply link = %q, %v", rl, err)
	}
}

// A note on a commit's own diff (no scope) is addressed at the commit; one
// written in a commit-pair scope at the pair.
func TestNoteLinkTextPlainAndPairNotes(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	plain, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "plain",
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "big.go"}, Side: model.NoteSideNew, Range: [2]int{5, 5}})
	if err != nil {
		t.Fatal(err)
	}
	if l, err := svc.NoteLinkText(ctx, plain.ID); err != nil || !strings.HasSuffix(l, "/big.go@"+head+":5?note="+plain.ID) {
		t.Fatalf("plain: %q %v", l, err)
	}
	base := revParse(t, repoDir(t, svc), head+"^")
	pair, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "pair", Preview: base[:7] + ".." + head[:7],
		Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "big.go"}, Side: model.NoteSideNew, Range: [2]int{5, 5}})
	if err != nil {
		t.Fatal(err)
	}
	if l, err := svc.NoteLinkText(ctx, pair.ID); err != nil || !strings.HasSuffix(l, "/big.go@"+base[:7]+".."+head[:7]+":5?note="+pair.ID) {
		t.Fatalf("pair: %q %v", l, err)
	}
}

// A shelf-entry note has no place a link can name.
func TestNoteLinkTextShelfNoteIsRefused(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	n := model.Note{ID: "5he1f001", Source: model.NoteSourceAgent, Author: "gg", Summary: "shelved", Address: ShelfEntryNote("e1")}
	if err := svc.notesStore(context.Background()).Put(n); err != nil { // NoteAdd wants the entry; the link needs only the record
		t.Fatal(err)
	}
	if l, err := svc.NoteLinkText(context.Background(), n.ID); err == nil || !strings.Contains(err.Error(), "shelf") {
		t.Fatalf("shelf note: %q %v", l, err)
	}
}

func TestNoteLinkTextWorkingNoteCarriesAFingerprint(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "w.txt", "one\ntwo\n", "w")
	writeFile(t, dir, "w.txt", "one\ntwo changed\n") // uncommitted (conflict_test.go's helper)
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: "w",
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "w.txt"}, Side: model.NoteSideNew, Range: [2]int{2, 2}})
	if err != nil {
		t.Fatal(err)
	}
	link, err := svc.NoteLinkText(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := ":2~" + model.LineFingerprint("two changed") + "?note=" + n.ID
	if !strings.HasSuffix(link, want) {
		t.Fatalf("link = %q, want suffix %q", link, want)
	}
}

func TestNoteLinkTextRemarkAndForge(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	link, err := svc.NoteLinkText(context.Background(), "review:"+rid+":0")
	if err != nil || !strings.Contains(link, "?review="+rid) {
		t.Fatalf("remark: %q %v", link, err)
	}
	if _, err := svc.NoteLinkText(context.Background(), "forge:C1"); err == nil {
		t.Fatal("a forge comment has no local link")
	}
}

// Review Focus 2: a link to a deleted note refuses to open.
func TestResolveNoteLinkOfAGoneNote(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	id := addPRNote(t, svc, head, "big.go", 5, "here")
	link, err := svc.NoteLinkText(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	l, err := model.ParseLink(link)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc}); err != nil {
		t.Fatalf("a live note must resolve: %v", err)
	}
	if desc := svc.DescribeLink(context.Background(), l); !strings.Contains(desc, "note") || !strings.Contains(desc, "here") {
		t.Fatalf("DescribeLink = %q, want the note named with its summary", desc)
	}
	if err := svc.NoteRemove(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	_, err = ResolveLink(context.Background(), l, ResolveOpts{Cwd: svc})
	if !errors.Is(err, ErrNoteLinkGone) || !errors.Is(err, model.ErrLink) || !strings.Contains(err.Error(), "note "+id+" is not here") {
		t.Fatalf("err = %v", err)
	}
}
