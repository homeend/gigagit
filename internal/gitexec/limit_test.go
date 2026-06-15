package gitexec

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// countingRunner tracks concurrent Run calls and records the peak.
type countingRunner struct {
	cur, peak int32
	release   chan struct{}
}

func (r *countingRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	n := atomic.AddInt32(&r.cur, 1)
	for {
		p := atomic.LoadInt32(&r.peak)
		if n <= p || atomic.CompareAndSwapInt32(&r.peak, p, n) {
			break
		}
	}
	<-r.release // hold so concurrency builds up
	atomic.AddInt32(&r.cur, -1)
	return Result{}, nil
}

func (r *countingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *countingRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	return r.Run(ctx, name, argv)
}

func TestLimitRunnerCapsConcurrency(t *testing.T) {
	inner := &countingRunner{release: make(chan struct{})}
	lr := NewLimitRunner(inner)

	const n = gitConcurrency + 12
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); lr.Run(context.Background(), "git x", nil) }()
	}
	time.Sleep(50 * time.Millisecond) // let goroutines pile against the semaphore
	close(inner.release)
	wg.Wait()

	if peak := atomic.LoadInt32(&inner.peak); peak > gitConcurrency {
		t.Fatalf("peak concurrency = %d, want <= %d", peak, gitConcurrency)
	}
}
