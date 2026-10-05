package domain

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// Final review #2: a remark reply's stored parent is the REVIEW itself, so an
// older gg (whose orphan prune keys on ParentID) keeps it; the remark it
// answers rides in Remark.
func TestRemarkReplyHangsOffTheReviewNote(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	rep, err := svc.NoteReply(context.Background(), remarkID(rid, 1), model.Note{Author: "B", Summary: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.ParentID != rid || rep.Remark != remarkID(rid, 1) {
		t.Fatalf("reply = parent %q remark %q", rep.ParentID, rep.Remark)
	}
}

// Final review #1, probe 1: after a re-save moved the remarks, resolving the
// remark now at an index another (moved) remark's resolution was made under
// must not take that resolution away; and reopening a remark shown resolved
// must work by what it is, not where it was.
func TestResolveAfterAResaveKeepsEachRemarksState(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil { // "third"
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed)                                   // third is #0, first is #2
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil { // "first"
		t.Fatal(err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if th[0].Resolution == nil || th[2].Resolution == nil || th[1].Resolution != nil {
		t.Fatalf("third and first must both be resolved: %+v", th)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 0), false, "B"); err != nil { // reopen "third"
		t.Fatalf("reopening the remark shown resolved: %v", err)
	}
	th, _ = mustReview(t, svc, rid).RemarkThreads()
	if th[0].Resolution != nil || th[2].Resolution == nil {
		t.Fatalf("only third reopens: %+v", th)
	}
}

// Final review #1, probe 2: replying to an answer after a re-save stays in
// the answered remark's thread.
func TestReplyToAnAnswerAfterAResaveStaysInItsThread(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	ans, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"})
	if err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed)
	if _, err := svc.NoteReply(ctx, ans.ID, model.Note{Author: "A", Summary: "thanks"}); err != nil {
		t.Fatal(err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if len(th[0].Replies) != 2 || len(th[2].Replies) != 0 {
		t.Fatalf("the follow-up must join third's thread (#0): %+v", th)
	}
}

// An answer whose remark is gone cannot be resolved by an id that now names
// another remark.
func TestResolvingAnOutdatedThreadIsRefused(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	ans, err := svc.NoteReply(ctx, remarkID(rid, 2), model.Note{Author: "B", Summary: "on third"})
	if err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocWithoutThird)
	if _, err := svc.NoteResolve(ctx, ans.ID, true, "B"); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("resolving an outdated thread: %v", err)
	}
}

// Final review #8: a working review's remark resolves (its part is the
// worktree's) and the working-tree diff shows it.
func TestWorkingReviewRemarkResolves(t *testing.T) {
	t.Parallel()
	_, svc, id := workingReviewOf(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(id, 0), model.Note{Author: "B", Summary: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteResolve(ctx, remarkID(id, 0), true, "B"); err != nil {
		t.Fatal(err)
	}
	r := mustReview(t, svc, id)
	if th, _ := r.RemarkThreads(); th[0].Resolution == nil || len(th[0].Replies) != 1 {
		t.Fatalf("working review thread = %+v", th)
	}
}

// Final review #1 (withResolutions): a resolution keyed by a remark id never
// stamps a remark the fingerprint join left open.
func TestWithResolutionsLeavesRemarksToTheJoin(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 2), true, "B"); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, threadDocReversed)
	open := []ResolvedNote{{Note: model.Note{ID: remarkID(rid, 2), Source: model.NoteSourceAgent}}}
	if got := svc.withResolutions(ctx, open); got[0].Resolution != nil {
		t.Fatalf("a stale index-keyed entry stamped remark #2: %+v", got[0].Resolution)
	}
}

// Final review #3: an outdated thread is shown without an id — the id it was
// made under now names another remark (or none).
func TestReviewShowOutdatedCarriesNoAddressableID(t *testing.T) {
	t.Parallel()
	svc, rid, tg := threadReview(t)
	ctx := context.Background()
	if _, err := svc.NoteReply(ctx, remarkID(rid, 1), model.Note{Author: "B", Summary: "on second"}); err != nil {
		t.Fatal(err)
	}
	resave(t, svc, rid, tg, `{"version":1,"summary":"ok","files":[{"path":"g.txt","annotations":[{"newRange":[1,1],"summary":"first"},{"newRange":[3,3],"summary":"third"}]}]}`)
	rs, err := svc.ReviewShow(ctx, rid)
	if err != nil || len(rs.Outdated) != 1 {
		t.Fatalf("show = %+v, %v", rs, err)
	}
	raw, _ := json.Marshal(rs.Outdated)
	if strings.Contains(string(raw), "review:") {
		t.Fatalf("an outdated thread carries a live remark id: %s", raw)
	}
}

// resolvedCountingStore counts full resolution reads.
type resolvedCountingStore struct {
	notes.Store
	loads int
}

func (c *resolvedCountingStore) LoadAllResolved() ([]model.ThreadResolution, error) {
	c.loads++
	return c.Store.LoadAllResolved()
}

// Final review #6: one query reads the resolutions once, however many files
// its notes sit on.
func TestNotesOverviewReadsResolutionsOnce(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	commitFile(t, dir, "g.txt", "one\ntwo\n", "add g")
	g := revParse(t, dir, "HEAD")
	commitFile(t, dir, "h.txt", "one\ntwo\n", "add h")
	h := revParse(t, dir, "HEAD")
	for _, a := range []model.FileAddress{
		{State: model.StateCommitted, Commit: g, Path: "g.txt"},
		{State: model.StateCommitted, Commit: h, Path: "h.txt"},
	} {
		if _, err := svc.NoteAdd(ctx, model.Note{Address: a, Side: model.NoteSideNew, Range: [2]int{1, 1}, Summary: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	c := &resolvedCountingStore{Store: svc.notesStore(ctx)}
	svc.SetNotesStore(c)
	ov, err := svc.NotesOverview(ctx)
	if err != nil {
		t.Fatal(err)
	}
	files := 0
	for _, cm := range ov.Commits {
		files += len(cm.Files)
	}
	if files < 2 || c.loads != 1 {
		t.Fatalf("files=%d resolution reads=%d, want ≥2 files and ONE read", files, c.loads)
	}
}
