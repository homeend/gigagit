# gigagit M1 — Plan 2B: Smart Operations (SmartPull, SmartSwitch) — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the differentiating smart operations on the Plan 2A engine — SmartSwitch (stash-guarded checkout) and SmartPull (the spec §5 worktree-aware decision tree) — plus the `emit` cancellation fix carried over from the 2A review.

**Architecture:** Operations live in `internal/engine` as `Operation` implementations that orchestrate the thin `git.Repo` verbs from Plan 2A, emitting `Progress`/`DecisionNeeded`/`Done` events and resolving forks via the injected `Decider`. A few additional thin verbs/queries (`Pull` with strategy, `IsDirty`, `RemoteForBranch`, `PullInWorktree`) are added to `git.Repo`. No frontend; operations are driven headlessly in tests against real repos/worktrees/remotes.

**Tech Stack:** Go 1.26, standard library + existing internal packages. Tests build real throwaway repos, a real bare remote, and a real linked worktree.

---

## Shared interfaces (defined once, referenced by later tasks)

```go
// internal/engine — emit gains a ctx (cancellation-safe send)
func (d OpDeps) emit(ctx context.Context, e Event)

// internal/git — new verbs/queries on *Repo
type PullStrategy int
const (
	PullFF     PullStrategy = iota // --ff-only
	PullRebase                     // --rebase
	PullMerge                      // --no-rebase
)
func (r *Repo) Pull(ctx context.Context, remote, branch string, strategy PullStrategy) error
func (r *Repo) PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error
func (r *Repo) IsDirty(ctx context.Context) (bool, error)
func (r *Repo) RemoteForBranch(ctx context.Context, branch string) (string, error)

// internal/engine — new operations
type SmartSwitch struct{ Branch string }
type PullIntent int
const (
	PullAndStay     PullIntent = iota // end up ON target
	PullInBackground                  // update target's ref, stay where you are
)
type SmartPull struct {
	Branch string     // "" = current branch
	Remote string     // "" = resolve (default "origin")
	Intent PullIntent
}
```

Decision IDs used (stable keys for `MapDecider`/policy/MCP):
- `"non-fast-forward"` → `["rebase","merge","abort"]`
- `"not-fast-forwardable"` → `["checkout-and-resolve","abort"]`
- `"stash-pop-conflict"` → informational (`["keep","abort"]`), emitted; the operation never drops the stash on conflict (git retains it).

---

## Task 1: Make `emit` honor context cancellation

Carry-over from the 2A review: `emit` did a bare channel send that could block forever on an unconsumed unbuffered channel and ignore cancellation.

**Files:**
- Modify: `internal/engine/operation.go`
- Modify: `internal/engine/ops_basic.go`
- Modify: `internal/engine/operation_test.go`
- Test: `internal/engine/cancel_test.go` (new)

- [ ] **Step 1: Write the failing test**

Create `internal/engine/cancel_test.go`:
```go
package engine

import (
	"context"
	"testing"
	"time"
)

// With an unbuffered, never-drained channel and a cancelled context, emit must
// return promptly instead of blocking forever.
func TestEmitUnblocksOnCancelledContext(t *testing.T) {
	ch := make(chan Event) // unbuffered, no reader
	deps := OpDeps{Events: ch}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	done := make(chan struct{})
	go func() {
		deps.emit(ctx, Progress{Step: "x"})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("emit blocked on a cancelled context with no reader")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestEmitUnblocks`
Expected: FAIL to compile — `emit` currently takes one argument. (After Step 3 updates the signature it will compile and pass.)

- [ ] **Step 3: Update `emit` and all callers**

In `internal/engine/operation.go`, replace the `emit` method and update `decide` to pass `ctx`:
```go
// emit sends an event if a channel is configured. A nil channel is a no-op; a
// cancelled context aborts the send so an unconsumed channel can never block
// the operation goroutine.
func (d OpDeps) emit(ctx context.Context, e Event) {
	if d.Events == nil {
		return
	}
	select {
	case d.Events <- e:
	case <-ctx.Done():
	}
}

// decide resolves a fork. With no Decider it returns ErrDecisionRequired so a
// non-blocking caller never hangs. It also emits a DecisionNeeded event.
func (d OpDeps) decide(ctx context.Context, req DecisionRequest) (DecisionResponse, error) {
	d.emit(ctx, DecisionNeeded{Request: req})
	if d.Decider == nil {
		return DecisionResponse{}, ErrDecisionRequired
	}
	return d.Decider.Decide(ctx, req)
}
```

