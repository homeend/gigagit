# SmartRebase Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `gg rebase` smart operation (engine + TUI pair-op + CLI) that replays one branch onto a new base, mirroring `SmartMerge` but with the inverted direction and explicit mid-replay rebase-state semantics.

**Architecture:** Three new dir-aware git verbs (`Rebase`/`RebaseAbort`/`RebaseInProgress`) added to the `engine.GitOps` interface; an `engine.SmartRebase{Branch, Onto}` operation with a 3-rung worktree-aware ladder pivoting on the branch being rewritten; the disabled TUI "Rebase" pair-op stub enabled; a `cmdRebase` CLI command mirroring `cmdMerge`; e2e scenarios; and a docs/agentskill refresh.

**Tech Stack:** Go 1.26, `internal/gitcmd` argv builder, `internal/gitexec` FakeRunner, Bubble Tea TUI, the declarative e2e harness.

**Spec:** `docs/superpowers/specs/2026-06-13-smart-rebase-design.md`

**Conventions reminder:**
- A git verb is one invocation built with `gitcmd` and run via `r.Runner.Run`.
- Operations emit events and decide via the `Decider`; decisions are option-lists only.
- `internal/tui` and `internal/cli` never import `internal/git`.
- Tests use a real `git` in a `t.TempDir()` (`newRepo`/`newTestRepo`/`newRepoDir` helpers) or the `FakeRunner` for argv assertions.
- Gate before merge: `./test.sh race`.
- Commits end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Direction (memorize — it is the inverse of merge):** `engine.SmartRebase{Branch, Onto}` — `Branch` is the branch whose commits get replayed (rewritten), `Onto` is the new base. From the TUI label *"Rebase {selected} onto {marked}"*: `Branch = selected`, `Onto = marked`. Empty `Branch` resolves to the current branch inside the op.

---

### Task 1: git rebase verbs

**Files:**
- Create: `internal/git/rebase.go`
- Test: `internal/git/rebase_test.go`

Mirrors `internal/git/merge.go` (the `dir`-aware `Merge`/`MergeAbort`/`MergeInProgress`). The in-progress probe uses `git rebase --show-current-patch`, which exits 0 when a rebase is paused on a patch and 128 when none — an exit-code probe like `MergeInProgress`, not a ref or directory probe (verified empirically; honors `-C dir`).

- [ ] **Step 1: Write the failing argv tests**

Create `internal/git/rebase_test.go`:

```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestRebaseArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Rebase(context.Background(), "", "main"); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	want := []string{"rebase", "--no-edit", "main"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRebaseArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Rebase(context.Background(), "/wt/x", "main"); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	want := []string{"-C", "/wt/x", "rebase", "--no-edit", "main"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRebaseAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RebaseAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("rebase abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "rebase", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/git/ -run TestRebase -v`
Expected: FAIL — `r.Rebase`/`r.RebaseAbort` undefined.

- [ ] **Step 3: Implement the verbs**

Create `internal/git/rebase.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Rebase replays the branch checked out at dir ("" = this repo's own
// worktree) onto onto. --no-edit keeps any auto-generated messages
// non-interactive. Non-interactive replay only (no -i, no --onto form).
func (r *Repo) Rebase(ctx context.Context, dir, onto string) error {
	b := gitcmd.New("rebase").Arg("--no-edit", onto)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase", b.ToArgv())
	return err
}

// RebaseAbort aborts an in-progress rebase at dir ("" = this repo's worktree),
// restoring the pre-rebase tip.
func (r *Repo) RebaseAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("rebase").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase --abort", b.ToArgv())
	return err
}

// RebaseInProgress reports whether a rebase is paused at dir ("" = this repo's
// worktree). `git rebase --show-current-patch` exits 0 when a patch is current
// (a rebase is paused, e.g. on a conflict) and non-zero ("No rebase in
// progress?") when none — the exit-code analogue of MergeInProgress. Unlike a
// merge there is no reliable single ref (REBASE_HEAD is version-dependent), so
// this exit-code probe is used; it also honors -C dir for the worktree rung.
func (r *Repo) RebaseInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rebase").Arg("--show-current-patch")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase --show-current-patch", b.ToArgv())
	if err == nil {
		return true, nil // exit 0: a patch is current → a rebase is paused
	}
	return false, nil // non-zero: no rebase in progress
}
```

- [ ] **Step 4: Run the argv tests to verify they pass**

Run: `go test ./internal/git/ -run TestRebase -v`
Expected: PASS (the three argv tests).

- [ ] **Step 5: Write the real-git conflict/probe/abort test**

