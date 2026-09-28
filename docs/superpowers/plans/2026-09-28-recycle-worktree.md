# Recycle a Worktree Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Check a local branch out in an existing, idle worktree from the Branches tab (and `gg worktree recycle`), committing or discarding that worktree's uncommitted work first when it has any.

**Architecture:** A `-C <dir>` runner wrapper gives `*git.Repo` an `InDir(dir)` view; `OpDeps.RepoAt` hands ops that view (domain wires it, tests inject it). One new op, `RecycleWorktree{Dir, Branch}`, does the pre-checks, raises the `recycle.dirty` decision (`commit` / `discard` / `abort`) and switches. The TUI adds a branch-menu row that turns the action menu into a worktree picker; the CLI adds `gg worktree recycle`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, real `git` in tests, `gitexec.FakeRunner` for argv assertions.

**Spec:** `docs/superpowers/specs/2026-09-28-recycle-worktree-design.md`

**Seam note (the spec already says this):** the spec put the seam on `GitOps.InDir(dir) GitOps`. A `*git.Repo` method cannot return engine's `GitOps` (engine imports git; the reverse would be a cycle), so the view is injected as `OpDeps.RepoAt func(dir string) GitOps`, nil-safe in the style of `HookRunner`/`CaptureRunner`. `*git.Repo.InDir(dir) *Repo` is the production implementation; `domain.Execute` sets `RepoAt: s.repo.InDir` (method value; `*Repo` satisfies `GitOps`, the func literal below adapts the return type).

## Global Constraints

- Worktree: `/mnt/t/others/gigagit/.claude/worktrees/recycle-worktree`, branch `feat/recycle-worktree`. Every command uses this absolute path (`git -C`, `go test` from a `cd` in the same command). Never touch the main checkout.
- A git verb is one invocation built with `gitcmd`; run via `r.Runner.Run`.
- Ops never block on a human: decisions go through `deps.decide`, option values stay English: `commit`, `discard`, `abort`.
- Every TUI-visible string goes through `i18n.T` with the literal key present in all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`). Engine summary/progress/prompt format literals must also be in all four bundles (`engine_prose_test.go`). Every decision option value needs an `optionDisplayName` case + bundle entries (`options_vocab_test.go`).
- `internal/tui` and `internal/cli` never import `internal/git`.
- Tests: `t.Parallel()` in every new test; real git via `newRepo`/`addWorktree`/`gitIn` (engine), `newTestRepo` (git), `newCLIRepo` (cli).
- Commit message for the automated commit, exactly: `Committed changes due to worktree recycle 2006-01-02 15:04` (Go layout, local time).
- Commit after every task with the attribution trailers from the session reminder.

## Review Focus

Inputs the spec implies but no task's tests would otherwise exercise; each line's test is added to the owning task.

1. **Target path given in a different notation** (trailing slash, symlink, Windows drive-letter case): the op must match it against `git worktree list` output loosely, the way `RemoveWorktree.samePath` does, or refuse with "not found" — never act on the wrong tree. → Task 3 (`TestRecycleWorktreeAcceptsTrailingSlash`).
2. **A file that is both untracked and ignored** in the target: `discard` must keep it (no `-x`). → Task 3 (`TestRecycleWorktreeDiscardKeepsIgnoredFiles`).
3. **Only untracked files in the target** (IsDirty says clean): the op must still prompt. → Task 3 (`TestRecycleWorktreeUntrackedOnlyPrompts`).
4. **Background refresh moved the Branches selection** between opening the picker and pressing enter: the picker must act on the branch captured at open, not the current selection. → Task 5 (`TestRecyclePickerUsesCapturedBranch`).
5. **`--on-dirty` given a value that is not an option**: usage error, exit 2, nothing run. → Task 6 (`TestWorktreeRecycleRejectsBadOnDirty`).

---

### Task 1: `git.Repo.InDir` — a `-C <dir>` view of the repository

**Files:**
- Create: `internal/git/indir.go`
- Test: `internal/git/indir_test.go`

**Interfaces:**
- Produces: `func (r *Repo) InDir(dir string) *Repo` — same `Runner` semantics, every argv prefixed with `-C <dir>`, `Root = dir`.

- [ ] **Step 1: Write the failing tests**

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

// InDir must prefix every invocation with -C <dir> and leave the rest of
// the argv untouched.
func TestInDirPrefixesArgv(t *testing.T) {
	t.Parallel()
	fake := gitexec.NewFakeRunner()
	repo := &Repo{Runner: fake}
	wt := repo.InDir("/elsewhere/wt")
	if _, err := wt.CurrentBranch(context.Background()); err != nil {
		t.Fatalf("CurrentBranch: %v", err)
	}
	if len(fake.Calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.Calls))
	}
	got := fake.Calls[0].Argv
	if len(got) < 3 || got[0] != "-C" || got[1] != "/elsewhere/wt" {
		t.Fatalf("argv = %v, want -C /elsewhere/wt …", got)
	}
	if !slices.Contains(got[2:], "rev-parse") && !slices.Contains(got[2:], "symbolic-ref") && !slices.Contains(got[2:], "branch") {
		t.Fatalf("argv after the prefix = %v, want the CurrentBranch verb", got[2:])
	}
	if wt.Root != "/elsewhere/wt" {
		t.Fatalf("Root = %q, want the dir", wt.Root)
	}
}

// Against a real repo with a linked worktree, the view answers for the
// OTHER worktree: its branch and its dirty state, not ours.
func TestInDirReadsTheOtherWorktree(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	wt := filepath.Join(filepath.Dir(dir), "wt-other")
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", "other", wt, "main")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("x\n"), 0o644)

	view := repo.InDir(wt)
	ctx := context.Background()
	if b, err := view.CurrentBranch(ctx); err != nil || b != "other" {
		t.Fatalf("CurrentBranch = %q, %v; want other", b, err)
	}
	st, err := view.Status(ctx)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Counts().Untracked != 1 {
		t.Fatalf("other worktree untracked = %d, want 1 (%+v)", st.Counts().Untracked, st.Files)
	}
	if top, err := view.TopLevel(ctx); err != nil || top != filepath.Clean(wt) {
		t.Fatalf("TopLevel = %q, %v; want %s", top, err, wt)
	}
	// The original repo is untouched.
	if b, err := repo.CurrentBranch(ctx); err != nil || b != "main" {
		t.Fatalf("original CurrentBranch = %q, %v; want main", b, err)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/git -run 'TestInDir' 2>&1 | head -20`
Expected: build failure `repo.InDir undefined`.

- [ ] **Step 3: Implement**

`internal/git/indir.go`:

```go
package git

import (
	"context"
	"path/filepath"

	"github.com/homeend/gigagit/internal/gitexec"
)

// InDir returns a view of the repository whose every git invocation runs
// against the worktree at dir (git -C <dir> …). dir must be a worktree top
// level of THIS repository; the view shares the runner (and so the
// subprocess cap and the span recorder) and pins Root so TopLevel answers
// without a process. Ops reach it through OpDeps.RepoAt.
func (r *Repo) InDir(dir string) *Repo {
	return &Repo{Runner: dirRunner{dir: dir, inner: r.Runner}, Root: filepath.Clean(dir)}
}

// dirRunner prefixes every argv with -C <dir>. It is the one place the
// prefix is spelled, so a FakeRunner test sees exactly "-C", dir, verb….
type dirRunner struct {
	dir   string
	inner gitexec.Runner
}

func (d dirRunner) prefix(argv []string) []string {
	out := make([]string, 0, len(argv)+2)
	out = append(out, "-C", d.dir)
	return append(out, argv...)
}

func (d dirRunner) Run(ctx context.Context, name string, argv []string) (gitexec.Result, error) {
	return d.inner.Run(ctx, name, d.prefix(argv))
}

func (d dirRunner) RunEnv(ctx context.Context, name string, argv, env []string) (gitexec.Result, error) {
	return d.inner.RunEnv(ctx, name, d.prefix(argv), env)
}

func (d dirRunner) Stream(ctx context.Context, name string, argv []string, onLine func(string)) (gitexec.Result, error) {
	return d.inner.Stream(ctx, name, d.prefix(argv), onLine)
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/git -run 'TestInDir' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add internal/git/indir.go internal/git/indir_test.go && git commit -m "feat(git): Repo.InDir — a -C <dir> view for acting on another worktree"
```

---

### Task 2: `OpDeps.RepoAt` seam, wired by domain

**Files:**
- Modify: `internal/engine/operation.go` (the `OpDeps` struct + a nil-safe accessor)
- Modify: `internal/engine/gitops.go:1-15` (doc comment: how ops reach another worktree)
- Modify: `internal/domain/service.go:383-389` (`Execute`'s `OpDeps` literal)
- Test: `internal/engine/repoat_test.go`

**Interfaces:**
- Produces: `OpDeps.RepoAt func(dir string) GitOps`; `func (d OpDeps) repoAt(dir string) (GitOps, error)` returning `ErrNoRepoAt` when unset.

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"errors"
	"testing"
)

func TestRepoAtNilIsATypedError(t *testing.T) {
	t.Parallel()
	_, err := OpDeps{}.repoAt("/x")
	if !errors.Is(err, ErrNoRepoAt) {
		t.Fatalf("err = %v, want ErrNoRepoAt", err)
	}
}

func TestRepoAtHandsBackTheView(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	deps := OpDeps{Repo: repo, RepoAt: func(dir string) GitOps { return repo.InDir(dir) }}
	view, err := deps.repoAt("/x")
	if err != nil || view == nil {
		t.Fatalf("repoAt = %v, %v", view, err)
	}
	if top, err := view.TopLevel(context.Background()); err != nil || top != "/x" {
		t.Fatalf("view.TopLevel = %q, %v; want /x (Root pinned by InDir)", top, err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/engine -run 'TestRepoAt' 2>&1 | head -5`
Expected: build failure `unknown field RepoAt`.

- [ ] **Step 3: Implement**

In `internal/engine/operation.go`, add to `OpDeps` after `CaptureRunner`:

```go
	// RepoAt returns a GitOps view acting on ANOTHER worktree of this
	// repository (git -C <dir>), for ops that recycle or inspect a worktree
	// gg is not running in. Nil (direct engine use, fakes) makes repoAt
	// return ErrNoRepoAt — an op that needs it fails cleanly instead of
	// acting on the wrong tree. domain.Execute wires it to *git.Repo.InDir.
	RepoAt func(dir string) GitOps
```

and after `captureRunner()`:

```go
// ErrNoRepoAt is returned by repoAt when OpDeps carries no RepoAt seam.
var ErrNoRepoAt = errors.New("this repository handle cannot act on another worktree")

// repoAt is the nil-safe form of RepoAt (style of hookRunner).
func (d OpDeps) repoAt(dir string) (GitOps, error) {
	if d.RepoAt == nil {
		return nil, ErrNoRepoAt
	}
	return d.RepoAt(dir), nil
}
```

(add `"errors"` to the imports if missing.)

In `internal/engine/gitops.go` doc comment, append one sentence after "…nil-embeds GitOps and implements only the verbs under test (see writefile_test.go).":

```go
// An op that must act on ANOTHER worktree asks OpDeps.repoAt(dir) for a
// GitOps view (git -C <dir>); the interface itself stays single-worktree.
```

In `internal/domain/service.go` `Execute`, extend the literal:

```go
	out, opErr := op.Run(ctx, engine.OpDeps{
		Repo:     s.repo,
		RepoAt:   func(dir string) engine.GitOps { return s.repo.InDir(dir) },
		Events:   events,
		Decider:  dec,
		Escalate: res.Escalate,
		Versions: versions,
	})
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go build ./... && go test ./internal/engine -run 'TestRepoAt' 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add internal/engine/operation.go internal/engine/gitops.go internal/engine/repoat_test.go internal/domain/service.go && git commit -m "feat(engine): OpDeps.RepoAt hands ops a git -C view of another worktree"
```

---

### Task 3: `RecycleWorktree` op

**Files:**
- Create: `internal/engine/recycle_worktree.go`
- Test: `internal/engine/recycle_worktree_test.go`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (engine prose keys — see Step 3b)

**Interfaces:**
- Consumes: `OpDeps.repoAt` (Task 2); `git.PausedOpIn(gitDir)`, `git.LockFiles(dirs...)`, `samePath` (`remove_worktree.go:130`).
- Produces:
  ```go
  const RecycleDirtyDecisionID = "recycle.dirty"
  type RecycleWorktree struct { Dir, Branch string; Now func() time.Time }
  const RecycleCommitLayout = "2006-01-02 15:04"
  func RecycleCommitMessage(now time.Time) string
  ```
  Summary shapes: `recycled <dir>: <old> → <branch>` (+ `; committed <sha>` | `; changes discarded`), `recycle cancelled`.

- [ ] **Step 1: Write the failing tests**

```go
package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// recycleFixture: main repo on main, a linked worktree "wt-a" on branch
// "a", and a loose branch "target" checked out nowhere. Returns (dir,
// deps-with-RepoAt, wtPath).
func recycleFixture(t *testing.T) (string, OpDeps, string) {
	t.Helper()
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "target")
	wt := addWorktree(t, dir, "a", "wt-a")
	deps := OpDeps{Repo: repo, RepoAt: func(d string) GitOps { return repo.InDir(d) }}
	return dir, deps, wt
}

var fixedNow = func() time.Time { return time.Date(2026, 9, 28, 14, 5, 0, 0, time.Local) }

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestRecycleWorktreeCleanSwitches(t *testing.T) {
	t.Parallel()
	dir, deps, wt := recycleFixture(t)
	ch := make(chan Event, 32)
	deps.Events = ch
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	close(ch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "recycled") || !strings.Contains(res.Summary, "a → target") {
		t.Fatalf("result = %+v", res)
	}
	if got := wtHead(t, wt); got != "target" {
		t.Fatalf("worktree HEAD = %q, want target", got)
	}
	if got := wtHead(t, dir); got != "main" {
		t.Fatalf("main worktree HEAD = %q, want main (untouched)", got)
	}
	for _, e := range drain(ch) {
		if _, ok := e.(DecisionNeeded); ok {
			t.Fatal("a clean worktree must not prompt")
		}
	}
}

func TestRecycleWorktreeDirtyCommit(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("edited\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "commit"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Summary, "; committed ") {
		t.Fatalf("summary = %q, want '; committed <sha>'", res.Summary)
	}
	// The commit landed on the OLD branch "a", with both files, and the
	// exact automated message.
	if got := gitOut(t, wt, "log", "-1", "--format=%s", "a"); got != "Committed changes due to worktree recycle 2026-09-28 14:05" {
		t.Fatalf("commit subject on a = %q", got)
	}
	if got := gitOut(t, wt, "show", "--stat", "--format=", "a"); !strings.Contains(got, "README.md") || !strings.Contains(got, "new.txt") {
		t.Fatalf("commit on a lacks a file:\n%s", got)
	}
	if got := wtHead(t, wt); got != "target" {
		t.Fatalf("worktree HEAD = %q, want target", got)
	}
	if got := gitOut(t, wt, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree not clean after recycle:\n%s", got)
	}
}

func TestRecycleWorktreeDirtyDiscard(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("edited\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "discard"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Summary, "; changes discarded") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); !os.IsNotExist(err) {
		t.Fatal("untracked new.txt must be deleted by discard")
	}
	if got := wtHead(t, wt); got != "target" {
		t.Fatalf("worktree HEAD = %q, want target", got)
	}
	if got := gitOut(t, wt, "log", "-1", "--format=%s", "a"); strings.Contains(got, "recycle") {
		t.Fatal("discard must not commit")
	}
}

// Review Focus 2: an ignored file survives discard (clean runs without -x).
func TestRecycleWorktreeDiscardKeepsIgnoredFiles(t *testing.T) {
	t.Parallel()
	dir, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("*.log\n"), 0o644)
	gitIn(t, dir, "add", ".gitignore")
	gitIn(t, dir, "commit", "-m", "ignore logs")
	gitIn(t, wt, "merge", "--ff-only", "main") // bring .gitignore into wt-a
	os.WriteFile(filepath.Join(wt, "debug.log"), []byte("keep me\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "discard"}
	if _, err := (RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}).Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "debug.log")); err != nil {
		t.Fatal("ignored debug.log must survive discard")
	}
}

