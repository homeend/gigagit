package domain

import (
	"context"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// pagingRunner serves `git log` from a fixed list of `total` commits, honoring
// -n <limit> and --skip=<n> from argv. Other span names return empty. Safe for
// concurrent use. If block is non-nil, the FIRST page read (skip>0) blocks on
// it (after signalling hit) — for the single-flight test, so the initial load
// completes normally and the first LoadMore is held in flight.
type pagingRunner struct {
	total    int
	logHits  int32
	pageHits int32
	block    chan struct{}
	hit      chan struct{}
}

func (r *pagingRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *pagingRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name != "git log" {
		return gitexec.Result{}, nil
	}
	atomic.AddInt32(&r.logHits, 1)
	limit, skip := 0, 0
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			limit, _ = strconv.Atoi(argv[i+1])
		}
		if len(a) > 7 && a[:7] == "--skip=" {
			skip, _ = strconv.Atoi(a[7:])
		}
	}
	if skip > 0 && r.block != nil && atomic.AddInt32(&r.pageHits, 1) == 1 {
		close(r.hit)
		<-r.block
	}
	var b []byte
	for i := skip; i < skip+limit && i < r.total; i++ {
		// logFormat = %H%x1f%P%x1f%an%x1f%at%x1f%s ; only Hash matters here.
		b = append(b, []byte("hash"+strconv.Itoa(i)+"\x1f\x1fauthor\x1f0\x1fsubject "+strconv.Itoa(i)+"\n")...)
	}
	return gitexec.Result{Stdout: string(b)}, nil
}

func (r *pagingRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func feedOver(total int) (*CommitFeed, *pagingRunner) {
	pr := &pagingRunner{total: total}
	return New(&git.Repo{Runner: pr}).CommitFeed(), pr
}

func TestLoadInitialFillsPageZero(t *testing.T) {
	f, _ := feedOver(120)
	st, err := f.LoadInitial(context.Background())
	if err != nil {
		t.Fatalf("load initial: %v", err)
	}
	if len(st.Commits) != commitInitialPage {
		t.Fatalf("initial page = %d, want %d", len(st.Commits), commitInitialPage)
	}
	if st.Exhausted {
		t.Fatal("exhausted with 120 commits and a 50 page")
	}
	if st.Commits[0].Hash != "hash0" {
		t.Fatalf("head commit = %q", st.Commits[0].Hash)
	}
}

func TestLoadInitialExhaustedWhenShort(t *testing.T) {
	f, _ := feedOver(30)
	st, _ := f.LoadInitial(context.Background())
	if len(st.Commits) != 30 || !st.Exhausted {
		t.Fatalf("short history: got %d exhausted=%v, want 30 exhausted", len(st.Commits), st.Exhausted)
	}
}

func TestLoadMoreAppendsAndExhausts(t *testing.T) {
	f, _ := feedOver(commitInitialPage + commitPageSize - 10)
	f.LoadInitial(context.Background())
	st, loaded, err := f.LoadMore(context.Background())
	if err != nil || !loaded {
		t.Fatalf("load more: loaded=%v err=%v", loaded, err)
	}
	if len(st.Commits) != commitInitialPage+commitPageSize-10 {
		t.Fatalf("after page: %d commits", len(st.Commits))
	}
	if !st.Exhausted {
		t.Fatal("a short second page must exhaust")
	}
	_, loaded2, _ := f.LoadMore(context.Background())
	if loaded2 {
		t.Fatal("LoadMore after exhaustion should be a no-op")
	}
}

func TestLoadMoreDedupesByHash(t *testing.T) {
	f, _ := feedOver(commitInitialPage + 5)
	f.LoadInitial(context.Background())
	st, _, _ := f.LoadMore(context.Background())
	seen := map[string]bool{}
	for _, c := range st.Commits {
		if seen[c.Hash] {
			t.Fatalf("duplicate hash %q after paging", c.Hash)
		}
		seen[c.Hash] = true
	}
	if len(st.Commits) != commitInitialPage+5 {
		t.Fatalf("got %d commits, want %d", len(st.Commits), commitInitialPage+5)
	}
}

func TestNeedsMore(t *testing.T) {
	f, _ := feedOver(500)
	f.LoadInitial(context.Background())
	if f.NeedsMore(0) {
		t.Fatal("sel at head should not need more")
	}
	if !f.NeedsMore(commitInitialPage - 1) {
		t.Fatal("sel at the last loaded row should need more")
	}
	if !f.NeedsMore(commitInitialPage - commitNearEnd) {
		t.Fatal("sel within threshold of the end should need more")
	}
	g, _ := feedOver(30)
	g.LoadInitial(context.Background())
	if g.NeedsMore(29) {
		t.Fatal("exhausted feed never needs more")
	}
}

func TestLoadMoreSingleFlight(t *testing.T) {
	// The initial load completes normally (no --skip); the first page read
	// (--skip>0) parks inside the runner, so the first LoadMore is held in
	// flight while we prove a second LoadMore no-ops instead of issuing its
	// own read. Deterministic: nothing races on whether the two overlap.
	pr := &pagingRunner{total: 500, block: make(chan struct{}), hit: make(chan struct{})}
	f := New(&git.Repo{Runner: pr}).CommitFeed()
	f.LoadInitial(context.Background())

	go func() { f.LoadMore(context.Background()) }()
	<-pr.hit // the first page read is parked; the feed's inFlight is set

	if _, loaded, _ := f.LoadMore(context.Background()); loaded {
		t.Fatal("a second LoadMore while one is in flight must no-op")
	}
	if got := atomic.LoadInt32(&pr.pageHits); got != 1 {
		t.Fatalf("page reads issued = %d, want 1 (the second LoadMore must not read)", got)
	}
	close(pr.block) // release the parked first page so the goroutine can finish
}

func TestSnapshotReturnsCopy(t *testing.T) {
	f, _ := feedOver(120)
	st, _ := f.LoadInitial(context.Background())
	st.Commits[0] = model.Commit{Hash: "tampered"}
	if f.Snapshot().Commits[0].Hash == "tampered" {
		t.Fatal("Snapshot must return a copy, not the feed's backing slice")
	}
}

func TestLogPageRuns(t *testing.T) {
	svc := New(&git.Repo{Runner: &pagingRunner{total: 10}})
	if _, err := svc.logPage(context.Background(), 50, 0); err != nil {
		t.Fatalf("logPage: %v", err)
	}
}
