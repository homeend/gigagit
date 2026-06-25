# Smart-push rejection recovery — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn a plain push rejected for being behind the remote into a one-keypress guided recovery (rebase / force / abort) instead of a dead-end error.

**Architecture:** Augment the existing `engine.Push` op (no new op, no new keybinding). On a non-force push whose error is classified as a non-fast-forward rejection, raise a new `push-rejected` decision and act on it, reusing the existing `Pull` verb, the existing `rebase-conflict` decision, and the existing `push-force` decision. A new pure `internal/pusherr` package holds the rejection-signature strings as the single source of truth shared by the engine trigger and the TUI status-bar message.

**Tech Stack:** Go 1.26, Bubble Tea TUI, `FakeRunner`/`MapDecider` for engine unit tests, real `git` in `t.TempDir()` for the integration test.

## Global Constraints

- Module path: `github.com/homeend/gigagit`. Go 1.26.
- **`internal/tui` and `internal/cli` never import `internal/git`** (archtest-guarded). The shared classifier therefore lives in a new pure package `internal/pusherr` that both `internal/engine` and `internal/tui` may import.
- **Operations never block on a human** — recovery is expressed only as `deps.decide(...)` forks over an option list. The third/escape option is named **`abort`** (codebase convention, matches `push-force` and `rebase-conflict`); the safer option leads at index 0.
- **A git verb is one invocation.** No new git verbs are needed — `Pull`, `Push`, `RebaseInProgress`, `RebaseAbort` already exist on `GitOps`.
- Decision IDs already in use: `push-force` (force-mode), `rebase-conflict` (keep-conflicts/abort), `non-fast-forward` (SmartPull divergence — **do not reuse**). The new ID is `push-rejected`.
- This branch already contains the push-error *message* classifier (`friendlyPushError` in `internal/tui/view.go`) and its tests. Task 1 refactors that to consume `internal/pusherr`; keep its existing tests green.

---

### Task 1: `internal/pusherr` classifier package

Centralize the push-rejection signature strings in a pure package so the engine's recovery trigger and the TUI's friendly message cannot drift.

**Files:**
- Create: `internal/pusherr/pusherr.go`
- Create: `internal/pusherr/pusherr_test.go`
- Modify: `internal/tui/view.go` (`friendlyPushError` consumes `pusherr`)

**Interfaces:**
- Produces:
  - `func IsNonFastForward(errText string) bool`
  - `func IsStaleInfo(errText string) bool`
  - `func IsHookRejection(errText string) bool`
  - (all case-insensitive; callers may pass raw or lowercased text)

- [ ] **Step 1: Write the failing test** — `internal/pusherr/pusherr_test.go`

```go
package pusherr

import "testing"

func TestIsNonFastForward(t *testing.T) {
	hits := []string{
		"git push failed (exit 1): ! [rejected] br -> br (non-fast-forward)",
		"hint: Updates were rejected because the tip of your current branch is behind",
		"! [rejected] (fetch first)",
		"NON-FAST-FORWARD", // case-insensitive
	}
	for _, s := range hits {
		if !IsNonFastForward(s) {
			t.Errorf("IsNonFastForward(%q) = false, want true", s)
		}
	}
	misses := []string{
		"(stale info)",
		"pre-receive hook declined",
		"could not read Username",
		"",
	}
	for _, s := range misses {
		if IsNonFastForward(s) {
			t.Errorf("IsNonFastForward(%q) = true, want false", s)
		}
	}
}

func TestIsStaleInfo(t *testing.T) {
	if !IsStaleInfo("! [rejected] br -> br (stale info)") {
		t.Error("want stale-info hit")
	}
	if IsStaleInfo("(non-fast-forward)") {
		t.Error("non-fast-forward must not read as stale info")
	}
}

func TestIsHookRejection(t *testing.T) {
	hits := []string{
		"remote: error: GH006: Protected branch update failed",
		"! [remote rejected] main -> main (pre-receive hook declined)",
	}
	for _, s := range hits {
		if !IsHookRejection(s) {
			t.Errorf("IsHookRejection(%q) = false, want true", s)
		}
	}
	if IsHookRejection("(non-fast-forward)") {
		t.Error("non-fast-forward must not read as a hook rejection")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/pusherr/ -run TestIs -v`
