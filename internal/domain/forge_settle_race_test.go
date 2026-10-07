package domain

import (
	"context"
	"sync"
	"testing"
	"time"

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
