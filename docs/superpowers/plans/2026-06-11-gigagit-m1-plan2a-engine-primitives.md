# gigagit M1 — Plan 2A: Engine Contract & Git Operation Primitives — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the frontend-agnostic engine contract (`Operation`/`Decider`/`Event`) and the thin mutating git verbs + query helpers that the smart operations (Plan 2B) will orchestrate, proven end-to-end with thin Commit/Push/Stash operations.

**Architecture:** A new `internal/engine` package defines an operation as `Run(ctx, OpDeps) (Result, error)` where `OpDeps` carries the repo, an event channel, and a `Decider` for mid-flight forks — designed so an MCP agent (which cannot block) drives it the same way a TUI does. `internal/git` gains thin mutating verbs (commit/switch/sync/stash) and read-only query helpers (current branch, worktree-for-branch, fast-forward check) on the existing `Repo`. No decision-tree orchestration yet — that's Plan 2B.

**Tech Stack:** Go 1.26, standard library + existing internal packages (`git`, `gitcmd`, `gitexec`, `model`, `observ`). Tests run against real throwaway git repos (reusing the `newTestRepo` helper already in `internal/git/repo_test.go`), with bare-remote helpers for push/pull.

---

## Shared interfaces (defined once, referenced by later tasks)

These are fixed across the plan. Later tasks must match exactly.

```go
// internal/engine — event.go
type Event interface{ isEvent() }
type Progress struct{ Step, Detail string }       // a high-level step
type GitLine struct{ Raw string }                  // one line of git output
type DecisionNeeded struct{ Request DecisionRequest }
type Timing struct{ Span observ.Span }             // a completed span
type Done struct{ Result Result }                  // terminal

// internal/engine — decider.go
type DecisionRequest struct {
	ID      string   // stable key, e.g. "non-fast-forward"
	Prompt  string
	Options []string // e.g. ["rebase","merge","abort"]
}
type DecisionResponse struct{ Option string }
type Decider interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}
var ErrDecisionRequired = errors.New("engine: decision required but no decider/answer available")

// internal/engine — operation.go
type Result struct {
	Summary string
	Changed bool
}
type OpDeps struct {
	Repo    *git.Repo
	Events  chan<- Event // may be nil
	Decider Decider      // may be nil
}
type Operation interface {
	Run(ctx context.Context, deps OpDeps) (Result, error)
}

// internal/git — new methods on *Repo (Runner-backed, thin)
func (r *Repo) Commit(ctx context.Context, message string, all bool) error
func (r *Repo) Switch(ctx context.Context, branch string) error
func (r *Repo) CreateBranch(ctx context.Context, name string) error
func (r *Repo) Fetch(ctx context.Context, remote string) error
func (r *Repo) PullFFOnly(ctx context.Context, remote, branch string) error
func (r *Repo) FastForwardRef(ctx context.Context, remote, branch string) error
func (r *Repo) Push(ctx context.Context, remote, branch string, setUpstream bool) error
func (r *Repo) StashPush(ctx context.Context, message string) error
func (r *Repo) StashPop(ctx context.Context) error
func (r *Repo) StashList(ctx context.Context) ([]string, error)
func (r *Repo) CurrentBranch(ctx context.Context) (string, error)
func (r *Repo) WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error)
func (r *Repo) CanFastForward(ctx context.Context, ancestor, descendant string) (bool, error)
```

---

## Task 1: Engine event types