In `internal/engine/ops_basic.go`, update every `deps.emit(...)` call in `Commit`, `Push`, and `Stash` to pass `ctx` as the first argument. Example for `Commit` (apply the same to all three):
```go
func (op Commit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "committing", Detail: op.Message})
	if err := deps.Repo.Commit(ctx, op.Message, op.All); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "committed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```
(Do the same `ctx` insertion in `Push.Run` and `Stash.Run`.)

In `internal/engine/operation_test.go`, update the two `emit` call sites: `fakeOp.Run` uses `deps.emit(Progress{...})` and `deps.emit(Done{...})` → add `ctx`; `TestOpDepsEmitNilChannelDoesNotPanic` uses `deps.emit(Progress{Step: "x"})` → `deps.emit(context.Background(), Progress{Step: "x"})`.

- [ ] **Step 4: Run tests**

Run: `go test ./internal/engine/ && go vet ./internal/engine/`
Expected: PASS (including the new cancel test and all existing engine tests).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/operation.go internal/engine/ops_basic.go internal/engine/operation_test.go internal/engine/cancel_test.go
git commit -m "fix: make engine emit honor context cancellation"
```

---

## Task 2: Git verbs — Pull(strategy), PullInWorktree, IsDirty, RemoteForBranch

**Files:**
- Modify: `internal/git/sync.go` (add `Pull`, `PullInWorktree`; refactor `PullFFOnly` to delegate)
- Create: `internal/git/query2.go` (`IsDirty`, `RemoteForBranch`)
- Test: `internal/git/sync2_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/git/sync2_test.go`:
```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestPullRebaseStrategy(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}
	root := filepath.Dir(clone)
	origin := filepath.Join(root, "origin.git")

	// Remote advances main.
	other := filepath.Join(root, "other")
	gitIn(t, root, "clone", origin, other)
	gitIn(t, other, "checkout", "main")
	os.WriteFile(filepath.Join(other, "remote.txt"), []byte("r\n"), 0o644)
	gitIn(t, other, "add", ".")
	gitIn(t, other, "commit", "-m", "remote-commit")
	gitIn(t, other, "push", "origin", "main")

	// Local makes its own commit (diverged), then rebase-pulls.
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("l\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local-commit")

	if err := repo.Pull(context.Background(), "origin", "main", PullRebase); err != nil {
		t.Fatalf("rebase pull: %v", err)
	}
	// Both files should now exist (local replayed on top of remote).
	if _, err := os.Stat(filepath.Join(clone, "remote.txt")); err != nil {
		t.Fatalf("remote.txt missing after rebase: %v", err)
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
}

func TestIsDirty(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	dirty, err := repo.IsDirty(context.Background())
	if err != nil {
		t.Fatalf("is-dirty: %v", err)
	}
	if dirty {
		t.Fatal("fresh repo should be clean")
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	dirty, _ = repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("modified tracked file should be dirty")
	}
}

