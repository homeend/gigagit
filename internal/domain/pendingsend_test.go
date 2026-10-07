package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/clock"
)

func TestPendingSendLifecycleAcrossTwoServices(t *testing.T) {
	t.Parallel()
	dir, agent := newRealRepo(t)
	_, user := newRealRepoAt(t, dir) // a second process on the same repo
	ctx := context.Background()
	e, err := agent.PendingSendAdd(ctx, PRSendRequest{PR: 7, Notes: []string{"n1"}}, "claude")
	if err != nil || e.ID == "" || e.State != PendingWaiting {
		t.Fatalf("Add = %+v, %v", e, err)
	}
	list, err := user.PendingSends(ctx)
	if err != nil || len(list) != 1 || list[0].Request.Notes[0] != "n1" || list[0].Requester != "claude" {
		t.Fatalf("the other process lists %+v, %v", list, err)
	}
	done := make(chan PendingSend, 1)
	go func() {
		got, err := agent.PendingSendWait(ctx, e.ID, 5*time.Second)
		if err != nil {
			t.Errorf("Wait: %v", err)
		}
		done <- got
	}()
	if _, err := user.PendingSendFinish(ctx, e.ID, PendingSent, "sent 1 comments to o/r #7"); err != nil {
		t.Fatal(err)
	}
	if got := <-done; got.State != PendingSent || got.Outcome != "sent 1 comments to o/r #7" {
		t.Fatalf("the waiter got %+v", got)
	}
	if _, err := user.PendingSendFinish(ctx, e.ID, PendingRejected, ""); !errors.Is(err, ErrPendingSendClosed) {
		t.Fatalf("finishing twice = %v", err)
	}
}

func TestPendingSendWaitTimesOut(t *testing.T) {
	t.Parallel()
	_, svc := newRealRepo(t)
	ctx := context.Background()
	e, _ := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude")
	got, err := svc.PendingSendWait(ctx, e.ID, 30*time.Millisecond)
	if !errors.Is(err, ErrPendingStillWaiting) || got.State != PendingWaiting {
		t.Fatalf("Wait = %+v, %v", got, err)
	}
}

// Serial: freezes the process clock.
func TestPendingSendExpiresAfterADay(t *testing.T) {
	_, svc := newRealRepo(t)
	ctx := context.Background()
	t0 := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	restore := clock.Freeze(t0)
	defer restore()
	e, _ := svc.PendingSendAdd(ctx, PRSendRequest{PR: 7, Mine: true}, "claude")
	clock.Freeze(t0.Add(25 * time.Hour))
	got, err := svc.PendingSendGet(ctx, e.ID)
	if err != nil || got.State != PendingExpired {
		t.Fatalf("after 25 h: %+v, %v", got, err)
	}
	clock.Freeze(t0.Add(50 * time.Hour))
	if _, err := svc.PendingSendGet(ctx, e.ID); !errors.Is(err, ErrPendingSendNotFound) {
		t.Fatalf("a finished entry is dropped a day later: %v", err)
	}
}
