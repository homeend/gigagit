package tui

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// pagingHistoryRunner serves `git log` from a mutable newest-first hash list,
// honoring -n <limit> and --skip=<n>, so a test can page in depth and then let
// history grow under it.
type pagingHistoryRunner struct {
	mu     sync.Mutex
	hashes []string
}

func newPagingHistoryRunner(n int) *pagingHistoryRunner {
	hs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		hs = append(hs, "c"+strconv.Itoa(i))
	}
	return &pagingHistoryRunner{hashes: hs}
}

func (r *pagingHistoryRunner) prepend(hashes ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hashes = append(append([]string(nil), hashes...), r.hashes...)
}

func (r *pagingHistoryRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

func (r *pagingHistoryRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name != "git log" {
		return gitexec.Result{}, nil
	}
	limit, skip := 0, 0
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			limit, _ = strconv.Atoi(argv[i+1])
		}
		if strings.HasPrefix(a, "--skip=") {
			skip, _ = strconv.Atoi(strings.TrimPrefix(a, "--skip="))
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	var b strings.Builder
	for i := skip; i < skip+limit && i < len(r.hashes); i++ {
		b.WriteString(r.hashes[i] + "\x1f\x1fauthor\x1f0\x1fsubject\x1f\n")
	}
	return gitexec.Result{Stdout: b.String()}, nil
}

func (r *pagingHistoryRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	return r.Run(ctx, name, argv)
}

// deepFeedModel returns a Model whose feed has page 0 plus one further page
// loaded (15 commits) over a 300-commit history.
func deepFeedModel(t *testing.T) (Model, *pagingHistoryRunner) {
	t.Helper()
	hr := newPagingHistoryRunner(300)
	m := New(domain.New(&git.Repo{Runner: hr}))
	m.feed.SetPageSizes(5, 10)
	if _, err := m.feed.LoadInitial(context.Background()); err != nil {
		t.Fatalf("load initial: %v", err)
	}
	if _, loaded, err := m.feed.LoadMore(context.Background()); err != nil || !loaded {
		t.Fatalf("load more: loaded=%v err=%v", loaded, err)
	}
	return m, hr
}

func readFeed(t *testing.T, m Model, opts reloadOpts) feedPayload {
	t.Helper()
	msg, ok := m.readSourceCmd(context.Background(), srcFeed, opts)().(dataAvailableMsg)
	if !ok {
		t.Fatal("srcFeed read did not produce a dataAvailableMsg")
	}
	if msg.err != nil {
		t.Fatalf("srcFeed read: %v", msg.err)
	}
	return msg.value.(feedPayload)
}

// An automatic refresh (background lane, file watch, post-op) must keep every
// page the user paged in — otherwise a deep ctrl+f search is wiped every tick.
func TestAutomaticFeedRefreshKeepsPagedHistory(t *testing.T) {
	m, hr := deepFeedModel(t)
	hr.prepend("new0")
	p := readFeed(t, m, reloadOpts{})
	if len(p.commits) != 16 {
		t.Fatalf("automatic refresh = %d commits, want 16 (15 kept + 1 new)", len(p.commits))
	}
	if p.commits[0].Hash != "new0" || p.commits[15].Hash != "c14" {
		t.Fatalf("head/tail after refresh: %q … %q", p.commits[0].Hash, p.commits[15].Hash)
	}
}

// A hard reload (manual r, sort/page-size change) still starts clean — the
// user's escape hatch from a stale deep tail.
func TestHardFeedReloadStartsFromPageZero(t *testing.T) {
	m, hr := deepFeedModel(t)
	hr.prepend("new0")
	p := readFeed(t, m, reloadOpts{manual: true, hardFeed: true})
	if len(p.commits) != 5 {
		t.Fatalf("hard reload = %d commits, want the 5-commit page 0", len(p.commits))
	}
	if p.commits[0].Hash != "new0" {
		t.Fatalf("hard reload head = %q, want new0", p.commits[0].Hash)
	}
}
