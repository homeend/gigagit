# Interactive Rebase — Slice 3: engine `InteractiveRebase` op — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An engine `InteractiveRebase` operation that drives a real `git rebase -i <base>` through the Slice-2 gg-as-editor protocol, with a stash-wrap (preserve the working tree + index split), a merge-commit guard, and conflict keep/abort handling — the first time slices 1–3 run end-to-end through real git.

**Architecture:** New verbs `RebaseInteractive(dir, onto, env)` (uses `gitexec.RunEnv` to set `GIT_SEQUENCE_EDITOR`) and `HasMergeCommits(dir, onto, branch)`. New op `engine.InteractiveRebase{Branch, Onto, Plan, GGBin}` mirroring `SmartRebase`'s three-rung ladder (checked-out-here / other-worktree / stash+switch), but driving the interactive rebase with a plan file and env. The plan file (a temp JSON) is removed on success/abort and **left in place when paused** (its `exec` steps are still needed by `git rebase --continue`).

**Tech Stack:** Go 1.26, `internal/gitexec` (RunEnv, Slice 1), `internal/rebaseplan` (Slice 2).

**Spec:** `docs/superpowers/specs/2026-06-15-interactive-rebase-design.md` (Slice 3).

**Conventions:** TDD; a git verb is one invocation; ops act on the `GitOps` interface; real-`git` tests in `t.TempDir()`; gate `./test.sh race`; commits end `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

**Scope decision — pushed-history warning deferred:** the spec lists a pushed-history rewrite warning. Deferred here, consistent with F2's deferred pushed-amend warning: since `gg push` never force-pushes, rewriting pushed history simply makes the next push fail visibly. The merge-commit guard IS implemented. (Update the spec's "Out of scope" accordingly during execution.)

**Real-git integration:** the op's happy paths can only be tested by driving a real `git rebase -i` through a real `gg` binary acting as the sequence editor. Task 2 builds the `gg` binary once (`go build`) and sets `GGBin` to it — this validates slices 1+2+3 together.

---

### Task 1: verbs — `RebaseInteractive` (env) + `HasMergeCommits`

**Files:**
- Modify: `internal/git/rebase.go` (+ test `internal/git/rebase_test.go`)
- Modify: `internal/engine/gitops.go` (interface)

- [ ] **Step 1: Write the failing verb tests**

Add to `internal/git/rebase_test.go` (create the file if absent; package `git`,
imports `context`, `reflect`, `testing`, `os`, `os/exec`, `path/filepath`,
`strings`, and `github.com/gigagit/gg/internal/gitexec`):

```go
func TestRebaseInteractiveArgvAndEnv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase -i", gitexec.Result{})
	r := &Repo{Runner: f}
	env := []string{"GIT_SEQUENCE_EDITOR=/usr/bin/gg __rebase-seq /tmp/p.json"}
	if err := r.RebaseInteractive(context.Background(), "", "main", env); err != nil {
		t.Fatalf("rebase -i: %v", err)
	}
	if want := []string{"rebase", "-i", "main"}; !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if !reflect.DeepEqual(f.Calls[0].Env, env) {
		t.Fatalf("env = %v, want %v", f.Calls[0].Env, env)
	}
}

