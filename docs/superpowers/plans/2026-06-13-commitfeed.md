# commitfeed: On-Demand Paged Commit History (CQRS Stage 3) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the Commits panel load history on demand via a stateful domain `CommitFeed` (the single source of truth for commits), removing the 50-commit cap.

**Architecture:** `git.Log` gains a `skip` offset. `domain.CommitFeed` accumulates pages (newest-first) through the gate + singleflight; `Snapshot` stops reading commits. The TUI holds `*domain.CommitFeed`, fires `Snapshot` + `feed.LoadInitial` concurrently at startup, and on selection nearing the end fires `feed.LoadMore` then mirrors `feed.Snapshot()` into its render slice. UI signals intent and subscribes; the domain owns the data.

**Tech Stack:** Go 1.26, stdlib `sync`/`context`. Spec: `docs/superpowers/specs/2026-06-13-commitfeed-design.md` and diagrams `docs/superpowers/specs/2026-06-13-commitfeed-diagrams.md` — read them first. Builds on stages 1–2 (`internal/repogate`, `internal/domain` with `Service`/`Snapshot`/`query`/`flightGroup`, all on `main`).

**Branch:** `feat/commitfeed` off `main`, developed in a worktree at `/mnt/t/others/gigagit.worktrees/commitfeed`.

**Conventions:** tests first (TDD); `gofmt -w`; comments state constraints not narration; commits end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: git.Log gains a skip offset

**Files:**
- Modify: `internal/git/log.go` (`Log`)
- Modify: `internal/git/log_test.go` (the existing `Log` caller)

- [ ] **Step 1: Update the failing test**

In `internal/git/log_test.go`, the existing call `repo.Log(context.Background(), 10)` becomes `repo.Log(context.Background(), 10, 0)`. Then ADD a skip test (append to the file):

```go
func TestLogSkipArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git log", gitexec.Result{Stdout: ""})
	repo := &git.Repo{Runner: f}

	if _, err := repo.Log(context.Background(), 200, 50); err != nil {
		t.Fatalf("log: %v", err)
	}
	var argv []string
	for _, c := range f.Calls {
		if c.Name == "git log" {
			argv = c.Argv
		}
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--skip=50") {
		t.Fatalf("skip>0 should add --skip=50, got: %v", argv)
	}

	f.Calls = nil
	if _, err := repo.Log(context.Background(), 50, 0); err != nil {
		t.Fatalf("log: %v", err)
	}
	for _, c := range f.Calls {
		if c.Name == "git log" {
			if strings.Contains(strings.Join(c.Argv, " "), "--skip") {
				t.Fatalf("skip==0 must omit --skip, got: %v", c.Argv)
			}
		}
	}
}
```