**Files:**
- Create: `internal/engine/event.go`
- Test: `internal/engine/event_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/event_test.go`:
```go
package engine

import "testing"

func TestEventsImplementInterface(t *testing.T) {
	// Each variant must satisfy Event; a type switch must distinguish them.
	events := []Event{
		Progress{Step: "fetching", Detail: "origin"},
		GitLine{Raw: "remote: counting"},
		DecisionNeeded{Request: DecisionRequest{ID: "x"}},
		Done{Result: Result{Summary: "ok", Changed: true}},
	}
	var got []string
	for _, e := range events {
		switch ev := e.(type) {
		case Progress:
			got = append(got, "progress:"+ev.Step)
		case GitLine:
			got = append(got, "line:"+ev.Raw)
		case DecisionNeeded:
			got = append(got, "decision:"+ev.Request.ID)
		case Done:
			got = append(got, "done:"+ev.Result.Summary)
		default:
			t.Fatalf("unknown event type %T", e)
		}
	}
	want := []string{"progress:fetching", "line:remote: counting", "decision:x", "done:ok"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got[%d]=%q want %q", i, got[i], want[i])
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/`
Expected: FAIL — undefined `Event`, `Progress`, etc.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/event.go`:
```go
// Package engine defines the frontend-agnostic operation contract: operations
// emit Events and resolve forks via a Decider, so a TUI, CLI, or MCP agent can
// all drive the same operation.
package engine

import "github.com/gigagit/gg/internal/observ"

// Event is a tagged union streamed by an operation to its consumer.
type Event interface{ isEvent() }

// Progress reports a high-level step ("stashing", "fetching", "pulling").
type Progress struct {
	Step   string
	Detail string
}

// GitLine carries one raw line of git stdout/stderr for a live log view.
type GitLine struct{ Raw string }

// DecisionNeeded is emitted alongside a Decider call so passive observers can
// render the prompt.
type DecisionNeeded struct{ Request DecisionRequest }

// Timing carries a completed span (a git subprocess or an operation step).
type Timing struct{ Span observ.Span }

// Done is the terminal event carrying the operation result.
type Done struct{ Result Result }

func (Progress) isEvent()       {}
func (GitLine) isEvent()        {}
func (DecisionNeeded) isEvent() {}
func (Timing) isEvent()         {}
func (Done) isEvent()           {}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/`
Expected: FAIL to compile still — `DecisionRequest` and `Result` are defined in Tasks 2 and 3. To make THIS task self-contained, the test above only references types defined here plus `DecisionRequest`/`Result`. Therefore, temporarily add minimal stubs at the bottom of `event.go` IS NOT allowed (DRY/structure). Instead, this task's test depends on Task 2 and Task 3 types. **Reorder rule:** implement Task 2 and Task 3 type declarations first if executing strictly; but since each is a separate file in the same package, the package compiles only once all three exist.

To keep tasks independently runnable, **add the `DecisionRequest`, `DecisionResponse`, and `Result` struct declarations in their own files in Tasks 2 and 3, and run the engine package test suite after Task 3.** For Task 1 in isolation, verify compilation of `event.go` with:

Run: `go build ./internal/engine/`
Expected: FAIL — `DecisionRequest`, `Result` undefined (they arrive in Tasks 2–3). This is expected; do not stub them.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/event.go internal/engine/event_test.go
git commit -m "feat: add engine Event tagged union"
```

---

## Task 2: Engine decider + policy

**Files:**
- Create: `internal/engine/decider.go`
- Test: `internal/engine/decider_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/decider_test.go`:
```go
package engine

import (
	"context"
	"errors"
	"testing"
)

func TestMapDeciderAnswersByID(t *testing.T) {
	d := MapDecider{"non-fast-forward": "rebase"}
	resp, err := d.Decide(context.Background(), DecisionRequest{ID: "non-fast-forward"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Option != "rebase" {
		t.Fatalf("option = %q, want rebase", resp.Option)
	}
}

func TestMapDeciderUnansweredReturnsErrDecisionRequired(t *testing.T) {
	d := MapDecider{}
	_, err := d.Decide(context.Background(), DecisionRequest{ID: "x"})
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("err = %v, want ErrDecisionRequired", err)
	}
}

func TestDeciderFuncAdapts(t *testing.T) {
	d := DeciderFunc(func(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
		return DecisionResponse{Option: req.Options[0]}, nil
	})
	resp, err := d.Decide(context.Background(), DecisionRequest{Options: []string{"abort", "merge"}})
	if err != nil || resp.Option != "abort" {
		t.Fatalf("resp=%v err=%v, want option abort", resp, err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run 'Decider|MapDecider'`
Expected: FAIL — undefined `MapDecider`, `DeciderFunc`, `DecisionRequest`, `ErrDecisionRequired`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/decider.go`:
```go
package engine