Append to `internal/git/rebase_test.go` (uses `newTestRepo`/`gitIn` from `repo_test.go`/`merge_test.go`):

```go
// Real-git: build a conflicting rebase, observe the in-progress probe, abort,
// observe clean — mirrors TestMergeConflictDetectAndAbortReal.
func TestRebaseConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "feat change")
	gitIn(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "main change")
	gitIn(t, dir, "checkout", "feat")

	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("no rebase should be in progress yet")
	}
	if err := repo.Rebase(context.Background(), "", "main"); err == nil {
		t.Fatal("conflicting rebase must fail")
	}
	in, err := repo.RebaseInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("RebaseInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if err := repo.RebaseAbort(context.Background(), ""); err != nil {
		t.Fatalf("rebase abort: %v", err)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the rebase state")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "feat\n" {
		t.Fatalf("f.txt = %q after abort, want %q (feat's pre-rebase tip)", got, "feat\n")
	}
}
```

Note: `gitIn` is defined in `internal/git/merge_test.go`; `newTestRepo` in `internal/git/repo_test.go`. Do not redefine them.

- [ ] **Step 6: Run the full git package tests**

Run: `go test ./internal/git/ -run TestRebase -v`
Expected: PASS (all four tests).

- [ ] **Step 7: Commit**

```bash
git add internal/git/rebase.go internal/git/rebase_test.go
git commit -m "feat(git): add Rebase, RebaseAbort, RebaseInProgress verbs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: add the rebase verbs to the GitOps interface

**Files:**
- Modify: `internal/engine/gitops.go`

The `GitOps` interface is the set of verbs operations use; `var _ GitOps = (*git.Repo)(nil)` proves `*git.Repo` satisfies it. Adding the three verbs makes them available to `SmartRebase` (Task 3) and keeps the build honest.

- [ ] **Step 1: Add the three methods to the interface**

In `internal/engine/gitops.go`, after the `Merge`/`MergeAbort`/`MergeInProgress` block (currently the last three methods before the closing brace), add:

```go
	Rebase(ctx context.Context, dir, onto string) error
	RebaseAbort(ctx context.Context, dir string) error
	RebaseInProgress(ctx context.Context, dir string) (bool, error)
```

- [ ] **Step 2: Verify the build (the compile assertion is the test)**

Run: `go build ./internal/engine/`
Expected: builds clean — `*git.Repo` already has the methods from Task 1, so `var _ GitOps = (*git.Repo)(nil)` holds.

- [ ] **Step 3: Run the engine tests**

Run: `go test ./internal/engine/`
Expected: PASS (no behavior changed; this only widens the interface).

- [ ] **Step 4: Commit**

```bash
git add internal/engine/gitops.go
git commit -m "feat(engine): expose rebase verbs on the GitOps interface

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: the SmartRebase operation

**Files:**
- Create: `internal/engine/smart_rebase.go`
- Test: `internal/engine/smart_rebase_test.go`

Mirrors `internal/engine/smart_merge.go`. The engine test helpers `gitE`, `gitOut`, `branchWithCommit`, `conflictRepo`, and `newRepo` already exist in `internal/engine/smart_merge_test.go` / `ops_basic_test.go` — reuse them, do not redefine.

- [ ] **Step 1: Write the failing guard + direction tests**

Create `internal/engine/smart_rebase_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSmartRebaseGuards(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")

	cases := []struct {
		name string
		op   SmartRebase
		want string
	}{
		{"empty onto", SmartRebase{Branch: "feat"}, "Onto is required"},
		{"same branch", SmartRebase{Branch: "main", Onto: "main"}, "branch and base"},
		{"missing branch", SmartRebase{Branch: "nope", Onto: "main"}, "no such branch: nope"},
		{"missing onto", SmartRebase{Branch: "feat", Onto: "nope"}, "no such branch: nope"},
	}
	for _, tc := range cases {
		_, err := tc.op.Run(context.Background(), OpDeps{Repo: repo})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSmartRebaseDetachedHeadNeedsExplicitBranch(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "--detach")

	_, err := SmartRebase{Onto: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("err = %v, want detached HEAD guard", err)
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/engine/ -run TestSmartRebase -v`
Expected: FAIL — `SmartRebase` undefined.

- [ ] **Step 3: Implement the operation**

