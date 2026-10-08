package repogate

import (
	"context"
	"errors"
	"testing"
	"time"
)

// A long-lived holder (a headless AI task, minutes) must not be queued
// behind: an incompatible Acquire is refused at once with a BusyError that
// names the holder, so a frontend can say "X is running" instead of
// freezing on "working…" — and so no queued writer can stall later reads.
func TestLongHolderRefusesIncompatibleAcquire(t *testing.T) {
	g := &Gate{}
	ctx := context.Background()
	long, err := g.AcquireLong(ctx, Read, "op ConflictAgent")
	if err != nil {
		t.Fatal(err)
	}
	defer long.Release()

	start := time.Now()
	_, err = g.Acquire(ctx, TreeWrite, "op Commit")
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected BusyError, got %v", err)
	}
	if busy.Holder != "op ConflictAgent" || busy.Mode != Read {
		t.Fatalf("BusyError must name the holder: %+v", busy)
	}
	if time.Since(start) > 20*time.Millisecond {
		t.Fatal("the refusal must be immediate, not a queue wait")
	}
	if q := g.Queue(); len(q) != 1 || q[0].Waiting {
		t.Fatalf("a refused acquire leaves nothing queued: %+v", q)
	}
}

func TestLongHolderStillOverlapsCompatible(t *testing.T) {
	g := &Gate{}
	ctx := context.Background()
	long := must(t)(g.AcquireLong(ctx, Read, "op ConflictAgent"))
	defer long.Release()
	rd := must(t)(g.Acquire(ctx, Read, "read status"))
	rd.Release()
	rw := must(t)(g.Acquire(ctx, RefWrite, "op CreateBranch"))
	rw.Release()
}

// A SHORT holder keeps the old behaviour: an incompatible Acquire queues and
// is granted when the holder releases (a 200 ms commit is worth waiting for).
func TestShortHolderStillQueues(t *testing.T) {
	g := &Gate{}
	ctx := context.Background()
	short := must(t)(g.Acquire(ctx, TreeWrite, "op Commit"))
	done := make(chan error, 1)
	go func() {
		r, err := g.Acquire(ctx, TreeWrite, "op Stash")
		if err == nil {
			r.Release()
		}
		done <- err
	}()
	time.Sleep(30 * time.Millisecond)
	short.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("queued acquire must be granted on release, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("queued acquire never granted")
	}
}

// Once the long holder releases, the gate is ordinary again.
func TestLongHolderReleaseClearsRefusal(t *testing.T) {
	g := &Gate{}
	ctx := context.Background()
	long := must(t)(g.AcquireLong(ctx, Read, "op ConflictAgent"))
	long.Release()
	tw := must(t)(g.Acquire(ctx, TreeWrite, "op Commit"))
	tw.Release()
}

// Escalate goes through Acquire, so it inherits the refusal: a RefWrite op
// running beside the agent that needs the whole tree gets the same answer.
func TestEscalateRefusedByLongHolder(t *testing.T) {
	g := &Gate{}
	ctx := context.Background()
	long := must(t)(g.AcquireLong(ctx, Read, "op ConflictAgent"))
	defer long.Release()
	rw := must(t)(g.Acquire(ctx, RefWrite, "op SmartPull"))
	err := rw.Escalate(ctx)
	var busy *BusyError
	if !errors.As(err, &busy) {
		t.Fatalf("expected BusyError, got %v", err)
	}
	if !rw.Released() {
		t.Fatal("a failed escalate leaves the reservation released")
	}
}

func TestBusyErrorMessage(t *testing.T) {
	e := &BusyError{Holder: "op ConflictAgent", Mode: Read}
	if got := e.Error(); got != "repository busy: op ConflictAgent is running" {
		t.Fatalf("unexpected message %q", got)
	}
}

// must returns a checker so a two-value Acquire can be its sole argument.
func must(t *testing.T) func(*Reservation, error) *Reservation {
	return func(r *Reservation, err error) *Reservation {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
}
