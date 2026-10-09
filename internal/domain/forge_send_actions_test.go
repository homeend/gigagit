package domain

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

func draftReply(t *testing.T, svc *Service, sum string) string {
	t.Helper()
	d, err := svc.NoteReply(context.Background(), model.ForgeNoteIDPrefix+"PRRC_c1", model.Note{Source: model.NoteSourceUser, Summary: sum})
	if err != nil {
		t.Fatal(err)
	}
	return d.ID
}

// A draft reply beside a new comment is one send (spec §5.2, R6): the
// comment goes as the review, the reply after it as the plan's Then.
func TestPlanSendDraftReplyBesideANoteGoesAfterIt(t *testing.T) {
	t.Parallel()
	svc, _, head, _ := prThreadSvc(t)
	d := draftReply(t, svc, "on it")
	n := addPRNote(t, svc, head, "a.go", 3, "a new comment")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{n, d}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendReview || len(p.Items) != 1 || p.Then == nil || len(p.Then.Items) != 1 || p.Then.Items[0].Key != d {
		t.Fatalf("plan = %+v then = %+v", p, p.Then)
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
	if len(p.Skipped) != 1 || p.Skipped[0].Reason != "it no longer exists" || p.Skipped[0].Label != "reply: "+gone {
		t.Fatalf("skipped = %+v", p.Skipped)
	}
}

type failingLoadStore struct{ notes.Store }

func (failingLoadStore) LoadAll() ([]model.Note, error) { return nil, errors.New("disk on fire") }

// A store that cannot be read is said as such — never mistaken for a mixed
// send.
func TestPlanSendSurfacesAStoreError(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	ctx := context.Background()
	svc.SetNotesStore(failingLoadStore{svc.notesStore(ctx)})
	_, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{"n1"}, Resolve: []string{"PRRT_1"}})
	if err == nil || errors.Is(err, ErrMixedSend) || !strings.Contains(err.Error(), "disk on fire") {
		t.Fatalf("err = %v", err)
	}
}
