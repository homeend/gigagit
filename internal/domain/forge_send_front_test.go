package domain

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The TUI renders the confirm from these fields (T4): reason codes are the
// exported constants, and every item and skip names its note's summary.
func TestPlanSendCarriesReasonCodesAndSummaries(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	ctx := context.Background()
	stale := addPRNote(t, svc, head, "big.go", 5, "about the old text")
	fresh := addPRNote(t, svc, head, "big.go", 25, "still here")
	if err := svc.notesStore(ctx).Edit(stale, func(n *model.Note) error { n.ContextHash = "gone"; return nil }); err != nil {
		t.Fatal(err)
	}
	svc.invalidateNoteCounts()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{stale, fresh}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Summary != "still here" {
		t.Fatalf("items = %+v", p.Items)
	}
	sk := p.Skipped
	if len(sk) != 1 || sk[0].Reason != SkipLinesChanged || sk[0].Path != "big.go" || sk[0].Line != 5 || sk[0].Summary != "about the old text" {
		t.Fatalf("skipped = %+v", sk)
	}
	for _, r := range SendSkipReasons() {
		if r == "" {
			t.Fatal("an empty reason code")
		}
	}
}

// T7: an edited body replaces the AI review's summary on GitHub; it stays
// signed by the agent and carries the send marker.
func TestPlanSendReviewHonoursAnEditedBody(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Review: rid, Body: "Edited: two things to fix."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "Edited: two things to fix.") || strings.Contains(p.Body, "looks fine") ||
		!strings.Contains(p.Body, "— claude via gg") || p.BodyText() == p.Body {
		t.Fatalf("body = %q", p.Body)
	}
}

func TestPRSendGroupsListsMineThenReviews(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine one")
	addPRNote(t, svc, head, "big.go", 25, "mine two")
	rid := savePRReview(t, svc, twoRemarks)
	gs, err := svc.PRSendGroups(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) != 2 || gs[0].ID != GroupMine || gs[0].Count != 2 ||
		gs[1].ID != "review:"+rid || gs[1].Count != 2 || gs[1].Agent != "claude" || gs[1].Summary == "" {
		t.Fatalf("groups = %+v", gs)
	}
	body, err := svc.ReviewBodyText(context.Background(), rid)
	if err != nil || !strings.Contains(body, "looks fine") || strings.Contains(body, "via gg") {
		t.Fatalf("ReviewBodyText = %q, %v", body, err)
	}
}

// W2 (user ruling 2026-10-08): the user cleared the body box — the review
// posts no body at all; with no body answer (the CLI without --body) the
// stored summary still goes.
func TestAnEmptiedReviewBodyIsPostedEmpty(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendRepo(t)
	rid := savePRReview(t, svc, twoRemarks)
	ctx := context.Background()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid, BodySet: true, Body: "   "})
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "" {
		t.Fatalf("an emptied body must post empty, got %q", p.Body)
	}
	for _, sk := range p.Skipped {
		if sk.Label == "review summary" {
			t.Fatalf("an emptied body is not a skipped summary: %+v", p.Skipped)
		}
	}
	p, err = svc.planSend(ctx, PRSendRequest{PR: 7, Review: rid})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "looks fine") || !strings.Contains(p.Body, "via gg") {
		t.Fatalf("no body given must post the signed summary, got %q", p.Body)
	}
}
