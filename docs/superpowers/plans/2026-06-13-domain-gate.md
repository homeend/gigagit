# Domain Layer + Repo Gate (CQRS Stage 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A per-repo reservation gate (`internal/repogate`) and a domain command layer (`internal/domain.Execute`) that every frontend uses to run engine operations, so concurrent activity on one repository is serialized at operation granularity.

**Architecture:** `repogate` provides three-mode (Read/RefWrite/TreeWrite) FIFO reservations keyed by git common dir in a process-global registry. `domain.Service.Execute` acquires the reservation, assembles `engine.OpDeps` (gaining an `Escalate` hook), runs the op, and emits the op span — absorbing the shell currently duplicated in `tui/op.go` and `cli/core.go`. No user-visible behavior changes; reads stay ungated until stage 2.

**Tech Stack:** Go 1.26, stdlib `sync`/`context` only (no new dependencies). Spec: `docs/superpowers/specs/2026-06-13-domain-gate-design.md` — read it first.

**Branch:** `feat/domain-gate` off `main`.

**Conventions that bind every task:** tests first (TDD); `gofmt -w` everything you touch; comments explain constraints, not narration; commit messages end with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.

---

### Task 1: repogate core — modes, Acquire/Release, FIFO fairness

**Files:**
- Create: `internal/repogate/gate.go`
- Create: `internal/repogate/gate_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/repogate/gate_test.go`:

```go
package repogate

import (
	"context"
	"testing"
	"time"
)

// tryAcquire attempts an Acquire bounded by a short deadline; ok=false means
// the gate kept us queued (the expected "excluded" outcome).
func tryAcquire(t *testing.T, g *Gate, mode Mode, label string) (*Reservation, bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	r, err := g.Acquire(ctx, mode, label)
	if err != nil {
		return nil, false
	}
	return r, true
}

// mustAcquire acquires with a generous deadline and fails the test otherwise.
func mustAcquire(t *testing.T, g *Gate, mode Mode, label string) *Reservation {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := g.Acquire(ctx, mode, label)
	if err != nil {
		t.Fatalf("acquire %v %q: %v", mode, label, err)
	}
	return r
}

func TestCompatibilityMatrix(t *testing.T) {
	cases := []struct {
		holder, req Mode
		overlap     bool
	}{
		{Read, Read, true},
		{Read, RefWrite, true},
		{RefWrite, Read, true},
		{RefWrite, RefWrite, false},
		{Read, TreeWrite, false},
		{RefWrite, TreeWrite, false},
		{TreeWrite, Read, false},
		{TreeWrite, RefWrite, false},
		{TreeWrite, TreeWrite, false},
	}
	for _, c := range cases {
		g := &Gate{}
		h := mustAcquire(t, g, c.holder, "holder")
		r, ok := tryAcquire(t, g, c.req, "req")
		if ok != c.overlap {
			t.Errorf("holder %v + request %v: overlap = %v, want %v", c.holder, c.req, ok, c.overlap)
		}
		if ok {
			r.Release()
		}
		h.Release()
		// After the holder is gone the request must always succeed.
		r2 := mustAcquire(t, g, c.req, "req-after")
		r2.Release()
	}
}

func TestWritersFIFO(t *testing.T) {
	g := &Gate{}
	first := mustAcquire(t, g, TreeWrite, "w0")
	order := make(chan string, 3)
	// Queue three writers one at a time, confirming each is queued before
	// launching the next so the FIFO order is deterministic.
	for _, name := range []string{"w1", "w2", "w3"} {
		name := name
		go func() {
			r := mustAcquire(t, g, TreeWrite, name)
			order <- name
			r.Release()
		}()
		waitQueued(t, g, name)
	}
	first.Release()
	for _, want := range []string{"w1", "w2", "w3"} {
		if got := <-order; got != want {
			t.Fatalf("grant order: got %q, want %q", got, want)
		}
	}
}

// waitQueued polls until label appears among the gate's waiters.
func waitQueued(t *testing.T, g *Gate, label string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, e := range g.Queue() {
			if e.Waiting && e.Label == label {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("%q never queued", label)
}

func TestWriterPreference(t *testing.T) {
	g := &Gate{}
	rd := mustAcquire(t, g, Read, "reader")
	granted := make(chan *Reservation, 1)
	go func() {
		granted <- mustAcquire(t, g, TreeWrite, "writer")
	}()
	waitQueued(t, g, "writer")
	// A NEW read must queue behind the waiting writer, not join the holder.
	if _, ok := tryAcquire(t, g, Read, "late-reader"); ok {
		t.Fatal("late read overlapped while a writer was waiting")
	}
	rd.Release()
	w := <-granted
	w.Release()
}

func TestAcquireCancelWhileQueued(t *testing.T) {
	g := &Gate{}
	h := mustAcquire(t, g, TreeWrite, "holder")
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() {
		_, err := g.Acquire(ctx, TreeWrite, "victim")
		errc <- err
	}()
	waitQueued(t, g, "victim")
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	// The cancelled waiter must not wedge the queue.
	h.Release()
	r := mustAcquire(t, g, TreeWrite, "after")
	r.Release()
}

func TestDoubleReleasePanics(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, Read, "r")
	r.Release()
	defer func() {
		if recover() == nil {
			t.Fatal("second Release did not panic")
		}
	}()
	r.Release()
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/repogate/ 2>&1 | head -20`
Expected: compile errors (`undefined: Gate`, `Mode`, …) — the package doesn't exist yet.

- [ ] **Step 3: Implement the core gate**

Create `internal/repogate/gate.go` (Queue/Escalate/For/spans come in Task 2; include `Queue` now because the tests use it):

