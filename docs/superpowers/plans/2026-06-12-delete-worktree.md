# Delete Worktree Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user remove a linked worktree (optionally its branch, with reactive force) from gigagit's TUI and CLI.

**Architecture:** A Decider-driven `engine.RemoveWorktree` op picks worktree-only vs. worktree+branch up front, runs the *safe* git command, and offers force only when git refuses. Two thin git verbs back it; the TUI binds `d` and the CLI adds `gg worktree remove`. Spec: `docs/superpowers/specs/2026-06-12-delete-worktree-design.md`.

**Tech Stack:** Go 1.26, Bubble Tea (TUI), `internal/gitexec` runner + `gitcmd` builder, `internal/engine` Decider/Event contract.

**Branch:** Create a feature branch off `main` (e.g. `feat/worktree-delete`) before Task 1. `main` is the trunk (not `master`).

## File Structure

- **`internal/git/worktree.go`** (modify) — add `RemoveWorktree` and `DeleteBranch` verbs next to `AddWorktree`.
- **`internal/git/worktree_verbs_test.go`** (modify) — behavioral tests for the two verbs.
- **`internal/engine/remove_worktree.go`** (create) — the `RemoveWorktree` operation + `samePath` guard helper.
- **`internal/engine/remove_worktree_test.go`** (create) — decision-flow + guard tests against real git.
- **`internal/tui/model.go`** (modify) — `d` key handler; selection clamp in the `dataLoadedMsg` case.
- **`internal/tui/worktree_delete_test.go`** (create) — delete handshake + selection-clamp tests.
- **`internal/cli/worktree.go`** (modify) — `cmdWorktreeRemove` + `"remove"` dispatch case.
- **`internal/cli/worktree_test.go`** (modify) — CLI remove tests.

---

### Task 1: git verbs — RemoveWorktree & DeleteBranch

**Files:**
- Modify: `internal/git/worktree.go`
- Test: `internal/git/worktree_verbs_test.go`

- [ ] **Step 1: Write the failing tests**

Append to `internal/git/worktree_verbs_test.go`:

```go
func TestRemoveWorktreeRemovesLinkedTree(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt := filepath.Join(filepath.Dir(dir), "wt-rm")
	if err := repo.AddWorktree(context.Background(), wt, "feature/rm", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := repo.RemoveWorktree(context.Background(), wt, false, nil); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
}

func TestRemoveWorktreeRefusesDirtyUntilForced(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt := filepath.Join(filepath.Dir(dir), "wt-dirty")
	if err := repo.AddWorktree(context.Background(), wt, "feature/d", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// Make the worktree dirty so a non-forced removal is refused.
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(context.Background(), wt, false, nil); err == nil {
		t.Fatal("non-forced removal of a dirty worktree should fail")
	}
	if err := repo.RemoveWorktree(context.Background(), wt, true, nil); err != nil {
		t.Fatalf("forced removal: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after force: %v", err)
	}
}

func TestDeleteBranchRefusesUnmergedUntilForced(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitDo := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Create an unmerged branch: commit on it, then return to main.
	gitDo("checkout", "-b", "feature/unmerged")
	os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x\n"), 0o644)
	gitDo("add", ".")
	gitDo("commit", "-m", "unmerged work")
	gitDo("checkout", "main")

	if err := repo.DeleteBranch(context.Background(), "feature/unmerged", false); err == nil {
		t.Fatal("safe delete of an unmerged branch should fail")
	}
	if err := repo.DeleteBranch(context.Background(), "feature/unmerged", true); err != nil {
		t.Fatalf("forced delete: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--list", "feature/unmerged").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("branch still present after force delete: %q", out)
	}
}
```

Add `"strings"` to the test file's imports if not already present (it already imports `os`, `os/exec`, `path/filepath`, `context`, `testing`).

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/git/ -run 'RemoveWorktree|DeleteBranch' -v`
Expected: FAIL — `repo.RemoveWorktree` / `repo.DeleteBranch` undefined (compile error).

- [ ] **Step 3: Implement the verbs**

Append to `internal/git/worktree.go` (it already imports `context`, `strings`, and `gitcmd`):

```go
// RemoveWorktree removes the linked worktree at path
// (`git worktree remove [--force] <path>`). Output lines are forwarded to onLine
// (nil is allowed). A non-zero exit (e.g. a dirty tree without force) is an error.
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("remove").ArgIf(force, "--force").Arg(path).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree remove", argv, onLine)
	return err
}

