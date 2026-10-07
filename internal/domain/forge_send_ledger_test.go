package domain

import (
	"context"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repogate"
)

var settleT0 = time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)

// settleSvc: prThreadSvc's PR with the clock at settleT0 and two stored
// notes on a.go; it returns the fake forge to script the next read.
func settleSvc(t *testing.T) (*Service, *fakeForge, string, string) {
	t.Helper()
	svc, _, head, ff := prThreadSvc(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	add := func(sum string) string {
		n, err := svc.NoteAdd(ctx, model.Note{Source: model.NoteSourceUser, Summary: sum,
			Address: model.FileAddress{State: model.StateCommitted, Commit: head, Path: "a.go"},
			Side:    model.NoteSideNew, Range: [2]int{1, 1}})
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	return svc, ff, add("one"), add("two")
}

func stampNote(t *testing.T, svc *Service, id string, s model.NoteSend) {
	t.Helper()
	if err := svc.notesStore(context.Background()).Edit(id, func(n *model.Note) error { n.Send = &s; return nil }); err != nil {
		t.Fatal(err)
	}
}

func noteByID(t *testing.T, svc *Service, id string) (model.Note, bool) {
	t.Helper()
	all, err := svc.notesStore(context.Background()).LoadAll()
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range all {
		if n.ID == id {
			return n, true
		}
	}
	return model.Note{}, false
}

func TestSettleDeletesWhatGitHubHas(t *testing.T) {
	t.Parallel()
	svc, ff, a, b := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_new", Thread: "PRRT_new1", At: settleT0.Add(-time.Minute)})
	stampNote(t, svc, b, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_new1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one\n\n" + forge.SendMarker(a), ThreadID: "PRRT_new1", ReviewID: "PRR_new"})
	ff.mu.Unlock()
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if _, ok := noteByID(t, svc, a); ok {
		t.Error("a sent note must be deleted locally (GitHub owns it now)")
	}
	n, ok := noteByID(t, svc, b)
	if !ok || n.Send != nil {
		t.Errorf("a stamp whose review is gone must be cleared: %+v %v", n.Send, ok)
	}
}

func TestSettleKeepsAPendingReviewsItems(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Thread: "PRRT_p1", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	pr := ff.byNum[7]
	pr.ViewerPendingReview = "PRR_p"
	ff.byNum[7] = pr
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_p1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one", ThreadID: "PRRT_p1", ReviewID: "PRR_p"})
	ff.mu.Unlock()
	ctx := context.Background()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	n, ok := noteByID(t, svc, a)
	if !ok || n.Send == nil || n.Send.Review != "PRR_p" {
		t.Fatalf("an interrupted send stays stamped: %+v %v", n.Send, ok)
	}
	if rev, keys := svc.PRInterrupted(ctx, 7); rev != "PRR_p" || len(keys) != 1 || keys[0] != a {
		t.Fatalf("PRInterrupted = %q %v", rev, keys)
	}
}

func TestSettleNeverJudgesAStampNewerThanTheRead(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	// Stamped by another process after this read began: the read cannot see
	// its pending review yet, so "gone" would be a lie.
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_racing", At: settleT0.Add(time.Second)})
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, _ := noteByID(t, svc, a); n.Send == nil || n.Send.Review != "PRR_racing" {
		t.Fatalf("a newer stamp was judged: %+v", n.Send)
	}
}

func TestSettleClearsAWholeReviewWhoseReviewIsGone(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	if err := svc.notesStore(ctx).Edit(r.ID, func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_deleted", At: settleT0.Add(-time.Minute)}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	svc.settleSends(ctx, model.PullRequest{Number: 7}, nil, settleT0)
	r2, _ := svc.Review(ctx, r.ID)
	n, _ := noteByID(t, svc, r2.ID)
	if n.Send != nil {
		t.Fatalf("a whole review whose GitHub review is gone stays sending forever: %+v", n.Send)
	}
}

// The op calls Settle while holding a Read reservation: a writer queued
// behind it must not deadlock a gate-taking read inside Settle (R12).
func TestSettleNeverTakesTheGate(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	ctx := context.Background()
	gate := svc.gateFor(ctx)
	held, err := gate.Acquire(ctx, repogate.Read, "op SendToForge")
	if err != nil {
		t.Fatal(err)
	}
	defer held.Release()
	go func() { // a writer queues behind the op
		if w, err := gate.Acquire(ctx, repogate.TreeWrite, "op Commit"); err == nil {
			w.Release()
		}
	}()
	for len(gate.Queue()) < 2 { // wait until the writer is queued
		time.Sleep(time.Millisecond)
	}
	done := make(chan error, 1)
	go func() { done <- svc.sendLedger(7).Settle(ctx) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Settle blocked on the repo gate under a queued writer")
	}
}

func TestSettleNeverTakesAnExistingThreadForASentReply(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	// A standalone reply stamped before its Reply call answered (a crash):
	// its thread exists on GitHub, its comment does not.
	stampNote(t, svc, a, model.NoteSend{PR: 7, Thread: "PRRT_t1", At: settleT0.Add(-time.Minute)})
	_ = ff
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	n, ok := noteByID(t, svc, a)
	if !ok || n.Send != nil {
		t.Fatalf("the reply was never posted: keep it, local again (%+v, %v)", n.Send, ok)
	}
}

func TestSettleLeavesFailuresAndMatchesALostStamp(t *testing.T) {
	t.Parallel()
	svc, ff, a, b := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Err: "HTTP 403", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_x", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "two\n\n" + forge.SendMarker(b), ThreadID: "PRRT_x", ReviewID: "PRR_x"})
	ff.mu.Unlock()
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	if n, ok := noteByID(t, svc, a); !ok || n.Send == nil || n.Send.Err != "HTTP 403" {
		t.Errorf("a failed stamp must stay: %+v", n.Send)
	}
	if _, ok := noteByID(t, svc, b); ok {
		t.Error("an unstamped note whose marker GitHub echoes was sent: delete it")
	}
}

func TestLedgerStampsAndFailsRemarks(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	ctx := context.Background()
	fp := r.remarkFPs()[0]
	l := svc.sendLedger(7)
	key := RemarkKey(r.ID, fp)
	if err := l.Stamp(ctx, key, model.NoteSend{PR: 7, Review: "R1", Thread: "T1", At: settleT0}); err != nil {
		t.Fatal(err)
	}
	r2, _ := svc.Review(ctx, r.ID)
	if len(r2.RemarkSends) != 1 || r2.RemarkSends[0].Send.Thread != "T1" || r2.RemarkSends[0].Moved {
		t.Fatalf("after Stamp: %+v", r2.RemarkSends)
	}
	l.Fail(ctx, []string{key}, errFake("HTTP 500"))
	r3, _ := svc.Review(ctx, r.ID)
	if s := r3.RemarkSends[0].Send; s.Err != "HTTP 500" || s.Thread != "" {
		t.Fatalf("after Fail: %+v", s)
	}
}

type errFake string

func (e errFake) Error() string { return string(e) }
func TestASentNoteKeepsItsGroup(t *testing.T) {
	t.Parallel()
	svc, ff, a, _ := settleSvc(t)
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_new", Thread: "PRRT_new1", At: settleT0.Add(-time.Minute)})
	ff.mu.Lock()
	ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_new1", Kind: model.ForgeCommentInline, Path: "a.go",
		Line: 1, Body: "one", ThreadID: "PRRT_new1", ReviewID: "PRR_new"})
	ff.mu.Unlock()
	ctx := context.Background()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	set, _ := svc.PreviewNotes(ctx, git.PRRef(7), "main")
	got, _ := svc.PreviewNotesAt(ctx, set, "a.go")
	for _, r := range got {
		if r.Note.ID == model.ForgeNoteIDPrefix+"PRRC_new1" {
			if r.Group != GroupMine {
				t.Fatalf("the sent note's GitHub thread lost its group: %q", r.Group)
			}
			return
		}
	}
	t.Fatalf("the sent thread is not in the PR's notes: %+v", got)
}