```go
// Package repogate serializes access to a git repository within one gg
// process. The unit of exclusion is a whole high-level operation — which may
// span many git invocations and block on user decisions — not a single git
// call, because operations like SmartPull leave the repo in deliberately
// wrong intermediate states between invocations. Gates are keyed by the
// repository's git common dir, so all linked worktrees of a repo share one
// gate. Cross-process coordination is out of scope; git's own index.lock
// remains the backstop there.
package repogate

import (
	"context"
	"sync"
)

// Mode is the kind of access a reservation grants.
type Mode int

const (
	// Read observes repo state (status, branches, log, …).
	Read Mode = iota
	// RefWrite moves refs only, never index/worktree/HEAD (e.g. a
	// background fast-forward). Ref updates are atomic, so reads overlap.
	RefWrite
	// TreeWrite may touch index, worktree, or HEAD. Exclusive.
	TreeWrite
)

func (m Mode) String() string {
	switch m {
	case Read:
		return "read"
	case RefWrite:
		return "ref-write"
	default:
		return "tree-write"
	}
}

// compatible reports whether a reservation of mode b may run alongside an
// active holder of mode a: reads overlap reads and ref-writes; everything
// else excludes.
func compatible(a, b Mode) bool {
	if a == TreeWrite || b == TreeWrite {
		return false
	}
	return !(a == RefWrite && b == RefWrite)
}

// holder is the in-gate identity of one granted reservation; Escalate swaps
// a Reservation's holder, so identity must live here, not on Reservation.
type holder struct {
	mode  Mode
	label string
}

// waiter is one queued Acquire.
type waiter struct {
	mode  Mode
	label string
	ready chan struct{} // closed on grant; h is set before the close
	h     *holder
}

// Gate serializes reservations for one repository.
type Gate struct {
	mu      sync.Mutex
	holders []*holder
	waiters []*waiter // FIFO
}

// Reservation is a granted hold on the gate.
type Reservation struct {
	g *Gate
	h *holder // nil once released
}

// Acquire blocks until the reservation is granted or ctx is cancelled.
// label names the holder in Queue() and wait spans (e.g. "op SmartPull").
func (g *Gate) Acquire(ctx context.Context, mode Mode, label string) (*Reservation, error) {
	g.mu.Lock()
	// Immediate grant only when nobody is queued: a non-empty queue means
	// someone arrived first, and FIFO fairness (which is also the writer
	// preference — new reads queue behind a waiting writer) wins over
	// opportunistic overlap.
	if len(g.waiters) == 0 && g.holdersCompatibleWith(mode) {
		h := &holder{mode: mode, label: label}
		g.holders = append(g.holders, h)
		g.mu.Unlock()
		return &Reservation{g: g, h: h}, nil
	}
	w := &waiter{mode: mode, label: label, ready: make(chan struct{})}
	g.waiters = append(g.waiters, w)
	g.mu.Unlock()

	select {
	case <-w.ready:
		return &Reservation{g: g, h: w.h}, nil
	case <-ctx.Done():
		g.mu.Lock()
		for i, q := range g.waiters {
			if q == w { // still queued: just leave
				g.waiters = append(g.waiters[:i], g.waiters[i+1:]...)
				g.grant() // removing a queue head may unblock the rest
				g.mu.Unlock()
				return nil, ctx.Err()
			}
		}
		g.mu.Unlock()
		// Lost the race: granted concurrently with cancellation. Take the
		// grant and immediately give it back.
		<-w.ready
		(&Reservation{g: g, h: w.h}).Release()
		return nil, ctx.Err()
	}
}

// holdersCompatibleWith reports whether mode can run beside every holder.
// Callers hold g.mu.
func (g *Gate) holdersCompatibleWith(mode Mode) bool {
	for _, h := range g.holders {
		if !compatible(h.mode, mode) {
			return false
		}
	}
	return true
}

// grant admits queue heads while they are compatible with the active
// holders — strict FIFO with batch grants, so a run of compatible reads at
// the head is admitted together. Callers hold g.mu.
func (g *Gate) grant() {
	for len(g.waiters) > 0 {
		w := g.waiters[0]
		if !g.holdersCompatibleWith(w.mode) {
			return
		}
		g.waiters = g.waiters[1:]
		w.h = &holder{mode: w.mode, label: w.label}
		g.holders = append(g.holders, w.h)
		close(w.ready)
	}
}

// Release ends the reservation. Releasing twice panics (programming error).
func (r *Reservation) Release() {
	g := r.g
	g.mu.Lock()
	defer g.mu.Unlock()
	if r.h == nil {
		panic("repogate: reservation released twice")
	}
	for i, h := range g.holders {
		if h == r.h {
			g.holders = append(g.holders[:i], g.holders[i+1:]...)
			break
		}
	}
	r.h = nil
	g.grant()
}

// Entry describes one holder (Waiting=false) or queued waiter.
type Entry struct {
	Label   string
	Mode    Mode
	Waiting bool
}

// Queue snapshots current holders then waiters, in FIFO order, for
// frontends to render ("queued: smart pull (2nd)").
func (g *Gate) Queue() []Entry {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]Entry, 0, len(g.holders)+len(g.waiters))
	for _, h := range g.holders {
		out = append(out, Entry{Label: h.label, Mode: h.mode})
	}
	for _, w := range g.waiters {
		out = append(out, Entry{Label: w.label, Mode: w.mode, Waiting: true})
	}
	return out
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `go test -race ./internal/repogate/`
Expected: PASS (all six tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/repogate && git add internal/repogate && git commit -m "feat(repogate): three-mode FIFO reservation gate

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 2: repogate — Escalate, registry, wait spans

**Files:**
- Modify: `internal/repogate/gate.go`
- Modify: `internal/repogate/gate_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/repogate/gate_test.go`:

```go
func TestEscalateJoinsWriterQueue(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, RefWrite, "escalator")
	order := make(chan string, 2)
	go func() {
		w := mustAcquire(t, g, TreeWrite, "earlier-writer")
		order <- "earlier-writer"
		w.Release()
	}()
	waitQueued(t, g, "earlier-writer")
	go func() {
		if err := r.Escalate(context.Background()); err != nil {
			t.Errorf("escalate: %v", err)
		}
		order <- "escalator"
	}()
	// Escalate releases first, so the earlier writer must win, then the
	// escalator re-acquires.
	if got := <-order; got != "earlier-writer" {
		t.Fatalf("first grant = %q, want earlier-writer", got)
	}
	if got := <-order; got != "escalator" {
		t.Fatalf("second grant = %q, want escalator", got)
	}
	// The escalated reservation is now exclusive.
	if _, ok := tryAcquire(t, g, Read, "probe"); ok {
		t.Fatal("read overlapped a TreeWrite escalated reservation")
	}
	r.Release() // the same Reservation value remains usable after Escalate
}