// DeleteBranch deletes a local branch (`git branch -d|-D <name>`). Without force
// git refuses to delete a branch that is not fully merged; force uses -D.
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	argv := gitcmd.New("branch").Arg(flag, name).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch (delete)", argv)
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/git/ -run 'RemoveWorktree|DeleteBranch' -v`
Expected: PASS (3 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git/worktree.go internal/git/worktree_verbs_test.go
go vet ./internal/git/
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): add RemoveWorktree and DeleteBranch verbs"
```

---

### Task 2: engine — RemoveWorktree operation

**Files:**
- Create: `internal/engine/remove_worktree.go`
- Test: `internal/engine/remove_worktree_test.go`

Background: engine tests use the real-git `newRepo(t) (string, *git.Repo)` helper and answer decisions with `MapDecider{"<id>": "<option>"}` (both already defined in the `engine` test package). `drain(ch)` collects emitted events. The op emits a `DecisionNeeded` event alongside every `decide`, so option lists can be asserted from drained events.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/remove_worktree_test.go`:

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
)

// addWorktree creates a linked worktree of dir at a sibling path on a new
// branch and returns its absolute path.
func addWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

// gitIn runs a git command in dir, failing the test on error.
func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func branchExists(t *testing.T, dir, branch string) bool {
	t.Helper()
	err := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+branch).Run()
	return err == nil
}

func TestRemoveWorktreeOnlyKeepsBranch(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/keep", "wt-only")

	ch := make(chan Event, 32)
	res, err := RemoveWorktree{Path: wt, Branch: "feature/keep"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{"remove-scope": "worktree-only"}})
	close(ch)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("worktree dir still present: %v", statErr)
	}
	if !branchExists(t, dir, "feature/keep") {
		t.Fatal("branch should be kept for worktree-only")
	}
}

func TestRemoveWorktreeAndBranchDeletesBoth(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/both", "wt-both")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/both"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"remove-scope": "worktree-and-branch"}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "feature/both") {
		t.Fatal("branch should be deleted for worktree-and-branch")
	}
}

func TestRemoveWorktreeAbortAtScopeDoesNothing(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/abort", "wt-abort")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/abort"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"remove-scope": "abort"}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if res.Changed {
		t.Fatal("abort should not change anything")
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree should still exist after abort: %v", statErr)
	}
}

func TestRemoveWorktreeDirtyForced(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/dirty", "wt-dirty")
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RemoveWorktree{Path: wt, Branch: "feature/dirty"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":   "worktree-only",
			"worktree-dirty": "force",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatalf("dirty worktree not removed after force: %v", statErr)
	}
}

func TestRemoveWorktreeDirtyAbortLeavesIt(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/dirty2", "wt-dirty2")
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := RemoveWorktree{Path: wt, Branch: "feature/dirty2"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":   "worktree-only",
			"worktree-dirty": "abort",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if res.Changed {
		t.Fatal("aborting the dirty prompt should leave the worktree")
	}
	if _, statErr := os.Stat(wt); statErr != nil {
		t.Fatalf("worktree should still exist: %v", statErr)
	}
}

func TestRemoveWorktreeUnmergedBranchForceDelete(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/unm", "wt-unm")
	// Commit inside the worktree so the branch has an unmerged commit, but the
	// tree stays clean (so `worktree remove` succeeds without force).
	gitIn(t, wt, "config", "user.email", "t@t")
	gitIn(t, wt, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "unmerged")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/unm"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":    "worktree-and-branch",
			"branch-unmerged": "force-delete",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "feature/unm") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestRemoveWorktreeUnmergedBranchKept(t *testing.T) {
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/unk", "wt-unk")
	gitIn(t, wt, "config", "user.email", "t@t")
	gitIn(t, wt, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(wt, "f.txt"), []byte("y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, wt, "add", ".")
	gitIn(t, wt, "commit", "-m", "unmerged")

	res, err := RemoveWorktree{Path: wt, Branch: "feature/unk"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"remove-scope":    "worktree-and-branch",
			"branch-unmerged": "keep",
		}})
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !branchExists(t, dir, "feature/unk") {
		t.Fatal("branch should be kept")
	}
	if !strings.Contains(res.Summary, "kept") {
		t.Fatalf("summary should mention the branch was kept: %q", res.Summary)
	}
}

func TestRemoveWorktreeDetachedOffersNoBranchOption(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-detached")
	gitIn(t, dir, "worktree", "add", "--detach", wt, "main")

	ch := make(chan Event, 32)
	_, err := RemoveWorktree{Path: wt, Branch: ""}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{"remove-scope": "worktree-only"}})
	close(ch)
	if err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	var opts []string
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == "remove-scope" {
			opts = d.Request.Options
		}
	}
	want := []string{"worktree-only", "abort"}
	if strings.Join(opts, ",") != strings.Join(want, ",") {
		t.Fatalf("detached scope options = %v, want %v", opts, want)
	}
}

func TestRemoveWorktreeGuardsCurrentWorktree(t *testing.T) {
	dir, repo := newRepo(t)
	// repo is rooted at dir, which is the current (and primary) worktree.
	_, err := RemoveWorktree{Path: dir, Branch: "main"}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "currently in") {
		t.Fatalf("want current-worktree guard error, got %v", err)
	}
}

func TestRemoveWorktreeGuardsPrimaryWorktree(t *testing.T) {
	dir, _ := newRepo(t)
	wt := addWorktree(t, dir, "feature/fromhere", "wt-from")
	// Root the repo at the LINKED worktree, then target the primary (dir).
	linked := &git.Repo{Runner: gitexec.NewExecRunner("git", wt, observ.NewRing(50))}
	_, err := RemoveWorktree{Path: dir, Branch: "main"}.Run(
		context.Background(),
		OpDeps{Repo: linked, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Fatalf("want primary-worktree guard error, got %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/engine/ -run RemoveWorktree -v`