import (
	"context"
	"errors"
	"fmt"
)

// DecisionRequest describes a fork an operation cannot resolve on its own.
type DecisionRequest struct {
	ID      string   // stable key, e.g. "non-fast-forward"
	Prompt  string   // human-readable question
	Options []string // allowed answers, e.g. ["rebase","merge","abort"]
}

// DecisionResponse is the chosen option.
type DecisionResponse struct{ Option string }

// Decider resolves a DecisionRequest. A TUI prompts a human; a headless/MCP
// caller uses a MapDecider seeded with pre-answers.
type Decider interface {
	Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error)
}

// ErrDecisionRequired is returned when a fork has no available answer. A
// non-blocking (MCP) caller surfaces this so the agent can re-invoke with an
// added answer rather than hanging.
var ErrDecisionRequired = errors.New("engine: decision required but no decider/answer available")

// MapDecider answers decisions from a fixed ID->option map (the "policy").
type MapDecider map[string]string

func (m MapDecider) Decide(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
	if opt, ok := m[req.ID]; ok {
		return DecisionResponse{Option: opt}, nil
	}
	return DecisionResponse{}, fmt.Errorf("%w: %s", ErrDecisionRequired, req.ID)
}

// DeciderFunc adapts a function to the Decider interface.
type DeciderFunc func(ctx context.Context, req DecisionRequest) (DecisionResponse, error)

func (f DeciderFunc) Decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	return f(ctx, req)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run 'Decider|MapDecider'`
Expected: PASS (the engine package still won't fully build until Task 3 adds `Result`; if so, run `go vet ./internal/engine/` after Task 3).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/decider.go internal/engine/decider_test.go
git commit -m "feat: add engine Decider, MapDecider, and DeciderFunc"
```

---

## Task 3: Engine Operation / OpDeps / Result

**Files:**
- Create: `internal/engine/operation.go`
- Test: `internal/engine/operation_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/operation_test.go`:
```go
package engine

import (
	"context"
	"testing"
)

// fakeOp emits a Progress then a Done, and asks a decision.
type fakeOp struct{}

func (fakeOp) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "working"})
	resp, err := deps.decide(ctx, DecisionRequest{ID: "go?", Options: []string{"yes", "no"}})
	if err != nil {
		return Result{}, err
	}
	res := Result{Summary: "did " + resp.Option, Changed: resp.Option == "yes"}
	deps.emit(Done{Result: res})
	return res, nil
}

func TestOpDepsEmitAndDecide(t *testing.T) {
	ch := make(chan Event, 8)
	deps := OpDeps{
		Events:  ch,
		Decider: MapDecider{"go?": "yes"},
	}
	res, err := fakeOp{}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Summary != "did yes" || !res.Changed {
		t.Fatalf("result = %+v, want did yes / changed", res)
	}
	close(ch)
	var kinds []string
	for e := range ch {
		switch e.(type) {
		case Progress:
			kinds = append(kinds, "progress")
		case Done:
			kinds = append(kinds, "done")
		}
	}
	if len(kinds) != 2 || kinds[0] != "progress" || kinds[1] != "done" {
		t.Fatalf("events = %v, want [progress done]", kinds)
	}
}

func TestOpDepsEmitNilChannelDoesNotPanic(t *testing.T) {
	deps := OpDeps{} // nil Events, nil Decider
	deps.emit(Progress{Step: "x"})
	_, err := deps.decide(context.Background(), DecisionRequest{ID: "y"})
	if err == nil {
		t.Fatal("decide with nil Decider should return an error")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/`
Expected: FAIL — undefined `OpDeps`, `Result`, `emit`, `decide`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/operation.go`:
```go
package engine

import (
	"context"

	"github.com/gigagit/gg/internal/git"
)

// Result is the outcome of an operation.
type Result struct {
	Summary string
	Changed bool
}

// OpDeps is everything an operation needs: the repo to act on, an optional
// event channel, and an optional Decider for mid-flight forks.
type OpDeps struct {
	Repo    *git.Repo
	Events  chan<- Event
	Decider Decider
}