func TestEscalateAlreadyTreeWrite(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, TreeWrite, "w")
	if err := r.Escalate(context.Background()); err != nil {
		t.Fatalf("escalate no-op: %v", err)
	}
	r.Release()
}

func TestEscalateCancelled(t *testing.T) {
	g := &Gate{}
	r := mustAcquire(t, g, RefWrite, "escalator")
	blocker := mustAcquire(t, g, Read, "blocker") // keeps TreeWrite ungrantable
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- r.Escalate(ctx) }()
	waitQueued(t, g, "escalator")
	cancel()
	if err := <-errc; err != context.Canceled {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	blocker.Release()
	// The failed escalation released the original reservation; the gate
	// must be fully free now.
	r2 := mustAcquire(t, g, TreeWrite, "after")
	r2.Release()
}

func TestQueueSnapshot(t *testing.T) {
	g := &Gate{}
	h := mustAcquire(t, g, Read, "holder")
	go func() { mustAcquire(t, g, TreeWrite, "waiter").Release() }()
	waitQueued(t, g, "waiter")
	q := g.Queue()
	if len(q) != 2 {
		t.Fatalf("queue len = %d, want 2 (%+v)", len(q), q)
	}
	if q[0].Label != "holder" || q[0].Mode != Read || q[0].Waiting {
		t.Fatalf("q[0] = %+v, want holding Read holder", q[0])
	}
	if q[1].Label != "waiter" || q[1].Mode != TreeWrite || !q[1].Waiting {
		t.Fatalf("q[1] = %+v, want waiting TreeWrite waiter", q[1])
	}
	h.Release()
}

func TestForRegistry(t *testing.T) {
	a1 := For("/repo-a/.git")
	a2 := For("/repo-a/.git")
	b := For("/repo-b/.git")
	if a1 != a2 {
		t.Fatal("same key returned different gates")
	}
	if a1 == b {
		t.Fatal("different keys shared a gate")
	}
	// The e2e invariant: distinct repos (distinct common dirs) never block
	// each other, even in one process.
	ra := mustAcquire(t, a1, TreeWrite, "a")
	rb := mustAcquire(t, b, TreeWrite, "b")
	ra.Release()
	rb.Release()
}