Expected: FAIL — `RemoveWorktree` undefined (compile error).

- [ ] **Step 3: Implement the operation**

Create `internal/engine/remove_worktree.go`:

```go
package engine

import (
	"context"
	"fmt"
	"path/filepath"
)

// RemoveWorktree removes a linked worktree at Path, optionally deleting its
// Branch. Force is resolved reactively via the Decider — only when git refuses
// the safe command. Branch is "" for a detached worktree (nothing to delete).
// Path/Branch are resolved by the frontend (see spec §"Engine").
type RemoveWorktree struct {
	Path   string // absolute path of the worktree to remove
	Branch string // its short branch name; "" if detached
}

func (op RemoveWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Path == "" {
		return Result{}, fmt.Errorf("remove worktree: Path is required")
	}

	// Guard: never remove the worktree we're standing in, nor the primary one.
	// git refuses both anyway, but a clean up-front message avoids a pointless
	// "force?" prompt that would also fail.
	top, err := deps.Repo.TopLevel(ctx)
	if err != nil {
		return Result{}, err
	}
	if samePath(op.Path, top) {
		return Result{}, fmt.Errorf("remove worktree: cannot remove the worktree you are currently in (%s)", op.Path)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	// `git worktree list` always lists the main (primary) worktree first.
	if len(wts) > 0 && samePath(op.Path, wts[0].Path) {
		return Result{}, fmt.Errorf("remove worktree: cannot remove the main worktree (%s)", op.Path)
	}

	// Decision 1: scope. A detached worktree has no branch to offer.
	scopeOpts := []string{"worktree-only", "abort"}
	if op.Branch != "" {
		scopeOpts = []string{"worktree-only", "worktree-and-branch", "abort"}
	}
	scope, err := deps.decide(ctx, DecisionRequest{
		ID:      "remove-scope",
		Prompt:  "Remove worktree at " + op.Path + "?",
		Options: scopeOpts,
	})
	if err != nil {
		return Result{}, err
	}
	if scope.Option == "abort" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	// Step 2: remove the worktree (safe, then reactive force on any failure).
	deps.emit(ctx, Progress{Step: "removing worktree", Detail: op.Path})
	onLine := func(line string) { deps.emit(ctx, GitLine{Raw: line}) }
	if err := deps.Repo.RemoveWorktree(ctx, op.Path, false, onLine); err != nil {
		force, derr := deps.decide(ctx, DecisionRequest{
			ID:      "worktree-dirty",
			Prompt:  "Cannot remove " + op.Path + " cleanly (uncommitted changes or untracked files). Force?",
			Options: []string{"force", "abort"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if force.Option != "force" {
			return Result{Summary: "cancelled; worktree not removed", Changed: false}, nil
		}
		if err := deps.Repo.RemoveWorktree(ctx, op.Path, true, onLine); err != nil {
			return Result{}, fmt.Errorf("remove worktree (force): %w", err)
		}
	}

	summary := "removed worktree " + op.Path

	// Step 3: delete the branch if requested. Must follow worktree removal —
	// git refuses to delete a branch still checked out in a worktree.
	if scope.Option == "worktree-and-branch" && op.Branch != "" {
		deps.emit(ctx, Progress{Step: "deleting branch", Detail: op.Branch})
		if err := deps.Repo.DeleteBranch(ctx, op.Branch, false); err != nil {
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      "branch-unmerged",
				Prompt:  "Branch " + op.Branch + " is not fully merged; force-delete discards its unmerged commits.",
				Options: []string{"force-delete", "keep"},
			})
			if derr != nil {
				return Result{}, derr
			}
			if choice.Option == "force-delete" {
				if err := deps.Repo.DeleteBranch(ctx, op.Branch, true); err != nil {
					return Result{}, fmt.Errorf("delete branch (force): %w", err)
				}
				summary += " and branch " + op.Branch
			} else {
				summary += " (branch " + op.Branch + " kept)"
			}
		} else {
			summary += " and branch " + op.Branch
		}
	}

	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// samePath compares two paths after resolving symlinks (git's --show-toplevel
// may resolve symlinks while `worktree list` may not). Falls back to the raw
// string when a path cannot be resolved.
func samePath(a, b string) bool {
	ra, err := filepath.EvalSymlinks(a)
	if err != nil {
		ra = a
	}
	rb, err := filepath.EvalSymlinks(b)
	if err != nil {
		rb = b
	}
	return ra == rb
}

var _ Operation = RemoveWorktree{}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/engine/ -run RemoveWorktree -v`
Expected: PASS (10 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/engine/remove_worktree.go internal/engine/remove_worktree_test.go
go vet ./internal/engine/
git add internal/engine/remove_worktree.go internal/engine/remove_worktree_test.go
git commit -m "feat(engine): add reactive-force RemoveWorktree operation"
```

---

### Task 3: TUI — `d` to delete + selection clamp

**Files:**
- Modify: `internal/tui/model.go` (normal-key switch ~line 134; `dataLoadedMsg` case ~line 70)
- Test: `internal/tui/worktree_delete_test.go` (create)

Background: the TUI modal infrastructure (`m.modal`, ↑/↓/enter/esc) already renders any `DecisionRequest`, so the three forks need no new UI. The delete path completes via `loadCmd` (a reload), which — unlike `reRoot` — does **not** clear `m.sel`; a clamp in the load handler prevents a stale index from panicking the next `m.worktrees[...]` access.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/worktree_delete_test.go`:

```go
package tui

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDeleteKeyRemovesWorktreeThroughModal presses `d` on a linked worktree and
// answers the remove-scope modal with "worktree-only", then drives the op to
// completion and asserts the worktree is gone from disk and the panel.
func TestDeleteKeyRemovesWorktreeThroughModal(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-del")
	runGit(t, dir, "worktree", "add", "-b", "feature/del", wt, "main")

	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)

	// Focus the Worktrees panel and select the linked (non-current) worktree.
	m.focus = panelWorktrees
	idx := -1
	for i, w := range m.worktrees {
		if w.Path != m.currentWorktree {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("expected a linked worktree in %v", m.worktrees)
	}
	m.sel[panelWorktrees] = idx

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)

	answered := false
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			if m.modal.req.ID != "remove-scope" {
				t.Fatalf("unexpected decision %q", m.modal.req.ID)
			}
			// selection 0 == "worktree-only"
			u, _ := m.Update(keyMsg("enter"))
			m = u.(Model)
			answered = true
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if m.running {
		t.Fatal("operation did not finish")
	}
	if !answered {
		t.Fatal("expected a remove-scope decision modal")
	}
	// Apply the post-op reload.
	if cmd != nil {
		u, _ := m.Update(cmd())
		m = u.(Model)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
	for _, w := range m.worktrees {
		if w.Path == wt {
			t.Fatal("removed worktree still in panel after reload")
		}
	}
}

// TestSelectionClampAfterWorktreeReload deletes the last worktree out-of-band
// and reloads; the stale selection index must be clamped so subsequent indexing
// does not panic.
func TestSelectionClampAfterWorktreeReload(t *testing.T) {
	dir, repo := newRepoDir(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-clamp")
	runGit(t, dir, "worktree", "add", "-b", "feature/clamp", wt, "main")

	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	if len(m.worktrees) < 2 {
		t.Fatalf("expected 2 worktrees, got %d", len(m.worktrees))
	}
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = len(m.worktrees) - 1 // select the last row

	// Remove the worktree out-of-band, then reload.
	runGit(t, dir, "worktree", "remove", wt)
	reloaded, _ := m.Update(m.loadCmd()())
	m = reloaded.(Model)

	if got := m.sel[panelWorktrees]; got > len(m.worktrees)-1 {
		t.Fatalf("sel[panelWorktrees] = %d not clamped to <= %d", got, len(m.worktrees)-1)
	}
	// Pressing d on the clamped selection must not panic (index in range).
	_, _ = m.Update(keyMsg("d"))
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'DeleteKey|SelectionClamp' -v`
Expected: FAIL — `d` is unhandled so no op starts (`m.running` stays false → "expected a remove-scope decision modal"); clamp test may panic or report an unclamped index.

- [ ] **Step 3a: Add the `d` key handler**

In `internal/tui/model.go`, in the normal-key `switch msg.String()` block, add a case immediately after the `case "enter":` block (around line 146):

```go
		case "d":
			if !m.running && !m.loading && m.focus == panelWorktrees && len(m.worktrees) > 0 {
				wt := m.worktrees[m.sel[panelWorktrees]]
				return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
			}
```

(`engine` is already imported in model.go.)

- [ ] **Step 3b: Add the selection clamp in the load handler**

In the `case dataLoadedMsg:` block, inside `if msg.err == nil { ... }`, after the existing field assignments (after `m.gitCommonDir = msg.gitCommonDir`), add:

```go
			// Clamp selections so a row removed since the last load (e.g. a
			// deleted worktree) can't leave an index pointing past the end.
			for p := panel(0); p < panelCount; p++ {
				if n := m.panelLen(p); m.sel[p] >= n {
					if n > 0 {
						m.sel[p] = n - 1
					} else {
						m.sel[p] = 0
					}
				}
			}
```

(`m.sel` is a `map[panel]int`; reading a missing key yields 0, which is in range, so the clamp only ever lowers an out-of-range index.)

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'DeleteKey|SelectionClamp' -v`
Expected: PASS (2 tests).

- [ ] **Step 5: Add the footer hint and commit**

In `internal/tui/view.go` at line 99, add `[d]elete` to the footer string, immediately after `[w]orktree`:

```go
	footer := truncate("[p]ull [P]ush [s]witch [S]tash [u]ndo [w]orktree [d]elete  •  [tab] focus  [r] reload  [q] quit", w)
```

```bash
gofmt -w internal/tui/model.go internal/tui/view.go internal/tui/worktree_delete_test.go
go vet ./internal/tui/
go test ./internal/tui/
git add internal/tui/model.go internal/tui/view.go internal/tui/worktree_delete_test.go
git commit -m "feat(tui): d deletes the selected worktree; clamp selection on reload"
```

---

### Task 4: CLI — `gg worktree remove`

**Files:**
- Modify: `internal/cli/worktree.go` (dispatch in `cmdWorktree` ~line 26; add `cmdWorktreeRemove`)
- Test: `internal/cli/worktree_test.go` (append)

Background: `cliDecider{policy, in, out, interactive}` answers forks from a flag-built policy map, falling back to interactive stdin or erroring when non-interactive (`stdinIsTerminal()` checks `os.Stdin`, so tests with a `strings.Reader` are non-interactive and rely on the policy). `runOperation`, `finish`, and `cliDecider` live in `internal/cli/core.go`; `repoT = git.Repo`.

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/worktree_test.go` (imports already include `bytes`, `os`, `os/exec`, `path/filepath`, `strings`, `testing`):

```go
// addCLIWorktree creates a linked worktree of dir and returns its path.
func addCLIWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func TestWorktreeRemoveWorktreeOnly(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm1", "wt-cli-rm1")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", wt}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
	// Branch is kept without --with-branch.
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/rm1").Run() != nil {
		t.Fatal("branch should be kept without --with-branch")
	}
}

func TestWorktreeRemoveWithBranch(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm2", "wt-cli-rm2")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", "--with-branch", wt}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/rm2").Run() == nil {
		t.Fatal("branch should be deleted with --with-branch")
	}
}

func TestWorktreeRemoveDirtyNeedsForce(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm3", "wt-cli-rm3")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644)

	// Without --force and non-interactive: the worktree-dirty decision cannot be
	// answered, so the command fails and the worktree remains.
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "remove", wt}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("dirty removal without --force should fail non-interactively")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should still exist: %v", err)
	}
	// With --force it succeeds.
	var out2, errb2 bytes.Buffer
	if code := Run(dir, []string{"worktree", "remove", "--force", wt}, strings.NewReader(""), &out2, &errb2, ""); code != 0 {
		t.Fatalf("forced removal exit = %d, stderr=%s", code, errb2.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree not removed after --force: %v", err)
	}
}

func TestWorktreeRemoveUnknownPath(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", filepath.Join(dir, "nope")},
		strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("removing an unknown path should be a non-zero exit")
	}
	if !strings.Contains(errb.String(), "no worktree") {
		t.Fatalf("stderr should explain the unknown path: %s", errb.String())
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `go test ./internal/cli/ -run WorktreeRemove -v`
Expected: FAIL — `worktree remove` is an unknown subcommand (exit 2), so the success-path assertions fail.

- [ ] **Step 3: Implement the command and dispatch**

In `internal/cli/worktree.go`, add `"flag"` and `"github.com/gigagit/gg/internal/model"` to the imports. Add a `case "remove":` to the `switch args[0]` in `cmdWorktree`, and update the usage string:

```go
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg worktree <list|add|remove> [args]")
		return 2
	}
	switch args[0] {
	case "list":
		return cmdWorktreeList(repo, stdout, stderr)
	case "add":
		return cmdWorktreeAdd(repo, args[1:], stdin, stdout, stderr, cwdFile)
	case "remove":
		return cmdWorktreeRemove(repo, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "worktree: unknown subcommand %q (use list, add, or remove)\n", args[0])
		return 2
	}
