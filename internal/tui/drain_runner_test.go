package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// errRunnerDrained is what a drainRunner answers once its test is over.
var errRunnerDrained = errors.New("test runner drained: the test already returned")

// drainRunner wraps a test repo's Runner so the test's cleanup can wait out
// every git process still in flight. startOp runs an op in a goroutine, and a
// test that only asserts "the op started" returns while git is still writing
// into .git — t.TempDir's RemoveAll then loses the race ("unlinkat …/.git:
// directory not empty", seen on CI). close() refuses new invocations FIRST
// and only then waits, so an op goroutine that has not been scheduled yet is
// turned away instead of slipping in behind the wait.
type drainRunner struct {
	inner gitexec.Runner

	mu     sync.Mutex
	closed bool
	wg     sync.WaitGroup
}

func (d *drainRunner) enter() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.closed {
		return false
	}
	d.wg.Add(1)
	return true
}

func (d *drainRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if !d.enter() {
		return gitexec.Result{}, errRunnerDrained
	}
	defer d.wg.Done()
	return d.inner.Run(ctx, name, argv)
}

func (d *drainRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	if !d.enter() {
		return gitexec.Result{}, errRunnerDrained
	}
	defer d.wg.Done()
	return d.inner.RunEnv(ctx, name, argv, env)
}

func (d *drainRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	if !d.enter() {
		return gitexec.Result{}, errRunnerDrained
	}
	defer d.wg.Done()
	return d.inner.Stream(ctx, name, argv, onLine)
}

func (d *drainRunner) close() {
	d.mu.Lock()
	d.closed = true
	d.mu.Unlock()
	d.wg.Wait()
}

// testRepo is THE way a tui test builds a real-git repo handle for dir: its
// runner drains before the test's temp dir is removed (cleanups run LIFO, and
// dir's TempDir cleanup was registered before this one).
func testRepo(t testing.TB, dir string) *git.Repo {
	t.Helper()
	d := &drainRunner{inner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
	t.Cleanup(d.close)
	return &git.Repo{Runner: d}
}

// blockingRunner parks Run until released, standing in for a slow git.
type blockingRunner struct {
	gitexec.Runner
	started, release chan struct{}
}

func (b blockingRunner) Run(context.Context, string, []string) (gitexec.Result, error) {
	close(b.started)
	<-b.release
	return gitexec.Result{}, nil
}

func TestDrainRunnerCloseWaitsForInFlightRun(t *testing.T) {
	t.Parallel()
	b := blockingRunner{started: make(chan struct{}), release: make(chan struct{})}
	d := &drainRunner{inner: b}
	go d.Run(context.Background(), "git x", nil)
	<-b.started

	closed := make(chan struct{})
	go func() { d.close(); close(closed) }()
	select {
	case <-closed:
		t.Fatal("close returned while a Run was still in flight")
	case <-time.After(50 * time.Millisecond):
	}
	close(b.release)
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("close never returned after the Run finished")
	}
}

func TestDrainRunnerRefusesAfterClose(t *testing.T) {
	t.Parallel()
	d := &drainRunner{inner: blockingRunner{started: make(chan struct{}), release: make(chan struct{})}}
	d.close()
	if _, err := d.Run(context.Background(), "git x", nil); !errors.Is(err, errRunnerDrained) {
		t.Fatalf("Run after close: err = %v, want errRunnerDrained", err)
	}
}