func TestWaitSpanOnlyWhenWaiting(t *testing.T) {
	var buf syncBuffer
	observ.SetSpanSink(&buf)
	defer observ.SetSpanSink(nil)

	g := &Gate{}
	r := mustAcquire(t, g, Read, "instant") // no wait → no span
	r.Release()
	if s := buf.String(); strings.Contains(s, "gate wait") {
		t.Fatalf("zero-wait acquire emitted a span: %s", s)
	}

	h := mustAcquire(t, g, TreeWrite, "holder")
	done := make(chan struct{})
	go func() {
		mustAcquire(t, g, Read, "queued-reader").Release()
		close(done)
	}()
	waitQueued(t, g, "queued-reader")
	h.Release()
	<-done
	s := buf.String()
	if !strings.Contains(s, "gate wait") || !strings.Contains(s, "queued-reader") || !strings.Contains(s, "read") {
		t.Fatalf("waited acquire span missing or incomplete: %s", s)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer (spans are emitted from the
// acquiring goroutine).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
```

Add to the test file's imports: `"bytes"`, `"strings"`, `"sync"`, and `"github.com/gigagit/gg/internal/observ"`.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/repogate/ 2>&1 | head -10`
Expected: compile errors (`undefined: For`, `r.Escalate`).

- [ ] **Step 3: Implement Escalate, For, and wait spans**

In `internal/repogate/gate.go`, add `"time"` and `"github.com/gigagit/gg/internal/observ"` to the imports, and append:

```go
// Escalate trades the held reservation for an exclusive (TreeWrite) one: it
// RELEASES the current reservation and joins the queue — there is no atomic
// upgrade (the classic deadlock). Callers must therefore only escalate at a
// boundary where the operation holds no partial state of its own. A
// reservation that is already TreeWrite returns immediately. On error (ctx
// cancelled while queued) the reservation is gone — the caller must not
// Release it.
func (r *Reservation) Escalate(ctx context.Context) error {
	if r.h == nil {
		panic("repogate: escalate after release")
	}
	if r.h.mode == TreeWrite {
		return nil
	}
	g, label := r.g, r.h.label
	r.Release()
	nr, err := g.Acquire(ctx, TreeWrite, label)
	if err != nil {
		return err
	}
	r.h = nr.h
	return nil
}

var (
	regMu sync.Mutex
	gates = map[string]*Gate{}
)

// For returns the process-wide gate for key (a git common dir), creating it
// on first use.
func For(key string) *Gate {
	regMu.Lock()
	defer regMu.Unlock()
	g, ok := gates[key]
	if !ok {
		g = &Gate{}
		gates[key] = g
	}
	return g
}
```

Then make `Acquire` emit the wait span on the queued path only — change the granted arm of the select to:

```go
	select {
	case <-w.ready:
		observ.EmitSpan(observ.Span{
			Name:     "gate wait",
			Args:     []string{mode.String(), label},
			Start:    start,
			Duration: time.Since(start),
		})
		return &Reservation{g: g, h: w.h}, nil
```

and record `start := time.Now()` as the first line of `Acquire` (before taking the mutex). The immediate-grant path emits nothing — the common single-user case stays noise-free.

- [ ] **Step 4: Run the tests with the race detector**

Run: `go test -race ./internal/repogate/`
Expected: PASS (all twelve tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/repogate && git add internal/repogate && git commit -m "feat(repogate): escalation, common-dir registry, wait spans

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 3: engine — OpDeps.Escalate hook + SmartPull lock mode

**Files:**
- Modify: `internal/engine/operation.go`
- Modify: `internal/engine/smart_pull.go:68`
- Modify: `internal/engine/operation_test.go` (or create if the escalate test fits nowhere)
- Modify: `internal/engine/smart_pull_test.go`

(This task precedes the domain package so domain's tests can use the real `OpDeps` shape.)

- [ ] **Step 1: Write the failing tests**

Append to `internal/engine/smart_pull_test.go`:

```go
func TestSmartPullLockMode(t *testing.T) {
	if got := (SmartPull{Intent: PullInBackground}).LockMode(); got != repogate.RefWrite {
		t.Fatalf("background lock mode = %v, want RefWrite", got)
	}
	if got := (SmartPull{Intent: PullAndStay}).LockMode(); got != repogate.TreeWrite {
		t.Fatalf("stay lock mode = %v, want TreeWrite", got)
	}
	if got := (SmartPull{}).LockMode(); got != repogate.TreeWrite {
		t.Fatalf("default lock mode = %v, want TreeWrite", got)
	}
}

// TestSmartPullBackgroundEscalatesBeforeCheckout proves the escalation hook
// fires BEFORE checkoutPull touches the worktree: a failing Escalate must
// abort the operation with the current branch untouched.
func TestSmartPullBackgroundEscalatesBeforeCheckout(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	// Make dev diverge so FastForwardRef cannot succeed: local dev has its
	// own commit, origin/dev advanced separately.
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/main")
	gitAt(t, clone, "switch", "dev")
	gitAt(t, clone, "commit", "--allow-empty", "-m", "local-dev")
	gitAt(t, clone, "switch", "main")
	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "origin-dev")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	escErr := errors.New("escalation denied")
	called := false
	_, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(), OpDeps{
		Repo:    repo,
		Decider: MapDecider{"not-fast-forwardable": "checkout-and-resolve"},
		Escalate: func(context.Context) error {
			called = true
			return escErr
		},
	})
	if !called {
		t.Fatal("Escalate was never called on the checkout-and-resolve path")
	}
	if !errors.Is(err, escErr) {
		t.Fatalf("err = %v, want the escalation error", err)
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur != "main" {
		t.Fatalf("current branch = %q, want main (must not checkout before escalation succeeds)", cur)
	}
}
```

Add `"errors"` and `"github.com/gigagit/gg/internal/repogate"` to the test file's imports.

Append to `internal/engine/operation_test.go` (create the file with `package engine` and imports `"context"`, `"testing"` if it does not exist):

```go
// TestOpDepsEscalateNilSafe: direct engine users (tests, future callers)
// configure no Escalate; the helper must be a successful no-op, matching the
// nil-channel/nil-decider style of emit/decide.
func TestOpDepsEscalateNilSafe(t *testing.T) {
	if err := (OpDeps{}).escalate(context.Background()); err != nil {
		t.Fatalf("nil escalate = %v, want nil", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run 'TestSmartPullLockMode|TestSmartPullBackgroundEscalates|TestOpDepsEscalateNilSafe' 2>&1 | head -10`
Expected: compile errors (`LockMode` undefined, `Escalate` field unknown, `escalate` undefined).

- [ ] **Step 3: Implement the OpDeps field and SmartPull changes**

In `internal/engine/operation.go`, extend `OpDeps` and add the helper:

```go
// OpDeps is everything an operation needs: the repo to act on, an optional
// event channel, an optional Decider for mid-flight forks, and an optional
// hook to escalate the operation's gate reservation.
type OpDeps struct {
	Repo    *git.Repo
	Events  chan<- Event
	Decider Decider
	// Escalate trades the operation's gate reservation for an exclusive
	// (TreeWrite) one. Nil (direct engine use, tests) is a no-op. Call it
	// only at a boundary where the operation holds no partial state.
	Escalate func(ctx context.Context) error
}

// escalate is the nil-safe form of Escalate (style of emit/decide).
func (d OpDeps) escalate(ctx context.Context) error {
	if d.Escalate == nil {
		return nil
	}
	return d.Escalate(ctx)
}
```

In `internal/engine/smart_pull.go`, add the lock-mode declaration (next to the `var _ Operation = SmartPull{}` line) and the escalation call:

```go
// LockMode declares the gate reservation SmartPull needs: a background
// fast-forward moves only refs; every other path may touch the worktree.
func (op SmartPull) LockMode() repogate.Mode {
	if op.Intent == PullInBackground {
		return repogate.RefWrite
	}
	return repogate.TreeWrite
}
```

and in `Run`, the checkout-and-resolve arm becomes:

```go
		if resp.Option == "checkout-and-resolve" {
			// Held only a RefWrite reservation so far; checkoutPull touches
			// the worktree. This boundary is safe to escalate across: the
			// failed FastForwardRef left no partial state.
			if err := deps.escalate(ctx); err != nil {
				return Result{}, err
			}
			return op.checkoutPull(ctx, deps, remote, target, cur)
		}
```

Add `"github.com/gigagit/gg/internal/repogate"` to smart_pull.go's imports.

- [ ] **Step 4: Run the engine tests**

Run: `go test -race ./internal/engine/`
Expected: PASS (all existing tests plus the three new ones).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/engine && git add internal/engine && git commit -m "feat(engine): OpDeps.Escalate hook; SmartPull declares lock mode

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 4: domain — Service with Open/New/Repo/Execute

**Files:**
- Create: `internal/domain/service.go`
- Create: `internal/domain/service_test.go`

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/service_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/repogate"
)

// fakeOp runs body under whatever OpDeps Execute assembled.
type fakeOp struct {
	mode *repogate.Mode // nil = no LockMode method behavior (see lockedOp)
	body func(ctx context.Context, deps engine.OpDeps) (engine.Result, error)
}

func (o fakeOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	return o.body(ctx, deps)
}

// lockedOp is a fakeOp that declares a LockMode.
type lockedOp struct{ fakeOp }

func (o lockedOp) LockMode() repogate.Mode { return *o.mode }

// svcWithKey builds a Service over a FakeRunner that resolves the common dir
// to key — each test uses a unique key because the gate registry is global.
func svcWithKey(key string) (*Service, *gitexec.FakeRunner) {
	fr := gitexec.NewFakeRunner()
	fr.SetResponse("git rev-parse (common-dir)", gitexec.Result{Stdout: key + "\n"})
	return New(&git.Repo{Runner: fr}), fr
}

func TestExecuteHoldsTreeWriteByDefault(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-default")
	var seen []repogate.Entry
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		seen = repogate.For("/domain-test-default").Queue()
		return engine.Result{Summary: "ok"}, nil
	}}
	res, err := svc.Execute(context.Background(), op, nil, nil)
	if err != nil || res.Summary != "ok" {
		t.Fatalf("execute: %v %+v", err, res)
	}
	if len(seen) != 1 || seen[0].Mode != repogate.TreeWrite || seen[0].Waiting {
		t.Fatalf("mid-op gate state = %+v, want one held TreeWrite", seen)
	}
	if q := repogate.For("/domain-test-default").Queue(); len(q) != 0 {
		t.Fatalf("gate not released after Execute: %+v", q)
	}
}

func TestExecuteRespectsLockMode(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-mode")
	mode := repogate.Read
	var seen []repogate.Entry
	op := lockedOp{fakeOp{mode: &mode, body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		seen = repogate.For("/domain-test-mode").Queue()
		return engine.Result{}, nil
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(seen) != 1 || seen[0].Mode != repogate.Read {
		t.Fatalf("mid-op gate state = %+v, want one held Read", seen)
	}
}

func TestExecuteWiresEscalate(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-escalate")
	var after []repogate.Entry
	op := lockedOp{fakeOp{mode: modePtr(repogate.RefWrite), body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		if deps.Escalate == nil {
			t.Fatal("Execute did not wire Escalate")
		}
		if err := deps.Escalate(ctx); err != nil {
			return engine.Result{}, err
		}
		after = repogate.For("/domain-test-escalate").Queue()
		return engine.Result{}, nil
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(after) != 1 || after[0].Mode != repogate.TreeWrite {
		t.Fatalf("post-escalate gate state = %+v, want one held TreeWrite", after)
	}
	if q := repogate.For("/domain-test-escalate").Queue(); len(q) != 0 {
		t.Fatalf("gate not released after escalated Execute: %+v", q)
	}
}

func modePtr(m repogate.Mode) *repogate.Mode { return &m }

func TestExecuteCancelledWhileQueued(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-cancel")
	hold, err := repogate.For("/domain-test-cancel").Acquire(context.Background(), repogate.TreeWrite, "blocker")
	if err != nil {
		t.Fatal(err)
	}
	defer hold.Release()
	ran := false
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ran = true
		return engine.Result{}, nil
	}}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.Execute(ctx, op, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded", err)
	}
	if ran {
		t.Fatal("op ran despite the gate never granting")
	}
}

func TestExecuteSameGateAcrossCalls(t *testing.T) {
	// Two Executes on one Service must contend on the same gate even though
	// the common dir resolves only once (it is cached).
	svc, fr := svcWithKey("/domain-test-shared")
	started := make(chan struct{})
	release := make(chan struct{})
	go svc.Execute(context.Background(), fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		close(started)
		<-release
		return engine.Result{}, nil
	}}, nil, nil)
	<-started
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := svc.Execute(ctx, fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		return engine.Result{}, nil
	}}, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("second Execute did not contend with the first")
	}
	close(release)
	// The common dir was resolved exactly once.
	n := 0
	for _, c := range fr.Calls {
		if c.Name == "git rev-parse (common-dir)" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("common-dir resolved %d times, want 1 (cached)", n)
	}
}

