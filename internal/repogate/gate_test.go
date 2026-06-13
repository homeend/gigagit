package repogate

import (
	"bytes"
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/observ"
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

func TestEscalateJoinsWriterQueue(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, RefWrite, "escalator")
	order := make(chan string, 2)
	go func() {
		w := mustAcquire(t, g, TreeWrite, "earlier-writer")
		order <- "earlier-writer"
		w.Release()
	}()
	waitQueued(t, g, "earlier-writer")
	go func() {
		if err := r.Escalate(context.Background()); err != nil {
			t.Errorf("escalate: %v", err)
		}
		order <- "escalator"
	}()
	// Escalate releases first, so the earlier writer must win, then the
	// escalator re-acquires.
	if got := <-order; got != "earlier-writer" {
		t.Fatalf("first grant = %q, want earlier-writer", got)
	}
	if got := <-order; got != "escalator" {
		t.Fatalf("second grant = %q, want escalator", got)
	}
	// The escalated reservation is now exclusive.
	if _, ok := tryAcquire(t, g, Read, "probe"); ok {
		t.Fatal("read overlapped a TreeWrite escalated reservation")
	}
	r.Release() // the same Reservation value remains usable after Escalate
}

func TestEscalateAlreadyTreeWrite(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, TreeWrite, "w")
	if err := r.Escalate(context.Background()); err != nil {
		t.Fatalf("escalate no-op: %v", err)
	}
	r.Release()
}

func TestEscalateCancelled(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, RefWrite, "escalator")
	blocker := mustAcquire(t, g, Read, "blocker") // keeps TreeWrite ungrantable
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- r.Escalate(ctx) }()
	waitQueued(t, g, "escalator")
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	blocker.Release()
	// The failed escalation released the original reservation; the gate
	// must be fully free now.
	r2 := mustAcquire(t, g, TreeWrite, "after")
	r2.Release()
}

func TestReleasedAccessor(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, Read, "r")
	if r.Released() {
		t.Fatal("fresh reservation reports released")
	}
	r.Release()
	if !r.Released() {
		t.Fatal("released reservation reports held")
	}
}

func TestQueueSnapshot(t *testing.T) {
	g := &Gate{}
	h := mustAcquire(t, g, Read, "holder")
	go func() { mustAcquire(t, g, TreeWrite, "waiter").Release() }()
	waitQueued(t, g, "waiter")
	q := g.Queue()
	if len(q) != 2 {
		t.Fatalf("queue len = %d, want 2 (%+v)", len(q), q)
	}
	if q[0].Label != "holder" || q[0].Mode != Read || q[0].Waiting {
		t.Fatalf("q[0] = %+v, want holding Read holder", q[0])
	}
	if q[1].Label != "waiter" || q[1].Mode != TreeWrite || !q[1].Waiting {
		t.Fatalf("q[1] = %+v, want waiting TreeWrite waiter", q[1])
	}
	h.Release()
}

func TestForRegistry(t *testing.T) {
	a1 := For("/repo-a/.git")
	a2 := For("/repo-a/.git")
	b := For("/repo-b/.git")
	if a1 != a2 {
		t.Fatal("same key returned different gates")
	}
	if a1 == b {
		t.Fatal("different keys shared a gate")
	}
	// The e2e invariant: distinct repos (distinct common dirs) never block
	// each other, even in one process.
	ra := mustAcquire(t, a1, TreeWrite, "a")
	rb := mustAcquire(t, b, TreeWrite, "b")
	ra.Release()
	rb.Release()
}

func TestWaitSpanOnlyWhenWaiting(t *testing.T) {
	var buf syncBuffer
	observ.SetSpanSink(&buf)
	defer observ.SetSpanSink(nil)

	g := &Gate{}
	r := mustAcquire(t, g, Read, "instant") // no wait → no span
	r.Release()
	if s := buf.String(); strings.Contains(s, "gate wait") {
		t.Fatalf("zero-wait acquire emitted a span: %s", s)
	}

	h := mustAcquire(t, g, TreeWrite, "holder")
	done := make(chan struct{})
	go func() {
		mustAcquire(t, g, Read, "queued-reader").Release()
		close(done)
	}()
	waitQueued(t, g, "queued-reader")
	h.Release()
	<-done
	s := buf.String()
	if !strings.Contains(s, "gate wait") || !strings.Contains(s, "queued-reader") || !strings.Contains(s, "read") {
		t.Fatalf("waited acquire span missing or incomplete: %s", s)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer (spans are emitted from the
// acquiring goroutine).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