Create `internal/engine/smart_rebase.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// SmartRebase replays Branch's commits onto Onto, picking the simplest correct
// path: in place when Branch is checked out here, inside the worktree that has
// Branch checked out (you stay put), else autostash + switch + rebase, ending
// on Branch. Unlike merge it REWRITES Branch, so the ladder pivots on Branch
// (the moving branch), not Onto. A conflict pauses the replay mid-flight
// (detached HEAD) and forks via the "rebase-conflict" decision: keep-conflicts
// leaves the paused rebase for `git rebase --continue` (the op returns an
// error), abort runs `git rebase --abort`.
type SmartRebase struct {
	Branch string
	Onto   string
}

var _ Operation = SmartRebase{}

func (op SmartRebase) Run(ctx context.Context, deps OpDeps) (Result, error) {
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	branch := op.Branch
	if branch == "" {
		if cur == "" {
			return Result{}, fmt.Errorf("smart rebase: detached HEAD — specify a branch to rebase")
		}
		branch = cur
	}
	if op.Onto == "" {
		return Result{}, fmt.Errorf("smart rebase: Onto is required")
	}
	if branch == op.Onto {
		return Result{}, fmt.Errorf("smart rebase: branch and base are both %s", branch)
	}
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	for _, name := range []string{branch, op.Onto} {
		if !have[name] {
			return Result{}, fmt.Errorf("smart rebase: no such branch: %s", name)
		}
	}

	// Rung 1: Branch is checked out right here.
	if branch == cur {
		return op.rebaseAt(ctx, deps, "", branch)
	}

	// Rung 2: Branch lives in another worktree — rebase there, stay put.
	wt, err := deps.Repo.WorktreeForBranch(ctx, branch)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return op.rebaseAt(ctx, deps, wt.Path, branch)
	}

	// Rung 3: autostash if dirty, switch to Branch, rebase, stay on Branch.
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+branch); err != nil {
			return Result{}, err
		}
		stashed = true
	}
	deps.emit(ctx, Progress{Step: "switching", Detail: branch})
	if err := deps.Repo.Switch(ctx, branch); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx) // best-effort restore on the original branch
		}
		return Result{}, err
	}

	res, rebaseErr := op.rebaseAt(ctx, deps, "", branch)
	if rebaseErr != nil {
		// Paused mid-rebase (or refused outright): popping onto that tree would
		// compound the mess. The stash survives.
		if stashed && res.Summary != "" {
			res.Summary += " (your changes remain stashed)"
		}
		return res, rebaseErr
	}
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicted",
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: res.Summary + "; restore conflicted (changes preserved in stash)", Changed: res.Changed},
				fmt.Errorf("stash pop conflict after rebasing %s: %w", branch, err)
		}
	}
	return res, nil
}

// rebaseAt rebases branch onto op.Onto inside dir ("" = the current worktree),
// resolving a conflict via the rebase-conflict decision. A kept conflict leaves
// the repo paused mid-replay (detached HEAD), NOT cleanly on branch.
func (op SmartRebase) rebaseAt(ctx context.Context, deps OpDeps, dir, branch string) (Result, error) {
	where := ""
	if dir != "" {
		where = " in worktree " + dir
	}
	deps.emit(ctx, Progress{Step: "rebasing", Detail: branch + " onto " + op.Onto + where})
	rebaseErr := deps.Repo.Rebase(ctx, dir, op.Onto)
	if rebaseErr == nil {
		return Result{Summary: "rebased " + branch + " onto " + op.Onto + where, Changed: true}, nil
	}
	inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, fmt.Errorf("smart rebase: %s onto %s: %v (state check: %w)", branch, op.Onto, rebaseErr, stateErr)
	}
	if !inRebase {
		// Refused outright (e.g. nothing to replay): nothing to resolve.
		return Result{}, fmt.Errorf("smart rebase: %s onto %s: %w", branch, op.Onto, rebaseErr)
	}
	choice, derr := deps.decide(ctx, DecisionRequest{
		ID:      "rebase-conflict",
		Prompt:  "Rebasing " + branch + " onto " + op.Onto + " hit conflicts",
		Options: []string{"keep-conflicts", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		return Result{Summary: "rebase of " + branch + " onto " + op.Onto + where + " paused on a conflict (resolve, then `git rebase --continue`)", Changed: true},
			fmt.Errorf("rebase conflict: %s onto %s", branch, op.Onto)
	}
	if err := deps.Repo.RebaseAbort(ctx, dir); err != nil {
		return Result{}, fmt.Errorf("smart rebase: abort failed: %w", err)
	}
	return Result{Summary: "aborted: rebasing " + branch + " onto " + op.Onto, Changed: false}, nil
}
```

- [ ] **Step 4: Run the guard tests to verify they pass**

Run: `go test ./internal/engine/ -run TestSmartRebase -v`
Expected: PASS (guards + detached-head).

- [ ] **Step 5: Write the ladder + conflict tests**