func TestExecuteFallbackKeyWhenCommonDirFails(t *testing.T) {
	fr := gitexec.NewFakeRunner()
	fr.SetError("git rev-parse (common-dir)", errors.New("not a repo"))
	svc := New(&git.Repo{Runner: fr})
	ran := false
	if _, err := svc.Execute(context.Background(), fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ran = true
		return engine.Result{}, nil
	}}, nil, nil); err != nil || !ran {
		t.Fatalf("execute with fallback key: err=%v ran=%v", err, ran)
	}
}

func TestRepoExposesUnderlyingRepo(t *testing.T) {
	r := &git.Repo{Runner: gitexec.NewFakeRunner()}
	if New(r).Repo() != r {
		t.Fatal("Repo() must hand back the wrapped repo")
	}
}

// TestExecuteEmitsOpSpan: the "op <Name>" span (with error fields on
// failure) moved from the frontend shells into Execute — pin it here.
func TestExecuteEmitsOpSpan(t *testing.T) {
	var buf syncBuffer
	observ.SetSpanSink(&buf)
	defer observ.SetSpanSink(nil)

	svc, _ := svcWithKey("/domain-test-span")
	boom := errors.New("boom")
	op := fakeOp{body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		return engine.Result{}, boom
	}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	s := buf.String()
	if !strings.Contains(s, "op fakeOp") || !strings.Contains(s, "boom") {
		t.Fatalf("op span missing or lacks error fields: %s", s)
	}
}

