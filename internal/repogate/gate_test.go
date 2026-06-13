package repogate

import (
	"context"
	"testing"
	"time"
)

// tryAcquire attempts an Acquire bounded by a short deadline; ok=false means
// the gate kept us queued (the expected "excluded" outcome).
func tryAcquire(t *testing.T, g *Gate, mode Mode, label string) (*Reservation, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r, err := g.Acquire(ctx, mode, label)
	if err != nil {
		return nil, false
	}
	return r, true
}

// mustAcquire acquires with a generous deadline and fails the test otherwise.
func mustAcquire(t *testing.T, g *Gate, mode Mode, label string) *Reservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := g.Acquire(ctx, mode, label)
	if err != nil {
		t.Fatalf("acquire %v %q: %v", mode, label, err)
	}
	return r
}

func TestCompatibilityMatrix(t *testing.T) {
	cases := []struct {
		holder, req Mode
		overlap     bool
	}{
		{Read, Read, true},
		{Read, RefWrite, true},
		{RefWrite, Read, true},
		{RefWrite, RefWrite, false},
		{Read, TreeWrite, false},
		{RefWrite, TreeWrite, false},
		{TreeWrite, Read, false},
		{TreeWrite, RefWrite, false},
		{TreeWrite, TreeWrite, false},
	}
	for _, c := range cases {
		g := &Gate{}
		h := mustAcquire(t, g, c.holder, "holder")
		r, ok := tryAcquire(t, g, c.req, "req")
		if ok != c.overlap {
			t.Errorf("holder %v + request %v: overlap = %v, want %v", c.holder, c.req, ok, c.overlap)
		}
		if ok {
			r.Release()
		}
		h.Release()
		// After the holder is gone the request must always succeed.
		r2 := mustAcquire(t, g, c.req, "req-after")
		r2.Release()
	}
}

func TestWritersFIFO(t *testing.T) {
	g := &Gate{}
	first := mustAcquire(t, g, TreeWrite, "w0")
	order := make(chan string, 3)
	// Queue three writers one at a time, confirming each is queued before
	// launching the next so the FIFO order is deterministic.
	for _, name := range []string{"w1", "w2", "w3"} {
		name := name
		go func() {
			r := mustAcquire(t, g, TreeWrite, name)
			order <- name
			r.Release()
		}()
		waitQueued(t, g, name)
	}
	first.Release()
	for _, want := range []string{"w1", "w2", "w3"} {
		if got := <-order; got != want {
			t.Fatalf("grant order: got %q, want %q", got, want)
		}
	}
}

// waitQueued polls until label appears among the gate's waiters.
func waitQueued(t *testing.T, g *Gate, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range g.Queue() {
			if e.Waiting && e.Label == label {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%q never queued", label)
}

func TestWriterPreference(t *testing.T) {
	g := &Gate{}
	rd := mustAcquire(t, g, Read, "reader")
	granted := make(chan *Reservation, 1)
	go func() {
		granted <- mustAcquire(t, g, TreeWrite, "writer")
	}()
	waitQueued(t, g, "writer")
	// A NEW read must queue behind the waiting writer, not join the holder.
	if _, ok := tryAcquire(t, g, Read, "late-reader"); ok {
		t.Fatal("late read overlapped while a writer was waiting")
	}
	rd.Release()
	w := <-granted
	w.Release()
}

func TestAcquireCancelWhileQueued(t *testing.T) {
	g := &Gate{}
	h := mustAcquire(t, g, TreeWrite, "holder")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := g.Acquire(ctx, TreeWrite, "victim")
		errc <- err
	}()
	waitQueued(t, g, "victim")
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// The cancelled waiter must not wedge the queue.
	h.Release()
	r := mustAcquire(t, g, TreeWrite, "after")
	r.Release()
}

func TestDoubleReleasePanics(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, Read, "r")
	r.Release()
	defer func() {
		if recover() == nil {
			t.Fatal("second Release did not panic")
		}
	}()
	r.Release()
}