// Real-git: --merges counting over a range with and without a merge commit.
func TestHasMergeCommits(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit "initial" on main
	repo := &Repo{Runner: runner}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// linear: main -> feature with one normal commit, no merges in main..feature
	git("checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "feat a")
	has, err := repo.HasMergeCommits(context.Background(), "", "main", "feature")
	if err != nil {
		t.Fatalf("has-merge: %v", err)
	}
	if has {
		t.Fatal("linear range must report no merge commits")
	}
	// add a merge commit into feature
	git("checkout", "-b", "side", "main")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "side b")
	git("checkout", "feature")
	git("merge", "--no-ff", "-m", "merge side", "side")
	has, err = repo.HasMergeCommits(context.Background(), "", "main", "feature")
	if err != nil {
		t.Fatalf("has-merge: %v", err)
	}
	if !has {
		t.Fatal("range with a merge commit must report true")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run 'TestRebaseInteractive|TestHasMergeCommits' -v`
Expected: FAIL — `RebaseInteractive` / `HasMergeCommits` undefined.

- [ ] **Step 3: Implement the verbs**

Add to `internal/git/rebase.go` (it already imports `context` and `gitcmd`; add
`"strings"`):

```go
// RebaseInteractive replays the branch at dir ("" = this worktree) onto onto
// under an interactive rebase, carrying env (e.g. GIT_SEQUENCE_EDITOR) for this
// one invocation. The todo is driven non-interactively by the editor named in
// env; no terminal editor opens.
func (r *Repo) RebaseInteractive(ctx context.Context, dir, onto string, env []string) error {
	b := gitcmd.New("rebase").Arg("-i").Arg(onto)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git rebase -i", b.ToArgv(), env)
	return err
}

// HasMergeCommits reports whether onto..branch contains any merge commits, at
// dir ("" = this worktree). Interactive rebase v1 refuses such ranges.
func (r *Repo) HasMergeCommits(ctx context.Context, dir, onto, branch string) (bool, error) {
	b := gitcmd.New("rev-list").Arg("--merges").Arg("--count").Arg(onto + ".." + branch)
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-list --merges", b.ToArgv())
	if err != nil {
		return false, err
	}
	n := strings.TrimSpace(res.Stdout)
	return n != "" && n != "0", nil
}
```

- [ ] **Step 4: Add both to the `GitOps` interface**

In `internal/engine/gitops.go`, next to the existing rebase verbs:

```go
	Rebase(ctx context.Context, dir, onto string) error
	RebaseInteractive(ctx context.Context, dir, onto string, env []string) error
	HasMergeCommits(ctx context.Context, dir, onto, branch string) (bool, error)
	RebaseAbort(ctx context.Context, dir string) error
	RebaseInProgress(ctx context.Context, dir string) (bool, error)
```

- [ ] **Step 5: Run to verify it passes**

Run: `go build ./... && go test ./internal/git/ -run 'TestRebaseInteractive|TestHasMergeCommits' -v`
Expected: build clean (`*git.Repo` satisfies the grown interface); tests PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/git/rebase.go internal/git/rebase_test.go internal/engine/gitops.go
git commit -m "feat(git): RebaseInteractive (env-driven) + HasMergeCommits verbs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: the `InteractiveRebase` op + real-git integration

**Files:**
- Create: `internal/engine/interactive_rebase.go`
- Test: `internal/engine/interactive_rebase_test.go`

- [ ] **Step 1: Write the op**

Create `internal/engine/interactive_rebase.go`:

```go
package engine

import (
	"context"
	"fmt"
	"os"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// InteractiveRebase drives `git rebase -i Onto` for Branch through the
// gg-as-editor protocol (Slice 2): the plan is written to a temp file and
// GIT_SEQUENCE_EDITOR points at `GGBin __rebase-seq <plan>`. It mirrors
// SmartRebase's rungs (here / other worktree / stash+switch), wraps the rebase
// in a stash so the working tree + index split survive (message-only intent),
// and forks on conflict via "rebase-conflict" (keep-conflicts / abort).
type InteractiveRebase struct {
	Branch string
	Onto   string
	Plan   rebaseplan.Plan
	GGBin  string // path to the gg binary (the sequence editor)
}

var _ Operation = InteractiveRebase{}

func (op InteractiveRebase) Run(ctx context.Context, deps OpDeps) (Result, error) {
	switch {
	case op.Branch == "":
		return Result{}, fmt.Errorf("interactive rebase: Branch is required")
	case op.Onto == "":
		return Result{}, fmt.Errorf("interactive rebase: Onto is required")
	case op.Branch == op.Onto:
		return Result{}, fmt.Errorf("interactive rebase: branch and base are both %s", op.Branch)
	case op.GGBin == "":
		return Result{}, fmt.Errorf("interactive rebase: GGBin is required")
	case len(op.Plan.Entries) == 0:
		return Result{}, fmt.Errorf("interactive rebase: empty plan")
	}

	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	for _, name := range []string{op.Branch, op.Onto} {
		if !have[name] {
			return Result{}, fmt.Errorf("interactive rebase: no such branch: %s", name)
		}
	}

	hasMerges, err := deps.Repo.HasMergeCommits(ctx, "", op.Onto, op.Branch)
	if err != nil {
		return Result{}, err
	}
	if hasMerges {
		return Result{}, fmt.Errorf("interactive rebase: %s..%s contains merge commits (not supported yet)", op.Onto, op.Branch)
	}

	planPath, err := writePlanFile(op.Plan)
	if err != nil {
		return Result{}, err
	}
	env := []string{"GIT_SEQUENCE_EDITOR=" + op.GGBin + " __rebase-seq " + planPath}

	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		os.Remove(planPath)
		return Result{}, err
	}

	var (
		res    Result
		paused bool
		runErr error
	)
	switch {
	case op.Branch == cur:
		// Rung 1: checked out here.
		res, paused, runErr = op.wrapped(ctx, deps, "", env)
	default:
		wt, werr := deps.Repo.WorktreeForBranch(ctx, op.Branch)
		if werr != nil {
			os.Remove(planPath)
			return Result{}, werr
		}
		if wt != nil {
			// Rung 2: branch lives in another worktree — rebase there, stay put.
			res, paused, runErr = op.irebaseAt(ctx, deps, wt.Path, env)
		} else {
			// Rung 3: stash + switch + rebase, stay on branch.
			res, paused, runErr = op.wrapped(ctx, deps, op.Branch, env)
		}
	}

	// The plan's exec steps are still needed by `git rebase --continue` when
	// paused; only remove it once the rebase is fully done.
	if !paused {
		os.Remove(planPath)
	}
	return res, runErr
}

// wrapped runs the rebase against the current worktree (dir ""), stashing the
// working tree first when dirty (and switching to switchTo first when non-empty),
// then restoring the stash + the staged/unstaged split on success.
func (op InteractiveRebase) wrapped(ctx context.Context, deps OpDeps, switchTo string, env []string) (Result, bool, error) {
	stashed, staged, err := op.stashBegin(ctx, deps)
	if err != nil {
		return Result{}, false, err
	}
	if switchTo != "" {
		deps.emit(ctx, Progress{Step: "switching", Detail: switchTo})
		if err := deps.Repo.Switch(ctx, switchTo); err != nil {
			if stashed {
				_ = deps.Repo.StashPop(ctx, "")
			}
			return Result{}, false, err
		}
	}
	res, paused, rebaseErr := op.irebaseAt(ctx, deps, "", env)
	if rebaseErr != nil {
		if stashed && res.Summary != "" {
			res.Summary += " (your changes remain stashed)"
		}
		return res, paused, rebaseErr
	}
	if stashed {
		deps.emit(ctx, Progress{Step: "restoring changes"})
		if err := deps.Repo.StashPop(ctx, ""); err != nil {
			deps.emit(ctx, DecisionNeeded{Request: DecisionRequest{
				ID:      "stash-pop-conflict",
				Prompt:  "Restoring your changes conflicted",
				Options: []string{"keep", "abort"},
			}})
			return Result{Summary: res.Summary + "; restore conflicted (changes preserved in stash)", Changed: res.Changed},
				false, fmt.Errorf("stash pop conflict after rebasing %s: %w", op.Branch, err)
		}
		if len(staged) > 0 {
			if err := deps.Repo.StagePaths(ctx, staged); err != nil {
				return Result{Summary: res.Summary + "; could not restore the staged index", Changed: res.Changed}, false, err
			}
		}
	}
	return res, false, nil
}

// stashBegin stashes the current worktree (tracked + untracked) when dirty,
// recording which tracked paths were staged so the index split can be restored.
func (op InteractiveRebase) stashBegin(ctx context.Context, deps OpDeps) (stashed bool, staged []string, err error) {
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil || !dirty {
		return false, nil, err
	}
	st, err := deps.Repo.Status(ctx)
	if err != nil {
		return false, nil, err
	}
	for _, f := range st.Files {
		if f.Kind == model.KindUntracked || f.Kind == model.KindUnmerged {
			continue
		}
		if f.Staged != '.' && f.Staged != 0 {
			staged = append(staged, f.Path)
		}
	}
	deps.emit(ctx, Progress{Step: "stashing"})
	if err := deps.Repo.StashPush(ctx, "gg-irebase:"+op.Branch, nil, true); err != nil {
		return false, nil, err
	}
	return true, staged, nil
}

// irebaseAt drives the interactive rebase in dir ("" = current worktree),
// returning paused=true when a conflict left it for `git rebase --continue`.
func (op InteractiveRebase) irebaseAt(ctx context.Context, deps OpDeps, dir string, env []string) (Result, bool, error) {
	where := ""
	if dir != "" {
		where = " in worktree " + dir
	}
	deps.emit(ctx, Progress{Step: "rebasing", Detail: op.Branch + " onto " + op.Onto + where})
	rebaseErr := deps.Repo.RebaseInteractive(ctx, dir, op.Onto, env)
	if rebaseErr == nil {
		return Result{Summary: "rebased " + op.Branch + " onto " + op.Onto + where, Changed: true}, false, nil
	}
	inRebase, stateErr := deps.Repo.RebaseInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, false, fmt.Errorf("interactive rebase: %s onto %s: %v (state check: %w)", op.Branch, op.Onto, rebaseErr, stateErr)
	}
	if !inRebase {
		return Result{}, false, fmt.Errorf("interactive rebase: %s onto %s: %w", op.Branch, op.Onto, rebaseErr)
	}
	choice, derr := deps.decide(ctx, DecisionRequest{
		ID:      "rebase-conflict",
		Prompt:  "Rebasing " + op.Branch + " onto " + op.Onto + " hit conflicts",
		Options: []string{"keep-conflicts", "abort"},
	})
	if derr != nil {
		return Result{}, false, derr
	}
	if choice.Option == "keep-conflicts" {
		return Result{Summary: "rebase of " + op.Branch + " onto " + op.Onto + where + " paused on a conflict (resolve, then `git rebase --continue`)", Changed: true},
			true, fmt.Errorf("rebase conflict: %s onto %s", op.Branch, op.Onto)
	}
	if err := deps.Repo.RebaseAbort(ctx, dir); err != nil {
		return Result{}, false, fmt.Errorf("interactive rebase: abort failed: %w", err)
	}
	return Result{Summary: "aborted: interactive rebase of " + op.Branch + " onto " + op.Onto, Changed: false}, false, nil
}

// writePlanFile serializes the plan to a temp JSON file and returns its path.
// The caller removes it (except when the rebase pauses — the exec steps still
// need it).
func writePlanFile(p rebaseplan.Plan) (string, error) {
	b, err := rebaseplan.Marshal(p)
	if err != nil {
		return "", err
	}
	f, err := os.CreateTemp("", "gg-rebase-plan-*.json")
	if err != nil {
		return "", err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}
```

- [ ] **Step 2: Run to verify it builds (no tests yet)**

Run: `go build ./internal/engine/`
Expected: clean.

- [ ] **Step 3: Write the integration tests**

Create `internal/engine/interactive_rebase_test.go` (package `engine`):

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/rebaseplan"
)

// buildGG builds the gg binary once for use as the rebase sequence editor.
func buildGG(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gg-test-bin")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/gigagit/gg/cmd/gg").CombinedOutput()
	if err != nil {
		t.Fatalf("build gg: %v\n%s", err, out)
	}
	return bin
}

// shaOf returns the full sha of <rev> in dir.
func shaOf(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

func subjects(t *testing.T, dir, rangeSpec string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "--pretty=%s", rangeSpec).Output()
	if err != nil {
		t.Fatalf("log %s: %v", rangeSpec, err)
	}
	var s []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			s = append(s, l)
		}
	}
	return s // newest-first
}

// threeCommitBranch builds: main(initial) -> wip1 -> wip2 -> wip3 on branch
// "work", and returns (dir, repo, ggBin). The plan is built oldest-first.
func threeCommitBranch(t *testing.T) (string, *gitRepoForTest) {
	t.Helper()
	dir, repo := newRepo(t) // initial commit on main
	gitE(t, dir, "checkout", "-b", "work")
	for _, n := range []string{"wip1", "wip2", "wip3"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte(n+"\n"), 0o644)
		gitE(t, dir, "add", ".")
		gitE(t, dir, "commit", "-m", n)
	}
	return dir, repo
}

func TestInteractiveRebaseRewordDropReorder(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	// oldest-first plan over main..work = [wip1, wip2, wip3]:
	// reword wip1 -> "wip1 reworded", drop wip2, keep wip3.
	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: shaOf(t, dir, "work~1"), Action: rebaseplan.Drop, Orig: "wip2"},
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	res, err := InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("interactive rebase: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	got := subjects(t, dir, "main..work") // newest-first
	want := []string{"wip3", "wip1 reworded"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("subjects = %v, want %v", got, want)
	}
}

func TestInteractiveRebaseRefusesMergeCommits(t *testing.T) {
	gg := buildGG(t)
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "a")
	gitE(t, dir, "checkout", "-b", "side", "main")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "b")
	gitE(t, dir, "checkout", "work")
	gitE(t, dir, "merge", "--no-ff", "-m", "merge side", "side")

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "merge side"},
	}}
	_, err := InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "merge commits") {
		t.Fatalf("err = %v, want a merge-commits refusal", err)
	}
}

func TestInteractiveRebaseStashWrapPreservesWorkingTree(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	// Dirty the tree: one staged new file, one unstaged edit to a committed file.
	os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("s\n"), 0o644)
	gitE(t, dir, "add", "staged.txt")
	os.WriteFile(filepath.Join(dir, "wip1.txt"), []byte("edited\n"), 0o644) // unstaged

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: shaOf(t, dir, "work~1"), Action: rebaseplan.Pick, Orig: "wip2"},
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	if _, err := (InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}).
		Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("interactive rebase: %v", err)
	}
	// reword applied
	if got := subjects(t, dir, "main..work"); got[len(got)-1] != "wip1 reworded" {
		t.Fatalf("oldest subject = %q, want 'wip1 reworded'", got[len(got)-1])
	}
	// working tree restored with the index split intact
	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var stagedNew, unstagedEdit bool
	for _, f := range st.Files {
		if f.Path == "staged.txt" && f.Staged != '.' && f.Staged != 0 {
			stagedNew = true
		}
		if f.Path == "wip1.txt" && f.Unstaged != '.' && f.Unstaged != 0 {
			unstagedEdit = true
		}
	}
	if !stagedNew {
		t.Error("staged.txt should be restored as staged")
	}
	if !unstagedEdit {
		t.Error("wip1.txt edit should be restored as unstaged")
	}
}
```

> **Helper alignment:** `newRepo(t) (string, *git.Repo)` and `gitE(t, dir,
> args...)` already exist in `internal/engine/ops_basic_test.go` /
> `smart_merge_test.go`. Replace the placeholder `*gitRepoForTest` return type
> in `threeCommitBranch` with whatever `newRepo` actually returns
> (`*git.Repo`) — match the existing helper signature; do not invent a new type.

- [ ] **Step 4: Run the integration tests**

Run: `go test ./internal/engine/ -run TestInteractiveRebase -v`
Expected: PASS — reword+drop+reorder produces `[wip3, wip1 reworded]`; merge
commits refused; stash-wrap restores the staged/unstaged split.

- [ ] **Step 5: Commit**

```bash
git add internal/engine/interactive_rebase.go internal/engine/interactive_rebase_test.go
git commit -m "feat(engine): InteractiveRebase op drives git rebase -i via the plan

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Final verification (after all tasks)

- [ ] Update the spec's "Out of scope" to record the deferred pushed-history warning; commit the doc tweak.
- [ ] `./test.sh race` — vet+gofmt clean, all unit + e2e green.
- [ ] `superpowers:finishing-a-development-branch`.
- [ ] **After merge, RE-RUN `./test.sh race` on merged `main`** — drift discipline.

---

## Self-Review

**1. Spec coverage (Slice 3):**
- "Plan → driven `git rebase -i <base>`" → Task 2 op + Task 1 `RebaseInteractive` (env). ✓
- "stash-wrap (clean tree → rebase → restore + re-stage)" → `wrapped`/`stashBegin` + `StagePaths` restore. ✓
- "conflict keep/abort like SmartRebase" → `irebaseAt` `rebase-conflict` decision. ✓
- "guards (refuse merge commits …)" → `HasMergeCommits` guard. Pushed-history warning **deferred** (documented, consistent with F2). ✓
- "worktree-aware like SmartRebase" → three rungs (here / other worktree / stash+switch). ✓
- End-to-end through real git → Task 2 integration tests build `gg` and drive a real `rebase -i`. ✓

**2. Placeholder scan:** complete code throughout; the two explicit notes (delete the unused `strings` import; match `newRepo`'s real return type) are concrete instructions, not vague gaps. The `*gitRepoForTest` placeholder is called out for replacement with `*git.Repo`.

**3. Type consistency:** `InteractiveRebase{Branch, Onto, Plan rebaseplan.Plan, GGBin}` used identically in the op and all three tests. `RebaseInteractive(ctx, dir, onto, env)` and `HasMergeCommits(ctx, dir, onto, branch)` match across verb (Task 1), `GitOps`, and op call sites. `wrapped`/`irebaseAt`/`stashBegin`/`writePlanFile` signatures are internally consistent; `irebaseAt` returns `(Result, bool, error)` (the `paused` flag) and every caller threads it. The conflict decision ID `rebase-conflict` and options `keep-conflicts`/`abort` match SmartRebase's contract (frontends already map them).

**Deferred to Slice 4:** the TUI editor + the scriptable CLI `gg rebase -i --plan` + the e2e scenario (which sets `GGBin` from `os.Executable()` at the composition root).