// syncBuffer is a goroutine-safe bytes.Buffer for span-sink capture.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
```

(That adds `"bytes"`, `"strings"`, `"sync"`, and `"github.com/gigagit/gg/internal/observ"` to service_test.go's imports. The `syncBuffer` here is deliberately duplicated from repogate's tests — test helpers don't cross packages.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/domain/ 2>&1 | head -10`
Expected: compile errors — the package doesn't exist.

- [ ] **Step 3: Implement the Service**

Create `internal/domain/service.go`:

```go
// Package domain is the frontend-facing layer of gg: commands (engine
// operations) run through Execute under a per-repo reservation, and — in
// later stages — queries (snapshot, commit feed) run here too. Frontends
// call domain; nothing above the engine acquires gates or assembles OpDeps
// by hand.
package domain

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
	"github.com/gigagit/gg/internal/repogate"
)

// Service couples one repository with its process-wide gate.
type Service struct {
	repo    *git.Repo
	workdir string // fallback gate key when common-dir resolution fails

	mu   sync.Mutex
	gate *repogate.Gate // resolved lazily on first Execute
}

// Open builds a Service rooted at workdir with the standard runner — the
// one place frontends construct the repo stack. It runs no git command.
func Open(workdir string) *Service {
	s := New(&git.Repo{Runner: gitexec.NewExecRunner("git", workdir, observ.NewRing(200))})
	s.workdir = workdir
	return s
}

// New wraps an existing repo (tests, callers with their own runner wiring).
func New(repo *git.Repo) *Service {
	return &Service{repo: repo}
}

// Repo exposes the underlying repo for READ verbs. Transitional: stage 2
// moves frontend reads into domain queries; stage 4 removes this.
func (s *Service) Repo() *git.Repo { return s.repo }

// gateFor resolves (once) the gate for this repo, keyed by the git common
// dir so all linked worktrees share one gate. A repo whose common dir
// cannot be resolved falls back to the workdir (or a per-Service key) —
// sound for everything except cross-worktree races in a broken repo, where
// the verb error surfaces anyway.
func (s *Service) gateFor(ctx context.Context) *repogate.Gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil {
		return s.gate
	}
	key := ""
	if cd, err := s.repo.GitCommonDir(ctx); err == nil {
		key = strings.TrimSpace(cd)
	}
	if key == "" {
		key = s.workdir
	}
	if key == "" {
		key = fmt.Sprintf("repo-%p", s.repo)
	}
	s.gate = repogate.For(key)
	return s.gate
}

// lockModer is implemented by operations that need less than the exclusive
// default (e.g. a background pull that only moves refs).
type lockModer interface{ LockMode() repogate.Mode }

// Execute runs one command under its gate reservation: acquire (honoring
// ctx while queued), run the op with fully assembled OpDeps, release, and
// emit the "op <Name>" span. Execute is synchronous; frontends keep their
// own goroutine and event-pumping structure around it.
func (s *Service) Execute(ctx context.Context, op engine.Operation,
	events chan<- engine.Event, dec engine.Decider) (engine.Result, error) {
	mode := repogate.TreeWrite
	if lm, ok := op.(lockModer); ok {
		mode = lm.LockMode()
	}
	label := "op " + engine.OpName(op)
	res, err := s.gateFor(ctx).Acquire(ctx, mode, label)
	if err != nil {
		return engine.Result{}, err
	}
	// A failed Escalate releases without re-acquiring, so the reservation
	// may already be gone by the time the op returns.
	defer func() {
		if !res.Released() {
			res.Release()
		}
	}()

	opStart := time.Now()
	out, opErr := op.Run(ctx, engine.OpDeps{
		Repo:     s.repo,
		Events:   events,
		Decider:  dec,
		Escalate: res.Escalate,
	})
	span := observ.Span{Name: label, Start: opStart, Duration: time.Since(opStart)}
	if opErr != nil {
		span.ExitCode = 1
		span.Err = opErr.Error()
	}
	observ.EmitSpan(span)
	return out, opErr
}
```

The guarded defer needs a tiny accessor in `internal/repogate/gate.go` (add it in this task, plus a one-line test in gate_test.go asserting `Released()` flips after Release). The guard exists because `Escalate`'s error path calls `Release` before trying to re-acquire — after a failed escalation `r.h` is already nil and an unconditional deferred `Release` would panic:

```go
// Released reports whether the reservation has ended (also true after a
// failed Escalate, which releases before re-acquiring).
func (r *Reservation) Released() bool {
	r.g.mu.Lock()
	defer r.g.mu.Unlock()
	return r.h == nil
}
```

And add a domain test for exactly that path:

```go
func TestExecuteEscalateCancelledReleasesCleanly(t *testing.T) {
	svc, _ := svcWithKey("/domain-test-esc-cancel")
	blocker, err := repogate.For("/domain-test-esc-cancel").Acquire(context.Background(), repogate.Read, "blocker")
	if err != nil {
		t.Fatal(err)
	}
	op := lockedOp{fakeOp{mode: modePtr(repogate.RefWrite), body: func(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
		ictx, icancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer icancel()
		return engine.Result{}, deps.Escalate(ictx) // blocked by the Read holder
	}}}
	if _, err := svc.Execute(context.Background(), op, nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want deadline exceeded from the failed escalation", err)
	}
	blocker.Release()
	// No panic from the deferred release, and the gate is free.
	if q := repogate.For("/domain-test-esc-cancel").Queue(); len(q) != 0 {
		t.Fatalf("gate state after failed escalation = %+v, want empty", q)
	}
}
```

- [ ] **Step 4: Run the tests with the race detector**

Run: `go test -race ./internal/domain/ ./internal/repogate/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/domain internal/repogate && git add internal/domain internal/repogate && git commit -m "feat(domain): Service.Execute runs operations under the repo gate

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 5: CLI — runOperation delegates to domain

**Files:**
- Modify: `internal/cli/core.go:69-103` (the `runOperation` body)

- [ ] **Step 1: Rewrite runOperation**

In `internal/cli/core.go`, replace the body of `runOperation` (keep the signature — gates are process-global by common dir, so a fresh Service per call contends correctly with every other Service of the same repo; the full frontends-hold-a-Service narrowing is stage 4):

```go
// runOperation runs op through the domain layer (which serializes it on the
// repo's gate and emits the op span), printing each Progress step to
// progress. The op runs in a goroutine so events stream live; decisions are
// resolved by dec (which may prompt).
func runOperation(ctx context.Context, repo *git.Repo, op engine.Operation, dec engine.Decider, progress io.Writer) (engine.Result, error) {
	events := make(chan engine.Event, 32)
	var (
		res engine.Result
		err error
	)
	done := make(chan struct{})
	go func() {
		res, err = domain.New(repo).Execute(ctx, op, events, dec)
		close(events)
		close(done)
	}()
	for e := range events {
		if p, ok := e.(engine.Progress); ok {
			if p.Detail != "" {
				fmt.Fprintf(progress, "→ %s: %s\n", p.Step, p.Detail)
			} else {
				fmt.Fprintf(progress, "→ %s\n", p.Step)
			}
		}
	}
	<-done
	return res, err
}
```

Imports: add `"github.com/gigagit/gg/internal/domain"`; remove `"time"` and `"github.com/gigagit/gg/internal/observ"` if now unused (the span moved into Execute).

- [ ] **Step 2: Run the CLI and e2e tests**

Run: `go test ./internal/cli/ ./e2e/`
Expected: PASS. If any test fails on a FakeRunner call-sequence expectation, the cause is the new leading `git rev-parse (common-dir)` invocation Execute performs — update that expectation (real-git tests are unaffected; the extra rev-parse just runs).

- [ ] **Step 3: Commit**

```bash
gofmt -w internal/cli && git add internal/cli && git commit -m "refactor(cli): run operations through domain.Execute

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 6: TUI — Service field, cancellable op context

**Files:**
- Modify: `internal/tui/model.go` (Model fields, `New`, `reRoot:445`, `opFinishedMsg` handler ~:392)
- Modify: `internal/tui/op.go:55-84` (`startOp`)
- Modify: `internal/tui/run.go:23`
- Test: `internal/tui/op_test.go` (or the file holding existing startOp tests)

- [ ] **Step 1: Write the failing test**

Add to the TUI test file that already exercises `startOp` (search: `grep -rln "startOp" internal/tui/*_test.go`; if none exists, create `internal/tui/op_test.go` with `package tui`):

```go
// blockingOp parks until its context is cancelled — the shape of an op
// orphaned by the user quitting mid-run.
type blockingOp struct{}

func (blockingOp) Run(ctx context.Context, deps engine.OpDeps) (engine.Result, error) {
	<-ctx.Done()
	return engine.Result{}, ctx.Err()
}

func TestQuitCancelsRunningOp(t *testing.T) {
	m := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	m2, _ := m.startOp(blockingOp{})
	if m2.opCancel == nil {
		t.Fatal("startOp did not arm opCancel")
	}
	m2.opCancel() // what run.go does when the program exits mid-op
	deadline := time.After(5 * time.Second)
	for {
		select {
		case msg := <-m2.opMsgs:
			if fin, ok := msg.(opFinishedMsg); ok {
				if !errors.Is(fin.err, context.Canceled) {
					t.Fatalf("op finished with %v, want context.Canceled", fin.err)
				}
				return
			}
		case <-deadline:
			t.Fatal("cancelled op never finished")
		}
	}
}
```

(Imports as needed: `"context"`, `"errors"`, `"testing"`, `"time"`, `"github.com/gigagit/gg/internal/engine"`, `"github.com/gigagit/gg/internal/git"`, `"github.com/gigagit/gg/internal/gitexec"`.)

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestQuitCancelsRunningOp 2>&1 | head -5`
Expected: compile error (`m2.opCancel` undefined).

- [ ] **Step 3: Implement the wiring**

`internal/tui/model.go`:

1. Model fields — next to the existing `repo` field add:

```go
	svc      *domain.Service    // command layer; m.repo == svc.Repo()
	opCancel context.CancelFunc // cancels the in-flight op's context; nil when idle