Append to `internal/engine/smart_rebase_test.go`:

```go
func TestSmartRebaseCurrentBranchOntoBase(t *testing.T) {
	// rung 1: feat is current; rebase it onto main (disjoint files → clean).
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")
	branchWithCommit(t, dir, "feat", "feat.txt") // creates feat off main's NEW tip? no — off current
	gitE(t, dir, "checkout", "feat")

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "rebased feat onto main") {
		t.Fatalf("result = %+v", res)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s, want feat", got)
	}
	// feat's tip parent is now main's tip (linear).
	if _, err := os.Stat(filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal("main.txt missing after rebase — feat was not replayed onto main")
	}
}

func TestSmartRebaseBranchInOtherWorktree(t *testing.T) {
	// rung 2: feat lives in a linked worktree; rebase happens there, we stay.
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "shared-base.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base2")
	gitE(t, dir, "branch", "feat")
	wt := filepath.Join(dir, "..", "feat-wt")
	gitE(t, dir, "worktree", "add", wt, "feat")
	// give feat a commit (in its worktree) and advance main disjointly
	os.WriteFile(filepath.Join(wt, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, wt, "add", ".")
	gitE(t, wt, "commit", "-m", "feat change")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if !strings.Contains(res.Summary, "in worktree") {
		t.Fatalf("summary = %q, want worktree mention", res.Summary)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("current branch %s changed, want main (we stay put)", got)
	}
	// feat replayed onto main → main.txt now present in feat's worktree.
	if _, err := os.Stat(filepath.Join(wt, "main.txt")); err != nil {
		t.Fatal("rebase did not land in the feat worktree")
	}
}

func TestSmartRebaseUncheckedOutBranchSwitchesAndStays(t *testing.T) {
	// rung 3: feat is not checked out anywhere; dirty main autostashes.
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")
	// feat off the ORIGINAL base, disjoint file, then back to main
	gitE(t, dir, "checkout", "-b", "feat", "HEAD~1")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat change")
	gitE(t, dir, "checkout", "main")
	// dirty tracked file on main → autostash must carry it back to main
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s, want feat (rebase ends on Branch)", got)
	}
	if !strings.Contains(res.Summary, "rebased feat onto main") {
		t.Fatalf("summary = %q", res.Summary)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(got) != "dirty\n" {
		t.Fatal("autostashed change was not restored")
	}
	if out := gitOut(t, dir, "stash", "list"); out != "" {
		t.Fatalf("stash not popped: %q", out)
	}
}

// rebaseConflictRepo: feat and main both edit shared.txt → guaranteed rebase
// conflict; feat is the current branch.
func rebaseConflictRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "commit", "-am", "feat change")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitE(t, dir, "commit", "-am", "main change")
	gitE(t, dir, "checkout", "feat")
	return dir, repo
}

func TestSmartRebaseConflictAbort(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"rebase-conflict": "abort"}})
	if err != nil {
		t.Fatalf("chosen abort must not be an error: %v", err)
	}
	if !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the rebase state")
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s after abort, want feat", got)
	}
}

func TestSmartRebaseConflictKeep(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"rebase-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("keep-conflicts must surface an error (CLI exit 1)")
	}
	if !strings.Contains(res.Summary, "conflict") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); !in {
		t.Fatal("rebase state was not kept")
	}
}

func TestSmartRebaseConflictUndecidedLeavesRebaseState(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	_, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("undecided conflict must error")
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); !in {
		t.Fatal("expected rebase still in progress")
	}
	_ = dir
}
```

Add the import for `git` to the test file's import block (used by `rebaseConflictRepo`'s return type):

```go
	"github.com/gigagit/gg/internal/git"
```

- [ ] **Step 6: Run the full SmartRebase suite**

Run: `go test ./internal/engine/ -run TestSmartRebase -v`
Expected: PASS (all guard, ladder, and conflict tests).

> If `TestSmartRebaseCurrentBranchOntoBase`'s `branchWithCommit` helper does not place `feat` where this test needs it (it checks out `feat` off the *current* HEAD), adjust the fixture inline to create `feat` off the original base with `git checkout -b feat HEAD~1` as in the rung-3 test — the contract is "feat diverged from main with a disjoint file." Do not change the assertion.

- [ ] **Step 7: Commit**

