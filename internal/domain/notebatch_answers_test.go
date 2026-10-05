package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notebatch"
	"github.com/homeend/gigagit/internal/notes"
)

func TestBatchAnswersAReview(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	yes := true
	b := notebatch.Batch{Items: []notebatch.Item{
		{ReplyTo: remarkID(rid, 0), Summary: "agreed", Resolve: &yes},
		{ReplyTo: remarkID(rid, 1), Summary: "fixed", Link: "HEAD"},
	}}
	planned, _, err := svc.PlanNoteBatch(ctx, b, false, "", "B", NoteSideBoth)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.ApplyNoteBatch(ctx, planned); err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, rid)
	th, _ := r.RemarkThreads()
	if th[0].Resolution == nil || len(th[1].Replies) != 1 || len(th[1].Replies[0].Link) != 40 {
		t.Fatalf("threads = %+v", th)
	}
	// A bad replyTo fails the PLAN, before any write.
	bad := notebatch.Batch{Items: []notebatch.Item{{ReplyTo: remarkID(rid, 9), Summary: "x"}}}
	if _, _, err := svc.PlanNoteBatch(ctx, bad, false, "", "B", NoteSideBoth); !errors.Is(err, ErrNoSuchRemark) {
		t.Fatalf("plan: %v", err)
	}
}

// failResolveStore fails Resolve for one root: the seam that reaches the
// resolve-failure rollback (a reply failure never gets that far).
type failResolveStore struct {
	notes.Store
	failRoot string
}

func (f *failResolveStore) Resolve(r model.ThreadResolution) error {
	if r.Root == f.failRoot {
		return errors.New("disk full")
	}
	return f.Store.Resolve(r)
}

func TestBatchResolveFailureRestoresAndRollsBack(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	// Remark 0 is already resolved by A before the batch.
	if _, err := svc.NoteResolve(ctx, remarkID(rid, 0), true, "A"); err != nil {
		t.Fatal(err)
	}
	yes, no := true, false
	planned, _, err := svc.PlanNoteBatch(ctx, notebatch.Batch{Items: []notebatch.Item{
		{ReplyTo: remarkID(rid, 0), Summary: "reopen", Resolve: &no},
		{ReplyTo: remarkID(rid, 1), Summary: "done", Resolve: &yes},
	}}, false, "", "B", NoteSideBoth)
	if err != nil {
		t.Fatal(err)
	}
	svc.SetNotesStore(&failResolveStore{Store: svc.notesStore(ctx), failRoot: remarkID(rid, 1)})
	if _, err := svc.ApplyNoteBatch(ctx, planned); err == nil || !strings.Contains(err.Error(), "rolled back") {
		t.Fatalf("expected a rolled-back failure, got %v", err)
	}
	th, _ := mustReview(t, svc, rid).RemarkThreads()
	if len(th[0].Replies) != 0 || len(th[1].Replies) != 0 {
		t.Fatalf("the batch's replies survived: %+v", th)
	}
	if th[0].Resolution == nil || th[0].Resolution.By != "A" {
		t.Fatalf("remark 0 must be resolved by A again: %+v", th[0].Resolution)
	}
	if th[1].Resolution != nil {
		t.Fatalf("remark 1 must stay open: %+v", th[1].Resolution)
	}
}

func TestNoteLinkNormalisesOrRefuses(t *testing.T) {
	t.Parallel()
	svc, rid, _ := threadReview(t)
	ctx := context.Background()
	rep, err := svc.NoteReply(ctx, remarkID(rid, 0), model.Note{Summary: "fixed", Link: "HEAD"})
	if err != nil || len(rep.Link) != 40 {
		t.Fatalf("reply = %+v, %v", rep, err)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 0), model.Note{Summary: "x", Link: "no-such-rev"}); !errors.Is(err, ErrNoteLink) {
		t.Fatalf("bad rev: %v", err)
	}
	if _, err := svc.NoteReply(ctx, remarkID(rid, 0), model.Note{Summary: "x", Link: "gg://"}); !errors.Is(err, ErrNoteLink) {
		t.Fatalf("bad gg link: %v", err)
	}
}
