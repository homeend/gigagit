package domain

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// pendingOnGitHub makes the viewer's pending review PRR_p on sendRepo's PR.
func pendingOnGitHub(ff *fakeForge) {
	ff.mu.Lock()
	pr := ff.byNum[7]
	pr.ViewerPendingReview = "PRR_p"
	ff.byNum[7] = pr
	ff.mu.Unlock()
}

// Final review I1: a send whose pending review could not be deleted leaves
// FAILED stamps naming it. They are an interrupted send (finish/discard
// reach it), and a fresh send is refused instead of re-adding the same
// threads into that review.
func TestAnUndeletedPendingReviewIsAnInterruptedSend(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 5, "a")
	b := addPRNote(t, svc, head, "big.go", 25, "b")
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Err: "network down"})
	pendingOnGitHub(ff)
	ctx := context.Background()
	if _, err := svc.PRRevalidate(ctx, 7); err != nil { // what every send does first
		t.Fatal(err)
	}
	if rev, keys, joined := svc.PRInterrupted(ctx, 7); rev != "PRR_p" || len(keys) != 1 || keys[0] != a || joined {
		t.Fatalf("PRInterrupted = %q %v %v", rev, keys, joined)
	}
	if _, err := svc.planSend(ctx, PRSendRequest{PR: 7, Notes: []string{b}}); !errors.Is(err, ErrInterruptedPending) {
		t.Fatalf("a fresh send over gg's pending review = %v", err)
	}
	if p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Discard: true}); err != nil || p.Mode != engine.SendDiscard {
		t.Fatalf("--discard = %+v, %v", p, err)
	}
}

// Final review I2: a JOINED pending review is the user's own browser draft:
// --discard is refused (only --finish), the draft is never deleted by gg.
func TestDiscardRefusesAJoinedReview(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 5, "a")
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Joined: true, Err: "HTTP 502"})
	pendingOnGitHub(ff)
	ctx := context.Background()
	if _, err := svc.planSend(ctx, PRSendRequest{PR: 7, Discard: true}); !errors.Is(err, ErrDiscardJoined) {
		t.Fatalf("--discard of a joined review = %v", err)
	}
	if p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Finish: true}); err != nil || p.Mode != engine.SendFinish {
		t.Fatalf("--finish = %+v, %v", p, err)
	}
}

// Final review I3: --mine / --review with nothing sendable posts nothing.
func TestPlanSendWithEverythingSkippedIsRefused(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	_ = addPRNote(t, svc, head, "other.go", 1, "not in the PR")
	if _, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Mine: true}); !errors.Is(err, engine.ErrNothingToSend) {
		t.Fatalf("--mine with only skipped notes = %v", err)
	}
}
