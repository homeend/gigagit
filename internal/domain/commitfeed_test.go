package domain

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
		// logFormat = %H%x1f%P%x1f%an%x1f%at%x1f%s%x1f%D ; only Hash matters here
		// (trailing empty %D field keeps the 6-field count).
		b = append(b, []byte("hash"+strconv.Itoa(i)+"\x1f\x1fauthor\x1f0\x1fsubject "+strconv.Itoa(i)+"\x1f\n")...)
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
	if _, err := svc.logPage(context.Background(), 50, 0, LogScope{}, 0); err != nil {
		t.Fatalf("logPage: %v", err)
	}
}

// TestReloadDoesNotCoalesceOntoCancelled reproduces the cancellation×singleflight
// bug: re-toggling to a scope whose just-cancelled load is still in flight must
// NOT coalesce onto that load's context.Canceled result and blank the panel. The
// per-load gen in the singleflight key prevents it.
func TestReloadDoesNotCoalesceOntoCancelled(t *testing.T) {
	f := gitexec.NewFakeRunner()
	var calls int32
	f.SetHandler("git log", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		if atomic.AddInt32(&calls, 1) == 1 {
			<-ctx.Done() // first (soon-superseded) load: dies when cancelled
			return gitexec.Result{}, ctx.Err()
		}
		return gitexec.Result{Stdout: "h2\x1f\x1fA\x1f0\x1fs\x1f\n"}, nil // later loads succeed
	})
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetScope(LogScope{Branches: []string{"feat"}})

	go func() { _, _ = feed.LoadInitial(context.Background()) }() // A: parks in handler call #1
	for atomic.LoadInt32(&calls) < 1 {
		time.Sleep(time.Millisecond)
	}
	// C: same scope reload — cancels A and (with the gen in the key) runs its OWN
	// handler call rather than coalescing onto A's cancelled one.
	st, _ := feed.LoadInitial(context.Background())
	if len(st.Commits) != 1 {
		t.Fatalf("reload over a cancelled in-flight load returned %d commits (coalesced onto cancelled?)", len(st.Commits))
	}
}

func TestFeedScopeChangesRefspec(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	feed := New(&git.Repo{Runner: f}).CommitFeed()
	feed.SetScope(LogScope{Branches: []string{"feat"}})
	_, _ = feed.LoadInitial(context.Background())
	joined := ""
	for _, c := range f.Calls {
		if c.Name == "git log" {
			joined = strings.Join(c.Argv, " ")
		}
	}
	if !strings.Contains(joined, "feat") || strings.Contains(joined, "--branches") {
		t.Fatalf("solo scope should walk 'feat', not --branches: %q", joined)
	}
}

func TestFeedSupersedeCancelsAndStampsGen(t *testing.T) {
	f := gitexec.NewFakeRunner()
	started := make(chan context.Context, 1)
	release := make(chan struct{})
	f.SetHandler("git log", func(ctx context.Context, argv []string) (gitexec.Result, error) {
		started <- ctx
		<-release
		return gitexec.Result{}, ctx.Err()
	})
	feed := New(&git.Repo{Runner: f}).CommitFeed()

	firstDone := make(chan FeedState, 1)
	go func() { st, _ := feed.LoadInitial(context.Background()); firstDone <- st }()
	ctx1 := <-started // first load parked in the handler

	// A second LoadInitial supersedes the first; its ctx must be cancelled.
	go func() { _, _ = feed.LoadInitial(context.Background()) }()
	select {
	case <-ctx1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("prior load ctx was not cancelled on supersede")
	}
	close(release) // let the parked handlers return

	st := <-firstDone // the superseded load's state must carry the OLDER gen (1)
	if st.Gen != 1 {
		t.Fatalf("superseded load should stamp gen0=1, got %d", st.Gen)
	}
}
