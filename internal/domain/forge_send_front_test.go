package domain

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
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
	svc, _, head := sendRepo(t)
	rid := saveHeadReview(t, svc, head, twoRemarks)
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
	rid := saveHeadReview(t, svc, head, twoRemarks)
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

func TestPendingOutcomeMirrorsTheApprovalRules(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		res     engine.Result
		err     error
		state   string
		waiting bool
	}{
		{engine.Result{Summary: "sent 1 comments to o/r #7"}, nil, PendingSent, false},
		{engine.Result{Summary: "aborted: sending to o/r #7"}, nil, PendingRejected, false},
		{engine.Result{}, errors.New("HTTP 502"), PendingFailed, false},
		{engine.Result{}, engine.ErrDecisionRequired, PendingWaiting, true},
	} {
		st, _, waiting := PendingOutcome(c.res, c.err)
		if st != c.state || waiting != c.waiting {
			t.Errorf("%+v %v → %q waiting=%v", c.res, c.err, st, waiting)
		}
	}
}

func TestPendingSendsPathIsTheQueueFile(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	p, err := svc.PendingSendsPath(ctx)
	if err != nil || p == "" {
		t.Fatalf("path %q, %v", p, err)
	}
	if _, err := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("the queue file is not at %s: %v", p, err)
	}
}