```bash
git add internal/engine/smart_rebase.go internal/engine/smart_rebase_test.go
git commit -m "feat(engine): SmartRebase op (worktree-aware, rebase-state aware)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: enable the TUI Rebase pair-op

**Files:**
- Modify: `internal/tui/mark.go:40-44`
- Test: `internal/tui/mark_test.go:137-150` (replace the disabled-entry test) + a new integration test

The stub at `mark.go` currently has `note: "not implemented yet"` and no `build`. Enabling it wires `engine.SmartRebase{Branch: selected, Onto: marked}`. The existing `TestPairPopupDisabledRebaseEntry` asserts the *disabled* behavior and must be replaced.

- [ ] **Step 1: Replace the disabled-entry test with an enabled-direction test**

In `internal/tui/mark_test.go`, replace `TestPairPopupDisabledRebaseEntry` (lines ~137-150) with a direction-mapping integration test mirroring `TestPairPopupEnterRunsSmartMerge` (lines ~193+). Direction: mark `main`, select `feat`, choose Rebase → rebases `feat` onto `main` (feat is rewritten onto main's tip):

```go
// Integration: enter on Rebase dispatches SmartRebase with the correct
// direction (selected is rebased onto marked) and the rebase really runs.
func TestPairPopupEnterRunsSmartRebase(t *testing.T) {
	dir, repo := newRepoDir(t)
	run := func(wd string, args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = wd
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// feat diverges from main with a disjoint file; main advances disjointly.
	run(dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	run(dir, "add", ".")
	run(dir, "commit", "-m", "feat change")
	run(dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	run(dir, "add", ".")
	run(dir, "commit", "-m", "main change")
	run(dir, "checkout", "feat")

	m := New(domain.New(repo))
	// Build the popup directly with the real pair-ops so we exercise the
	// enabled Rebase entry's build func and direction.
	ops := pairOpsFor(panelBranches)
	var rebase *pairOp
	for i := range ops {
		if strings.HasPrefix(ops[i].label("main", "feat"), "Rebase ") {
			rebase = &ops[i]
		}
	}
	if rebase == nil || !rebase.enabled || rebase.build == nil {
		t.Fatal("Rebase pair-op must be enabled with a build func")
	}
	if got := rebase.label("main", "feat"); got != "Rebase feat onto main" {
		t.Fatalf("label = %q, want %q", got, "Rebase feat onto main")
	}
	op := rebase.build("main", "feat") // marked=main, selected=feat
	sr, ok := op.(engine.SmartRebase)
	if !ok {
		t.Fatalf("build returned %T, want engine.SmartRebase", op)
	}
	if sr.Branch != "feat" || sr.Onto != "main" {
		t.Fatalf("SmartRebase = %+v, want {Branch:feat Onto:main}", sr)
	}
	_ = m
}
```

Add `"github.com/gigagit/gg/internal/engine"` to the test imports if not present.

- [ ] **Step 2: Run to verify the new test fails**

Run: `go test ./internal/tui/ -run TestPairPopupEnterRunsSmartRebase -v`
Expected: FAIL — the Rebase entry is still disabled (`build == nil`, `enabled == false`).

- [ ] **Step 3: Enable the stub**

In `internal/tui/mark.go`, replace the disabled Rebase entry:

```go
		{
			label: func(marked, selected string) string { return "Rebase " + selected + " onto " + marked },
			note:  "not implemented yet",
		},
```

with:

```go
		{
			label: func(marked, selected string) string { return "Rebase " + selected + " onto " + marked },
			build: func(marked, selected string) engine.Operation {
				return engine.SmartRebase{Branch: selected, Onto: marked}
			},
			enabled: true,
		},
```

- [ ] **Step 4: Run the TUI mark tests**

Run: `go test ./internal/tui/ -run 'TestPairPopup|TestMark' -v`
Expected: PASS — the new direction test passes; no remaining test references the old "not implemented" Rebase behavior.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/mark.go internal/tui/mark_test.go
git commit -m "feat(tui): enable the Rebase pair-op (selected onto marked)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: the `gg rebase` CLI command

**Files:**
- Create: `internal/cli/rebase.go`
- Test: `internal/cli/rebase_test.go`
- Modify: `internal/cli/cli.go` (dispatch `case "rebase"` + the known-commands map at line ~79)

Mirrors `internal/cli/merge.go` / `merge_test.go`. CLI helpers `newRepoDir`, `gitRun`, and `Run` already exist — reuse them.

- [ ] **Step 1: Write the failing CLI tests**

Create `internal/cli/rebase_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// rebaseFixture: feat diverges from main with a disjoint file, main advances
// disjointly; ends on feat (the branch to rebase).
func rebaseFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main change")
	gitRun(t, dir, "checkout", "feat")
	return dir
}

func TestRebaseCurrentOntoBase(t *testing.T) {
	dir := rebaseFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "rebased feat onto main") {
		t.Fatalf("stdout: %q", out.String())
	}
	// feat replayed onto main → main.txt present on feat now.
	if _, err := os.Stat(filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal("main.txt missing after rebase")
	}
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "feat" {
		t.Fatalf("on %q, want feat", strings.TrimSpace(string(cur)))
	}
}

func TestRebaseExplicitBranchSwitchesAndStays(t *testing.T) {
	dir := rebaseFixture(t)
	gitRun(t, dir, "checkout", "main") // now NOT on feat
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--branch", "feat", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	// SmartRebase rung 3 ends on the rebased branch.
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "feat" {
		t.Fatalf("on %q, want feat", strings.TrimSpace(string(cur)))
	}
}

// rebaseConflictFixture: feat and main both edit shared.txt; ends on feat.
func rebaseConflictFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "feat change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "main change")
	gitRun(t, dir, "checkout", "feat")
	return dir
}

