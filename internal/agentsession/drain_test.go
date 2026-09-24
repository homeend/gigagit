package agentsession

import (
	"sync/atomic"
	"testing"
	"time"
)

// ConPTY never ends the output stream on its own (xpty keeps the pipe's
// write end open until Close), so the drain after exit ends once output has
// been quiet for a moment instead of waiting out the bound.
func TestDrainEndsWhenOutputGoesQuiet(t *testing.T) {
	t.Parallel()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	began := time.Now()
	drainOutput(make(chan struct{}), &last, 100*time.Millisecond, 2*time.Second)
	if d := time.Since(began); d < 100*time.Millisecond || d > time.Second {
		t.Fatalf("drain took %v, want about the 100ms quiet window", d)
	}
}

func TestDrainWaitsWhileOutputFlows(t *testing.T) {
	t.Parallel()
	var last atomic.Int64
	stop := make(chan struct{})
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				last.Store(time.Now().UnixNano())
				time.Sleep(20 * time.Millisecond)
			}
		}
	}()
	time.AfterFunc(400*time.Millisecond, func() { close(stop) })
	began := time.Now()
	drainOutput(make(chan struct{}), &last, 100*time.Millisecond, 2*time.Second)
	if d := time.Since(began); d < 400*time.Millisecond || d > 1500*time.Millisecond {
		t.Fatalf("drain took %v, want the output burst plus the quiet window", d)
	}
}

func TestDrainEndsAtEOF(t *testing.T) {
	t.Parallel()
	var last atomic.Int64
	last.Store(time.Now().UnixNano())
	done := make(chan struct{})
	close(done)
	began := time.Now()
	drainOutput(done, &last, time.Second, 2*time.Second)
	if d := time.Since(began); d > 50*time.Millisecond {
		t.Fatalf("drain took %v after end-of-stream", d)
	}
}

func TestDrainIsBounded(t *testing.T) {
	t.Parallel()
	var last atomic.Int64
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
				last.Store(time.Now().UnixNano())
				time.Sleep(10 * time.Millisecond)
			}
		}
	}()
	began := time.Now()
	drainOutput(make(chan struct{}), &last, 100*time.Millisecond, 300*time.Millisecond)
	if d := time.Since(began); d > 800*time.Millisecond {
		t.Fatalf("drain took %v, past its 300ms bound", d)
	}
}
