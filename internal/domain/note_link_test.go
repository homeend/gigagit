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

// A reply to a review remark lives with its review (ParentID = the review
// note), but its place is the remark's: its link is the remark's review
// link, and its thread is the remark with that remark's replies.
func TestNoteLinkAndThreadOfAReplyToARemark(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	rid := savePRReview(t, svc, twoRemarks)
	remark := "review:" + rid + ":1"
	rep, err := svc.NoteReply(ctx, remark, model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "on the second"})
	if err != nil {
		t.Fatal(err)
	}
	want, err := svc.ReviewRemarkLink(ctx, remark)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.NoteLinkText(ctx, rep.ID); err != nil || got != want {
		t.Fatalf("reply link = %q, %v; want the remark's %q", got, err, want)
	}
	for _, id := range []string{rep.ID, remark} {
		root, replies, resolved, err := svc.NoteThread(ctx, id)
		if err != nil {
			t.Fatalf("NoteThread(%s): %v", id, err)
		}
		if root.ID != remark || root.Address.Path == "" || root.Range[0] == 0 || root.Summary == "" {
			t.Fatalf("NoteThread(%s) root = %+v, want the remark", id, root)
		}
		if len(replies) != 1 || replies[0].ID != rep.ID || resolved != nil {
			t.Fatalf("NoteThread(%s) replies = %+v resolved = %v", id, replies, resolved)
		}
	}
	// The other remark's thread is empty: a reply does not spill over.
	if _, replies, _, err := svc.NoteThread(ctx, "review:"+rid+":0"); err != nil || len(replies) != 0 {
		t.Fatalf("remark 0: %+v %v", replies, err)
	}
}

// A range note whose end lies past the file's end (lines deleted since it
// was written) gets no fingerprint: a range link carries a block
// fingerprint or none, never a line's — which the resolver would read as a
// stale block.
func TestNoteLinkTextRangePastTheEndHasNoFingerprint(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "w.txt", "one\ntwo\nthree\nfour\n", "w")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	n, err := svc.NoteAdd(context.Background(), model.Note{Source: model.NoteSourceUser, Summary: "w",
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "w.txt"}, Side: model.NoteSideNew, Range: [2]int{2, 4}})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "w.txt", "one\ntwo\n") // the note's block now runs past the end
	link, err := svc.NoteLinkText(context.Background(), n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(link, "/w.txt:2-4?note="+n.ID) {
		t.Fatalf("link = %q, want :2-4 with no fingerprint", link)
	}
}

// A staged note links at @staged with the index's text fingerprinted; a
// range note carries the block's fingerprint over :a-b.
func TestNoteLinkTextStagedAndRangeNotes(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "w.txt", "one\ntwo\nthree\n", "w")
	writeFile(t, dir, "w.txt", "one\ntwo staged\nthree\n")
	runGitIn(t, dir, "add", "w.txt")
	writeFile(t, dir, "w.txt", "one\ntwo working\nthree\n") // the working copy differs from the index
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	staged, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "s",
		Address: model.FileAddress{State: model.StateStaged, Worktree: dir, Path: "w.txt"}, Side: model.NoteSideNew, Range: [2]int{2, 2}})
	if err != nil {
		t.Fatal(err)
	}
	if l, err := svc.NoteLinkText(ctx, staged.ID); err != nil || !strings.HasSuffix(l, "/w.txt@staged:2~"+model.LineFingerprint("two staged")+"?note="+staged.ID) {
		t.Fatalf("staged: %q %v", l, err)
	}
	rng, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: "r",
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: "w.txt"}, Side: model.NoteSideNew, Range: [2]int{1, 3}})
	if err != nil {
		t.Fatal(err)
	}
	want := "/w.txt:1-3~" + model.BlockFingerprint([]string{"one", "two working", "three"}) + "?note=" + rng.ID
	if l, err := svc.NoteLinkText(ctx, rng.ID); err != nil || !strings.HasSuffix(l, want) {
		t.Fatalf("range: %q %v, want suffix %q", l, err, want)
	}
}

// Review finding I1: a reply follows its remark across a re-save (the
// remark's fingerprint, not the index it was written under) — its link
// names the remark's new place, and its thread is that remark's. A reply
// whose remark the re-save dropped keeps an outdated thread: root = the
// summary it answered, no live remark.
func TestNoteLinkAndThreadOfAReplyFollowItsRemarkAcrossAReSave(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	rep, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Source: model.NoteSourceUser, Author: "me", Summary: "on third"})
	if err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed) // "third" is remark 0 now
	want, err := svc.ReviewRemarkLink(ctx, remarkID(rid, 0))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := svc.NoteLinkText(ctx, rep.ID); err != nil || got != want {
		t.Fatalf("reply link = %q, %v; want the moved remark's %q", got, err, want)
	}
	root, replies, _, err := svc.NoteThread(ctx, rep.ID)
	if err != nil || root.ID != remarkID(rid, 0) || root.Summary != "third" || len(replies) != 1 || replies[0].ID != rep.ID {
		t.Fatalf("thread after the re-save: root %+v replies %+v err %v", root, replies, err)
	}
	resave(t, svc, rid, tg, threadDocWithoutThird) // the remark is gone: the thread is outdated
	if _, err := svc.NoteLinkText(ctx, rep.ID); err == nil {
		t.Fatal("a reply whose remark is gone has no link")
	}
	root, replies, _, err = svc.NoteThread(ctx, rep.ID)
	if err != nil || root.ID != rep.Remark || root.Summary != "third" || root.Address.Path != "" || len(replies) != 1 || replies[0].ID != rep.ID {
		t.Fatalf("outdated thread: root %+v replies %+v err %v", root, replies, err)
	}
}

// Review finding I4: a working review's remark thread is rooted at the
// worktree's address (the review's own), never at an empty commit.
func TestNoteThreadOfAWorkingReviewRemarkIsAtTheWorktree(t *testing.T) {
	t.Parallel()
	dir, _ := newRealRepo(t)
	commitFile(t, dir, "w.txt", "one\ntwo\n", "w")
	writeFile(t, dir, "w.txt", "one\ntwo changed\n")
	_, svc := newRealRepoAt(t, dir)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewWorking}, Agent: "A",
		Text: `{"version":1,"summary":"w","files":[{"path":"w.txt","annotations":[{"newRange":[2,2],"summary":"changed"}]}]}`})
	if err != nil {
		t.Fatal(err)
	}
	root, _, _, err := svc.NoteThread(ctx, remarkID(rid, 0))
	if err != nil {
		t.Fatal(err)
	}
	if root.Address.State != model.StateUnstaged || root.Address.Worktree == "" || root.Address.Commit != "" || root.Address.Path != "w.txt" {
		t.Fatalf("root address = %+v, want the worktree's", root.Address)
	}
}