// Review Focus 3: only untracked files — IsDirty says clean, the op must
// still prompt.
func TestRecycleWorktreeUntrackedOnlyPrompts(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	ch := make(chan Event, 32)
	deps.Events = ch
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "abort"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	close(ch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	var asked bool
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == RecycleDirtyDecisionID {
			asked = true
			if strings.Join(d.Request.Options, ",") != "commit,discard,abort" {
				t.Fatalf("options = %v", d.Request.Options)
			}
		}
	}
	if !asked {
		t.Fatal("expected the recycle.dirty decision")
	}
	if res.Changed || res.Summary != "recycle cancelled" {
		t.Fatalf("abort result = %+v", res)
	}
	if got := wtHead(t, wt); got != "a" {
		t.Fatalf("abort must not switch; HEAD = %q", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err != nil {
		t.Fatal("abort must keep new.txt")
	}
}

func TestRecycleWorktreeNoDeciderIsAnError(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	_, err := RecycleWorktree{Dir: wt, Branch: "target"}.Run(context.Background(), deps)
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("err = %v, want ErrDecisionRequired", err)
	}
	if got := wtHead(t, wt); got != "a" {
		t.Fatalf("HEAD = %q, want a (untouched)", got)
	}
}

func TestRecycleWorktreeRefusals(t *testing.T) {
	t.Parallel()
	t.Run("branch checked out elsewhere", func(t *testing.T) {
		t.Parallel()
		_, deps, wt := recycleFixture(t)
		_, err := RecycleWorktree{Dir: wt, Branch: "main"}.Run(context.Background(), deps)
		if err == nil || !strings.Contains(err.Error(), "already checked out") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("unknown dir", func(t *testing.T) {
		t.Parallel()
		_, deps, _ := recycleFixture(t)
		_, err := RecycleWorktree{Dir: filepath.Join(t.TempDir(), "nope"), Branch: "target"}.Run(context.Background(), deps)
		if err == nil || !strings.Contains(err.Error(), "not a worktree") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("paused merge", func(t *testing.T) {
		t.Parallel()
		_, deps, wt := recycleFixture(t)
		gitDir := gitOut(t, wt, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
		os.WriteFile(filepath.Join(gitDir, "MERGE_HEAD"), []byte("0000000000000000000000000000000000000000\n"), 0o644)
		_, err := RecycleWorktree{Dir: wt, Branch: "target"}.Run(context.Background(), deps)
		if err == nil || !strings.Contains(err.Error(), "merge in progress") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("lock file", func(t *testing.T) {
		t.Parallel()
		_, deps, wt := recycleFixture(t)
		gitDir := gitOut(t, wt, "rev-parse", "--path-format=absolute", "--absolute-git-dir")
		os.WriteFile(filepath.Join(gitDir, "index.lock"), nil, 0o644)
		_, err := RecycleWorktree{Dir: wt, Branch: "target"}.Run(context.Background(), deps)
		if err == nil || !strings.Contains(err.Error(), "locked") {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("no RepoAt seam", func(t *testing.T) {
		t.Parallel()
		_, deps, wt := recycleFixture(t)
		deps.RepoAt = nil
		_, err := RecycleWorktree{Dir: wt, Branch: "target"}.Run(context.Background(), deps)
		if !errors.Is(err, ErrNoRepoAt) {
			t.Fatalf("err = %v, want ErrNoRepoAt", err)
		}
	})
}

// Review Focus 1: a trailing slash still names the same worktree.
func TestRecycleWorktreeAcceptsTrailingSlash(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	if _, err := (RecycleWorktree{Dir: wt + string(filepath.Separator), Branch: "target", Now: fixedNow}).Run(context.Background(), deps); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := wtHead(t, wt); got != "target" {
		t.Fatalf("HEAD = %q, want target", got)
	}
}

func TestRecycleWorktreeDetachedTargetCommits(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	gitIn(t, wt, "switch", "--detach")
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "commit"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Summary, "detached → target") || !strings.Contains(res.Summary, "; committed ") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if got := wtHead(t, wt); got != "target" {
		t.Fatalf("HEAD = %q, want target", got)
	}
}

func TestRecycleCommitMessageLayout(t *testing.T) {
	t.Parallel()
	got := RecycleCommitMessage(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	if got != "Committed changes due to worktree recycle 2026-01-02 03:04" {
		t.Fatalf("message = %q", got)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/engine -run 'TestRecycle' 2>&1 | head -5`
Expected: build failure `undefined: RecycleWorktree`.

- [ ] **Step 3a: Implement the op**

`internal/engine/recycle_worktree.go`:

```go
package engine

import (
	"errors"
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/git"
)

// RecycleDirtyDecisionID is raised when the target worktree has staged,
// unstaged or untracked changes: commit them on the branch that is leaving,
// discard them (untracked files are deleted, ignored files kept), or abort.
const RecycleDirtyDecisionID = "recycle.dirty"

// RecycleCommitLayout is the timestamp layout in the automated commit
// subject (local time).
const RecycleCommitLayout = "2006-01-02 15:04"

// RecycleCommitMessage is the subject of the commit "commit" makes.
func RecycleCommitMessage(now time.Time) string {
	return "Committed changes due to worktree recycle " + now.Format(RecycleCommitLayout)
}

// RecycleWorktree checks Branch out in the existing worktree at Dir — a
// worktree gg is NOT running in — replacing whatever it has checked out.
// Uncommitted work there is handled through the recycle.dirty decision
// before the switch; every pre-check runs before any mutation. Default
// TreeWrite reservation: the gate is keyed by the git common dir, so it
// already covers the other worktree.
type RecycleWorktree struct {
	Dir    string           // target worktree top level
	Branch string           // local branch to check out there
	Now    func() time.Time // clock for the commit message; nil = time.Now
}

var _ Operation = RecycleWorktree{}

func (op RecycleWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Dir == "" || op.Branch == "" {
		return Result{}, fmt.Errorf("recycle worktree: Dir and Branch are required")
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	var target string
	for _, w := range wts {
		if w.Bare {
			continue
		}
		if samePath(w.Path, op.Dir) {
			target = w.Path
		}
		if w.Branch == op.Branch {
			return Result{}, fmt.Errorf("%s is already checked out in %s", op.Branch, w.Path)
		}
	}
	if target == "" {
		return Result{}, fmt.Errorf("%s is not a worktree of this repository", filepath.Clean(op.Dir))
	}

	wt, err := deps.repoAt(target)
	if err != nil {
		return Result{}, err
	}
	gitDir, err := wt.GitDir(ctx)
	if err != nil {
		return Result{}, err
	}
	if paused := git.PausedOpIn(gitDir); paused != "" {
		return Result{}, fmt.Errorf("%s has a %s in progress", target, paused)
	}
	if locks := git.LockFiles(gitDir); len(locks) > 0 {
		return Result{}, fmt.Errorf("%s is locked (%s)", target, locks[0].Name)
	}

	old, err := wt.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if old == "" {
		old = "detached"
	}
	st, err := wt.Status(ctx)
	if err != nil {
		return Result{}, err
	}
	c := st.Counts()
	var tail string
	if c.Staged+c.Unstaged+c.Conflicted+c.Untracked > 0 {
		resp, err := deps.decide(ctx, PromptReq(RecycleDirtyDecisionID,
			"%s has uncommitted changes on %s", []string{"commit", "discard", "abort"}, target, old))
		if err != nil {
			return Result{}, err
		}
		switch resp.Option {
		case "commit":
			deps.emit(ctx, Progress{Step: "committing", Detail: target})
			if err := wt.StageAll(ctx); err != nil {
				return Result{}, err
			}
			now := op.Now
			if now == nil {
				now = time.Now
			}
			if err := wt.Commit(ctx, RecycleCommitMessage(now()), false, false); err != nil {
				return Result{}, err
			}
			sha := ""
			if line, lerr := wt.CommitLine(ctx, "HEAD"); lerr == nil {
				sha = line.Hash
			}
			tail = "; committed " + sha
		case "discard":
			deps.emit(ctx, Progress{Step: "discarding", Detail: target})
			// Same repo-root pathspec Discard{All} uses; clean without -x
			// keeps ignored files. Both run even if the first fails.
			var errs []error
			if err := wt.RestoreWorktree(ctx, []string{":/"}); err != nil {
				errs = append(errs, fmt.Errorf("restore: %w", err))
			}
			if err := wt.CleanUntracked(ctx, []string{":/"}); err != nil {
				errs = append(errs, fmt.Errorf("clean: %w", err))
			}
			if len(errs) > 0 {
				return Result{}, errors.Join(errs...)
			}
			tail = "; changes discarded"
		default:
			return Result{}.WithSummary("recycle cancelled"), nil
		}
	}

	deps.emit(ctx, Progress{Step: "switching", Detail: op.Branch})
	if err := wt.Switch(ctx, op.Branch); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true}.WithSummary("recycled %s: %s → %s", target, old, op.Branch)
	if tail != "" {
		res = res.AppendSummary(tail)
	}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

Check that `GitOps` already lists `Worktrees`, `GitDir`, `CurrentBranch`, `Status`, `StageAll`, `Commit`, `CommitLine`, `RestoreWorktree`, `CleanUntracked`, `Switch` (`grep -n 'StageAll\|CommitLine\|RestoreWorktree\|CleanUntracked\|Switch(' internal/engine/gitops.go`). If `StageAll` is missing, add `StageAll(ctx context.Context) error` to the interface next to the other stage verbs.

- [ ] **Step 3b: Add the engine prose keys to the four bundles**

`engine_prose_test.go` needs every new literal: `"%s has uncommitted changes on %s"`, `"recycled %s: %s → %s"`, `"recycle cancelled"` (`"; committed "` and `"; changes discarded"` reach `AppendSummary` through the `tail` variable, so the prose scan skips them). Append to each of `internal/i18n/lang/{ja,ko,zh,ru}.toml` near the other engine prose (search for `"switched to %s"` in each file and add after it):

```toml
"%s has uncommitted changes on %s" = "%s に %s のコミットされていない変更があります"
"recycled %s: %s → %s" = "%s を再利用: %s → %s"
"recycle cancelled" = "再利用を中止しました"
```

ko: `"%s에 %s의 커밋되지 않은 변경 사항이 있습니다"`, `"%s 재사용: %s → %s"`, `"재사용을 취소했습니다"`.
zh: `"%s 上有 %s 的未提交更改"`, `"已回收 %s：%s → %s"`, `"已取消回收"`.
ru: `"В %s есть незакоммиченные изменения на %s"`, `"переиспользован %s: %s → %s"`, `"переиспользование отменено"`.

- [ ] **Step 4: Run to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/engine -run 'TestRecycle|TestRepoAt' 2>&1 | tail -15 && go test ./internal/tui -run 'TestEngineProse|TestDecisionOptionValuesTranslated' 2>&1 | tail -15`
Expected: engine `ok`. The TUI option-vocab test FAILS for `commit`/`discard` — that is Task 4's job; the prose test must pass now.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add internal/engine/recycle_worktree.go internal/engine/recycle_worktree_test.go internal/engine/gitops.go internal/i18n/lang && git commit -m "feat(engine): RecycleWorktree checks a branch out in another worktree, committing or discarding its changes first"
```

---

### Task 4: TUI — option labels, refresh mapping, the branch-menu row and the worktree picker

**Files:**
- Modify: `internal/tui/i18n_display.go:167-…` (`optionDisplayName` cases `commit`, `discard`)
- Modify: `internal/tui/source.go:308-…` (`opAffectedSources`)
- Modify: `internal/tui/action_menu.go:299-301` (row registration after `showInWorktreesRow`)
- Create: `internal/tui/recycle_worktree.go`
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/recycle_worktree_test.go`

**Interfaces:**
- Consumes: `engine.RecycleWorktree{Dir, Branch}`, `engine.RecycleDirtyDecisionID`; `m.worktrees`, `m.currentWorktree`, `m.branches`, `m.backingIndex(panelBranches)`, `m.opsIdle()`, `m.startOp(op)`, `actionMenu{rows}`, `elidePath`, `domain.Sessions().List()` (`SessionInfo.Dir`), `domain.SameCheckout`.
- Produces: row id `recycle-worktree`; picker row ids `recycle-into:<path>`; `func (m Model) recycleWorktreeRow() (actionRow, bool)`; `func (m Model) openRecyclePicker(branch string) Model`.

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// recycleModel: Branches tab; "loose" is checked out nowhere, "feature" is
// in a linked worktree, "main" is the current worktree's branch.
func recycleModel() Model {
	m := showInWorktreesModel()
	m.currentWorktree = "/repo"
	m.sel[panelBranches] = 2 // "loose"
	return m
}

func TestRecycleRowOfferedOnlyForUncheckedOutBranch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		sel  int
		want bool
	}{
		{0, false}, // feature: in a worktree → Show in Worktrees instead
		{1, false}, // main: current worktree
		{2, true},  // loose
	} {
		m := recycleModel()
		m.sel[panelBranches] = tc.sel
		got := ids(availableActions(m))
		if got["recycle-worktree"] != tc.want {
			t.Errorf("branch %q: recycle-worktree offered = %v, want %v", m.branches[tc.sel].Name, got["recycle-worktree"], tc.want)
		}
		if got["recycle-worktree"] && got["show-in-worktrees"] {
			t.Errorf("branch %q: the two rows must be mutually exclusive", m.branches[tc.sel].Name)
		}
	}
}

func TestRecycleRowHiddenWhenNoOtherWorktree(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.worktrees = []model.Worktree{{Path: "/repo", Branch: "main"}}
	if ids(availableActions(m))["recycle-worktree"] {
		t.Fatal("no candidate worktree → no row")
	}
}

func TestRecyclePickerListsOtherWorktreesOnly(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	m.worktrees = append(m.worktrees, model.Worktree{Path: "/repo-bare", Bare: true}, model.Worktree{Path: "/repo-wt/det", Detached: true})
	row, ok := rowByID(availableActions(m), "recycle-worktree")
	if !ok {
		t.Fatal("row not offered")
	}
	nm, _ := row.run(m)
	m = nm.(Model)
	if m.actionMenu == nil {
		t.Fatal("picker must re-populate the action menu")
	}
	got := ids(m.actionMenu.rows)
	for _, want := range []string{"recycle-into:/repo-wt/other", "recycle-into:/repo-wt/feature", "recycle-into:/repo-wt/det"} {
		if !got[want] {
			t.Errorf("missing picker row %s (have %v)", want, got)
		}
	}
	for _, no := range []string{"recycle-into:/repo", "recycle-into:/repo-bare"} {
		if got[no] {
			t.Errorf("picker must not list %s", no)
		}
	}
	// Labels: path elided in the middle + the current branch / "detached".
	var sawDetached bool
	for _, r := range m.actionMenu.rows {
		if strings.HasSuffix(r.id, "/det") && strings.Contains(r.label, "detached") {
			sawDetached = true
		}
	}
	if !sawDetached {
		t.Fatal("detached worktree row must say detached")
	}
}

func TestRecyclePickerEnterStartsTheOp(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	pick, ok := rowByID(m.actionMenu.rows, "recycle-into:/repo-wt/other")
	if !ok {
		t.Fatal("picker row missing")
	}
	nm, cmd := pick.run(m)
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("expected the op to start")
	}
	if !m.running || m.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want a running RecycleWorktree", m.running, m.opName)
	}
}

// Review Focus 4: the picker acts on the branch captured when it opened.
func TestRecyclePickerUsesCapturedBranch(t *testing.T) {
	t.Parallel()
	m := recycleModel()
	row, _ := rowByID(availableActions(m), "recycle-worktree")
	nm, _ := row.run(m)
	m = nm.(Model)
	m.sel[panelBranches] = 0 // a background refresh moved the cursor
	if m.recycleBranch != "loose" {
		t.Fatalf("recycleBranch = %q, want loose (captured at open)", m.recycleBranch)
	}
	pick, _ := rowByID(m.actionMenu.rows, "recycle-into:/repo-wt/other")
	nm, _ = pick.run(m)
	m = nm.(Model)
	if !m.running || m.opName != engine.OpName(engine.RecycleWorktree{}) {
		t.Fatalf("running=%v opName=%q, want a running RecycleWorktree", m.running, m.opName)
	}
	// recycleInto is the one dispatch path; it reads m.recycleBranch, so the
	// op carried "loose" — pin that on the pure builder too.
	if op := recycleOpFor("/repo-wt/other", m.recycleBranch); op.Branch != "loose" || op.Dir != "/repo-wt/other" {
		t.Fatalf("recycleOpFor = %+v", op)
	}
}

func TestRecycleOptionLabelsTranslated(t *testing.T) {
	t.Parallel()
	for _, v := range []string{"commit", "discard", "abort"} {
		if optionDisplayName(v) == "" {
			t.Errorf("optionDisplayName(%q) empty", v)
		}
	}
}

func TestRecycleOpRefreshesBranchesAndWorktrees(t *testing.T) {
	t.Parallel()
	got := opAffectedSources(engine.RecycleWorktree{})
	want := map[sourceKey]bool{srcBranches: true, srcWorktrees: true}
	for _, s := range got {
		delete(want, s)
	}
	if len(want) > 0 {
		t.Fatalf("opAffectedSources(RecycleWorktree) = %v, missing %v", got, want)
	}
}
```

`m.running` / `m.opName` are the existing dispatch observables (`switch_dirty_test.go` uses them); the test model's `svc` runs over a `FakeRunner`, so the dispatched op fails harmlessly in the background.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/tui -run 'TestRecycle' 2>&1 | head -10`
Expected: build failure or `recycle-worktree offered = false`.

- [ ] **Step 3: Implement**

`internal/tui/recycle_worktree.go`:

```go
package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// recycleWorktreeRow offers "Recycle a worktree…" on a LOCAL branch that no
// worktree has checked out — the exact inverse of showInWorktreesRow — when
// there is at least one worktree to recycle (not the one gg runs in, not
// bare). Picking it turns the action menu into the worktree picker.
func (m Model) recycleWorktreeRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelBranches)
	if !ok || bi < 0 || bi >= len(m.branches) {
		return actionRow{}, false
	}
	b := m.branches[bi]
	if _, has := m.worktreeAbsPathForBranch(b.Name); has {
		return actionRow{}, false
	}
	if len(m.recycleCandidates()) == 0 {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "recycle-worktree",
		label: i18n.T("Recycle a worktree…"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openRecyclePicker(name), nil },
	}, true
}

// recycleCandidates is every worktree the picker lists: linked or main, not
// bare, and not the one gg is running in.
func (m Model) recycleCandidates() []model.Worktree {
	var out []model.Worktree
	for _, w := range m.worktrees {
		if w.Bare || w.Path == "" || domain.SameCheckout(w.Path, m.currentWorktree) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// openRecyclePicker re-populates the action menu with one row per candidate
// worktree. The branch is captured at open into m.recycleBranch: a
// background refresh may move the Branches cursor before the user picks,
// and a Model field (not a closure) keeps the capture test-observable. A
// worktree with a running agent session asks once before the op starts.
func (m Model) openRecyclePicker(branch string) Model {
	m.recycleBranch = branch
	live := map[string]bool{}
	for _, info := range domain.Sessions().List() {
		live[filepath.Clean(info.Dir)] = true
	}
	width := max(24, m.width/2)
	var rows []actionRow
	for _, w := range m.recycleCandidates() {
		dir := w.Path
		cur := w.Branch
		if cur == "" {
			cur = i18n.T("detached")
		}
		label := elidePath(dir, width) + "  " + cur
		isLive := live[filepath.Clean(dir)]
		if isLive {
			label += "  " + i18n.T("(agent session running)")
		}
		rows = append(rows, actionRow{
			id:    "recycle-into:" + dir,
			label: label,
			run:   func(m Model) (tea.Model, tea.Cmd) { return m.recycleInto(dir, isLive) },
		})
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// recycleOpFor is the one place the op is built from a picked dir and the
// captured branch (pure, so tests pin the pairing).
func recycleOpFor(dir, branch string) engine.RecycleWorktree {
	return engine.RecycleWorktree{Dir: dir, Branch: branch}
}

// recycleInto dispatches the op for the picked worktree; a live agent
// session there gets the yes/no gate first.
func (m Model) recycleInto(dir string, live bool) (tea.Model, tea.Cmd) {
	op := recycleOpFor(dir, m.recycleBranch)
	if live {
		return m.mustConfirmOp(op, i18n.T("An agent session is running in %s. Recycle it anyway?", dir))
	}
	mm, cmd := m.startOp(op)
	return mm, cmd
}
```

Add the field to `Model` in `internal/tui/model.go` next to `currentWorktree` (line ~79):

```go
	recycleBranch string // branch captured when the Recycle-a-worktree picker opened
```

`runVisibleRow` sets `m.actionMenu = nil` before calling `run`, so the picker closes on enter by itself. Go 1.26 has the builtin `max`.

In `internal/tui/action_menu.go` after the `showInWorktreesRow` block (line ~299):

```go
	if r, ok := m.recycleWorktreeRow(); ok {
		out = append(out, r)
	}
```

In `internal/tui/i18n_display.go` `optionDisplayName`, add (keep the switch alphabetical where the file does):

```go
	case "commit":
		return i18n.T("commit")
	case "discard":
		return i18n.T("discard")
```

In `internal/tui/source.go` `opAffectedSources`, add next to `RemoveWorktree`:

```go
	case engine.RecycleWorktree:
		// Another worktree's HEAD moved: Branches shows per-branch worktree
		// markers, Worktrees shows the branch per path. Our own status is
		// untouched.
		return []sourceKey{srcBranches, srcWorktrees}
```

Bundles — add to all four (`"commit"` already exists in ja; check each file with `grep -c '^"commit" =' internal/i18n/lang/*.toml` and only add where missing):

```toml
"Recycle a worktree…" = "ワークツリーを再利用…"
"detached" = "デタッチ"
"(agent session running)" = "(エージェントセッション実行中)"
"An agent session is running in %s. Recycle it anyway?" = "%s でエージェントセッションが実行中です。それでも再利用しますか？"
"discard" = "破棄"
```

ko: `"워크트리 재사용…"`, `"분리됨"`, `"(에이전트 세션 실행 중)"`, `"%s에서 에이전트 세션이 실행 중입니다. 그래도 재사용할까요?"`, `"버리기"`, and `"commit" = "커밋"` if missing.
zh: `"回收工作树…"`, `"分离"`, `"（代理会话运行中）"`, `"%s 中有代理会话正在运行。仍要回收吗？"`, `"丢弃"`, `"commit" = "提交"` if missing.
ru: `"Переиспользовать рабочее дерево…"`, `"отсоединён"`, `"(сеанс агента запущен)"`, `"В %s запущен сеанс агента. Всё равно переиспользовать?"`, `"отбросить"`, `"commit" = "закоммитить"` if missing.

If `"detached"` already exists as a key in a bundle, keep the existing translation.

- [ ] **Step 4: Run the TUI package (the i18n gates run here)**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/tui 2>&1 | tail -20`
Expected: `ok`. If `i18n_scan_test`/`menu_labels_test`/`options_vocab_test` name a missing key, add exactly that key to the bundle it names.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add internal/tui/recycle_worktree.go internal/tui/recycle_worktree_test.go internal/tui/action_menu.go internal/tui/i18n_display.go internal/tui/source.go internal/i18n/lang && git commit -m "feat(tui): Recycle a worktree… on an unchecked-out branch — pick a worktree, commit/discard its changes, switch"
```

---

### Task 5: Headless TUI check of the whole flow

**Files:**
- none created; uses `./tui-capture.sh` (see the `driving-tui-headless` skill) and a scratch repo under the scratchpad directory.

- [ ] **Step 1: Build the binary in the worktree**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go build -o /tmp/claude-1000/-mnt-t-others-gigagit/22501cfc-34d0-48fe-8b7d-ff6528344fbf/scratchpad/gg ./cmd/gg`

- [ ] **Step 2: Make a scratch repo with a dirty linked worktree**

```bash
S=/tmp/claude-1000/-mnt-t-others-gigagit/22501cfc-34d0-48fe-8b7d-ff6528344fbf/scratchpad
rm -rf $S/rc && mkdir -p $S/rc && cd $S/rc && git init -q -b main r && cd r && echo a > a.txt && git add . && git -c user.name=t -c user.email=t@t commit -qm init && git branch loose && git worktree add -q -b wt-branch ../wt main && echo dirty > ../wt/dirty.txt
```

- [ ] **Step 3: Drive it**

Invoke the `driving-tui-headless` skill and run a keyscript that: opens the Branches tab, moves to `loose`, presses `.`, types `recy`, enter, picks the `wt` row, enter, and captures the modal; then answers `commit` and captures the status line. Assert in the captures: the picker row shows `…/wt  wt-branch`; the modal shows the three options; the final status line contains `recycled` and `wt-branch → loose`; `git -C $S/rc/wt log -1 --format=%s wt-branch` prints `Committed changes due to worktree recycle …`.

- [ ] **Step 4: Fix anything the captures show** (row label cut, modal wording), re-run Task 4's tests, commit with `fix(tui): …` if a change was needed.

---

### Task 6: CLI `gg worktree recycle`

**Files:**
- Modify: `internal/cli/worktree.go:22-44` (dispatch + usage line) and add `cmdWorktreeRecycle`
- Test: `internal/cli/worktree_recycle_test.go`

**Interfaces:**
- Consumes: `runOperation`, `finish`, `cliDecider{policy, in, out, interactive}`, `stdinIsTerminal()`, `svc.Worktrees`, `engine.RecycleWorktree`, `engine.RecycleDirtyDecisionID`.
- Produces: `gg worktree recycle <path> <branch> [--on-dirty=commit|discard|abort]`.

- [ ] **Step 1: Write the failing tests**

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

func cliWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeRecycleCleanTarget(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "recycled") {
		t.Fatalf("stdout = %q", out.String())
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

func TestWorktreeRecycleOnDirtyCommit(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=commit", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	subj, _ := exec.Command("git", "-C", wt, "log", "-1", "--format=%s", "a").Output()
	if !strings.HasPrefix(string(subj), "Committed changes due to worktree recycle ") {
		t.Fatalf("subject on a = %q", subj)
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

func TestWorktreeRecycleDirtyWithoutFlagIsRefusedInAPipeline(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("a dirty target with no --on-dirty must not succeed in a pipeline")
	}
	if !strings.Contains(errb.String(), "recycle.dirty") {
		t.Fatalf("stderr = %q, want the decision id", errb.String())
	}
	if got := headOf(t, wt); got != "a" {
		t.Fatalf("worktree HEAD = %q, want a (untouched)", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "x.txt")); err != nil {
		t.Fatal("x.txt must survive")
	}
}

// Review Focus 5.
func TestWorktreeRecycleRejectsBadOnDirty(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=shelve", "/nowhere", "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--on-dirty") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestWorktreeRecycleUsage(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "recycle", "only-one-arg"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}
```

Check whether the other CLI worktree tests call `t.Parallel()` (`grep -c 't.Parallel' internal/cli/worktree_test.go`); if they don't because `Run` mutates process state, drop `t.Parallel()` from these too.

- [ ] **Step 2: Run to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/cli -run 'TestWorktreeRecycle' 2>&1 | head -10`
Expected: `unknown subcommand "recycle"` → non-zero exits where 0 is wanted.

- [ ] **Step 3: Implement**

In `cmdWorktree`: usage string becomes `usage: gg worktree <list|add|remove|move|rename|prune|recycle> [args]`; add

```go
	case "recycle":
		return cmdWorktreeRecycle(svc, args[1:], stdin, stdout, stderr)
```

and in the default message add `recycle` to the list. Then:

```go
// cmdWorktreeRecycle checks <branch> out in the existing worktree <path>
// (which gg is not running in), committing or discarding that worktree's
// uncommitted work first as --on-dirty says. Without the flag an interactive
// terminal is asked on stdin; a pipeline fails with the decision id so
// nothing is destroyed unseen.
func cmdWorktreeRecycle(svc *domain.Service, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("worktree recycle", flag.ContinueOnError)
	fs.SetOutput(stderr)
	onDirty := fs.String("on-dirty", "", "what to do with the target's uncommitted changes: commit, discard, or abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 2 || fs.Arg(0) == "" || fs.Arg(1) == "" {
		fmt.Fprintln(stderr, "usage: gg worktree recycle [--on-dirty=commit|discard|abort] <path> <branch>")
		return 2
	}
	policy := map[string]string{}
	switch *onDirty {
	case "":
	case "commit", "discard", "abort":
		policy[engine.RecycleDirtyDecisionID] = *onDirty
	default:
		fmt.Fprintf(stderr, "worktree recycle: --on-dirty must be commit, discard, or abort (got %q)\n", *onDirty)
		return 2
	}
	target := fs.Arg(0)
	if abs, err := filepath.Abs(target); err == nil {
		target = abs
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), svc, engine.RecycleWorktree{Dir: target, Branch: fs.Arg(1)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

Note Go's `flag` stops at the first positional, so the flag must come BEFORE `<path>` — the usage line says so, and the e2e scenario in Task 7 follows it. (`TestWorktreeRecycleOnDirtyCommit` above already places it first.)

- [ ] **Step 4: Run to verify they pass**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./internal/cli -run 'TestWorktreeRecycle|TestWorktree' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add internal/cli/worktree.go internal/cli/worktree_recycle_test.go && git commit -m "feat(cli): gg worktree recycle <path> <branch> [--on-dirty=…]"
```

---

### Task 7: e2e scenario

**Files:**
- Create: `e2e/scenarios/s101_worktree_recycle.toml` (s100 is the current highest)

- [ ] **Step 1: Write the scenario**

```toml
name = "worktree recycle --on-dirty=commit: commits the target's work, then checks the branch out there"

[input]
steps = [
  { write = "README.md", content = "hello\n" },
  { commit = "initial" },
  { branch = "loose" },
  { branch = "feature/x" },
  { worktree = "wt-x", branch = "feature/x" },
  { write = "work.txt", content = "in progress\n", cwd = "wt-x" },
]

[[run]]
cmd  = ["worktree", "recycle", "--on-dirty=commit", "../wt-x", "loose"]
exit = 0
stdout_contains = ["recycled", "feature/x → loose", "committed"]

[expect]
branch    = "main"
worktrees = ["wt-x"]

[expect.worktree."wt-x".status]
untracked = []
unstaged  = []
staged    = []
```

Check how `[expect.worktree.<rel>]` asserts a clean tree in an existing scenario (`grep -l 'expect.worktree' e2e/scenarios/*.toml | head -2`) and copy that shape if the `status` block above is not how it is spelled.

- [ ] **Step 2: Run the scenario**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go test ./e2e -run 'TestScenarios/s101' 2>&1 | tail -15` (check the harness test name with `grep -n 'func Test' e2e/harness_test.go`).
Expected: PASS. If `stdout_contains` fails because the summary goes to stderr, look at `finish` in `internal/cli/cli.go:279` for where the summary is printed and move the assertion to `stderr_contains`.

- [ ] **Step 3: Commit**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add e2e/scenarios/s101_worktree_recycle.toml && git commit -m "test(e2e): worktree recycle commits the target's work then switches it"
```

---

### Task 8: Docs, skill, spec deviation note, full gate

**Files:**
- Modify: `CHANGELOG.md` (new top section under Unreleased)
- Modify: `README.md` (worktrees / branch-menu mention)
- Modify: `internal/agentskill/using-gg.md:716-730` (worktree verbs list) + `internal/agentskill/agentskill.go:22` (`Version = 101`)
- Modify: `docs/CLAUDE-details.md` (engine op + decision id, one paragraph)

- [ ] **Step 1: CHANGELOG** — add above the current top section:

```markdown
## Recycle a worktree

### Added

- **Branches `.` → "Recycle a worktree…"** on a local branch no worktree has
  checked out: pick one of the repo's other worktrees and gg checks the
  branch out THERE. A dirty target asks `commit` (everything, untracked
  included, as `Committed changes due to worktree recycle <date>` on the
  branch that is leaving) / `discard` (untracked files deleted, ignored
  files kept) / `abort`. Paused ops, lock files and an already-checked-out
  branch are refused up front. The worktree gg runs in is never listed.
- **`gg worktree recycle [--on-dirty=commit|discard|abort] <path> <branch>`**
  — the same from the CLI; a pipeline without the flag fails rather than
  touch the tree.
- Engine: `RecycleWorktree{Dir, Branch}` and the `OpDeps.RepoAt` seam
  (`git.Repo.InDir`, a `-C <dir>` view) — the first op that acts on a
  worktree gg is not running in.

Follow-ups: a `shelve` answer (after the multi-file shelf), remote-only
branches, and the web UI.
```

- [ ] **Step 2: README** — in the keys table near the `w`/`W` worktree row, add one sentence: "`.` on a branch that is checked out nowhere offers **Recycle a worktree…**: check it out in an existing worktree, committing or discarding that worktree's changes first."

- [ ] **Step 3: using-gg.md** — after the `gg worktree prune` bullet add:

```markdown
- `gg worktree recycle [--on-dirty=commit|discard|abort] <path> <branch>` —
  check an EXISTING local branch out in an existing worktree `<path>` (not
  the one you are in), replacing what it has checked out. A dirty target
  needs `--on-dirty`: `commit` commits everything there (untracked included,
  subject `Committed changes due to worktree recycle <date>`) on the branch
  that is leaving; `discard` deletes the changes (untracked files too,
  ignored files kept); `abort` does nothing. Without the flag a pipeline
  exits 1 naming `recycle.dirty`. Refused: a paused rebase/merge, a lock
  file, a branch already checked out somewhere. Flags go BEFORE `<path>`.
```

Bump `const Version = 100` → `101` in `internal/agentskill/agentskill.go`. Run `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && go run ./cmd/gg init --update` and commit whatever it regenerates under `.claude/skills/using-gg/` if that dir is tracked (`git status --short`).

- [ ] **Step 4: CLAUDE-details.md** — find the engine section's worktree ops paragraph (`grep -n 'RemoveWorktree' docs/CLAUDE-details.md | head -1`) and append:

```markdown
`RecycleWorktree{Dir, Branch, Now}` checks a local branch out in ANOTHER
worktree (the first op to act outside the current one): it takes a `-C <dir>`
`GitOps` view from `OpDeps.RepoAt` (nil ⇒ `ErrNoRepoAt`; `domain.Execute`
wires `git.Repo.InDir`), refuses up front a bare/unknown dir, a branch
checked out anywhere, a paused op (`git.PausedOpIn` on the target's git dir)
and a lock file (`git.LockFiles`), then — when staged+unstaged+conflicted+
untracked > 0 (NOT `IsDirty`, which ignores untracked) — raises
`RecycleDirtyDecisionID = "recycle.dirty"` (`commit`: `add -A` + commit
`RecycleCommitMessage(now)` on the leaving branch; `discard`: restore + clean
on `:/` without `-x`; anything else cancels with no change), and finally
`git switch`. TreeWrite by default; the gate is per common dir so it covers
the other worktree. Summary `recycled <dir>: <old> → <branch>[; committed
<sha>|; changes discarded]`.
```

- [ ] **Step 5: Full gate**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && ./test.sh race 2>&1 | tail -30`
Expected: all stages green. Fix and re-run until they are.

- [ ] **Step 6: Commit + verify binary**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/recycle-worktree && git add CHANGELOG.md README.md internal/agentskill docs/CLAUDE-details.md .claude/skills && git commit -m "docs: recycle a worktree — changelog, README, using-gg skill v101, CLAUDE-details"
```

Then build the verify binary in the worktree (`go build -o ./gg-verify ./cmd/gg`, gitignored or removed after) and hand its path to the user with the merge question — the human merges.