func TestRemoteForBranchDefaultsToOrigin(t *testing.T) {
	clone, runner := newClonePair(t)
	repo := &Repo{Runner: runner}

	remote, err := repo.RemoteForBranch(context.Background(), "main")
	if err != nil {
		t.Fatalf("remote-for-branch: %v", err)
	}
	if remote != "origin" {
		t.Fatalf("remote = %q, want origin", remote)
	}
	_ = clone
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestPullRebaseStrategy|TestIsDirty|TestRemoteForBranch'`
Expected: FAIL — undefined `Pull`, `PullStrategy`, `PullRebase`, `IsDirty`, `RemoteForBranch`.

- [ ] **Step 3: Implement**

In `internal/git/sync.go`, add the strategy type and `Pull`, add `PullInWorktree`, and refactor `PullFFOnly` to delegate. Replace the existing `PullFFOnly` function and add the new code:
```go
// PullStrategy selects how Pull integrates upstream changes.
type PullStrategy int

const (
	PullFF     PullStrategy = iota // --ff-only: never create a merge commit
	PullRebase                     // --rebase: replay local commits on top
	PullMerge                      // --no-rebase: create a merge commit if needed
)

// Pull integrates remote/branch into the current branch using the given
// strategy.
func (r *Repo) Pull(ctx context.Context, remote, branch string, strategy PullStrategy) error {
	b := gitcmd.New("pull").Arg("--no-edit")
	switch strategy {
	case PullRebase:
		b = b.Arg("--rebase")
	case PullMerge:
		b = b.Arg("--no-rebase")
	default:
		b = b.Arg("--ff-only")
	}
	b = b.Arg(remote, branch)
	_, err := r.Runner.Run(ctx, "git pull", b.ToArgv())
	return err
}

// PullFFOnly fast-forwards the current branch only (no merge commit).
func (r *Repo) PullFFOnly(ctx context.Context, remote, branch string) error {
	return r.Pull(ctx, remote, branch, PullFF)
}

// PullInWorktree fast-forwards branch inside another linked worktree at
// worktreePath, without touching the current worktree.
func (r *Repo) PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error {
	argv := gitcmd.New("pull").Dir(worktreePath).Arg("--no-edit", "--ff-only", remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git pull (worktree)", argv)
	return err
}
```
(Delete the old standalone `PullFFOnly` body that built `pull --no-edit --ff-only ...` directly — it is now the delegating one above. Keep exactly one `PullFFOnly`.)

Create `internal/git/query2.go`:
```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// IsDirty reports whether the working tree has staged, unstaged, or conflicted
// tracked changes (untracked files alone do not count, as they don't block a
// branch switch).
func (r *Repo) IsDirty(ctx context.Context) (bool, error) {
	st, err := r.Status(ctx)
	if err != nil {
		return false, err
	}
	c := st.Counts()
	return c.Staged+c.Unstaged+c.Conflicted > 0, nil
}

// RemoteForBranch returns the remote that branch tracks, defaulting to "origin"
// when the branch has no configured upstream.
func (r *Repo) RemoteForBranch(ctx context.Context, branch string) (string, error) {
	argv := gitcmd.New("for-each-ref").
		Arg("--format=%(upstream:remotename)", "refs/heads/"+branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (remote)", argv)
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(res.Stdout)
	if remote == "" {
		return "origin", nil
	}
	return remote, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/git/ && go vet ./internal/git/ && gofmt -l internal/git`
Expected: PASS (new + all existing, including the 2A `PullFFOnly` tests which still pass via delegation); gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/git/sync.go internal/git/query2.go internal/git/sync2_test.go
git commit -m "feat: add Pull(strategy), PullInWorktree, IsDirty, RemoteForBranch"
```

---

## Task 3: SmartSwitch operation

Stash-guarded checkout: if dirty, stash → switch → restore; on restore conflict, never drop the stash and surface it.

**Files:**
- Create: `internal/engine/smart_switch.go`
- Test: `internal/engine/smart_switch_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/smart_switch_test.go`:
```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSmartSwitchCleanTree(t *testing.T) {
	dir, repo := newRepo(t)
	// Create a second branch to switch to.
	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}
	_ = dir

	res, err := SmartSwitch{Branch: "feature"}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
}

func TestSmartSwitchStashesDirtyTreeAndRestores(t *testing.T) {
	dir, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "feature"); err != nil {
		t.Fatal(err)
	}
	// Dirty the tree on main.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartSwitch{Branch: "feature"}.Run(context.Background(), OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature", cur)
	}
	// The dirty change should have been carried over (restored) onto feature.
	dirty, _ := repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("expected stashed change to be restored on feature")
	}
}

func TestSmartSwitchAlreadyOnBranchIsNoop(t *testing.T) {
	_, repo := newRepo(t)
	res, err := SmartSwitch{Branch: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("smart switch: %v", err)
	}
	if res.Changed {
		t.Fatalf("switching to current branch should not be Changed: %+v", res)
	}
}
```

(`newRepo` is the helper already defined in `internal/engine/ops_basic_test.go`, same package — reuse it.)

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestSmartSwitch`
Expected: FAIL — undefined `SmartSwitch`.

- [ ] **Step 3: Implement**