```

2. `New(repo *git.Repo)` (model.go:79) — first lines become:

```go
func New(repo *git.Repo) Model {
	svc := domain.New(repo)
	m := Model{
		repo: repo,
		svc:  svc,
		// …existing field initialization continues unchanged…
```

(Adapt to the function's actual shape: the only requirement is `m.svc = domain.New(repo)` alongside the existing `m.repo = repo`.)

3. `reRoot` (model.go:445) — replace the repo construction line with:

```go
	m.svc = domain.Open(path)
	m.repo = m.svc.Repo()
```

4. `case opFinishedMsg:` (model.go:392) — first lines of the handler:

```go
		if m.opCancel != nil {
			m.opCancel() // op already returned; this only frees the ctx
			m.opCancel = nil
		}
```

5. Import `"github.com/gigagit/gg/internal/domain"`; drop `gitexec`/`observ` imports if reRoot was their last use.

`internal/tui/op.go` — `startOp` becomes (span code is gone; Execute emits it):

```go
// startOp launches op through the domain layer in a goroutine, forwarding
// its events and completion onto a fresh message channel, and returns the
// command that waits for the next msg. The op context is cancelled when the
// program exits (run.go) so an op can never outlive the UI silently.
func (m Model) startOp(op engine.Operation) (Model, tea.Cmd) {
	msgs := make(chan tea.Msg, 32)
	events := make(chan engine.Event, 32)
	svc := m.svc
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		res, err := svc.Execute(ctx, op, events, uiDecider{msgs: msgs})
		close(events)
		msgs <- opFinishedMsg{res: res, err: err}
	}()
	go func() {
		for e := range events {
			msgs <- opEventMsg{event: e}
		}
	}()
	m.running = true
	m.statusMsg = "working…"
	m.opMsgs = msgs
	m.opCancel = cancel
	return m, waitForOp(msgs)
}
```

(`time` and `observ` imports drop out of op.go.)

`internal/tui/run.go:23` — cancel any in-flight op once the program exits, before the error check (this single chokepoint covers every quit path: `q` in any view, popup quits, ctrl+c):

```go
	final, err := p.Run()
	if fm, ok := final.(Model); ok && fm.opCancel != nil {
		fm.opCancel()
	}
	if err != nil {
		return "", err
	}
```

- [ ] **Step 4: Run the TUI tests**

Run: `go test -race ./internal/tui/`
Expected: PASS — the new test plus all existing op/model tests (startOp's observable behavior is unchanged: same messages, same channels; only the span emitter and ctx plumbing moved).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui && git add internal/tui && git commit -m "refactor(tui): run operations through domain.Execute with cancellable ctx

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

### Task 7: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md` (top of `### Added`)
- Modify: `CLAUDE.md` (architecture diagram, package map, conventions)

- [ ] **Step 1: CHANGELOG entry**

Insert as the FIRST subsection under `### Added`:

```markdown
#### Domain layer & repo gate
- New `internal/domain` command layer: both frontends now run engine
  operations through `domain.Execute`, which serializes them per repository
  on a three-mode reservation gate (`internal/repogate`: Read / RefWrite /
  TreeWrite, writer-preferring FIFO, keyed by git common dir so linked
  worktrees share one gate). Reservations are held across user decisions —
  the exclusion unit is the whole operation, not one git call. Background
  pulls hold only a ref-write reservation and escalate at a safe boundary
  when the user chooses checkout-and-resolve. Queued reservations emit
  "gate wait" spans, and a TUI op's context is now cancelled when the
  program exits. Foundation for concurrent operations (workspace group
  sync, MCP); no behavior change for single operations.
```

- [ ] **Step 2: CLAUDE.md updates**

1. Architecture diagram — add the domain layer:

```
        TUI (Bubble Tea)   CLI (scriptable)   MCP (future)
                   \            |            /
                    \           |           /
                     internal/domain  ← commands run via Execute under a
                                        per-repo reservation (repogate)
                                |
                      internal/engine  ← operations emit Events,
                                          resolve forks via a Decider
                                |
        internal/git (verbs) → internal/gitcmd (argv builder)
                                → internal/gitexec (process Runner + Fake)
```

2. Package map — add two rows after `engine`:

```markdown
| `domain`     | Frontend-facing command layer: `Execute` runs an operation under its repo-gate reservation and emits the op span. Queries (snapshot, commit feed) land here in later stages. |
| `repogate`   | Per-repo reservation gate (Read/RefWrite/TreeWrite, writer-preferring FIFO, escalation), process-global registry keyed by git common dir. |
```

3. Conventions — add one bullet after "Operations never block on a human":

```markdown
- **Frontends run operations via `domain.Execute`**, never by assembling
  `OpDeps` directly. Ops needing less than exclusive access declare
  `LockMode()` (see SmartPull's background ref-write); escalation happens
  only at boundaries with no partial state.
```

- [ ] **Step 3: Full staged gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, e2e scenarios pass.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md CLAUDE.md && git commit -m "docs: domain layer + repo gate

Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>"
```

---

## Plan self-review notes

- Spec coverage: gate modes/matrix/fairness/escalation/queue/spans (Tasks 1–2), domain Open/New/Repo/Execute/lazy-key/fallback (Task 4), OpDeps.Escalate + SmartPull LockMode/escalation (Task 3), CLI wiring (Task 5), TUI wiring + cancellable ctx + run.go chokepoint (Task 6), docs (Task 7). Spec's "no reads through the gate / no queue UI / no cache" need no tasks by design.
- Task 3 precedes Task 4 because domain's Execute assembles the extended OpDeps.
- The registry being process-global means test keys must be unique per test (`/domain-test-*`); both test files follow that.
- Engine tests use real git repos (`cloneOnMainBehindOrigin`, `gitAt`, `MapDecider`) — Task 3's escalation test follows that house style rather than FakeRunner.
