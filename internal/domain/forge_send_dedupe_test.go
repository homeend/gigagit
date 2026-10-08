package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Review Focus 1: a note named twice is one thread, in first-occurrence order.
func TestPlanSendCollapsesDuplicateNotes(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 25, "second line")
	b := addPRNote(t, svc, head, "big.go", 5, "first line")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{a, b, a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || len(p.Skipped) != 0 || p.Items[0].Key != a || p.Items[1].Key != b {
		t.Fatalf("items %+v skipped %+v", p.Items, p.Skipped)
	}
}

// A thread named by its id and by one of its comments is resolved once; a
// draft reply named twice is posted once.
func TestPlanSendCollapsesDuplicateActions(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := prThreadSvc(t)
	d := draftReply(t, svc, "on it")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{d, d},
		Resolve: []string{"PRRT_t1", model.ForgeNoteIDPrefix + "PRRC_c1", "PRRT_t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendActions || len(p.Items) != 2 {
		t.Fatalf("plan = %+v", p.Items)
	}
}

// Item 14: an interrupted send's rows name what waits, not its ledger key.
func TestFinishRowsNameTheNotes(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 5, "rename this")
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Err: "network down"})
	pendingOnGitHub(ff)
	if _, err := svc.PRRevalidate(context.Background(), 7); err != nil {
		t.Fatal(err)
	}
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Finish: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Key != a || p.Items[0].Summary != "rename this" ||
		p.Items[0].Label != "rename this (waiting in the pending review)" {
		t.Fatalf("items = %+v", p.Items)
	}
}