Expected: FAIL — build error, `pusherr.go` does not exist yet.

- [ ] **Step 3: Write the implementation** — `internal/pusherr/pusherr.go`

```go
// Package pusherr classifies git push-rejection stderr. It is pure (no git/TUI
// deps) so both the engine's recovery trigger and the TUI's status-bar message
// match against one source of truth and cannot drift.
package pusherr

import "strings"

// IsNonFastForward reports whether errText is a push rejected because the remote
// branch has commits the local tip lacks — the recoverable case.
func IsNonFastForward(errText string) bool {
	low := strings.ToLower(errText)
	return strings.Contains(low, "non-fast-forward") ||
		strings.Contains(low, "fetch first") ||
		strings.Contains(low, "tip of your current branch is behind")
}

// IsStaleInfo reports whether errText is a --force-with-lease rejection because
// the remote moved since the last fetch.
func IsStaleInfo(errText string) bool {
	return strings.Contains(strings.ToLower(errText), "stale info")
}

// IsHookRejection reports whether errText is a server-side rejection (protected
// branch or pre-receive hook), which pull/rebase cannot fix.
func IsHookRejection(errText string) bool {
	low := strings.ToLower(errText)
	return strings.Contains(low, "pre-receive hook declined") ||
		strings.Contains(low, "protected branch") ||
		strings.Contains(low, "[remote rejected]")
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/pusherr/ -run TestIs -v`
Expected: PASS.

- [ ] **Step 5: Refactor `friendlyPushError` to consume `pusherr`** — `internal/tui/view.go`

Replace the body of `friendlyPushError` (the `switch` that string-matches inline) with calls into `pusherr`, keeping the exact same returned copy. Add the import `"github.com/homeend/gigagit/internal/pusherr"`.

```go
func friendlyPushError(low string) (string, bool) {
	// Messages are kept tight so the remedy survives status-bar truncation on an
	// ~80-col terminal — the action is the whole point of the rewrite.
	switch {
	case pusherr.IsHookRejection(low):
		return "error: push rejected by the remote (protected branch or server-side hook)", true
	case pusherr.IsStaleInfo(low):
		return "error: force-with-lease refused — remote moved; fetch & review, then retry", true
	case pusherr.IsNonFastForward(low):
		return "error: push rejected — remote has new commits; pull/rebase first, or force-push", true
	}
	return "", false
}
```

- [ ] **Step 6: Run the TUI message tests to confirm no regression**

Run: `go test ./internal/tui/ -run 'TestFriendlyOpError' -v`
Expected: PASS (all four: credentials, non-fast-forward, stale-info, hook).

- [ ] **Step 7: Vet, fmt, commit**

```bash
go vet ./internal/pusherr/ ./internal/tui/
gofmt -l internal/pusherr/pusherr.go internal/tui/view.go   # expect no output
git add internal/pusherr/ internal/tui/view.go
git commit -m "feat(pusherr): shared push-rejection classifier; friendlyPushError consumes it"
```

---

### Task 2: Engine `push-rejected` decision — abort and force branches

Add the recovery decision and its two non-rebase branches. The trigger fires only when a **non-force** push returns a non-fast-forward error.

**Files:**
- Modify: `internal/engine/ops_basic.go` (`Push.Run` + helpers)
- Create: `internal/engine/push_reject_test.go`

**Interfaces:**
- Consumes: `pusherr.IsNonFastForward` (Task 1); existing `git.PushNoForce/PushForceWithLease/PushForcePlain`; `deps.decide`.
- Produces (internal to the op, relied on by Task 3): the methods `decideForce(ctx, deps) (git.PushForce, bool, error)`, `push(ctx, deps, force) (Result, error)`, and `recoverRejected(ctx, deps) (Result, error)` on `Push`. The `push-rejected` decision has `Options: []string{"rebase", "force", "abort"}`.

- [ ] **Step 1: Write the failing tests** — `internal/engine/push_reject_test.go`