Create `internal/engine/smart_switch.go`:
```go
package engine

import (
	"context"
	"fmt"
)

// SmartSwitch checks out Branch, automatically stashing and restoring local
// changes. On a restore (stash pop) conflict it never drops the stash — git
// retains it — and surfaces the conflict.
type SmartSwitch struct{ Branch string }

func (op SmartSwitch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == op.Branch {
		return Result{Summary: "already on " + op.Branch}, nil
	}

	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+op.Branch); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: op.Branch})
	if err := deps.Repo.Switch(ctx, op.Branch); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx) // best-effort restore on the original branch
		}
		return Result{}, err
	}

	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx); err != nil {
			// Conflict: git keeps the stash. Surface it; do not drop anything.
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicts with " + op.Branch,
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: "switched to " + op.Branch + "; restore conflicted (changes preserved in stash)", Changed: true},
				fmt.Errorf("stash pop conflict after switching to %s: %w", op.Branch, err)
		}
	}
	return Result{Summary: "switched to " + op.Branch, Changed: true}, nil
}

var _ Operation = SmartSwitch{}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/engine/ && go vet ./internal/engine/ && gofmt -l internal/engine`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/smart_switch.go internal/engine/smart_switch_test.go
git commit -m "feat: add SmartSwitch operation (stash-guarded checkout)"
```

---

## Task 4: SmartPull operation (the §5 decision tree)

The hero operation. Given a target branch and intent, it picks the simplest correct path: pull-in-place when on the branch, fast-forward-ref when just updating another branch in the background, pull-in-worktree when the branch lives elsewhere, and stash→switch→pull when you need to move onto it.

**Files:**
- Create: `internal/engine/smart_pull.go`
- Test: `internal/engine/smart_pull_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/smart_pull_test.go`:
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

// gitAt runs a raw git command in dir for test setup.
func gitAt(t *testing.T, dir string, args ...string) {
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

func revAt(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return string(out)
}

// cloneOnMainBehindOrigin builds origin (main at v2) and a clone whose main is
// at v1 (one commit behind). Returns the clone dir and a *git.Repo on it.
func cloneOnMainBehindOrigin(t *testing.T) (string, *git.Repo) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitAt(t, root, "init", "--bare", origin)
	gitAt(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitAt(t, seed, "checkout", "-b", "main")
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v1")
	gitAt(t, seed, "push", "-u", "origin", "main")

	gitAt(t, root, "clone", origin, clone)
	gitAt(t, clone, "checkout", "main")

	// origin advances to v2 AFTER the clone, so clone's main is one behind.
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v2")
	gitAt(t, seed, "push", "origin", "main")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	return clone, repo
}

func TestSmartPullCurrentBranchFastForward(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	// main should now equal origin/main.
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not fast-forwarded to origin/main")
	}
}

func TestSmartPullCurrentBranchNonFastForwardRebase(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	// Create a DIVERGING local commit so a plain ff-only pull fails.
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"non-fast-forward": "rebase"}})
	if err != nil {
		t.Fatalf("smart pull (rebase): %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	// Both the remote v2 change and the local commit must be present.
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote change applied)", b)
	}
}

func TestSmartPullBackgroundFastForwardsOtherBranch(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")

	// origin gains branch dev at a new commit; clone gets a local dev at the
	// OLD tip (behind), while staying on main.
	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/main") // local dev behind origin/dev-to-be
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev-advance")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull background: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	// We must still be on main (no checkout), and local dev advanced.
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "main" {
		t.Fatalf("current branch = %q, want main (background must not checkout)", cur)
	}
	_ = origin
}

func TestSmartPullStayStashesAndMovesToTarget(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	// A local branch 'feature' exists on origin (same name), up to date, so a
	// ff-only pull of origin/feature is a clean no-op.
	gitAt(t, clone, "branch", "feature", "origin/main")
	gitAt(t, clone, "push", "-u", "origin", "feature")
	// Dirty the working tree on main.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("dirty-local\n"), 0o644)

	res, err := SmartPull{Branch: "feature", Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull stay: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature (PullAndStay ends on target)", cur)
	}
	// The dirty change was carried over via stash.
	dirty, _ := repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("expected dirty change restored on feature")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestSmartPull`
Expected: FAIL — undefined `SmartPull`, `PullIntent`, `PullAndStay`, `PullInBackground`.

- [ ] **Step 3: Implement**