// emit sends an event if a channel is configured; a nil channel is a no-op.
func (d OpDeps) emit(e Event) {
	if d.Events != nil {
		d.Events <- e
	}
}

// decide resolves a fork. With no Decider it returns ErrDecisionRequired so a
// non-blocking caller never hangs. It also emits a DecisionNeeded event so
// passive observers can see the prompt.
func (d OpDeps) decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	d.emit(DecisionNeeded{Request: req})
	if d.Decider == nil {
		return DecisionResponse{}, ErrDecisionRequired
	}
	return d.Decider.Decide(ctx, req)
}

// Operation is a long-running, cancellable git workflow driven via OpDeps.
type Operation interface {
	Run(ctx context.Context, deps OpDeps) (Result, error)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ && go vet ./internal/engine/`
Expected: PASS (all engine tests from Tasks 1–3 now compile and pass).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/operation.go internal/engine/operation_test.go
git commit -m "feat: add engine Operation, OpDeps, and Result"
```

---

## Task 4: Git mutating verbs — commit, switch, create-branch

**Files:**
- Create: `internal/git/mutate.go`
- Test: `internal/git/mutate_test.go`

These verbs are thin: build argv, run via the Runner, return error. Tests use the existing `newTestRepo(t)` helper (same package).

- [ ] **Step 1: Write the failing test**

Create `internal/git/mutate_test.go`:
```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestCommitAllAndCurrentBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second", true); err != nil {
		t.Fatalf("commit: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Staged+c.Unstaged != 0 {
		t.Fatalf("expected clean tree after commit -a, got %+v", c)
	}
}

func TestCreateBranchAndSwitch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
		t.Fatalf("create branch: %v", err)
	}
	if err := repo.Switch(context.Background(), "feature"); err != nil {
		t.Fatalf("switch: %v", err)
	}
	cur, err := repo.CurrentBranch(context.Background())
	if err != nil {
		t.Fatalf("current branch: %v", err)
	}
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestCommit|TestCreateBranch'`
Expected: FAIL — undefined `Commit`, `CreateBranch`, `Switch`, `CurrentBranch`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/mutate.go`:
```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Commit records staged changes. When all is true, modified/deleted tracked
// files are staged first (commit -a).
func (r *Repo) Commit(ctx context.Context, message string, all bool) error {
	argv := gitcmd.New("commit").ArgIf(all, "-a").Arg("-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git commit", argv)
	return err
}

// Switch checks out an existing branch.
func (r *Repo) Switch(ctx context.Context, branch string) error {
	argv := gitcmd.New("switch").Arg(branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch", argv)
	return err
}

// CreateBranch creates a new branch at HEAD without switching to it.
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("branch").Arg(name).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch", argv)
	return err
}

// CurrentBranch returns the checked-out branch name, or "" if detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	argv := gitcmd.New("symbolic-ref").Arg("--quiet", "--short", "HEAD").ToArgv()
	res, err := r.Runner.Run(ctx, "git symbolic-ref", argv)
	if err != nil {
		// Detached HEAD: symbolic-ref exits non-zero. Treat as no branch.
		return "", nil
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/`
Expected: PASS (new + all existing git tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/mutate.go internal/git/mutate_test.go
git commit -m "feat: add commit/switch/create-branch/current-branch verbs"
```

---

## Task 5: Git sync verbs — fetch, ff-only pull, fast-forward-ref, push

**Files:**
- Create: `internal/git/sync.go`
- Test: `internal/git/sync_test.go`

Push/pull need a remote, so the test builds a bare remote + a clone with a helper.

- [ ] **Step 1: Write the failing test**

Create `internal/git/sync_test.go`:
```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// gitIn runs a raw git command in dir for test setup.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// newClonePair creates a bare remote with one commit on main, then clones it.
// Returns the clone working dir and a Runner scoped to it.
func newClonePair(t *testing.T) (string, gitexec.Runner) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	if err := os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	return clone, gitexec.NewExecRunner("git", clone, observ.NewRing(50))
}

func TestFetchAndPullFFOnly(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := repo.Fetch(context.Background(), "origin"); err != nil {
		t.Fatalf("fetch: %v", err)
	}
	// ff-only pull on an up-to-date branch should succeed (no-op).
	if err := repo.PullFFOnly(context.Background(), "origin", "main"); err != nil {
		t.Fatalf("pull ff-only: %v", err)
	}
	_ = clone
}

func TestPushPropagatesCommit(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	if err := os.WriteFile(filepath.Join(clone, "f.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "v2", true); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if err := repo.Push(context.Background(), "origin", "main", false); err != nil {
		t.Fatalf("push: %v", err)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestFetch|TestPush'`
Expected: FAIL — undefined `Fetch`, `PullFFOnly`, `Push` (and `FastForwardRef`, used in Task signature though not directly in these two tests).

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/sync.go`:
```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Fetch updates remote-tracking refs from remote. --no-write-fetch-head allows
// a concurrent pull without FETCH_HEAD contention.
func (r *Repo) Fetch(ctx context.Context, remote string) error {
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch", argv)
	return err
}

// PullFFOnly fetches and fast-forwards the current branch only; it never
// creates a merge commit. Fails if the remote is not a fast-forward.
func (r *Repo) PullFFOnly(ctx context.Context, remote, branch string) error {
	argv := gitcmd.New("pull").Arg("--no-edit", "--ff-only", remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git pull --ff-only", argv)
	return err
}

// FastForwardRef updates a NON-checked-out local branch to its remote tip
// without a checkout, via a fetch refspec (origin branch:branch). Fails if the
// update would not be a fast-forward.
func (r *Repo) FastForwardRef(ctx context.Context, remote, branch string) error {
	refspec := branch + ":" + branch
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote, refspec).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch (ff-ref)", argv)
	return err
}

// Push pushes branch to remote. When setUpstream is true it records the
// upstream tracking ref (-u).
func (r *Repo) Push(ctx context.Context, remote, branch string, setUpstream bool) error {
	argv := gitcmd.New("push").ArgIf(setUpstream, "-u").Arg(remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push", argv)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/sync.go internal/git/sync_test.go
git commit -m "feat: add fetch/pull-ff-only/fast-forward-ref/push verbs"
```

---

## Task 6: Git stash verbs — push, pop, list

**Files:**
- Create: `internal/git/stash.go`
- Test: `internal/git/stash_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/git/stash_test.go`:
```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStashPushListPop(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Make a tracked-file change so there is something to stash.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.StashPush(context.Background(), "gg-test"); err != nil {
		t.Fatalf("stash push: %v", err)
	}
	// Tree should now be clean.
	st, _ := repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 0 {
		t.Fatalf("expected clean tree after stash, got %+v", c)
	}
	list, err := repo.StashList(context.Background())
	if err != nil {
		t.Fatalf("stash list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("stash list = %v, want 1 entry", list)
	}
	if err := repo.StashPop(context.Background()); err != nil {
		t.Fatalf("stash pop: %v", err)
	}
	st, _ = repo.Status(context.Background())
	if c := st.Counts(); c.Unstaged != 1 {
		t.Fatalf("expected change restored after pop, got %+v", c)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestStash`
Expected: FAIL — undefined `StashPush`, `StashList`, `StashPop`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/stash.go`:
```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StashPush saves the working tree and index to a new stash with the given
// message, leaving a clean tree.
func (r *Repo) StashPush(ctx context.Context, message string) error {
	argv := gitcmd.New("stash").Arg("push", "-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git stash push", argv)
	return err
}

// StashPop restores the most recent stash and drops it. A conflict leaves the
// stash in place and returns an error (git's behavior).
func (r *Repo) StashPop(ctx context.Context) error {
	argv := gitcmd.New("stash").Arg("pop").ToArgv()
	_, err := r.Runner.Run(ctx, "git stash pop", argv)
	return err
}

// StashList returns the stash entries, newest first (one description per line).
func (r *Repo) StashList(ctx context.Context) ([]string, error) {
	argv := gitcmd.New("stash").Arg("list").ToArgv()
	res, err := r.Runner.Run(ctx, "git stash list", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/stash.go internal/git/stash_test.go
git commit -m "feat: add stash push/pop/list verbs"
```

---

## Task 7: Git query helpers — worktree-for-branch, can-fast-forward

**Files:**
- Create: `internal/git/query.go`
- Test: `internal/git/query_test.go`

`CurrentBranch` was added in Task 4. This task adds the two queries the smart-pull tree (Plan 2B) needs: which worktree (if any) has a branch checked out, and whether one ref is an ancestor of another (fast-forward check).

- [ ] **Step 1: Write the failing test**

Create `internal/git/query_test.go`:
```go
package git

import (
	"context"
	"testing"
)

func TestWorktreeForBranchFindsMain(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt, err := repo.WorktreeForBranch(context.Background(), "main")
	if err != nil {
		t.Fatalf("worktree for branch: %v", err)
	}
	if wt == nil {
		t.Fatal("expected to find a worktree on main")
	}
	if wt.Branch != "main" {
		t.Fatalf("wt.Branch = %q, want main", wt.Branch)
	}
}

func TestWorktreeForBranchMissingReturnsNil(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt, err := repo.WorktreeForBranch(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wt != nil {
		t.Fatalf("expected nil, got %+v", wt)
	}
}

func TestCanFastForward(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// HEAD is an ancestor of itself's child after a new commit.
	first, err := repo.CurrentBranch(context.Background())
	if err != nil || first == "" {
		t.Fatalf("current branch: %q err=%v", first, err)
	}
	// Capture first commit hash.
	gitIn(t, dir, "tag", "base")
	// Add a second commit.
	gitIn(t, dir, "commit", "--allow-empty", "-m", "second")

	ff, err := repo.CanFastForward(context.Background(), "base", "HEAD")
	if err != nil {
		t.Fatalf("can-ff: %v", err)
	}
	if !ff {
		t.Fatal("base should be an ancestor of HEAD (fast-forwardable)")
	}
	ff, err = repo.CanFastForward(context.Background(), "HEAD", "base")
	if err != nil {
		t.Fatalf("can-ff reverse: %v", err)
	}
	if ff {
		t.Fatal("HEAD should NOT be an ancestor of base")
	}
}
```

(`gitIn` is defined in `sync_test.go`, same package — reuse it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestWorktreeForBranch|TestCanFastForward'`
Expected: FAIL — undefined `WorktreeForBranch`, `CanFastForward`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/query.go`:
```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// WorktreeForBranch returns the worktree that currently has branch checked out,
// or nil if none does.
func (r *Repo) WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error) {
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	for i := range wts {
		if wts[i].Branch == branch {
			return &wts[i], nil
		}
	}
	return nil, nil
}

// CanFastForward reports whether ancestor is an ancestor of descendant, i.e.
// descendant can be fast-forwarded onto / from ancestor. Uses
// `git merge-base --is-ancestor`, which exits 0 (true) or 1 (false).
func (r *Repo) CanFastForward(ctx context.Context, ancestor, descendant string) (bool, error) {
	argv := gitcmd.New("merge-base").Arg("--is-ancestor", ancestor, descendant).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base --is-ancestor", argv)
	if err == nil {
		return true, nil
	}
	// Exit code 1 means "not an ancestor" (a normal answer, not a failure).
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/git/query.go internal/git/query_test.go
git commit -m "feat: add worktree-for-branch and can-fast-forward queries"
```

---

## Task 8: Thin engine operations — Commit, Push, Stash (proves the contract)

**Files:**
- Create: `internal/engine/ops_basic.go`
- Test: `internal/engine/ops_basic_test.go`

These operations wrap the verbs and emit Progress/Done, proving the contract end-to-end against a real repo. The smart, branching operations come in Plan 2B.

- [ ] **Step 1: Write the failing test**

Create `internal/engine/ops_basic_test.go`:
```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

func newRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir, &git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

func drain(ch chan Event) []Event {
	var out []Event
	for e := range ch {
		out = append(out, e)
	}
	return out
}

func TestCommitOperationEmitsProgressAndDone(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	ch := make(chan Event, 16)
	res, err := Commit{Message: "second", All: true}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("commit op: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	events := drain(ch)
	var sawProgress, sawDone bool
	for _, e := range events {
		switch e.(type) {
		case Progress:
			sawProgress = true
		case Done:
			sawDone = true
		}
	}
	if !sawProgress || !sawDone {
		t.Fatalf("events missing progress/done: %#v", events)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestCommitOperation`
Expected: FAIL — undefined `Commit` operation.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/ops_basic.go`:
```go
package engine

import "context"

// Commit stages (optionally) and commits with a message.
type Commit struct {
	Message string
	All     bool
}

func (op Commit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "committing", Detail: op.Message})
	if err := deps.Repo.Commit(ctx, op.Message, op.All); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "committed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Push pushes branch to remote, optionally setting upstream.
type Push struct {
	Remote      string
	Branch      string
	SetUpstream bool
}

func (op Push) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Stash saves the working tree to a new stash.
type Stash struct {
	Message string
}

func (op Stash) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(Progress{Step: "stashing", Detail: op.Message})
	if err := deps.Repo.StashPush(ctx, op.Message); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "stashed", Changed: true}
	deps.emit(Done{Result: res})
	return res, nil
}

// Compile-time checks that these satisfy Operation.
var (
	_ Operation = Commit{}
	_ Operation = Push{}
	_ Operation = Stash{}
)
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ && go vet ./internal/engine/`
Expected: PASS.

- [ ] **Step 5: Run the FULL suite + build**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all tests PASS; vet clean; gofmt prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/ops_basic.go internal/engine/ops_basic_test.go
git commit -m "feat: add thin Commit/Push/Stash engine operations"
```

---

## Self-Review

**Spec coverage (against `2026-06-11-gigagit-m1-design.md`):**
- §4 engine↔frontend contract (`Operation`, `OpDeps`, `Decider`, `Event` union incl. `Timing`, `DecisionNeeded`, `Done`) → Tasks 1–3.
- §4 non-blocking MCP behavior (`ErrDecisionRequired` when no answer; `MapDecider` as the policy) → Task 2; `OpDeps.decide` → Task 3.
- §5 smart-pull *primitives* — `FastForwardRef` (step 2), `WorktreeForBranch` (step 3), `CanFastForward`, stash/switch/pull verbs → Tasks 4–7. (The decision *tree* itself is Plan 2B.)
- §3 module layout: orchestration in `engine`, thin verbs in `git` → all tasks; `git` verbs stay thin (no branching logic).
- §3.1 `--no-write-fetch-head`, fetch-refspec fast-forward → Task 5.

**Deferred to Plan 2B:** the SmartPull decision tree, SmartSwitch, conflict-on-stash-pop decision handling, ref-only Undo, and credential-prompt routing through the Decider (needs the streaming/credential path, exercised by the smart ops).

**Placeholder scan:** none — every code step has complete code. (Task 1's Step 4 intentionally documents an inter-file compile dependency rather than stubbing types; this is a real instruction, not a placeholder.)

**Type consistency:** `OpDeps{Repo,Events,Decider}` + `emit`/`decide` consistent across Tasks 3 and 8; `Decider`/`DecisionRequest`/`DecisionResponse`/`ErrDecisionRequired` consistent across Tasks 2, 3, 8; verb signatures on `*git.Repo` (Tasks 4–7) match the operations' calls (Task 8) and the Shared Interfaces block.

**Note on Task ordering:** Tasks 1–3 form one compilable unit (the engine package only builds once all three files exist). When executing, treat a red/!compiles result for Task 1 in isolation as expected; the package goes green at the end of Task 3.

---

## Plan sequence (M1)

1. Plan 1 — Foundation & read-only inspection ✅ (merged).
2. **Plan 2A — Engine contract & git operation primitives** (this document).
3. Plan 2B — Smart operations: SmartPull (§5 tree), SmartSwitch, stash-pop conflict handling, ref-only Undo, credential routing.
4. Plan 3 — TUI (Bubble Tea) on top of the engine.