```go
package engine

import (
	"context"
	"errors"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
)

// rejectingPushRepo returns a fake whose first "git push" fails non-fast-forward.
func rejectingPushRepo() (*git.Repo, *gitexec.FakeRunner) {
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [rejected] main -> main (non-fast-forward)"))
	return &git.Repo{Runner: f}, f
}

func pushCallCount(f *gitexec.FakeRunner) int {
	n := 0
	for _, c := range f.Calls {
		if c.Name == "git push" {
			n++
		}
	}
	return n
}

func TestPushRejectedAbortDoesNotForce(t *testing.T) {
	repo, f := rejectingPushRepo()
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-rejected": "abort"}})
	if err != nil {
		t.Fatalf("abort should be a clean no-op, err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not report a change")
	}
	if pushCallCount(f) != 1 {
		t.Fatalf("abort must not push again, push calls=%d", pushCallCount(f))
	}
}

func TestPushRejectedForceChainsForceDecision(t *testing.T) {
	repo, f := rejectingPushRepo()
	// After the first (rejected) push, allow subsequent pushes to succeed.
	f.SetHandler("git push", forceSucceedAfter(1, f))
	dec := &captureDecider{answers: map[string]string{
		"push-rejected": "force",
		"push-force":    "force",
	}}
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	// The op asked push-rejected first, then chained push-force.
	if len(dec.seen) < 2 || dec.seen[0].ID != "push-rejected" || dec.seen[1].ID != "push-force" {
		t.Fatalf("decision order = %+v, want [push-rejected, push-force]", dec.seen)
	}
	argv, _ := lastPushArgv(f)
	if !hasArg(argv, "--force") {
		t.Fatalf("force branch must push with --force, got %v", argv)
	}
}

func TestPushRejectedNeverFiresOnHookRejection(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetError("git push", errors.New(
		"git push failed (exit 1): ! [remote rejected] main -> main (pre-receive hook declined)"))
	dec := &captureDecider{answers: map[string]string{}}
	_, err := Push{Remote: "origin", Branch: "main"}.Run(
		context.Background(), OpDeps{Repo: &git.Repo{Runner: f}, Decider: dec})
	if err == nil {
		t.Fatal("a hook rejection must surface as an error, not a recovery")
	}
	if len(dec.seen) != 0 {
		t.Fatalf("no decision may be raised for a hook rejection, saw %+v", dec.seen)
	}
}
```