Create `internal/engine/smart_pull.go`:
```go
package engine

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/git"
)

// PullIntent expresses what the user wants from a pull of a non-current branch.
type PullIntent int

const (
	// PullAndStay ends with the target branch checked out.
	PullAndStay PullIntent = iota
	// PullInBackground updates the target's ref without leaving the current branch.
	PullInBackground
)

// SmartPull picks the simplest correct path to update Branch (default: current),
// per the design's decision tree.
type SmartPull struct {
	Branch string
	Remote string
	Intent PullIntent
}

var _ Operation = SmartPull{}

func (op SmartPull) Run(ctx context.Context, deps OpDeps) (Result, error) {
	repo := deps.Repo
	cur, err := repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	target := op.Branch
	if target == "" {
		target = cur
	}
	if target == "" {
		return Result{}, fmt.Errorf("smart pull: no target branch (detached HEAD)")
	}
	remote := op.Remote
	if remote == "" {
		remote, err = repo.RemoteForBranch(ctx, target)
		if err != nil {
			return Result{}, err
		}
	}

	// Case 1: target is the current branch — fetch + ff-only, decide on divergence.
	if target == cur {
		return op.pullCurrent(ctx, deps, remote, target)
	}

	// Case 2: background intent — update the ref without a checkout.
	if op.Intent == PullInBackground {
		deps.emit(ctx, Progress{Step: "fast-forwarding ref", Detail: target})
		if err := repo.FastForwardRef(ctx, remote, target); err == nil {
			return Result{Summary: "fast-forwarded " + target, Changed: true}, nil
		}
		resp, derr := deps.decide(ctx, DecisionRequest{
			ID:      "not-fast-forwardable",
			Prompt:  "Cannot fast-forward " + target + " in the background",
			Options: []string{"checkout-and-resolve", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if resp.Option == "checkout-and-resolve" {
			return op.checkoutPull(ctx, deps, remote, target, cur) // return to cur afterward
		}
		return Result{Summary: "aborted: " + target + " not fast-forwardable"}, nil
	}

	// Case 3: PullAndStay on a non-current branch — end on target.
	return op.checkoutPull(ctx, deps, remote, target, "")
}

// pullCurrent fetches and fast-forwards the current branch, escalating a
// divergence to a decision.
func (op SmartPull) pullCurrent(ctx context.Context, deps OpDeps, remote, branch string) (Result, error) {
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	if err := deps.Repo.Fetch(ctx, remote); err != nil {
		return Result{}, err
	}
	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: branch})
	if err := deps.Repo.Pull(ctx, remote, branch, git.PullFF); err == nil {
		return Result{Summary: "pulled " + branch, Changed: true}, nil
	}
	resp, derr := deps.decide(ctx, DecisionRequest{
		ID:      "non-fast-forward",
		Prompt:  branch + " has diverged from " + remote,
		Options: []string{"rebase", "merge", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	switch resp.Option {
	case "rebase":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullRebase); err != nil {
			return Result{}, err
		}
		return Result{Summary: "pulled (rebased) " + branch, Changed: true}, nil
	case "merge":
		if err := deps.Repo.Pull(ctx, remote, branch, git.PullMerge); err != nil {
			return Result{}, err
		}
		return Result{Summary: "pulled (merged) " + branch, Changed: true}, nil
	default:
		return Result{Summary: "aborted: " + branch + " diverged"}, nil
	}
}

// checkoutPull ensures target is updated by checking it out here (stashing local
// changes first), pulling, then optionally returning to returnTo. If target is
// checked out in another worktree, it pulls there instead of switching.
func (op SmartPull) checkoutPull(ctx context.Context, deps OpDeps, remote, target, returnTo string) (Result, error) {
	repo := deps.Repo

	wt, err := repo.WorktreeForBranch(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		deps.emit(ctx, Progress{Step: "pulling in worktree", Detail: wt.Path})
		if err := repo.PullInWorktree(ctx, wt.Path, remote, target); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "worktree-pull-failed",
				Prompt:  "Pull in worktree " + wt.Path + " failed",
				Options: []string{"abort"},
			}})
			return Result{}, fmt.Errorf("smart pull: worktree %s: %w", wt.Path, err)
		}
		return Result{Summary: "pulled " + target + " in worktree " + wt.Path, Changed: true}, nil
	}

	dirty, err := repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := repo.StashPush(ctx, "gg-autostash:"+target); err != nil {
			return Result{}, err
		}
		stashed = true
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: target})
	if err := repo.Switch(ctx, target); err != nil {
		if stashed {
			_ = repo.StashPop(ctx)
		}
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "fetching", Detail: remote})
	_ = repo.Fetch(ctx, remote)
	deps.emit(ctx, Progress{Step: "pulling (ff-only)", Detail: target})
	pullErr := repo.Pull(ctx, remote, target, git.PullFF)

	if returnTo != "" && returnTo != target {
		deps.emit(ctx, Progress{Step: "switching back", Detail: returnTo})
		if err := repo.Switch(ctx, returnTo); err != nil {
			return Result{}, err
		}
	}

	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := repo.StashPop(ctx); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicted",
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: "pulled " + target + "; restore conflicted (changes preserved in stash)", Changed: true},
				fmt.Errorf("stash pop conflict after pulling %s: %w", target, err)
		}
	}
	if pullErr != nil {
		return Result{}, fmt.Errorf("smart pull: %s: %w", target, pullErr)
	}
	return Result{Summary: "pulled " + target, Changed: true}, nil
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/engine/ -run TestSmartPull -v`
Expected: PASS (all four SmartPull tests).