```

Then add the command function:

```go
// cmdWorktreeRemove implements `gg worktree remove [--with-branch] [--force] <path>`.
// Flags must precede the path. --with-branch also deletes the branch;
// --force ignores uncommitted changes and unmerged commits.
func cmdWorktreeRemove(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	withBranch := fs.Bool("with-branch", false, "also delete the worktree's branch")
	force := fs.Bool("force", false, "ignore uncommitted changes and unmerged commits")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "worktree remove: a worktree path is required")
		return 2
	}
	target := fs.Arg(0)

	ctxBg := context.Background()
	wts, err := repo.Worktrees(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	absTarget, _ := filepath.Abs(target)
	var match *model.Worktree
	for i := range wts {
		if wts[i].Path == target || wts[i].Path == absTarget {
			match = &wts[i]
			break
		}
	}
	if match == nil {
		fmt.Fprintf(stderr, "worktree remove: no worktree at %q\n", target)
		return 1
	}

	policy := map[string]string{"remove-scope": "worktree-only"}
	if *withBranch {
		policy["remove-scope"] = "worktree-and-branch"
	}
	if *force {
		policy["worktree-dirty"] = "force"
		policy["branch-unmerged"] = "force-delete"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}

	res, err := runOperation(ctxBg, repo,
		engine.RemoveWorktree{Path: match.Path, Branch: match.Branch}, dec, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	fmt.Fprintln(stdout, "✓ "+res.Summary)
	return 0
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/cli/ -run WorktreeRemove -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/cli/worktree.go internal/cli/worktree_test.go
go vet ./internal/cli/
git add internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): add gg worktree remove [--with-branch] [--force]"
```

---

### Final verification

- [ ] Run the full suite with the race detector and vet:

```bash
gofmt -l internal/ && go vet ./... && go test ./... -race
```

Expected: no `gofmt` output (all formatted), vet clean, all packages PASS.

- [ ] Confirm spec coverage: scope decision, reactive worktree-dirty force, reactive branch-unmerged (force-delete/keep), detached handling, current+primary guards, TUI `d`, selection clamp, CLI `remove` with `--with-branch`/`--force`. All present across Tasks 1–4.