Helper notes for the implementer:
- `forceSucceedAfter(n, f)` is a `func(ctx context.Context, argv []string) (gitexec.Result, error)` that returns the canned non-fast-forward error for the first `n` `git push` calls (count via `pushCallCount(f)`) and `gitexec.Result{}, nil` thereafter. Write it inline in the test file. Note `SetHandler` takes precedence over `SetError` for the same span name, so registering it overrides the `rejectingPushRepo` error.
- `lastPushArgv(f)` returns the argv of the final `git push` call — a 3-line loop over `f.Calls` (mirror `pushArgv` in `push_force_test.go`, returning the last match).
- `captureDecider` and `MapDecider` already exist in the engine test package.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run TestPushRejected -v`
Expected: FAIL — `Push.Run` does not yet raise `push-rejected` (abort test sees an error / wrong push count).

- [ ] **Step 3: Refactor `Push.Run` and add the abort + force branches** — `internal/engine/ops_basic.go`

Add the import `"github.com/homeend/gigagit/internal/pusherr"`. Replace the existing `Push.Run` with:

```go
func (op Push) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Force {
		force, ok, err := op.decideForce(ctx, deps)
		if err != nil {
			return Result{}, err
		}
		if !ok {
			return Result{Summary: "push cancelled", Changed: false}, nil
		}
		return op.push(ctx, deps, force)
	}

	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce)
	if err == nil {
		res := Result{Summary: "pushed", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	if !pusherr.IsNonFastForward(err.Error()) {
		return Result{}, err // credentials / hook / network: not recoverable here
	}
	return op.recoverRejected(ctx, deps)
}

// decideForce asks the push-force decision and maps it to a force mode. ok=false
// means the user aborted. The safer lease-protected option leads (index 0) so a
// modal's default enter never triggers a plain overwrite.
func (op Push) decideForce(ctx context.Context, deps OpDeps) (git.PushForce, bool, error) {
	choice, err := deps.decide(ctx, DecisionRequest{
		ID:      "push-force",
		Prompt:  "Force-push " + op.Branch + " to " + op.Remote + " (overwrites the remote branch)",
		Options: []string{"force-with-lease", "force", "abort"},
	})
	if err != nil {
		return git.PushNoForce, false, err
	}
	switch choice.Option {
	case "force-with-lease":
		return git.PushForceWithLease, true, nil
	case "force":
		return git.PushForcePlain, true, nil
	case "abort", "":
		return git.PushNoForce, false, nil
	default:
		return git.PushNoForce, false, fmt.Errorf("push: unknown force mode %q", choice.Option)
	}
}

// push performs the push with the given force mode and emits Done on success.
func (op Push) push(ctx context.Context, deps OpDeps, force git.PushForce) (Result, error) {
	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, force); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "pushed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// recoverRejected handles a non-fast-forward rejection on a plain push: rebase
// onto the remote then re-push, chain the force decision, or abort. esc lands on
// abort. The rebase branch is added in the next task.
func (op Push) recoverRejected(ctx context.Context, deps OpDeps) (Result, error) {
	choice, err := deps.decide(ctx, DecisionRequest{
		ID:      "push-rejected",
		Prompt:  "Remote has new commits on " + op.Branch + " — rebase onto them, force-push, or abort",
		Options: []string{"rebase", "force", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	switch choice.Option {
	case "rebase":
		return op.rebaseThenPush(ctx, deps)
	case "force":
		force, ok, derr := op.decideForce(ctx, deps)
		if derr != nil {
			return Result{}, derr
		}
		if !ok {
			return Result{Summary: "push cancelled", Changed: false}, nil
		}
		return op.push(ctx, deps, force)
	case "abort", "":
		return Result{Summary: "push cancelled", Changed: false}, nil
	default:
		return Result{}, fmt.Errorf("push: unknown recovery %q", choice.Option)
	}
}
```

To keep this task self-contained, add a temporary stub for the rebase branch (Task 3 replaces it):

```go
// rebaseThenPush is implemented in Task 3.
func (op Push) rebaseThenPush(ctx context.Context, deps OpDeps) (Result, error) {
	return Result{}, fmt.Errorf("push: rebase recovery not yet implemented")
}
```

- [ ] **Step 4: Run the new + existing push tests to verify they pass**

Run: `go test ./internal/engine/ -run 'TestPushRejected|TestPush' -v`
Expected: PASS — the three new tests plus the existing `push_force_test.go` suite (the refactor preserves the force path and plain-push behavior).

- [ ] **Step 5: Vet, fmt, commit**

```bash
go vet ./internal/engine/
gofmt -l internal/engine/ops_basic.go internal/engine/push_reject_test.go   # expect no output
git add internal/engine/ops_basic.go internal/engine/push_reject_test.go
git commit -m "feat(engine): push-rejected recovery decision (abort + force branches)"
```

---

### Task 3: Engine rebase branch (rebase → conflict fork → re-push)

Implement `rebaseThenPush`: pull --rebase, handle a conflict via the existing `rebase-conflict` decision, then re-push (one attempt, no loop).

**Files:**
- Modify: `internal/engine/ops_basic.go` (replace the Task 2 stub)
- Modify: `internal/engine/push_reject_test.go` (add rebase-branch tests)

**Interfaces:**
- Consumes: `git.PullRebase`; `deps.Repo.Pull`, `RebaseInProgress`, `RebaseAbort`, `Push`; the existing `rebase-conflict` decision (`keep-conflicts`/`abort`).

- [ ] **Step 1: Write the failing tests** — append to `internal/engine/push_reject_test.go`

```go
func TestPushRejectedRebaseThenPush(t *testing.T) {
	repo, f := rejectingPushRepo()
	f.SetResponse("git pull", gitexec.Result{})    // clean rebase
	f.SetHandler("git push", forceSucceedAfter(1, f)) // 1st push rejected, 2nd succeeds
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{"push-rejected": "rebase"}})
	if err != nil || !res.Changed {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if pushCallCount(f) != 2 {
		t.Fatalf("want exactly 2 pushes (rejected, then re-push), got %d", pushCallCount(f))
	}
	pulls := 0
	for _, c := range f.Calls {
		if c.Name == "git pull" {
			pulls++
			if !hasArg(c.Argv, "--rebase") {
				t.Fatalf("recovery pull must be --rebase, got %v", c.Argv)
			}
		}
	}
	if pulls != 1 {
		t.Fatalf("want one pull --rebase, got %d", pulls)
	}
}

func TestPushRejectedRebaseConflictKeepLeavesTreeAndErrors(t *testing.T) {
	repo, f := rejectingPushRepo()
	f.SetError("git pull", errors.New("git pull failed: CONFLICT (content)"))
	f.SetResponse("git rebase --show-current-patch", gitexec.Result{}) // exit 0 ⇒ rebase in progress
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{
			"push-rejected":   "rebase",
			"rebase-conflict": "keep-conflicts",
		}})
	if err == nil {
		t.Fatal("a kept conflict must return an error so the TUI conflict process engages")
	}
	if !res.Changed {
		t.Fatal("a kept conflict leaves a changed (conflicted) tree")
	}
	if pushCallCount(f) != 1 {
		t.Fatalf("a kept conflict must not re-push, push calls=%d", pushCallCount(f))
	}
}

func TestPushRejectedRebaseConflictAbortRunsRebaseAbort(t *testing.T) {
	repo, f := rejectingPushRepo()
	f.SetError("git pull", errors.New("git pull failed: CONFLICT (content)"))
	f.SetResponse("git rebase --show-current-patch", gitexec.Result{})
	f.SetResponse("git rebase --abort", gitexec.Result{})
	res, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{
			"push-rejected":   "rebase",
			"rebase-conflict": "abort",
		}})
	if err != nil {
		t.Fatalf("abort of a conflicted rebase is a clean no-op, err=%v", err)
	}
	if res.Changed {
		t.Fatal("abort must not report a change")
	}
	saw := false
	for _, c := range f.Calls {
		if c.Name == "git rebase --abort" {
			saw = true
		}
	}
	if !saw {
		t.Fatal("abort branch must call git rebase --abort")
	}
}