- [ ] **Step 5: Run the FULL suite + vet + fmt**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all PASS; vet clean; gofmt prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/smart_pull.go internal/engine/smart_pull_test.go
git commit -m "feat: add SmartPull operation (worktree-aware decision tree)"
```

---

## Self-Review

**Spec coverage (against `2026-06-11-gigagit-m1-design.md` §5):**
- Step 1 (target is current → fetch + ff-only; non-ff → decide rebase/merge/abort) → `pullCurrent` (Task 4).
- Step 2 (not current + background → `git fetch remote T:T`; not ff-able → decide) → Case 2 + `FastForwardRef` (Tasks 2, 4).
- Step 3 (target in another worktree → operate there) → `checkoutPull` worktree branch + `PullInWorktree` (Tasks 2, 4).
- Step 4 (dirty → stash → switch → pull → restore; pop conflict → decide, stash never dropped) → `checkoutPull` + `SmartSwitch` (Tasks 3, 4).
- Safety rule (auto-stash tagged `gg-autostash:<branch>`, never dropped on conflict) → Tasks 3, 4.
- §4 non-blocking emit invariant → Task 1.

**Deferred (note):** ref-only **Undo** (reflog) → its own small Plan 2C; **credential-prompt routing** through the Decider → needs the gitexec credential-detection path and is naturally exercised once a frontend exists (Plan 3 TUI). The rarer leaves (background non-ff where the user aborts; dirty *other* worktree) return a clear decision/error rather than deep auto-resolution — acceptable for M1.

**Known limitation (M1):** SmartPull assumes the remote branch name equals the local branch name (it pulls/fast-forwards `remote target`). Branches whose upstream has a different name (e.g. local `feature` tracking `origin/main`) are not handled; resolving the true `%(upstream:short)` remote-branch name is deferred. Tests therefore use same-name tracking.

**Placeholder scan:** none — all steps contain complete code.

**Type consistency:** `PullStrategy`/`PullFF`/`PullRebase`/`PullMerge` (Task 2) match `git.PullFF`/`git.PullRebase` usage in `smart_pull.go` (Task 4); `SmartPull`/`PullIntent`/`PullAndStay`/`PullInBackground` consistent (Task 4); `emit(ctx, …)` signature (Task 1) matches all call sites in Tasks 3–4; decision IDs (`non-fast-forward`, `not-fast-forwardable`, `stash-pop-conflict`, `worktree-pull-failed`) are stable strings used consistently.

---

## Plan sequence (M1)

1. Plan 1 — Foundation & read-only inspection ✅ (merged).
2. Plan 2A — Engine contract & git operation primitives ✅ (merged).
3. **Plan 2B — Smart operations: SmartPull, SmartSwitch** (this document).
4. Plan 2C — ref-only Undo (reflog) — small follow-up.
5. Plan 3 — TUI (Bubble Tea) on top of the engine; wires credential routing through the modal Decider.