Ensure `log_test.go` imports `"strings"`, `"github.com/gigagit/gg/internal/gitexec"`, and the `git` package as used (check the existing import style — the test may be `package git` internal or `package git_test`; match it. If internal `package git`, call `Log` unqualified and use `&Repo{}` instead of `&git.Repo{}`).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run 'TestLog' 2>&1 | head`
Expected: compile error — `Log` takes 2 args, test passes 3.

- [ ] **Step 3: Implement**

In `internal/git/log.go`, change `Log` to:

```go
// Log returns up to limit commits reachable from HEAD, newest first, skipping
// the first skip commits. skip=0 is the head of history (omits --skip).
func (r *Repo) Log(ctx context.Context, limit, skip int) ([]model.Commit, error) {
	argv := gitcmd.New("log").
		Arg("-n", strconv.Itoa(limit), "--format="+logFormat).
		ArgIf(skip > 0, "--skip="+strconv.Itoa(skip)).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git log", argv)
	if err != nil {
		return nil, err
	}
	return ParseLog([]byte(res.Stdout))
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/git/` — PASS. The build will be broken in `internal/domain` (the `Snapshot` caller still passes 2 args) — that is expected and fixed in Task 3; do NOT touch domain here. Confirm only `go test ./internal/git/` passes and `go vet ./internal/git/` is clean.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git && git add internal/git && git commit -m "feat(git): Log gains a skip offset for paging

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

(The domain build break is repaired in Task 3; the per-task green-build rule is satisfied at the package under test, and the next two tasks restore the whole build. If you prefer a green whole-build at every commit, also apply Task 3's one-line `s.repo.Log(ctx, 50)` → `s.repo.Log(ctx, 50, 0)` change here — but Task 3 removes that call entirely, so leaving it broken-then-removed is fine.)

To keep `go build ./...` green after this commit, make the trivial call-site fix now: in `internal/domain/query.go`, change `s.repo.Log(ctx, 50)` to `s.repo.Log(ctx, 50, 0)` (Task 3 deletes this line). Include it in this commit.

---

### Task 2: domain.CommitFeed + logPage + Service.CommitFeed

**Files:**
- Create: `internal/domain/commitfeed.go`
- Create: `internal/domain/commitfeed_test.go`
- Modify: `internal/domain/query.go` (add `logPage`)

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/commitfeed_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// commitFake returns a FakeRunner whose `git log` yields `total` synthetic
// commits, honoring -n and --skip so paging is realistic.
func commitFake(total int) *gitexec.FakeRunner {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: "/repo/.git\n"})
	f.logTotal = total // see note below
	return f
}
```

The FakeRunner can't synthesize per-argv output, so instead drive the feed with a small custom runner. Replace the helper above with a dedicated runner in the test file:

```go
// pagingRunner serves `git log` from a fixed list of `total` commits,
// honoring -n <limit> and --skip=<n> parsed from argv. Other span names
// return empty. Safe for concurrent use.
type pagingRunner struct {
	mu      sync.Mutex
	total   int
	logHits int32
	block   chan struct{} // if non-nil, the first git log blocks on it
	hit     chan struct{}
}

func (r *pagingRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	if name != "git log" {
		return gitexec.Result{}, nil
	}
	if atomic.AddInt32(&r.logHits, 1) == 1 && r.block != nil {
		close(r.hit)
		<-r.block
	}
	limit, skip := 0, 0
	for i, a := range argv {
		if a == "-n" && i+1 < len(argv) {
			limit, _ = strconv.Atoi(argv[i+1])
		}
		if len(a) > 7 && a[:7] == "--skip=" {
			skip, _ = strconv.Atoi(a[7:])
		}
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
	f, _ := feedOver(30) // fewer than commitInitialPage
	st, _ := f.LoadInitial(context.Background())
	if len(st.Commits) != 30 || !st.Exhausted {
		t.Fatalf("short history: got %d exhausted=%v, want 30 exhausted", len(st.Commits), st.Exhausted)
	}
}

func TestLoadMoreAppendsAndExhausts(t *testing.T) {
	f, _ := feedOver(commitInitialPage + commitPageSize - 10) // 50 + 190
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
	// A second LoadMore after exhaustion is a no-op.
	_, loaded2, _ := f.LoadMore(context.Background())
	if loaded2 {
		t.Fatal("LoadMore after exhaustion should be a no-op")
	}
}

func TestLoadMoreDedupesByHash(t *testing.T) {
	// total just over a page so the second page's --skip window still returns
	// fresh commits; then assert no duplicate hashes accumulate.
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
	f.LoadInitial(context.Background()) // 50 loaded, not exhausted
	if f.NeedsMore(0) {
		t.Fatal("sel at head should not need more")
	}
	if !f.NeedsMore(commitInitialPage - 1) {
		t.Fatal("sel at the last loaded row should need more")
	}
	if !f.NeedsMore(commitInitialPage - commitNearEnd) {
		t.Fatal("sel within threshold of the end should need more")
	}
	// Exhaust it, then NeedsMore is always false.
	g, _ := feedOver(30)
	g.LoadInitial(context.Background())
	if g.NeedsMore(29) {
		t.Fatal("exhausted feed never needs more")
	}
}

func TestLoadMoreSingleFlight(t *testing.T) {
	pr := &pagingRunner{total: 500, block: make(chan struct{}), hit: make(chan struct{})}
	f := New(&git.Repo{Runner: pr}).CommitFeed()
	// LoadInitial uses the first git log; let it through by closing block after
	// the initial hit.
	go func() { <-pr.hit; close(pr.block) }()
	f.LoadInitial(context.Background())

	// Now two concurrent LoadMore: the feed's inFlight guard must yield exactly
	// one additional git log.
	before := atomic.LoadInt32(&pr.logHits)
	var wg sync.WaitGroup
	results := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, loaded, _ := f.LoadMore(context.Background()); results <- loaded }()
	}
	wg.Wait()
	close(results)
	loadedCount := 0
	for r := range results {
		if r {
			loadedCount++
		}
	}
	after := atomic.LoadInt32(&pr.logHits)
	if after-before > 1 {
		t.Fatalf("two concurrent LoadMore issued %d git logs, want at most 1", after-before)
	}
	_ = loadedCount
}

func TestSnapshotReturnsCopy(t *testing.T) {
	f, _ := feedOver(120)
	st, _ := f.LoadInitial(context.Background())
	st.Commits[0] = model.Commit{Hash: "tampered"}
	if f.Snapshot().Commits[0].Hash == "tampered" {
		t.Fatal("Snapshot must return a copy, not the feed's backing slice")
	}
}

func TestLogPageGated(t *testing.T) {
	pr := &pagingRunner{total: 10}
	svc := New(&git.Repo{Runner: pr})
	if _, err := svc.logPage(context.Background(), 50, 0); err != nil {
		t.Fatalf("logPage: %v", err)
	}
	// (Reservation hold/release is covered by stage-2 query tests; here we just
	// confirm logPage runs through query without error.)
}

var _ = errors.New
var _ = time.Sleep
```

(Drop the unused `commitFake` helper — the real driver is `pagingRunner`/`feedOver`. Remove the `var _ =` lines if `errors`/`time` end up used or unused; keep the file compiling.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestLoad|TestNeedsMore|TestSnapshotReturnsCopy|TestLogPage' 2>&1 | head`
Expected: compile errors (`CommitFeed`, `commitInitialPage`, `logPage` undefined).

- [ ] **Step 3: Implement**

Add `logPage` to `internal/domain/query.go` (next to `Snapshot`):

```go
// logPage is the gated, singleflighted commit-page read the CommitFeed uses.
// The singleflight key includes skip so different pages don't collapse.
func (s *Service) logPage(ctx context.Context, limit, skip int) ([]model.Commit, error) {
	key := "commits:" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.Log(ctx, limit, skip)
	})
}
```

Add `"strconv"` to query.go's imports.

Create `internal/domain/commitfeed.go`:

```go
package domain

import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
)

// Page sizes (commits). Initial paint stays cheap; later pages are larger
// since the user has signaled interest by scrolling.
const (
	commitInitialPage = 50
	commitPageSize    = 200
	commitNearEnd     = 10 // load more when the selection is within this of the end
)

// CommitFeed is the single source of truth for the Commits panel: an
// incrementally loaded, newest-first view of HEAD history. Goroutine-safe;
// Snapshot returns a copy so a frontend can render while a page loads.
type CommitFeed struct {
	svc *Service

	mu        sync.Mutex
	commits   []model.Commit
	hashes    map[string]bool // dedupe set, mirrors commits
	skip      int             // next --skip offset (advances by raw page length)
	exhausted bool
	gen       int  // bumped by LoadInitial; tags pages so stale ones drop
	inFlight  bool // at most one page request outstanding
}

// CommitFeed returns a fresh feed for this Service's repo.
func (s *Service) CommitFeed() *CommitFeed {
	return &CommitFeed{svc: s, hashes: map[string]bool{}}
}

// FeedState is an immutable view handed to the frontend.
type FeedState struct {
	Commits   []model.Commit
	Exhausted bool
	Gen       int
}

// Gen returns the current generation (for a frontend's stale-page check).
func (f *CommitFeed) Gen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gen
}

// snapshotLocked builds a FeedState copy; caller holds f.mu.
func (f *CommitFeed) snapshotLocked() FeedState {
	cp := make([]model.Commit, len(f.commits))
	copy(cp, f.commits)
	return FeedState{Commits: cp, Exhausted: f.exhausted, Gen: f.gen}
}

// Snapshot returns a copy of the current state for rendering.
func (f *CommitFeed) Snapshot() FeedState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshotLocked()
}

// NeedsMore reports whether selection index sel is close enough to the end to
// warrant a page and the feed can serve one. Filter-suppression is the
// caller's concern.
func (f *CommitFeed) NeedsMore(sel int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exhausted || f.inFlight {
		return false
	}
	return sel >= len(f.commits)-commitNearEnd
}

// LoadInitial resets the feed (bumps gen, clears) and loads page 0. It is the
// reload primitive: callers re-fill a feed by calling LoadInitial again.
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	f.gen++
	gen0 := f.gen
	f.commits = nil
	f.hashes = map[string]bool{}
	f.skip = 0
	f.exhausted = false
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.svc.logPage(ctx, commitInitialPage, 0)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if err != nil {
		return f.snapshotLocked(), err
	}
	if f.gen != gen0 { // another LoadInitial raced; drop this page
		return f.snapshotLocked(), nil
	}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip = len(page)
	f.exhausted = len(page) < commitInitialPage
	return f.snapshotLocked(), nil
}

// LoadMore loads the next page when warranted. Returns (state, true) when a
// page was applied; (state, false) for a no-op (exhausted, in-flight, or a
// raced reset). Single-flight via inFlight; the Service query coalesces
// identical concurrent reads.
func (f *CommitFeed) LoadMore(ctx context.Context) (FeedState, bool, error) {
	f.mu.Lock()
	if f.exhausted || f.inFlight {
		st := f.snapshotLocked()
		f.mu.Unlock()
		return st, false, nil
	}
	f.inFlight = true
	gen0 := f.gen
	skip := f.skip
	f.mu.Unlock()

	page, err := f.svc.logPage(ctx, commitPageSize, skip)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if err != nil {
		return f.snapshotLocked(), false, err
	}
	if f.gen != gen0 { // a reload raced; drop the page
		return f.snapshotLocked(), false, nil
	}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip += len(page) // advance by raw page length to stay aligned with git's walk
	f.exhausted = len(page) < commitPageSize
	return f.snapshotLocked(), true, nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/domain/` — all stage-1/2 domain tests plus the new feed tests pass. Then `go vet ./internal/domain/` and `go build ./...` (the whole build should be green now: Task 1's call-site fix kept Snapshot compiling).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain && git add internal/domain && git commit -m "feat(domain): CommitFeed paged commit read-model + logPage

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Snapshot drops commits; TUI loadCmd fires two reads

**Files:**
- Modify: `internal/domain/query.go` (`Snapshot` struct + `loadSnapshot`)
- Modify: `internal/domain/query_test.go` (drop commit assertions from snapshot tests)
- Modify: `internal/tui/model.go` (Model `feed`/`commitsExhausted` fields, `New`, `reRoot`, `dataLoadedMsg` handler)
- Modify: `internal/tui/load.go` (`loadCmd`, `dataLoadedMsg`)

- [ ] **Step 1: Update the failing tests**

In `internal/domain/query_test.go`: remove `Commits` from any `Snapshot` assertions. Specifically, in `TestSnapshotFansOutAllReads` delete the `git log` response from `fakeReads` is NOT required (an extra configured response is harmless), but DELETE any assertion that reads `snap.Commits` (there is none in the stage-2 tests that must stay — verify; if `TestSnapshotCoalesces` keys off `git status` it is unaffected). Add an assertion that Snapshot no longer issues `git log`:

```go
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
```

In `internal/tui/load_test.go` (and any tui test referencing `dataLoadedMsg{... commits:}`), keep compiling — `dataLoadedMsg.commits` still exists. The stale-snapshot test is unaffected.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run TestSnapshotDoesNotReadCommits 2>&1 | head`
Expected: FAIL — Snapshot still issues `git log`.

- [ ] **Step 3: Implement**

In `internal/domain/query.go`:

(a) Remove the `Commits` field from the `Snapshot` struct:

```go
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	Worktrees       []model.Worktree
	CurrentWorktree string
	GitCommonDir    string
	HeadTimes       map[string]int64
}
```

(b) In `loadSnapshot`, DELETE the entire `run(func() { cs, err := s.repo.Log(ctx, 50, 0) ... })` goroutine (the commits read). Leave the other five `run(...)` blocks (Status, Branches, Worktrees+CommitTimes, TopLevel, GitCommonDir) unchanged. Remove the now-unused `logPage`? No — `logPage` stays (the feed uses it). Just remove the Log read from the fan-out.

In `internal/tui/load.go`:

(a) `dataLoadedMsg` gains `commitsExhausted bool` and `commitErr error` (keep the existing `commits` field):

```go
type dataLoadedMsg struct {
	gen              int
	status           model.WorkingTreeStatus
	branches         []model.Branch
	commits          []model.Commit
	commitsExhausted bool
	commitErr        error
	worktrees        []model.Worktree
	currentWorktree  string
	cfg              config.Config
	gitCommonDir     string
	headTimes        map[string]int64
	err              error
}
```

(b) `loadCmd` fires Snapshot and `feed.LoadInitial` concurrently:

```go
func (m Model) loadCmd() tea.Cmd {
	svc := m.svc
	feed := m.feed
	statePath := m.statePath
	gen := m.loadGen
	return func() tea.Msg {
		ctx := context.Background()
		var (
			snap     domainSnapshot
			snapErr  error
			fs       domainFeedState
			feedErr  error
			wg       sync.WaitGroup
		)
		wg.Add(2)
		go func() { defer wg.Done(); snap, snapErr = svc.Snapshot(ctx) }()
		go func() { defer wg.Done(); fs, feedErr = feed.LoadInitial(ctx) }()
		wg.Wait()
		if snapErr != nil {
			return dataLoadedMsg{gen: gen, err: snapErr}
		}
		out := dataLoadedMsg{
			gen:              gen,
			status:           snap.Status,
			branches:         snap.Branches,
			worktrees:        snap.Worktrees,
			currentWorktree:  snap.CurrentWorktree,
			gitCommonDir:     snap.GitCommonDir,
			headTimes:        snap.HeadTimes,
			commits:          fs.Commits,
			commitsExhausted: fs.Exhausted,
			commitErr:        feedErr,
			cfg:              config.Defaults(),
		}
		if snap.CurrentWorktree != "" {
			_ = repos.Touch(statePath, snap.CurrentWorktree, time.Now())
			if cfg, cfgErr := config.Load(config.DefaultGlobalPath(), filepath.Join(snap.CurrentWorktree, ".gg.toml")); cfgErr == nil {
				out.cfg = cfg
			}
		}
		return out
	}
}
```

Use the real type names: `domain.Snapshot` and `domain.FeedState` (the aliases `domainSnapshot`/`domainFeedState` above are placeholders — write `domain.Snapshot` / `domain.FeedState`). Add `"sync"` and `"github.com/gigagit/gg/internal/domain"` to load.go's imports.

In `internal/tui/model.go`:

(a) Add fields next to `feed`-adjacent state (near `commits`):

```go
	feed             *domain.CommitFeed // single source of truth for commits
	commitsExhausted bool               // false → "Commits N+", true → "Commits N"
```

(b) In `New(repo *git.Repo)`, after `svc := domain.New(repo)` (the stage-1 wiring), set the feed:

```go
	m.feed = svc.CommitFeed()
```

(adapt to the exact constructor shape; the requirement is `m.feed = m.svc.CommitFeed()`).

(c) In `reRoot`, after `m.svc = domain.Open(path)` / `m.repo = m.svc.Repo()`, add:

```go
	m.feed = m.svc.CommitFeed()
```

(d) In the `dataLoadedMsg` handler, after `m.commits = msg.commits` add:

```go
			m.commitsExhausted = msg.commitsExhausted
			if msg.commitErr != nil {
				m.statusMsg = "commits: " + msg.commitErr.Error()
			}
```

Ensure `model.go` imports `"github.com/gigagit/gg/internal/domain"` (stage 1 already added it).

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/domain/ ./internal/tui/` then `go build ./... && go vet ./internal/domain/ ./internal/tui/`. All green.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain internal/tui && git add internal/domain internal/tui && git commit -m "refactor(tui,domain): CommitFeed owns commits; Snapshot drops them; loadCmd fires both

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: Paging on scroll + Commits count label

**Files:**
- Modify: `internal/tui/load.go` (`loadMoreCmd`, `commitsPagedMsg`)
- Modify: `internal/tui/model.go` (movement arms, `commitsPagedMsg` handler, `maybeLoadMoreCommits` helper)
- Modify: `internal/tui/files_view.go` (`moveCommitUnderFilesView`)
- Modify: `internal/tui/viewstate.go` (`panelLabel`)
- Test: `internal/tui/commitfeed_test.go` (new)

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/commitfeed_test.go`:

```go
package tui

import (
	"strconv"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

func feedModel(n int, exhausted bool) Model {
	m := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.focus = panelCommits
	cs := make([]model.Commit, n)
	for i := range cs {
		cs[i] = model.Commit{Hash: "h" + strconv.Itoa(i), Subject: "s"}
	}
	m.commits = cs
	m.commitsExhausted = exhausted
	m.sel = map[panel]int{panelCommits: 0}
	return m
}

func TestCommitsLabel(t *testing.T) {
	if got := feedModel(37, true).panelLabel(panelCommits, "Commits"); !strings.Contains(got, "37") || strings.Contains(got, "+") {
		t.Fatalf("exhausted label = %q, want plain 37", got)
	}
	if got := feedModel(250, false).panelLabel(panelCommits, "Commits"); !strings.Contains(got, "250+") {
		t.Fatalf("non-exhausted label = %q, want 250+", got)
	}
}

func TestPagingFiresNearEnd(t *testing.T) {
	m := feedModel(50, false) // feed is empty (New), but NeedsMore reads the FEED
	// The feed from New() has 0 commits and is not exhausted, so NeedsMore(sel)
	// with sel>=−10 is true. Move to the last row and press down.
	m.sel[panelCommits] = 49
	_, cmd := m.Update(keyMsg("down"))
	if cmd == nil {
		t.Fatal("scrolling near the end should fire a load-more cmd")
	}
}

func TestPagingSuppressedWhileFiltering(t *testing.T) {
	m := feedModel(50, false)
	// filterActive(p) == (p == m.filterPanel && m.filterQuery != ""), so these
	// two fields make the commits filter active.
	m.filterPanel = panelCommits
	m.filterQuery = "x"
	m.sel[panelCommits] = 49
	_, cmd := m.Update(keyMsg("down"))
	if cmd != nil {
		t.Fatal("an active commits filter must suppress auto-paging")
	}
}
```

Note `TestPagingFiresNearEnd` depends on the feed (from `New`) having 0 commits and not exhausted → `NeedsMore(49)` true. That is the correct intent test (sel near/at end of a non-exhausted feed). If `New`'s feed needs priming, prime it: this test only needs `m.feed.NeedsMore` to return true, which it does for an empty non-exhausted feed at any sel>=−10. Good.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestCommitsLabel|TestPaging' 2>&1 | head`
Expected: `panelLabel` shows no count (label test fails); paging cmd is nil (no wiring yet).

- [ ] **Step 3: Implement**

In `internal/tui/viewstate.go`, extend `panelLabel` — add the commits count BEFORE the sort/filter suffixes:

```go
func (m Model) panelLabel(p panel, base string) string {
	if p == panelCommits {
		n := len(m.commits)
		if m.commitsExhausted {
			base += " " + strconv.Itoa(n)
		} else {
			base += " " + strconv.Itoa(n) + "+"
		}
	}
	if s := m.sortModes[p].String(); s != "" {
		base += " ·" + s
	}
	if m.filterTyping && p == m.filterPanel {
		base += " /" + m.filterQuery + "█"
	} else if m.filterActive(p) {
		base += " /" + m.filterQuery
	}
	return base
}
```

Add `"strconv"` to viewstate.go's imports if absent.

In `internal/tui/load.go`, add the page cmd and message:

```go
// commitsPagedMsg signals a commit page load completed; gen ties it to the
// feed generation that issued it so a reload mid-page is dropped.
type commitsPagedMsg struct{ gen int }

// loadMoreCmd loads the next commit page off the UI thread.
func (m Model) loadMoreCmd() tea.Cmd {
	feed := m.feed
	gen := feed.Gen()
	return func() tea.Msg {
		_, _, _ = feed.LoadMore(context.Background())
		return commitsPagedMsg{gen: gen}
	}
}
```

In `internal/tui/model.go`:

(a) Add the helper (near the other small Model helpers):

```go
// maybeLoadMoreCommits returns a cmd to page in more commits when the Commits
// selection nears the end and no commits filter is active; nil otherwise. The
// feed owns the "is there more / am I already loading" decision.
func (m Model) maybeLoadMoreCommits() tea.Cmd {
	if m.feed == nil {
		return nil
	}
	if m.filterTyping && m.filterPanel == panelCommits {
		return nil
	}
	if m.filterActive(panelCommits) {
		return nil
	}
	if !m.feed.NeedsMore(m.sel[panelCommits]) {
		return nil
	}
	return m.loadMoreCmd()
}
```

(b) Wire it into the three commit-moving arms. The `down`/`j` arm becomes:

```go
		case "down", "j":
			if m.sel[m.focus] < m.panelLen(m.focus)-1 {
				m.sel[m.focus]++
			}
			if m.focus == panelCommits {
				if cmd := m.maybeLoadMoreCommits(); cmd != nil {
					return m, cmd
				}
			}
```

The `pgdown` arm — after its clamp block, add the same `if m.focus == panelCommits { ... }` tail before falling through.

(c) Add the `commitsPagedMsg` handler (in the `Update` type switch, alongside `dataLoadedMsg`):

```go
	case commitsPagedMsg:
		if m.feed != nil && msg.gen == m.feed.Gen() {
			st := m.feed.Snapshot()
			m.commits = st.Commits
			m.commitsExhausted = st.Exhausted
		}
		return m, nil
```

In `internal/tui/files_view.go`, `moveCommitUnderFilesView` — compose the existing files-load cmd with a possible page cmd. Replace its tail:

```go
	m.filesHash = m.commits[bi].Hash
	filesCmd := m.loadCommitFilesCmd(m.commits[bi])
	if more := m.maybeLoadMoreCommits(); more != nil {
		return m, tea.Batch(filesCmd, more)
	}
	return m, filesCmd
```

and where it currently early-returns `return m, nil` after moving without a hash change (the `!ok || same-hash` branch), also offer paging:

```go
	bi, ok := m.backingIndex(panelCommits)
	if !ok || m.commits[bi].Hash == m.filesHash {
		return m, m.maybeLoadMoreCommits() // nil when not needed
	}
```

(`maybeLoadMoreCommits` is defined on `Model` in model.go; files_view.go is the same package. Ensure `tea` is imported in files_view.go — it already is.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/tui/` then `go vet ./internal/tui/ && go build ./...`. All green. Fix `TestPagingSuppressedWhileFiltering` to match the real `filterActive` predicate as noted.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui && git add internal/tui && git commit -m "feat(tui): page commits on scroll; Commits N+ count label

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md`
- Modify: `internal/tui/help.go`

- [ ] **Step 1: CHANGELOG**

Append to the `#### Domain layer & repo gate` subsection (after the stage-2 bullet):

```markdown
- Stage 3: the Commits panel is backed by a domain `CommitFeed` read-model and
  loads history **on demand** — no more 50-commit cap. Scrolling toward the
  end pages in more (50 initial, 200 per page) through the gate; the panel
  label shows `Commits N+` until history is exhausted, then `Commits N`. The
  feed is the single source of truth for commits (`Snapshot` no longer reads
  them); `git log` gained a `--skip` offset.
```

- [ ] **Step 2: CLAUDE.md**

(a) Update the `domain` package-map row to mention the feed:

```markdown
| `domain`     | Frontend-facing command + query layer: `Execute` runs an operation under its repo-gate reservation; `Snapshot`/`Status`/`Worktrees` run reads under a Read reservation, parallel and singleflight-coalesced; `CommitFeed` is the paged commit-history read-model backing the Commits panel. Emits the op span. |
```

(b) No conventions change needed beyond what stage 2 added.

- [ ] **Step 3: help.go**

In the Commits-panel section of `internal/tui/help.go`, add a row noting automatic paging (no new key):

```go
		r("(scroll)", "more commits load automatically as you near the end"),
```

(Match the existing `r(...)` helper signature in help.go; the Commits section is where `l`/movement rows live. This is documentation only — the Commits panel draws from `help.go`'s table; it is NOT a footer binding, so `TestHelpFooterCoverage` is unaffected because `(scroll)` is not a `[x]`-style key. If the coverage guard parses this row, use a label that the guard ignores — verify the test still passes in Step 4.)

- [ ] **Step 4: Full staged gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e green, ending `all green`. If `TestHelpFooterCoverage` complains about the `(scroll)` row, change it to a plain doc line that the guard doesn't treat as a binding (or place it as section prose), then re-run.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md CLAUDE.md internal/tui && git commit -m "docs: on-demand paged commit history (CQRS stage 3)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Plan self-review notes

- **Spec coverage:** Log skip (Task 1); CommitFeed + logPage + constants + LoadInitial/LoadMore/NeedsMore/Snapshot/Gen + single-flight + dedupe + gen-drop (Task 2); Snapshot drops commits + two-read loadCmd + Model wiring + handler (Task 3); paging on scroll incl. files-view + filter suppression + label (Task 4); docs (Task 5). Spec non-goals (cache, explicit key, config sizes, anchored paging, cancel-in-flight) → no tasks.
- **Type consistency:** `domain.Snapshot` (no Commits) ↔ loadCmd; `domain.FeedState{Commits,Exhausted,Gen}` ↔ handler/paged-msg; `CommitFeed.LoadInitial/LoadMore/NeedsMore/Snapshot/Gen` signatures consistent Task 2 ↔ Tasks 3/4; `commitsPagedMsg{gen int}` defined Task 4 load.go, handled Task 4 model.go; constants `commitInitialPage/commitPageSize/commitNearEnd` defined Task 2, used in tests.
- **Ordering / green build:** Task 1 includes the one-line `Log(ctx,50,0)` call-site fix so `go build ./...` stays green after every commit; Task 3 then deletes that call. Task 2's feed compiles against the skip-Log from Task 1. Tasks 3→4 keep the TUI green.
- **Known test-fitting:** `TestPagingSuppressedWhileFiltering` must be adapted to the real `filterActive` predicate (a method, not a field) — flagged inline. The `itoa` helper false-start is replaced by the clean `strconv` version.
- **Files-view paging:** `moveCommitUnderFilesView` already returns a cmd; composed via `tea.Batch` so follow-live diff loading and paging coexist.
