# Snapshot Query Service (CQRS Stage 2) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Move the TUI's 7-read startup load and the CLI's single reads behind the domain layer, running the snapshot's independent reads in parallel under one Read reservation with singleflight coalescing — collapsing sequential startup latency to the `git status` long pole.

**Architecture:** A hand-rolled per-`Service` singleflight group plus a generic gated-read helper (`query[T]`) let `domain.Service` expose `Snapshot` (7 reads, parallel, one Read reservation), `Status`, and `Worktrees`. The TUI's `loadCmd` calls `Snapshot` and adds a generation counter to drop stale in-flight loads; CLI reads call `domain.New(repo).Status/Worktrees`. `config.Load`/`repos.Touch` stay in the TUI (not git reads).

**Tech Stack:** Go 1.26, stdlib `sync`/`context` only (no new dependencies). Spec: `docs/superpowers/specs/2026-06-13-snapshot-query-design.md` — read it first. Builds on stage 1 (`internal/repogate`, `internal/domain`, already on `main`).

**Branch:** `feat/snapshot-query` off `main`, developed in a worktree at `/mnt/t/others/gigagit.worktrees/snapshot-query`.

**Conventions that bind every task:** tests first (TDD); `gofmt -w` everything you touch; comments state constraints, not narration; commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: Make FakeRunner safe for concurrent use

The Snapshot fan-out (Task 3) calls one Runner from 6+ goroutines. `FakeRunner` appends to `Calls` and reads its maps without locking — a data race. Guard `Run` with a mutex. `Calls` stays exported (tests read it after the run completes, when there is no concurrency).

**Files:**
- Modify: `internal/gitexec/fake.go`
- Test: `internal/gitexec/fake_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Create or append to `internal/gitexec/fake_test.go`:

```go
package gitexec

import (
	"context"
	"sync"
	"testing"
)