func TestPushRejectedRebaseSecondRejectionDoesNotLoop(t *testing.T) {
	repo, f := rejectingPushRepo() // every "git push" stays rejected
	f.SetResponse("git pull", gitexec.Result{})
	dec := &captureDecider{answers: map[string]string{"push-rejected": "rebase"}}
	_, err := Push{Remote: "origin", Branch: "main", SetUpstream: true}.Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec})
	if err == nil {
		t.Fatal("a second rejection after rebase must surface as an error")
	}
	// push-rejected asked exactly once — the recovery never re-enters.
	rejectAsks := 0
	for _, d := range dec.seen {
		if d.ID == "push-rejected" {
			rejectAsks++
		}
	}
	if rejectAsks != 1 {
		t.Fatalf("push-rejected asked %d times, want 1 (no loop)", rejectAsks)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run TestPushRejectedRebase -v`
Expected: FAIL — `rebaseThenPush` is the stub returning "not yet implemented".

- [ ] **Step 3: Replace the stub with the real `rebaseThenPush`** — `internal/engine/ops_basic.go`

```go
// rebaseThenPush replays the local commits on top of the remote tip (pull
// --rebase), then re-pushes once. A rebase conflict forks via the existing
// rebase-conflict decision: keep-conflicts leaves the tree for `git rebase
// --continue` (the TUI conflict process picks it up) and returns an error;
// abort restores the pre-rebase tip. After a clean rebase the branch is ahead,
// so the re-push fast-forwards. The recovery runs once — a second rejection is
// surfaced, never re-entered, so the op cannot loop.
func (op Push) rebaseThenPush(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "pull --rebase", Detail: op.Remote + " " + op.Branch})
	if rebaseErr := deps.Repo.Pull(ctx, op.Remote, op.Branch, git.PullRebase); rebaseErr != nil {
		inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, "")
		if stateErr != nil {
			return Result{}, fmt.Errorf("push rebase: %v (state check: %w)", rebaseErr, stateErr)
		}
		if !inRebase {
			return Result{}, fmt.Errorf("push rebase: %w", rebaseErr)
		}
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "rebase-conflict",
			Prompt:  "Rebasing " + op.Branch + " onto " + op.Remote + "/" + op.Branch + " hit conflicts",
			Options: []string{"keep-conflicts", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option == "keep-conflicts" {
			return Result{Summary: "rebase paused on a conflict (resolve, then `git rebase --continue`, then push)", Changed: true},
				fmt.Errorf("push rebase conflict: %s", op.Branch)
		}
		if err := deps.Repo.RebaseAbort(ctx, ""); err != nil {
			return Result{}, fmt.Errorf("push rebase: abort failed: %w", err)
		}
		return Result{Summary: "push cancelled (rebase aborted)", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "pushing", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.Push(ctx, op.Remote, op.Branch, op.SetUpstream, git.PushNoForce); err != nil {
		return Result{}, err // second rejection or other error: surface, no loop
	}
	res := Result{Summary: "rebased and pushed", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run the rebase tests + full engine suite to verify they pass**

Run: `go test ./internal/engine/ -run TestPushRejected -v && go test ./internal/engine/`
Expected: PASS.

- [ ] **Step 5: Vet, fmt, commit**

```bash
go vet ./internal/engine/
gofmt -l internal/engine/ops_basic.go internal/engine/push_reject_test.go   # expect no output
git add internal/engine/ops_basic.go internal/engine/push_reject_test.go
git commit -m "feat(engine): push rejection recovery via rebase-then-push"
```

---

### Task 4: CLI `--on-reject` policy flag

Make `gg push` answer the new fork from a flag so the CLI stays scriptable and non-blocking.

**Files:**
- Modify: `internal/cli/ops.go` (`cmdPush`)
- Create: `internal/cli/push_reject_test.go`

**Interfaces:**
- `gg push [--force | --force-with-lease] [--on-reject=rebase|force|force-with-lease|abort]`
- `--on-reject` maps to policy entries:
  - `rebase` → `{"push-rejected": "rebase"}`
  - `force` → `{"push-rejected": "force", "push-force": "force"}`
  - `force-with-lease` → `{"push-rejected": "force", "push-force": "force-with-lease"}`
  - `abort` / unset → `{"push-rejected": "abort"}` (the default; a non-interactive plain push that is rejected fails fast with the friendly message)
- Guard: `--on-reject` combined with `--force`/`--force-with-lease` is an error (those already force, so recovery never triggers).

- [ ] **Step 1: Write the failing tests** — `internal/cli/push_reject_test.go`

```go
package cli

import (
	"reflect"
	"testing"
)

func TestPushRejectPolicy(t *testing.T) {
	cases := map[string]map[string]string{
		"rebase":           {"push-rejected": "rebase"},
		"force":            {"push-rejected": "force", "push-force": "force"},
		"force-with-lease": {"push-rejected": "force", "push-force": "force-with-lease"},
		"abort":            {"push-rejected": "abort"},
		"":                 {"push-rejected": "abort"},
	}
	for in, want := range cases {
		got, err := pushRejectPolicy(in)
		if err != nil {
			t.Fatalf("pushRejectPolicy(%q) err=%v", in, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("pushRejectPolicy(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestPushRejectPolicyRejectsUnknown(t *testing.T) {
	if _, err := pushRejectPolicy("merge"); err == nil {
		t.Fatal("unknown --on-reject value must error")
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run TestPushReject -v`
Expected: FAIL — `pushRejectPolicy` undefined.

- [ ] **Step 3: Add `pushRejectPolicy` and wire `cmdPush`** — `internal/cli/ops.go`

Add the helper (pure, testable in isolation):

```go
// pushRejectPolicy maps the --on-reject flag value to the decision policy for a
// rejected plain push. An empty value means the safe default: abort.
func pushRejectPolicy(onReject string) (map[string]string, error) {
	switch onReject {
	case "", "abort":
		return map[string]string{"push-rejected": "abort"}, nil
	case "rebase":
		return map[string]string{"push-rejected": "rebase"}, nil
	case "force":
		return map[string]string{"push-rejected": "force", "push-force": "force"}, nil
	case "force-with-lease":
		return map[string]string{"push-rejected": "force", "push-force": "force-with-lease"}, nil
	default:
		return nil, fmt.Errorf("push: unknown --on-reject %q (want rebase|force|force-with-lease|abort)", onReject)
	}
}
```

Then in `cmdPush`, register the flag and merge the policy. Replace the flag/policy section so it reads:

```go
	force := fs.Bool("force", false, "force-push, overwriting the remote branch unconditionally (no lease)")
	lease := fs.Bool("force-with-lease", false, "force-push only if the remote branch has not moved")
	onReject := fs.String("on-reject", "", "if a plain push is rejected (remote ahead): rebase|force|force-with-lease|abort (default abort)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *force && *lease {
		fmt.Fprintln(stderr, "push: choose at most one of --force/--force-with-lease")
		return 2
	}
	if *onReject != "" && (*force || *lease) {
		fmt.Fprintln(stderr, "push: --on-reject applies to a plain push, not with --force/--force-with-lease")
		return 2
	}

	cur, err := svc.CurrentBranch(context.Background())
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if cur == "" {
		fmt.Fprintln(stderr, "push: detached HEAD; cannot push")
		return 1
	}

	policy := map[string]string{}
	switch {
	case *lease:
		policy["push-force"] = "force-with-lease"
	case *force:
		policy["push-force"] = "force"
	default:
		rp, perr := pushRejectPolicy(*onReject)
		if perr != nil {
			fmt.Fprintln(stderr, perr)
			return 2
		}
		policy = rp
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.Push{Remote: "origin", Branch: cur, SetUpstream: true, Force: *force || *lease}, dec, stderr)
	return finish(res, err, stdout, stderr)
```

- [ ] **Step 4: Run the CLI tests + full cli package to verify they pass**

Run: `go test ./internal/cli/ -run TestPushReject -v && go test ./internal/cli/`
Expected: PASS.

- [ ] **Step 5: Vet, fmt, commit**

```bash
go vet ./internal/cli/
gofmt -l internal/cli/ops.go internal/cli/push_reject_test.go   # expect no output
git add internal/cli/ops.go internal/cli/push_reject_test.go
git commit -m "feat(cli): gg push --on-reject for rejected plain pushes"
```

---

### Task 5: Real-git integration test (rebase-then-push happy path)

The FakeRunner tests prove the control flow; this proves the real `git` argv actually recovers against a moved remote.

**Files:**
- Create: `internal/engine/push_reject_realgit_test.go`

**Interfaces:**
- Consumes: real `git` via `gitexec.NewExecRunner`. Model the diverged-remote setup on `internal/engine/smart_pull_test.go:63` (a clone whose `origin/<branch>` is ahead of the local tip) — reuse/copy its setup helper rather than inventing one.

- [ ] **Step 1: Write the test** — `internal/engine/push_reject_realgit_test.go`

Construct, with real `git` in `t.TempDir()`:
1. A bare `origin`.
2. A `seed` clone: one commit on `main`, pushed to `origin`.
3. A `b` clone: adds `remote.txt`, commits, pushes to `origin/main` (the remote moves).
4. The `a` clone (the unit under test): from the seed state, adds `local.txt`, commits — now behind `origin/main` and diverged.
5. Run `Push{Remote:"origin", Branch:"main", SetUpstream:true}` on a `*git.Repo` rooted at `a` with `OpDeps{Repo: repo, Decider: MapDecider{"push-rejected":"rebase"}}`.

Assert:
- `err == nil` and `res.Summary == "rebased and pushed"`.
- After the op, `origin/main` (fetch in `a`, then `git log --oneline origin/main`) contains **both** `remote.txt`'s commit and `local.txt`'s commit (the local commit replayed on top).
- `a`'s working tree is clean (`IsDirty` false).

Use the existing real-git helpers for running setup git commands (see `smart_pull_test.go` and `internal/engine/conflict_test.go:47` for `gitexec.NewExecRunner("git", dir, observ.NewRing(50))`). Read those two files first and mirror their helper style; do not hand-roll a new git-exec wrapper.

- [ ] **Step 2: Run the test**

Run: `go test ./internal/engine/ -run TestPushRejectRebaseRealGit -v`
Expected: PASS. (If it fails because the local commit is missing from `origin/main`, the rebase/re-push order is wrong — re-check `rebaseThenPush`.)

- [ ] **Step 3: Vet, fmt, commit**

```bash
go vet ./internal/engine/
gofmt -l internal/engine/push_reject_realgit_test.go   # expect no output
git add internal/engine/push_reject_realgit_test.go
git commit -m "test(engine): real-git push rejection → rebase → push happy path"
```

---

### Task 6: Docs + agent skill

Update user-facing docs and the embedded agent skill for the new CLI surface.

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md`
- Modify: `internal/agentskill/using-gg.md`
- Modify: `internal/agentskill/version.go` (bump `Version`)

- [ ] **Step 1: CHANGELOG** — under `## [Unreleased] → ### Added`, add:

```markdown
- **Smart push recovery.** When a plain push is rejected because the remote has
  moved ahead, gg no longer dead-ends on an error: it offers **rebase onto the
  remote and push**, **force-push** (routing through the existing
  force-with-lease / force confirm), or **abort** — from the single push action.
  In the CLI, `gg push --on-reject=rebase|force|force-with-lease|abort` drives
  the same recovery non-interactively (default `abort`).
```

- [ ] **Step 2: README** — in the push/CLI section, document the `--on-reject` flag and the one-key TUI recovery (mirror the CHANGELOG wording, one or two sentences; match the existing `--force`/`--force-with-lease` documentation style).

- [ ] **Step 3: agent skill** — in `internal/agentskill/using-gg.md`, under the `gg push` entry, add the `--on-reject` flag and a one-line note that a plain rejected push otherwise aborts. Then bump the version constant in `internal/agentskill/version.go` by 1 (it is currently `30`).

- [ ] **Step 4: Build, confirm the skill version embeds**

Run: `go build ./cmd/gg`
Expected: builds clean.

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md internal/agentskill/using-gg.md internal/agentskill/version.go
git commit -m "docs: smart push recovery + gg push --on-reject; bump agentskill version"
```

---

## Final verification

- [ ] Run the full staged suite with the race detector before declaring done:

```bash
./test.sh race
```

Expected: vet+gofmt clean, all unit tests pass (including `internal/pusherr`, the new engine `push-rejected` suite, the CLI `--on-reject` suite, and the real-git integration test), e2e green.

- [ ] Build a binary for a live eyeball and hand the user its absolute path:

```bash
go build -o ./gg ./cmd/gg
echo "$(pwd)/gg"
```

---

## Self-review notes (coverage against the spec)

- Behavior (rebase / force / abort, esc→abort) → Tasks 2–3.
- Trigger only on non-fast-forward, never on hook/credential → Task 1 (`pusherr`) + Task 2 (`TestPushRejectedNeverFiresOnHookRejection`).
- Rebase conflict reuses `rebase-conflict` (keep/abort) → Task 3.
- "Force" chains the existing `push-force` confirm → Task 2 (`TestPushRejectedForceChainsForceDecision`).
- One-attempt bound (no loop) → Task 3 (`TestPushRejectedRebaseSecondRejectionDoesNotLoop`).
- Single source of truth for signatures across engine + TUI → Task 1 (`internal/pusherr`, resolving the `tui`↛`git` import ban — a refinement over the spec's "predicate in internal/git").
- CLI policy, default abort, guard against `--force` combo → Task 4.
- Real-git happy path → Task 5.
- Docs + agentskill version bump → Task 6.

### Refinements discovered during planning (flag to the user)
1. The shared classifier lives in a **new pure `internal/pusherr` package**, not `internal/git`, because `internal/tui` may not import `internal/git`.
2. The third recovery option is named **`abort`** (codebase convention), not "cancel".
3. The CLI flag is **`--on-reject=rebase|force|force-with-lease|abort`** (one flag sets both policy entries), cleaner than the spec's "reuse `--force`/`--force-with-lease` for the nested decision".
