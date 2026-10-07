package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func draftReply(t *testing.T, svc *Service, sum string) string {
	t.Helper()
	d, err := svc.NoteReply(context.Background(), model.ForgeNoteIDPrefix+"PRRC_c1", model.Note{Source: model.NoteSourceUser, Summary: sum})
	if err != nil {
		t.Fatal(err)
	}
	return d.ID
}

func TestPlanSendRefusesADraftReplyMixedWithNotes(t *testing.T) {
	t.Parallel()
	svc, _, head, _ := prThreadSvc(t)
	d := draftReply(t, svc, "on it")
	n := addPRNote(t, svc, head, "a.go", 3, "a new comment")
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{n, d}}); !errors.Is(err, ErrMixedSend) {
		t.Fatalf("err = %v, want ErrMixedSend (a draft reply is sent on its own)", err)
	}
}

func TestPlanSendActionsSkipADeletedDraft(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := prThreadSvc(t)
	ctx := context.Background()
	keep := draftReply(t, svc, "on it")
	gone := draftReply(t, svc, "deleted before the approve")
	if err := svc.NoteRemove(ctx, gone); err != nil {
		t.Fatal(err)
	}
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{keep, gone}, Resolve: []string{"PRRT_t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendActions || len(p.Items) != 2 {
		t.Fatalf("plan = %+v", p)
	}
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "it no longer exists" || p.Skipped[0].Label != "reply "+gone {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
}