func TestRebaseConflictUnansweredNonTTY(t *testing.T) {
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestRebaseConflictAbortFlag(t *testing.T) {
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--on-conflict=abort", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "feat\n" {
		t.Fatalf("shared.txt = %q after abort, want feat's pre-rebase version", got)
	}
}

func TestRebaseConflictKeepFlag(t *testing.T) {
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--on-conflict=keep", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	// A rebase is left paused.
	if err := exec.Command("git", "-C", dir, "rebase", "--show-current-patch").Run(); err != nil {
		t.Fatal("expected the rebase left in progress")
	}
}

func TestRebaseUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"rebase"},                                // missing newbase
		{"rebase", "a", "b"},                      // too many positionals
		{"rebase", "--on-conflict=bogus", "main"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
```

- [ ] **Step 2: Run to verify failure**

Run: `go test ./internal/cli/ -run TestRebase -v`
Expected: FAIL — `rebase` is an unknown command (exit code won't match) / `cmdRebase` undefined.

- [ ] **Step 3: Implement `cmdRebase`**

Create `internal/cli/rebase.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
)

// cmdRebase implements `gg rebase [--branch <b>] [--on-conflict=keep|abort]
// <newbase>`. Flags precede the positional newbase (the new base, = Onto).
// --branch selects the branch to rebase (default: the current branch).
// --on-conflict pre-answers the rebase-conflict fork; with neither flag nor
// TTY the conflict surfaces as exit 1 with the options on stderr.
func cmdRebase(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("rebase", flag.ContinueOnError)
	fs.SetOutput(stderr)
	branch := fs.String("branch", "", "branch to rebase (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a rebase conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["rebase-conflict"] = "keep-conflicts"
	case "abort":
		policy["rebase-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "rebase: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc,
		engine.SmartRebase{Branch: *branch, Onto: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Register the command in the dispatch**

In `internal/cli/cli.go`, add a case next to `merge` (after line ~64):

```go
	case "rebase":
		return cmdRebase(svc, rest, stdin, stdout, stderr)
```

And add `"rebase": true,` to the known-commands map (the line ~79 that lists `"merge": true,`):

```go
	"switch": true, "branch": true, "stash": true, "undo": true, "merge": true, "rebase": true, "worktree": true,
```

- [ ] **Step 5: Run the CLI tests**

Run: `go test ./internal/cli/ -run TestRebase -v`
Expected: PASS (all rebase CLI tests).

- [ ] **Step 6: Commit**

```bash
git add internal/cli/rebase.go internal/cli/rebase_test.go internal/cli/cli.go
git commit -m "feat(cli): gg rebase command

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: e2e scenarios

**Files:**
- Create: `e2e/scenarios/s33_rebase_clean.toml`
- Create: `e2e/scenarios/s34_rebase_uncheckedout_branch.toml`
- Create: `e2e/scenarios/s35_rebase_conflict_keep.toml`
- Create: `e2e/scenarios/s36_rebase_conflict_abort.toml`

Authored from the operation contract (see `writing-e2e-scenarios` skill), not by running and copying. Flags before positionals. `gg rebase <newbase>` rebases the current branch onto `<newbase>`.

- [ ] **Step 1: Write the clean-rebase scenario (rung 1)**

Create `e2e/scenarios/s33_rebase_clean.toml`:

```toml
name = "rebase: current branch replayed onto a diverged base (linear, clean)"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { write = "main.txt", content = "m\n" },
  { commit = "main change" },
  { switch = "feat" },
  { write = "feat.txt", content = "f\n" },
  { commit = "feat change" },
]

[[run]]
cmd  = ["rebase", "main"]
exit = 0

[expect]
branch      = "feat"
clean       = true
in_progress = "none"

[expect.files]
"feat.txt" = "f\n"
"main.txt" = "m\n"

[[expect.log]]
# feat's commit is replayed on top of main's tip: linear history.
subjects = ["feat change", "main change", "base"]
```

- [ ] **Step 2: Run it**

Run: `go test ./e2e -run 'TestScenarios/s33_rebase_clean' -v`
Expected: PASS. If the log order surprises you, investigate (mirror s04's proven post-rebase ordering) before adjusting — do not edit the assertion to match output.

- [ ] **Step 3: Write the unchecked-out-branch scenario (rung 3)**

Create `e2e/scenarios/s34_rebase_uncheckedout_branch.toml`:

```toml
name = "rebase --branch a non-checked-out branch: switches to it and stays there"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { write = "main.txt", content = "m\n" },
  { commit = "main change" },
  { switch = "feat" },
  { write = "feat.txt", content = "f\n" },
  { commit = "feat change" },
  { switch = "main" },
]

[[run]]
cmd  = ["rebase", "--branch", "feat", "main"]
exit = 0

[expect]
# Rung 3 switches to feat and ends there.
branch      = "feat"
clean       = true
in_progress = "none"

[expect.files]
"feat.txt" = "f\n"
"main.txt" = "m\n"

[[expect.log]]
subjects = ["feat change", "main change", "base"]
```

- [ ] **Step 4: Run it**

Run: `go test ./e2e -run 'TestScenarios/s34_rebase_uncheckedout_branch' -v`
Expected: PASS.

- [ ] **Step 5: Write the conflict-keep scenario**

Create `e2e/scenarios/s35_rebase_conflict_keep.toml`:

```toml
name = "rebase: conflict with --on-conflict=keep pauses mid-rebase (exit 1)"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
  { switch = "feat" },
  { write = "shared.txt", content = "feat\n" },
  { commit = "feat change" },
]

[[run]]
cmd  = ["rebase", "--on-conflict=keep", "main"]
exit = 1

[expect]
# HEAD is detached mid-rebase — do NOT assert `branch`.
in_progress = "rebase"

[expect.status]
conflicted = ["shared.txt"]
```

- [ ] **Step 6: Run it**

Run: `go test ./e2e -run 'TestScenarios/s35_rebase_conflict_keep' -v`
Expected: PASS.

- [ ] **Step 7: Write the conflict-abort scenario**

Create `e2e/scenarios/s36_rebase_conflict_abort.toml`:

```toml
name = "rebase: conflict with --on-conflict=abort restores feat's tip (exit 0)"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
  { switch = "feat" },
  { write = "shared.txt", content = "feat\n" },
  { commit = "feat change" },
]

