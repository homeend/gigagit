package domain

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// racingStore lets another process write record id between the settle
// pass's read and its first edit of that record.
type racingStore struct {
	notes.Store
	id   string
	once sync.Once
	race func(*model.Note)
}

func (r *racingStore) Edit(id string, fn func(*model.Note) error) error {
	if id == r.id {
		r.once.Do(func() { _ = r.Store.Edit(id, func(n *model.Note) error { r.race(n); return nil }) })
	}
	return r.Store.Edit(id, fn)
}

func TestSettleKeepsAStampWrittenAfterItsRead(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	ctx := context.Background()
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	fresh := model.NoteSend{PR: 7, Review: "PRR_fresh", At: settleT0.Add(time.Second)}
	svc.SetNotesStore(&racingStore{Store: svc.notesStore(ctx), id: a, race: func(n *model.Note) { s := fresh; n.Send = &s }})
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if n, ok := noteByID(t, svc, a); !ok || n.Send == nil || n.Send.Review != "PRR_fresh" {
		t.Fatalf("another process's fresh stamp was cleared by a stale judgment: %+v %v", n.Send, ok)
	}
}

func TestSettleKeepsARemarkStampWrittenAfterItsRead(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	fps := r.remarkFPs()
	if len(fps) < 2 {
		t.Fatalf("fixture needs two remarks, has %d", len(fps))
	}
	if err := svc.notesStore(ctx).Edit(r.ID, func(n *model.Note) error {
		n.RemarkSends = []model.RemarkSend{{RemarkFP: fps[0], Send: model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)}}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	svc.SetNotesStore(&racingStore{Store: svc.notesStore(ctx), id: r.ID, race: func(n *model.Note) {
		n.RemarkSends = append(n.RemarkSends, model.RemarkSend{RemarkFP: fps[1],
			Send: model.NoteSend{PR: 7, Review: "PRR_fresh", At: settleT0.Add(time.Second)}})
	}})
	svc.settleSends(ctx, model.PullRequest{Number: 7}, nil, settleT0)
	n, _ := noteByID(t, svc, r.ID)
	if _, ok := n.RemarkSend(fps[0]); ok {
		t.Errorf("the gone remark stamp must be cleared: %+v", n.RemarkSends)
	}
	if rs, ok := n.RemarkSend(fps[1]); !ok || rs.Send.Review != "PRR_fresh" {
		t.Fatalf("another process's fresh remark stamp was lost: %+v", n.RemarkSends)
	}
}

// editCountingStore counts the edits of one record.
type editCountingStore struct {
	notes.Store
	id    string
	edits int
}

func (c *editCountingStore) Edit(id string, fn func(*model.Note) error) error {
	if id == c.id {
		c.edits++
	}
	return c.Store.Edit(id, fn)
}

// A partly sent review's moved remarks echo on every read of the PR: the
// settle pass must not lock and rewrite its note when nothing changes.
func TestSettleLeavesASettledReviewUntouched(t *testing.T) {
	t.Parallel()
	svc, r := structuredReview(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	fps := r.remarkFPs()
	at := settleT0.Add(-time.Minute)
	if err := svc.notesStore(ctx).Edit(r.ID, func(n *model.Note) error {
		n.RemarkSends = []model.RemarkSend{
			{RemarkFP: fps[0], Moved: true, Send: model.NoteSend{PR: 7, Review: "PRR_done", At: at}},
			{RemarkFP: summaryFP, Moved: true, Send: model.NoteSend{PR: 7, Review: "PRR_done", At: at}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	cs := &editCountingStore{Store: svc.notesStore(ctx), id: r.ID}
	svc.SetNotesStore(cs)
	echo := []model.ForgeComment{{ID: "PRRC_x", Kind: model.ForgeCommentInline, ThreadID: "PRRT_x", ReviewID: "PRR_done",
		Body: "x\n\n" + forge.SendMarker(RemarkKey(r.ID, fps[0]))}}
	svc.settleSends(ctx, model.PullRequest{Number: 7}, echo, settleT0)
	if cs.edits != 0 {
		t.Fatalf("a settled review was edited %d times by a no-change settle pass", cs.edits)
	}
}

// Re-judged live, a stamp that now proves the note SENT is not cleared: it
// would make the note local again although GitHub has it.
func TestSettleNeverClearsAStampThatProvesASend(t *testing.T) {
	t.Parallel()
	svc, _, a, _ := settleSvc(t)
	ctx := context.Background()
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_gone", At: settleT0.Add(-time.Minute)})
	svc.SetNotesStore(&racingStore{Store: svc.notesStore(ctx), id: a, race: func(n *model.Note) {
		n.Send = &model.NoteSend{PR: 7, Thread: "PRRT_t1", Comment: "PRRC_c1", At: settleT0.Add(time.Second)}
	}})
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if n, ok := noteByID(t, svc, a); ok && n.Send == nil {
		t.Fatal("a stamp naming a comment GitHub has was cleared: the note is local again")
	}
}

type removeCountingStore struct {
	notes.Store
	removes int
}

func (c *removeCountingStore) Remove(id string) error {
	c.removes++
	return c.Store.Remove(id)
}

// A fully sent review goes in the same locked write that judged it: a reply
// written between a judging Edit and a later Remove would be lost.
func TestSettleRemovesASentReviewInOneLockedWrite(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	svc.forgeNow = func() time.Time { return settleT0 }
	ctx := context.Background()
	doc := `{"version":1,"summary":"looks fine","files":[{"path":"big.go","annotations":[
 {"newRange":[5,5],"summary":"check this"},{"newRange":[25,25],"summary":"and this"}]}]}`
	rid, _, err := svc.SaveReview(ctx, SaveReview{Target: ReviewTarget{Kind: ReviewRange, Range: head + "^.." + head, Label: "feat"},
		Agent: "claude", Text: doc})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, rid)
	fps := r.remarkFPs()
	at := settleT0.Add(-time.Minute)
	if err := svc.notesStore(ctx).Edit(rid, func(n *model.Note) error {
		n.Send = &model.NoteSend{PR: 7, Review: "PRR_done", At: at}
		n.RemarkSends = []model.RemarkSend{
			{RemarkFP: fps[0], Send: model.NoteSend{PR: 7, Review: "PRR_done", Thread: "PRRT_x", At: at}},
			{RemarkFP: fps[1], Send: model.NoteSend{PR: 7, Review: "PRR_done", Thread: "PRRT_y", At: at}},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	ff.mu.Lock()
	for i, th := range []string{"PRRT_x", "PRRT_y"} {
		ff.comments = append(ff.comments, model.ForgeComment{ID: "PRRC_" + th, Kind: model.ForgeCommentInline, Path: "big.go",
			Line: 5 + 20*i, Body: "x\n\n" + forge.SendMarker(RemarkKey(rid, fps[i])), ThreadID: th, ReviewID: "PRR_done"})
	}
	ff.mu.Unlock()
	c := &removeCountingStore{Store: svc.notesStore(ctx)}
	svc.SetNotesStore(c)
	svc.invalidateNoteCounts()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Review(ctx, rid); !errors.Is(err, ErrReviewNotFound) {
		t.Fatalf("the fully sent review is still local: %v", err)
	}
	if c.removes != 0 {
		t.Fatalf("removed by %d separate Remove call(s) after the judging Edit", c.removes)
	}
}
