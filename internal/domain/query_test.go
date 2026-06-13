package domain

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repogate"
)

// fakeReads returns a FakeRunner with all four FATAL reads configured to
// succeed (Status/Branches/Log/Worktrees). Best-effort reads are left
// unconfigured (they error, which Snapshot must tolerate). Worktrees yields
// one worktree with a known HEAD so the CommitTimes dependency is testable.
func fakeReads() *gitexec.FakeRunner {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git status", gitexec.Result{Stdout: "# branch.head main\x00"})
	f.SetResponse("git for-each-ref", gitexec.Result{Stdout: ""})
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	f.SetResponse("git worktree list", gitexec.Result{Stdout: "worktree /repo\nHEAD abc123\nbranch refs/heads/main\n\n"})
	f.SetResponse("git log (commit times)", gitexec.Result{Stdout: "abc123\x001700000000\n"})
	f.SetResponse("git rev-parse (toplevel)", gitexec.Result{Stdout: "/repo\n"})
	f.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: "/repo/.git\n"})
	return f
}

func TestSnapshotFansOutAllReads(t *testing.T) {
	f := fakeReads()
	snap, err := New(&git.Repo{Runner: f}).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if snap.Status.Branch != "main" {
		t.Fatalf("status not populated: %+v", snap.Status)
	}
	if len(snap.Worktrees) != 1 {
		t.Fatalf("worktrees = %+v", snap.Worktrees)
	}
	if snap.HeadTimes["abc123"] != 1700000000 {
		t.Fatalf("CommitTimes not wired to worktree HEAD: %+v", snap.HeadTimes)
	}
	var sawSha bool
	for _, c := range f.Calls {
		if c.Name == "git log (commit times)" {
			for _, a := range c.Argv {
				if a == "abc123" {
					sawSha = true
				}
			}
		}
	}
	if !sawSha {
		t.Fatal("CommitTimes was not called with the worktree HEAD sha")
	}
}

func TestSnapshotFatalReadErrors(t *testing.T) {
	for _, name := range []string{"git status", "git for-each-ref", "git worktree list"} {
		f := fakeReads()
		f.SetError(name, errors.New("kaboom"))
		if _, err := New(&git.Repo{Runner: f}).Snapshot(context.Background()); err == nil {
			t.Fatalf("fatal read %q failure did not propagate", name)
		}
	}
}

func TestSnapshotBestEffortReadsTolerated(t *testing.T) {
	f := fakeReads()
	f.SetError("git rev-parse (toplevel)", errors.New("x"))
	f.SetError("git rev-parse (common-dir)", errors.New("x"))
	f.SetError("git log (commit times)", errors.New("x"))
	snap, err := New(&git.Repo{Runner: f}).Snapshot(context.Background())
	if err != nil {
		t.Fatalf("best-effort failures must not fail Snapshot: %v", err)
	}
	if snap.CurrentWorktree != "" || snap.GitCommonDir != "" || len(snap.HeadTimes) != 0 {
		t.Fatalf("best-effort fields should be zero on failure: %+v", snap)
	}
}

func TestSnapshotCoalesces(t *testing.T) {
	f := &blockingFake{FakeRunner: fakeReads(), hit: make(chan struct{}), release: make(chan struct{})}
	svc := New(&git.Repo{Runner: f})
	const n = 3
	done := make(chan model.WorkingTreeStatus, n)
	go func() { s, _ := svc.Snapshot(context.Background()); done <- s.Status }()
	<-f.hit // first git status is in flight, holding the singleflight slot
	for i := 0; i < n-1; i++ {
		go func() { s, _ := svc.Snapshot(context.Background()); done <- s.Status }()
	}
	time.Sleep(20 * time.Millisecond)
	close(f.release)
	for i := 0; i < n; i++ {
		<-done
	}
	if got := atomic.LoadInt32(&f.statusCalls); got != 1 {
		t.Fatalf("git status ran %d times across %d concurrent Snapshots, want 1", got, n)
	}
}

// blockingFake blocks the first git status until release is closed, counting
// status invocations — to prove Snapshot coalesces concurrent callers.
type blockingFake struct {
	*gitexec.FakeRunner
	statusCalls int32
	hit         chan struct{}
	release     chan struct{}
}

func (b *blockingFake) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name == "git status" {
		if atomic.AddInt32(&b.statusCalls, 1) == 1 {
			close(b.hit)
			<-b.release
		}
	}
	return b.FakeRunner.Run(ctx, name, argv)
}

func TestSingleReadsReturnVerbResults(t *testing.T) {
	f := fakeReads()
	svc := New(&git.Repo{Runner: f})
	st, err := svc.Status(context.Background())
	if err != nil || st.Branch != "main" {
		t.Fatalf("Status: %v %+v", err, st)
	}
	wts, err := svc.Worktrees(context.Background())
	if err != nil || len(wts) != 1 {
		t.Fatalf("Worktrees: %v %+v", err, wts)
	}
}

func TestSnapshotDoesNotReadCommits(t *testing.T) {
	f := fakeReads()
	if _, err := New(&git.Repo{Runner: f}).Snapshot(context.Background()); err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	for _, c := range f.Calls {
		if c.Name == "git log" {
			t.Fatal("Snapshot must not read commits (the CommitFeed owns them)")
		}
	}
}

// TestQueryHoldsReadReservation: query acquires a Read reservation for the
// duration of fn (observed mid-call via the gate Queue) and releases after.
func TestQueryHoldsReadReservation(t *testing.T) {
	f := fakeReads()
	svc := New(&git.Repo{Runner: f})
	var (
		mu   sync.Mutex
		held []repogate.Entry
	)
	_, _ = query(context.Background(), svc, "probe", func(ctx context.Context) (int, error) {
		mu.Lock()
		held = svc.gateFor(ctx).Queue()
		mu.Unlock()
		return 1, nil
	})
	if len(held) != 1 || held[0].Mode != repogate.Read || held[0].Waiting {
		t.Fatalf("mid-query gate state = %+v, want one held Read", held)
	}
	if q := svc.gateFor(context.Background()).Queue(); len(q) != 0 {
		t.Fatalf("reservation not released after query: %+v", q)
	}
}