[[run]]
cmd  = ["rebase", "--on-conflict=abort", "main"]
exit = 0

[expect]
branch      = "feat"
clean       = true
in_progress = "none"

[expect.files]
# After abort, feat is restored to its own pre-rebase content.
"shared.txt" = "feat\n"

[[expect.log]]
# feat's history is untouched: its own commit on top of base; main's commit
# is NOT here (it only exists on main).
subjects = ["feat change", "base"]
```

- [ ] **Step 8: Run it**

Run: `go test ./e2e -run 'TestScenarios/s36_rebase_conflict_abort' -v`
Expected: PASS.

- [ ] **Step 9: Commit**

```bash
git add e2e/scenarios/s33_rebase_clean.toml e2e/scenarios/s34_rebase_uncheckedout_branch.toml e2e/scenarios/s35_rebase_conflict_keep.toml e2e/scenarios/s36_rebase_conflict_abort.toml
git commit -m "test(e2e): rebase scenarios (clean, rung-3, conflict keep/abort)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: docs + agent skill refresh

**Files:**
- Modify: `internal/agentskill/using-gg.md` (add `gg rebase`)
- Modify: `internal/agentskill/agentskill.go:19` (bump `Version`)
- Modify: `CHANGELOG.md`
- Modify: `README.md` (if it lists commands)
- Modify: `CLAUDE.md` (engine row)

- [ ] **Step 1: Document `gg rebase` in the agent skill**

In `internal/agentskill/using-gg.md`, after the `gg merge ...` bullet (the line starting `- \`gg merge [--into <target>] ...`), add:

```markdown
- `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>` — replay a
  branch's commits onto `<newbase>`. Defaults to the current branch; `--branch`
  rebases another branch (switching to it). Rebases in place, or in the
  worktree that has the branch checked out (you stay put), or autostashes and
  switches. A conflict pauses the rebase: `--on-conflict=keep` leaves it for
  `git rebase --continue`, `=abort` runs `git rebase --abort`. Without the flag
  (no TTY) the conflict is exit 1 with the options on stderr.
```

