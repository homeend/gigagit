package gitexec

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// stubRunner satisfies Runner and always returns zero Result, nil error.
type stubRunner struct{}

func (stubRunner) Run(ctx context.Context, name string, argv []string) (Result, error) {
	return Result{}, nil
}
func (stubRunner) RunEnv(ctx context.Context, name string, argv, env []string) (Result, error) {
	return Result{}, nil
}
func (stubRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (Result, error) {
	return Result{}, nil
}

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

// fillSem occupies all gitConcurrency slots so the next acquire must block.
func fillSem(t *testing.T) (release func()) {
	t.Helper()
	for i := 0; i < gitConcurrency; i++ {
		gitSem <- struct{}{}
	}
	return func() {
		for i := 0; i < gitConcurrency; i++ {
			<-gitSem
		}
	}
}

func TestLimitRunnerRunCancelsWhileBlocked(t *testing.T) {
	release := fillSem(t)
	defer release()
	lr := &LimitRunner{inner: stubRunner{}}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := lr.Run(ctx, "git", []string{"status"}); done <- err }()
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Run must return ctx error when cancelled while blocked on the semaphore")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not observe cancellation while blocked (bug #4 not fixed)")
	}
}