// TestFakeRunnerConcurrent: the Snapshot fan-out calls one Runner from many
// goroutines, so Run must be safe for concurrent use (run under -race).
func TestFakeRunnerConcurrent(t *testing.T) {
	f := NewFakeRunner()
	f.SetResponse("git x", Result{Stdout: "ok"})
	const n = 50
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if _, err := f.Run(context.Background(), "git x", nil); err != nil {
				t.Errorf("run: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(f.Calls) != n {
		t.Fatalf("recorded %d calls, want %d", len(f.Calls), n)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test -race ./internal/gitexec/ -run TestFakeRunnerConcurrent`
Expected: FAIL — `DATA RACE` on `f.Calls` (and possibly a short count).

- [ ] **Step 3: Add the mutex**

In `internal/gitexec/fake.go`: add `"sync"` to imports, add a mutex field, and guard `Run`:

```go
// FakeRunner is an in-memory Runner for tests. Run is safe for concurrent
// use (the domain Snapshot fan-out calls one runner from many goroutines).
type FakeRunner struct {
	mu        sync.Mutex
	responses map[string]Result
	errs      map[string]error
	Calls     []FakeCall
}
```

```go
func (f *FakeRunner) Run(_ context.Context, name string, argv []string) (Result, error) {
	f.mu.Lock()
	f.Calls = append(f.Calls, FakeCall{Name: name, Argv: argv})
	err := f.errs[name]
	r, ok := f.responses[name]
	f.mu.Unlock()
	if err != nil {
		return r, err
	}
	if !ok {
		return Result{}, fmt.Errorf("fake: no response configured for %q", name)
	}
	return r, nil
}
```

(`Stream` calls `Run`, so it is covered. `SetResponse`/`SetError` are called before the concurrent phase in every test, so they need no lock — leave them.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/gitexec/`
Expected: PASS (the new test plus all existing gitexec tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/gitexec && git add internal/gitexec && git commit -m "test(gitexec): make FakeRunner safe for concurrent Run

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: Hand-rolled singleflight group

**Files:**
- Create: `internal/domain/flight.go`
- Create: `internal/domain/flight_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/domain/flight_test.go`:

```go
package domain

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestFlightCoalesces: while a leader holds an in-flight call, every other
// caller of the same key shares its result without re-running fn.
func TestFlightCoalesces(t *testing.T) {
	var g flightGroup
	var calls int32
	entered := make(chan struct{})
	release := make(chan struct{})

	leader := func() (any, error) {
		atomic.AddInt32(&calls, 1)
		close(entered)
		<-release
		return 42, nil
	}
	const n = 4
	got := make(chan int, n)
	go func() { v, _ := g.Do("k", leader); got <- v.(int) }()
	<-entered // leader is now inside fn, holding the slot

	for i := 0; i < n-1; i++ {
		go func() {
			v, _ := g.Do("k", func() (any, error) {
				atomic.AddInt32(&calls, 1) // must NOT run for followers
				return -1, nil
			})
			got <- v.(int)
		}()
	}
	// Followers have entered Do and found the held key (the leader will not
	// release the slot until we close release).
	time.Sleep(20 * time.Millisecond)
	close(release)

	for i := 0; i < n; i++ {
		if v := <-got; v != 42 {
			t.Fatalf("caller got %d, want 42 (coalesced)", v)
		}
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("fn ran %d times, want 1", c)
	}
}

// TestFlightReleasesKey: after a call completes the key is freed, so a later
// call runs fn again (no permanent caching).
func TestFlightReleasesKey(t *testing.T) {
	var g flightGroup
	var calls int32
	fn := func() (any, error) { atomic.AddInt32(&calls, 1); return nil, nil }
	g.Do("k", fn)
	g.Do("k", fn)
	if c := atomic.LoadInt32(&calls); c != 2 {
		t.Fatalf("fn ran %d times across two sequential Do calls, want 2", c)
	}
}

// TestFlightPropagatesError: the leader's error reaches every follower.
func TestFlightPropagatesError(t *testing.T) {
	var g flightGroup
	boom := errors.New("boom")
	entered := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	errc := make(chan error, 3)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := g.Do("k", func() (any, error) {
			close(entered)
			<-release
			return nil, boom
		})
		errc <- err
	}()
	<-entered
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, err := g.Do("k", func() (any, error) { return nil, nil }); errc <- err }()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()
	close(errc)
	for err := range errc {
		if !errors.Is(err, boom) {
			t.Fatalf("caller got %v, want boom", err)
		}
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run TestFlight 2>&1 | head`
Expected: compile error (`undefined: flightGroup`).

- [ ] **Step 3: Implement**

Create `internal/domain/flight.go`:

```go
package domain

import "sync"

// flightGroup coalesces concurrent calls sharing a key: the first caller
// (the leader) runs fn; callers that arrive while it is in flight share its
// result without running fn again. Once the leader returns, the key is freed
// — there is no caching across completed calls. The zero value is ready.
//
// The leader's call governs the work; a leader whose context is cancelled
// affects its followers (same semantics as golang.org/x/sync/singleflight).
// That is a non-issue for the single-caller TUI and acceptable for a shared
// MCP service.
type flightGroup struct {
	mu sync.Mutex
	m  map[string]*flightCall
}

type flightCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

// Do runs fn under key, coalescing concurrent callers.
func (g *flightGroup) Do(key string, fn func() (any, error)) (any, error) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*flightCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err
	}
	c := &flightCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	c.val, c.err = fn()
	c.wg.Done()

	g.mu.Lock()
	delete(g.m, key)
	g.mu.Unlock()

	return c.val, c.err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/domain/ -run TestFlight`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain && git add internal/domain && git commit -m "feat(domain): hand-rolled singleflight group

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: Snapshot, Status, Worktrees query methods

**Files:**
- Modify: `internal/domain/service.go` (add the `flight` field to `Service`)
- Create: `internal/domain/query.go`
- Create: `internal/domain/query_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/domain/query_test.go`:

```go
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
	f.SetResponse("git status", gitexec.Result{Stdout: "# branch.head main\n"})
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
	// CommitTimes must have been called with the worktree's HEAD sha.
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
	for _, name := range []string{"git status", "git for-each-ref", "git log", "git worktree list"} {
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

func TestSingleReadsHoldReadReservation(t *testing.T) {
	f := fakeReads()
	svc := New(&git.Repo{Runner: f})
	// Status returns the verb result.
	st, err := svc.Status(context.Background())
	if err != nil || st.Branch != "main" {
		t.Fatalf("Status: %v %+v", err, st)
	}
	// Worktrees returns the verb result.
	wts, err := svc.Worktrees(context.Background())
	if err != nil || len(wts) != 1 {
		t.Fatalf("Worktrees: %v %+v", err, wts)
	}
}

// TestQueryHoldsReadReservation: query acquires a Read reservation for the
// duration of fn (observed mid-call via the gate Queue).
func TestQueryHoldsReadReservation(t *testing.T) {
	f := fakeReads()
	svc := New(&git.Repo{Runner: f})
	var (
		mu      sync.Mutex
		held    []repogate.Entry
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
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain/ -run 'TestSnapshot|TestSingleReads|TestQuery' 2>&1 | head`
Expected: compile error (`undefined: query`, `svc.Snapshot`, `Snapshot` type).

- [ ] **Step 3: Implement**

First, add the `flight` field to `Service` in `internal/domain/service.go` — change the struct to:

```go
// Service couples one repository with its process-wide gate and a
// singleflight group that coalesces concurrent identical reads.
type Service struct {
	repo    *git.Repo
	workdir string // fallback gate key when common-dir resolution fails

	mu   sync.Mutex
	gate *repogate.Gate // resolved lazily on first Execute/query

	flight flightGroup
}
```

(`sync` is already imported in service.go. No other change there.)

Then create `internal/domain/query.go`:

```go
package domain

import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
)

// Snapshot is the git-read half of a TUI load: everything loadCmd needs from
// git, fetched in parallel under one Read reservation. config.Load and
// repos.Touch are deliberately NOT here — they are not git reads and stay in
// the frontend.
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	Commits         []model.Commit
	Worktrees       []model.Worktree
	CurrentWorktree string // git toplevel; "" if TopLevel failed
	GitCommonDir    string // "" if it failed
	HeadTimes       map[string]int64
}

// query runs fn under a Read reservation on s's gate, coalescing concurrent
// calls with the same key. The reservation is outermost-via-singleflight: the
// leader acquires it and runs fn; followers share the result without
// acquiring.
func query[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogateRead, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		return fn(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

// Snapshot fetches the seven startup reads. Status/Branches/Log/Worktrees are
// fatal (the first failure is returned); CommitTimes/TopLevel/GitCommonDir are
// best-effort (failures leave zero values, exactly as loadCmd behaved).
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return query(ctx, s, "snapshot", s.loadSnapshot)
}

func (s *Service) loadSnapshot(ctx context.Context) (Snapshot, error) {
	var (
		snap     Snapshot
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	fatal := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }

	run(func() {
		st, err := s.repo.Status(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Status = st
		mu.Unlock()
	})
	run(func() {
		bs, err := s.repo.Branches(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Branches = bs
		mu.Unlock()
	})
	run(func() {
		cs, err := s.repo.Log(ctx, 50)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Commits = cs
		mu.Unlock()
	})
	run(func() {
		// Worktrees is fatal; CommitTimes (best-effort) depends on its result,
		// so it runs here after Worktrees returns.
		wts, err := s.repo.Worktrees(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Worktrees = wts
		mu.Unlock()
		shas := make([]string, 0, len(wts))
		for _, w := range wts {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		if times, terr := s.repo.CommitTimes(ctx, shas); terr == nil {
			mu.Lock()
			snap.HeadTimes = times
			mu.Unlock()
		}
	})
	run(func() {
		if top, err := s.repo.TopLevel(ctx); err == nil {
			mu.Lock()
			snap.CurrentWorktree = top
			mu.Unlock()
		}
	})
	run(func() {
		if gcd, err := s.repo.GitCommonDir(ctx); err == nil {
			mu.Lock()
			snap.GitCommonDir = gcd
			mu.Unlock()
		}
	})

	wg.Wait()
	if firstErr != nil {
		return Snapshot{}, firstErr
	}
	return snap, nil
}

// Status is a single gated read for the CLI status command.
func (s *Service) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	return query(ctx, s, "status", s.repo.Status)
}

// Worktrees is a single gated read for the CLI worktree commands.
func (s *Service) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	return query(ctx, s, "worktrees", s.repo.Worktrees)
}
```

Note: `repogateRead` is `repogate.Read`. Add `"github.com/gigagit/gg/internal/repogate"` to query.go's imports and use `repogate.Read` (the identifier `repogateRead` above is shorthand for the spec text — write `repogate.Read`). Final import block for query.go:

```go
import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repogate"
)
```

and the acquire line is `s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)`.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/domain/`
Expected: PASS (all stage-1 domain tests, the flight tests, and the new query/snapshot tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain && git add internal/domain && git commit -m "feat(domain): Snapshot/Status/Worktrees gated parallel queries

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: TUI loadCmd through Snapshot + generation counter

**Files:**
- Modify: `internal/tui/load.go` (`loadCmd`, `dataLoadedMsg`)
- Modify: `internal/tui/model.go` (`loadGen` field, the `dataLoadedMsg` handler ~line 140, three `loadCmd` call sites ~241/456/494)
- Test: `internal/tui/load_test.go` (or the existing model/load test file)

- [ ] **Step 1: Write the failing test**

Add to the TUI test file (search `grep -rln "dataLoadedMsg\|loadCmd" internal/tui/*_test.go`; create `internal/tui/load_test.go` with `package tui` if none):

```go
// TestStaleSnapshotDropped: a dataLoadedMsg from an older generation is
// ignored, so a superseded in-flight load cannot paint over a newer one.
func TestStaleSnapshotDropped(t *testing.T) {
	m := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m.loadGen = 5

	stale, _ := m.Update(dataLoadedMsg{gen: 4, branches: []model.Branch{{Name: "x"}}})
	if mm := stale.(Model); len(mm.branches) != 0 {
		t.Fatal("stale-generation snapshot was applied")
	}

	fresh, _ := m.Update(dataLoadedMsg{gen: 5, branches: []model.Branch{{Name: "y"}}})
	if mm := fresh.(Model); len(mm.branches) != 1 || mm.branches[0].Name != "y" {
		t.Fatal("current-generation snapshot was not applied")
	}
}
```

(Imports needed in the test file: `"github.com/gigagit/gg/internal/git"`, `"github.com/gigagit/gg/internal/gitexec"`, `"github.com/gigagit/gg/internal/model"`.)

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestStaleSnapshotDropped 2>&1 | head`
Expected: compile error (`dataLoadedMsg` has no field `gen`; `m.loadGen` undefined).

- [ ] **Step 3: Implement**

In `internal/tui/load.go`, replace `dataLoadedMsg` and `loadCmd` with:

```go
// dataLoadedMsg carries a full repo snapshot loaded off the UI thread. gen is
// the load generation it was issued for; a result whose gen no longer matches
// the model's loadGen is stale (superseded by a newer load) and dropped.
type dataLoadedMsg struct {
	gen             int
	status          model.WorkingTreeStatus
	branches        []model.Branch
	commits         []model.Commit
	worktrees       []model.Worktree
	currentWorktree string
	cfg             config.Config
	gitCommonDir    string
	headTimes       map[string]int64
	err             error
}

// loadCmd loads the repo snapshot via the domain layer (gated, parallel,
// coalesced) and, on success, layers in the non-git config + MRU touch. It
// bakes in the current loadGen so a stale result can be dropped.
func (m Model) loadCmd() tea.Cmd {
	svc := m.svc
	statePath := m.statePath
	gen := m.loadGen
	return func() tea.Msg {
		ctx := context.Background()
		snap, err := svc.Snapshot(ctx)
		if err != nil {
			return dataLoadedMsg{gen: gen, err: err}
		}
		out := dataLoadedMsg{
			gen:             gen,
			status:          snap.Status,
			branches:        snap.Branches,
			commits:         snap.Commits,
			worktrees:       snap.Worktrees,
			currentWorktree: snap.CurrentWorktree,
			gitCommonDir:    snap.GitCommonDir,
			headTimes:       snap.HeadTimes,
			cfg:             config.Defaults(),
		}
		// config and the MRU registry are not git reads; do them here, after
		// the gated snapshot, keyed off the toplevel it reported.
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

The import set for load.go becomes (drop `"context"` only if unused — it is still used; keep it; the `repo`-typed local is gone but `svc` replaces it):

```go
import (
	"context"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/config"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repos"
)
```

In `internal/tui/model.go`:

(a) Add the field next to `opCancel`:

```go
	loadGen int // bumped per superseding load; stale dataLoadedMsg are dropped
```

(b) Guard the `dataLoadedMsg` handler (first lines of `case dataLoadedMsg:` at ~line 140):

```go
	case dataLoadedMsg:
		if msg.gen != m.loadGen {
			return m, nil // superseded by a newer load
		}
		m.loading = false
		m.err = msg.err
		// …rest unchanged…
```

(c) Bump `loadGen` at the three superseding call sites:

The `r` reload (~line 241):
```go
		case "r":
			if !m.running {
				m.loadGen++
				m.loading = true
				return m, m.loadCmd()
			}
```

The post-operation reload (~line 456, the `return m, m.loadCmd()` after the switch/chain checks):
```go
		m.loadGen++
		return m, m.loadCmd()
```

`reRoot` (~line 494, just before its final `return m, m.loadCmd()`):
```go
	m.loadGen++
	return m, m.loadCmd()
}
```

`Init` (`return m.loadCmd()`) is the first load and is NOT bumped — it runs as generation 0, which the freshly constructed model also has.

- [ ] **Step 4: Run to verify it passes**

Run: `go test -race ./internal/tui/`
Expected: PASS — the new test plus all existing tui tests (loadCmd's success path is behavior-preserving: same fields populated, config/Touch still happen).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui && git add internal/tui && git commit -m "refactor(tui): load via domain.Snapshot with stale-load generation guard

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: CLI reads through domain

**Files:**
- Modify: `internal/cli/cli.go` (`cmdStatus`)
- Modify: `internal/cli/worktree.go` (`cmdWorktreeList` read; the worktree-remove `Worktrees` read ~line 178)

- [ ] **Step 1: Convert the read call sites**

In `internal/cli/cli.go`, `cmdStatus` — change the read line:

```go
func cmdStatus(repo *repoT, stdout, stderr io.Writer) int {
	st, err := domain.New(repo).Status(context.Background())
```

In `internal/cli/worktree.go`, `cmdWorktreeList`:

```go
	wts, err := domain.New(repo).Worktrees(context.Background())
```

and the worktree-remove read (~line 178, `wts, err := repo.Worktrees(ctxBg)`):

```go
	wts, err := domain.New(repo).Worktrees(ctxBg)
```

Add `"github.com/gigagit/gg/internal/domain"` to the imports of both files. (`domain.New(repo)` is cheap — no git call — and the gate is the process-global one keyed by common dir, so these reads serialize correctly against any concurrent op in-process; the CLI is process-per-command, so there is nothing to coalesce, but routing through domain completes the read-path goal for these sites.)

- [ ] **Step 2: Run the CLI and e2e suites**

Run: `go test ./internal/cli/ ./e2e/`
Expected: PASS. FakeRunner-based CLI tests that assert exact call sequences now see a leading `git rev-parse (common-dir)` (the gate resolving its key on the first gated read). Update those expectations to include it. Real-git CLI/e2e tests are unaffected.

Run: `go vet ./internal/cli/ && go build ./...`

- [ ] **Step 3: Commit**

```bash
gofmt -w internal/cli && git add internal/cli && git commit -m "refactor(cli): status and worktree reads through domain queries

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `CLAUDE.md`

- [ ] **Step 1: CHANGELOG entry**

In `CHANGELOG.md`, add this bullet to the existing `#### Domain layer & repo gate` subsection (append after its current paragraph, as the stage-2 follow-on):

```markdown
- Stage 2: the TUI startup load and the CLI's status/worktree reads now run
  as **domain queries** (`Snapshot`, `Status`, `Worktrees`) under a Read
  reservation. `Snapshot` fetches the seven startup reads in parallel
  (collapsing sequential startup latency to the `git status` long pole) and
  coalesces concurrent identical reads with a singleflight group; a load
  generation counter drops a stale in-flight snapshot so a superseded load
  can't paint over a newer one.
```

- [ ] **Step 2: CLAUDE.md updates**

(a) In the package-map table, update the `domain` row to:

```markdown
| `domain`     | Frontend-facing command + query layer: `Execute` runs an operation under its repo-gate reservation; `Snapshot`/`Status`/`Worktrees` run reads under a Read reservation, in parallel and singleflight-coalesced. Emits the op span. |
```

(b) In "## Conventions", extend the `domain.Execute` bullet (add a sentence):

```markdown
  Frontend reads likewise go through domain queries — `Snapshot` for the TUI
  startup load, `Status`/`Worktrees` for the CLI — not direct `internal/git`
  verb calls.
```

- [ ] **Step 3: Full staged gate**

From the worktree root run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e scenarios pass. If anything fails, report it — do not paper over a failure.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md && git commit -m "docs: snapshot query service (CQRS stage 2)

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Plan self-review notes

- **Spec coverage:** FakeRunner concurrency (Task 1, prerequisite the spec implies via the parallel fan-out); singleflight group (Task 2 ← spec "Singleflight"); `Snapshot`/`Status`/`Worktrees` + `query` helper + dependency-aware fan-out + fatal/best-effort split (Task 3 ← spec "API", "fan-out error handling"); TUI Snapshot wiring + generation-superseding + config/Touch staying in the frontend (Task 4 ← spec "Frontend wiring"); CLI reads (Task 5 ← spec "CLI"); docs (Task 6). No cache, no cancel-on-error, no commit paging — all spec non-goals, no tasks.
- **Type consistency:** `Snapshot` struct fields match between Task 3 (definition) and Task 4 (consumption: `snap.Status/Branches/Commits/Worktrees/CurrentWorktree/GitCommonDir/HeadTimes`). `dataLoadedMsg.gen` defined in Task 4 load.go and asserted in Task 4 test. `flightGroup.Do(key, func() (any, error))` signature consistent between Task 2 and its use in Task 3's `query`. `query[T]` signature consistent across Task 3.
- **Ordering:** Task 1 (FakeRunner safety) precedes Task 3 (parallel fan-out tests would otherwise race). Task 2 (flightGroup) precedes Task 3 (query uses it + the `flight` field). Task 3 precedes Tasks 4–5 (frontends call the new methods).
- The `model.Worktree.Head` field name is used in the fan-out (matches loadCmd's existing `w.Head`). `GitCommonDir` verb already trims its output, so the fan-out stores it as-is.