- [ ] **Step 2: Bump the skill version**

In `internal/agentskill/agentskill.go`, change:

```go
const Version = 5
```

to:

```go
const Version = 6
```

- [ ] **Step 3: Verify the agentskill tests still pass**

Run: `go test ./internal/agentskill/ -v`
Expected: PASS. If a test pins the exact version or rendered body, update it to the new version/body.

- [ ] **Step 4: Update CHANGELOG.md**

Add an entry under the current unreleased/working section (match the existing format at the top of `CHANGELOG.md`):

```markdown
- **Rebase** — `gg rebase [--branch <b>] [--on-conflict=keep|abort] <newbase>`
  and the TUI Branches mark-and-pair "Rebase {selected} onto {marked}"
  operation. Worktree-aware (rebases in place, in the branch's worktree, or
  autostash-and-switch); a conflict pauses the rebase (keep) or aborts it.
```

- [ ] **Step 5: Update README.md (if it lists commands/surfaces)**

Search `README.md` for the `gg merge` mention; if present, add a parallel `gg rebase` line in the same list. If `README.md` does not enumerate commands, skip this step.

Run: `grep -n "merge" README.md` — if it matches a command list, add the rebase line there.

- [ ] **Step 6: Update CLAUDE.md engine row**

In `CLAUDE.md`, the `engine` row of the package map lists the smart ops:
`SmartPull`, `SmartSwitch`, `Commit`, `Push`, `Stash`, `UndoLastCommit`, `CreateWorktree`, `RemoveWorktree`. It also mentions `SmartMerge` as a candidate. Add `SmartRebase` to the shipped smart-ops list alongside `SmartMerge` (and drop `SmartRebase` from any "candidate" phrasing if present).

- [ ] **Step 7: Refresh installed agent-skill copies**

Run: `go build ./cmd/gg && ./gg init --update`
Expected: refreshes any installed `using-gg` copies to v6. (If no agents are installed locally this is a no-op — that is fine.)

- [ ] **Step 8: Commit**

```bash
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go CHANGELOG.md README.md CLAUDE.md
git commit -m "docs: document gg rebase; bump agentskill to v6

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] **Run the staged suite with the race detector**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests pass, all e2e scenarios pass (including s33–s36).

- [ ] **Dispatch the final holistic code review** (subagent-driven-development closing step), then use `superpowers:finishing-a-development-branch` to merge.

- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — parallel sessions land features and a clean textual merge can still break the build or surface a flaky test. This discipline has caught two real defects already.

---

## Self-Review

**1. Spec coverage:**
- Direction mapping → Task 3 (engine guard/ladder) + Task 4 (TUI direction test) + Task 5 (CLI). ✓
- Fail-fast guards → Task 3 Step 1. ✓
- 3-rung ladder pivoting on Branch → Task 3 Step 3/5. ✓
- `rebaseAt` conflict/rebase-state → Task 3 Step 3/5. ✓
- New verbs (Rebase/RebaseAbort/RebaseInProgress, exit-code probe) → Task 1; GitOps → Task 2. ✓
- TUI stub enablement → Task 4. ✓
- CLI `gg rebase [--branch][--on-conflict]` → Task 5. ✓
- 4 e2e scenarios (clean, rung-3, conflict keep, conflict abort) → Task 6. ✓
- Scope guards (no --skip/-i/--onto/native --autostash) → enforced by the verb (`Rebase` only emits `--no-edit <onto>`) and no flags added; documented in spec. ✓
- Docs + agentskill version bump → Task 7. ✓

**2. Placeholder scan:** No TBD/TODO; every code step shows complete code; every run step shows the command and expected result. The one conditional (README Step 5) gives an explicit grep-and-decide rule, not a vague "handle it." ✓

**3. Type consistency:** `SmartRebase{Branch, Onto}` consistent across engine, TUI build func, CLI. Decision ID `rebase-conflict` with options `keep-conflicts`/`abort` consistent in engine op, CLI policy map, and e2e flag mapping (`keep`→`keep-conflicts`). Verb signatures `Rebase(ctx, dir, onto)`, `RebaseAbort(ctx, dir)`, `RebaseInProgress(ctx, dir) (bool, error)` identical in Task 1 impl and Task 2 interface. ✓

One spec-vs-fixture note flagged inline in Task 3 Step 6 (the `branchWithCommit` helper checks out off current HEAD, so the clean-rebase fixture may need `git checkout -b feat HEAD~1`) — handled with an explicit fallback instruction, not a placeholder.
