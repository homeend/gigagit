# Mark-and-Pair Operations + SmartMerge Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A generic TUI mark-a-row/pair-with-another mechanism (`m` key + pair-op popup) whose first consumer is a real `engine.SmartMerge` operation (merge branch A into branch B), wired through the TUI Branches panel and a new `gg merge` CLI command.

**Architecture:** Three new git verbs (`Merge`/`MergeAbort`/`MergeInProgress`, each taking an optional worktree dir) → `engine.SmartMerge` with a SmartPull-style decision ladder (in-place → other-worktree → autostash+switch+stay) and a `merge-conflict` decision → TUI mark state keyed by stable row identity (`panelList.Key`) + a generic per-panel pair-op registry + popup → CLI `gg merge [--into] [--on-conflict=keep|abort] <source>`. Spec: `docs/superpowers/specs/2026-06-12-mark-pair-merge-design.md`.

**Tech Stack:** Go 1.26, Bubble Tea/lipgloss (TUI), system git via `internal/gitcmd`+`internal/gitexec`, TOML e2e harness (`e2e/`).

**Cross-frontend API (do not vary):** decision `merge-conflict` = options `["keep-conflicts", "abort"]`. Reused decision: `stash-pop-conflict` = `["keep", "abort"]`.

**Conventions that apply to every task:**
- TDD: write the failing test, watch it fail, implement, watch it pass.
- A git verb is ONE git invocation built with `gitcmd`, run via `r.Runner.Run`.
- Gates before each commit: `go build ./... && go vet ./... && gofmt -l internal cmd` (gofmt must print nothing).
- Commit messages end with: `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`
- Run tests from the repo root `/mnt/t/others/gigagit` (the feature worktree if one is used).

---

## File structure

| File | Responsibility |
|------|----------------|
| `internal/git/merge.go` (new) | The three merge verbs. |
| `internal/git/merge_test.go` (new) | FakeRunner argv assertions + real-repo behavior. |
| `internal/engine/smart_merge.go` (new) | The SmartMerge operation (guards, ladder, conflict decision). |
| `internal/engine/smart_merge_test.go` (new) | Real-git tests for every rung and guard. |
| `internal/tui/viewstate.go` | `Key(i)` added to `panelList` + the four implementations. |
| `internal/tui/mark.go` (new) | `markState`, the `m`-key state machine, mark resolution, the pair-op registry. |
| `internal/tui/pairop_popup.go` (new) | `pairOpPopup`: keys + rendering. |
| `internal/tui/model.go` | `mark`/`pairPopup` fields, `m` and `esc` key cases, popup routing. |
| `internal/tui/view.go` | `◆` row prefix, status-line hint, footer, `windowRows` start return, popup overlay. |
| `internal/tui/tooltip.go` | One-line fix for the new `windowRows` signature. |
| `internal/tui/mark_test.go` (new) | Mark/popup behavior tests. |
| `internal/cli/merge.go` (new) | `cmdMerge`. |
| `internal/cli/merge_test.go` (new) | CLI behavior tests. |
| `internal/cli/cli.go` | Route `merge`; `commands` map entry. |
| `e2e/scenarios/s26…s32_merge_*.toml` (new) | Seven scenarios. |
| `.claude/skills/writing-e2e-scenarios/SKILL.md` | One contract-table row. |
| `internal/agentskill/{using-gg.md, agentskill.go}` + `.claude/skills/using-gg/SKILL.md` | `gg merge` bullet, Version 5, regenerated dogfood copy. |
| `README.md`, `CHANGELOG.md`, `.claude/skills/adding-tui-windows/SKILL.md` | Docs. |

---

### Task 1: git merge verbs

**Files:**
- Create: `internal/git/merge.go`
- Create: `internal/git/merge_test.go`

The package already has helpers used below: `newTestRepo(t)` returns `(dir, runner)` with a real initialized repo (see `internal/git/sync_test.go`), and `gitIn(t, dir, args...)` runs a raw git command. `FakeRunner` (in `internal/gitexec/fake.go`) records `Calls []FakeCall{Name, Argv}` and is configured per span-name via `SetResponse(name, Result)` / `SetError(name, err)` — an unconfigured name returns an error, so set responses for every call your test makes.

- [ ] **Step 1: Write the failing tests**

```go
package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestMergeArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Merge(context.Background(), "", "feat"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := []string{"merge", "--no-edit", "feat"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Merge(context.Background(), "/wt/x", "feat"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := []string{"-C", "/wt/x", "merge", "--no-edit", "feat"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.MergeAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("merge abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "merge", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeInProgressExitOneMeansNo(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse MERGE_HEAD", gitexec.Result{ExitCode: 1})
	f.SetError("git rev-parse MERGE_HEAD", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	in, err := r.MergeInProgress(context.Background(), "")
	if err != nil {
		t.Fatalf("MergeInProgress: %v", err)
	}
	if in {
		t.Fatal("exit 1 must mean 'no merge in progress'")
	}
}

// Real-git: build an actual conflict, observe MERGE_HEAD, abort, observe clean.
func TestMergeConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "branch", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "main change")
	gitIn(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "feat change")
	gitIn(t, dir, "checkout", "main")

	if in, _ := repo.MergeInProgress(context.Background(), ""); in {
		t.Fatal("no merge should be in progress yet")
	}
	if err := repo.Merge(context.Background(), "", "feat"); err == nil {
		t.Fatal("conflicting merge must fail")
	}
	in, err := repo.MergeInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("MergeInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if err := repo.MergeAbort(context.Background(), ""); err != nil {
		t.Fatalf("merge abort: %v", err)
	}
	if in, _ := repo.MergeInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the merge state")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "main\n" {
		t.Fatalf("f.txt = %q after abort, want %q", got, "main\n")
	}
}
```

NOTE: if `newTestRepo`'s init in this package doesn't create an initial
commit on `main` (check `sync_test.go`), keep the explicit base
write+add+commit above — it makes the test independent of that detail.
If a helper named `gitIn` doesn't exist in package `git`'s tests, check
for the actual raw-git helper name in `sync2_test.go` and use that.

- [ ] **Step 2: Run the tests, verify they fail to compile** (Merge undefined)

Run: `go test ./internal/git/ -run 'TestMerge' -v`
Expected: compile error `r.Merge undefined`.

- [ ] **Step 3: Implement the verbs**

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Merge merges branch into the branch checked out at dir ("" = this repo's
// own worktree). --no-edit keeps the merge-commit message non-interactive.
func (r *Repo) Merge(ctx context.Context, dir, branch string) error {
	b := gitcmd.New("merge").Arg("--no-edit", branch)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge", b.ToArgv())
	return err
}

// MergeAbort aborts an in-progress merge at dir ("" = this repo's worktree).
func (r *Repo) MergeAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("merge").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge --abort", b.ToArgv())
	return err
}

// MergeInProgress reports whether a merge is in progress at dir ("" = this
// repo's worktree), i.e. MERGE_HEAD resolves. rev-parse exit code 1 is the
// normal "no" answer, not a failure (the CanFastForward pattern).
func (r *Repo) MergeInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", "MERGE_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse MERGE_HEAD", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
```

- [ ] **Step 4: Run the tests, verify they pass**

Run: `go test ./internal/git/ -run 'TestMerge' -v`
Expected: PASS (all five).

- [ ] **Step 5: Full package + gates, commit**

```bash
go test ./internal/git/ && go vet ./... && gofmt -l internal cmd
git add internal/git/merge.go internal/git/merge_test.go
git commit -m "feat(git): merge verbs (merge, abort, in-progress probe)"
```

---

### Task 2: engine.SmartMerge

**Files:**
- Create: `internal/engine/smart_merge.go`
- Create: `internal/engine/smart_merge_test.go`

Context: operations implement `Run(ctx, OpDeps) (Result, error)`; `deps.emit` streams `Progress`, `deps.decide` resolves forks (emitting `DecisionNeeded` first; with no Decider it returns `ErrDecisionRequired`). `engine.MapDecider{"id": "option"}` (in `decider.go`) answers decisions in tests. The package test helper `newRepo(t)` (in `ops_basic_test.go`) returns `(dir, *git.Repo)` with one initial commit on `main`. Read `smart_pull.go` and `smart_switch.go` first — SmartMerge deliberately mirrors their autostash/switch/worktree shapes.

- [ ] **Step 1: Write the failing tests**

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitE runs a raw git command in dir (mirrors the run closure in newRepo).
func gitE(t *testing.T, dir string, args ...string) {
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

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// branchWithCommit creates branch off main with one extra commit, returns to main.
func branchWithCommit(t *testing.T, dir, branch, file string) {
	t.Helper()
	gitE(t, dir, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(dir, file), []byte(branch+"\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", branch+" change")
	gitE(t, dir, "checkout", "main")
}

func TestSmartMergeGuards(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")

	cases := []struct {
		name string
		op   SmartMerge
		want string
	}{
		{"empty source", SmartMerge{}, "Source is required"},
		{"same branch", SmartMerge{Source: "main", Target: "main"}, "source and target"},
		{"missing source", SmartMerge{Source: "nope"}, "no such branch: nope"},
		{"missing target", SmartMerge{Source: "feat", Target: "nope"}, "no such branch: nope"},
	}
	for _, tc := range cases {
		_, err := tc.op.Run(context.Background(), OpDeps{Repo: repo})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSmartMergeDetachedHeadNeedsExplicitTarget(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "--detach")

	_, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("err = %v, want detached HEAD guard", err)
	}
}

func TestSmartMergeIntoCurrentBranch(t *testing.T) {
	dir, repo := newRepo(t)
	branchWithCommit(t, dir, "feat", "feat.txt")

	res, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "merged feat into main") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing after merge")
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("on %s, want main", got)
	}
}

func TestSmartMergeIntoBranchInOtherWorktree(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "side")
	wt := filepath.Join(dir, "..", "side-wt")
	gitE(t, dir, "worktree", "add", wt, "side")
	// advance main so there is something to merge into side
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")

	res, err := SmartMerge{Source: "main", Target: "side"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(res.Summary, "in worktree") {
		t.Fatalf("summary = %q, want worktree mention", res.Summary)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("current branch %s changed, want main (we stay put)", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err != nil {
		t.Fatal("merge did not land in the side worktree")
	}
}

func TestSmartMergeIntoUncheckedOutBranchSwitchesAndStays(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "target")
	branchWithCommit(t, dir, "feat", "feat.txt")
	// dirty file on main → autostash must carry it to target and pop it back
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartMerge{Source: "feat", Target: "target"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "target" {
		t.Fatalf("on %s, want target (merge ends on Target)", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing on target after merge")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(got) != "dirty\n" {
		t.Fatal("autostashed change was not restored")
	}
	if !strings.Contains(res.Summary, "merged feat into target") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if out := gitOut(t, dir, "stash", "list"); out != "" {
		t.Fatalf("stash not popped: %q", out)
	}
}

// conflictRepo: main and feat both edit shared.txt → guaranteed conflict.
// (Add `"github.com/gigagit/gg/internal/git"` to the test imports.)
func conflictRepo(t *testing.T) (string, *git.Repo) {
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
	return dir, repo
}

func TestSmartMergeConflictAbort(t *testing.T) {
	dir, repo := conflictRepo(t)
	res, err := SmartMerge{Source: "feat"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"merge-conflict": "abort"}})
	if err != nil {
		t.Fatalf("chosen abort must not be an error: %v", err)
	}
	if !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort, want main's version", got)
	}
}

func TestSmartMergeConflictKeep(t *testing.T) {
	dir, repo := conflictRepo(t)
	res, err := SmartMerge{Source: "feat"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"merge-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("keep-conflicts must surface an error (CLI exit 1)")
	}
	if !strings.Contains(res.Summary, "conflicts") {
		t.Fatalf("summary = %q", res.Summary)
	}
	// MERGE_HEAD must still exist (merge left in progress)
	if gitOut(t, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == "" {
		t.Fatal("merge state was not kept")
	}
}

func TestSmartMergeConflictUndecidedLeavesMergeState(t *testing.T) {
	dir, repo := conflictRepo(t)
	_, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("undecided conflict must error")
	}
	// The decision fires only after the conflict exists: state stays.
	if gitOut(t, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == "" {
		t.Fatal("expected merge still in progress")
	}
}
```

- [ ] **Step 2: Run, verify compile failure** (`SmartMerge` undefined)

Run: `go test ./internal/engine/ -run 'TestSmartMerge' -v`
Expected: compile error.

- [ ] **Step 3: Implement SmartMerge**

```go
package engine

import (
	"context"
	"fmt"
)

// SmartMerge merges Source into Target (default: the current branch), picking
// the simplest correct path: in place when Target is checked out here, inside
// the worktree that has Target checked out (you stay put), else autostash +
// switch + merge, ending on Target. A conflicted merge forks via the
// "merge-conflict" decision: keep-conflicts leaves the tree for manual
// resolution (the op returns an error), abort runs `git merge --abort`.
type SmartMerge struct {
	Source string
	Target string
}

var _ Operation = SmartMerge{}

func (op SmartMerge) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Source == "" {
		return Result{}, fmt.Errorf("smart merge: Source is required")
	}
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	target := op.Target
	if target == "" {
		if cur == "" {
			return Result{}, fmt.Errorf("smart merge: detached HEAD — specify a target branch")
		}
		target = cur
	}
	if op.Source == target {
		return Result{}, fmt.Errorf("smart merge: source and target are both %s", target)
	}
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	have := make(map[string]bool, len(branches))
	for _, b := range branches {
		have[b.Name] = true
	}
	for _, name := range []string{op.Source, target} {
		if !have[name] {
			return Result{}, fmt.Errorf("smart merge: no such branch: %s", name)
		}
	}

	// Rung 1: Target is checked out right here.
	if target == cur {
		return op.mergeAt(ctx, deps, "", target)
	}

	// Rung 2: Target lives in another worktree — merge there, stay put.
	wt, err := deps.Repo.WorktreeForBranch(ctx, target)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return op.mergeAt(ctx, deps, wt.Path, target)
	}

	// Rung 3: autostash if dirty, switch to Target, merge, stay on Target.
	dirty, err := deps.Repo.IsDirty(ctx)
	if err != nil {
		return Result{}, err
	}
	stashed := false
	if dirty {
		deps.emit(ctx, Progress{Step: "stashing"})
		if err := deps.Repo.StashPush(ctx, "gg-autostash:"+target); err != nil {
			return Result{}, err
		}
		stashed = true
	}
	deps.emit(ctx, Progress{Step: "switching", Detail: target})
	if err := deps.Repo.Switch(ctx, target); err != nil {
		if stashed {
			_ = deps.Repo.StashPop(ctx) // best-effort restore on the original branch
		}
		return Result{}, err
	}

	res, mergeErr := op.mergeAt(ctx, deps, "", target)
	if mergeErr != nil {
		// Conflicts kept (or the merge failed outright): popping the stash onto
		// that tree would compound the mess. The stash survives.
		if stashed && res.Summary != "" {
			res.Summary += " (your changes remain stashed)"
		}
		return res, mergeErr
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
				fmt.Errorf("stash pop conflict after merging into %s: %w", target, err)
		}
	}
	return res, nil
}

// mergeAt merges op.Source into target inside dir ("" = the current
// worktree), resolving a conflict via the merge-conflict decision.
func (op SmartMerge) mergeAt(ctx context.Context, deps OpDeps, dir, target string) (Result, error) {
	where := ""
	if dir != "" {
		where = " in worktree " + dir
	}
	deps.emit(ctx, Progress{Step: "merging", Detail: op.Source + " into " + target + where})
	mergeErr := deps.Repo.Merge(ctx, dir, op.Source)
	if mergeErr == nil {
		return Result{Summary: "merged " + op.Source + " into " + target + where, Changed: true}, nil
	}
	inMerge, stateErr := deps.Repo.MergeInProgress(ctx, dir)
	if stateErr != nil {
		return Result{}, fmt.Errorf("smart merge: %s into %s: %v (state check: %w)", op.Source, target, mergeErr, stateErr)
	}
	if !inMerge {
		// Refused outright (e.g. unrelated histories): nothing to resolve.
		return Result{}, fmt.Errorf("smart merge: %s into %s: %w", op.Source, target, mergeErr)
	}
	choice, derr := deps.decide(ctx, DecisionRequest{
		ID:      "merge-conflict",
		Prompt:  "Merging " + op.Source + " into " + target + " hit conflicts",
		Options: []string{"keep-conflicts", "abort"},
	})
	if derr != nil {
		return Result{}, derr
	}
	if choice.Option == "keep-conflicts" {
		return Result{Summary: "merge of " + op.Source + " into " + target + where + " has conflicts (left in tree)", Changed: true},
			fmt.Errorf("merge conflict: %s into %s", op.Source, target)
	}
	if err := deps.Repo.MergeAbort(ctx, dir); err != nil {
		return Result{}, fmt.Errorf("smart merge: abort failed: %w", err)
	}
	return Result{Summary: "aborted: merging " + op.Source + " into " + target, Changed: false}, nil
}
```

- [ ] **Step 4: Run the tests, verify they pass**

Run: `go test ./internal/engine/ -run 'TestSmartMerge' -v`
Expected: PASS (all nine).

- [ ] **Step 5: Full package + gates, commit**

```bash
go test ./internal/engine/ && go vet ./... && gofmt -l internal cmd
git add internal/engine/smart_merge.go internal/engine/smart_merge_test.go
git commit -m "feat(engine): SmartMerge with worktree-aware ladder and merge-conflict fork"
```

---

### Task 3: TUI mark mechanism + pair-op popup

**Files:**
- Modify: `internal/tui/viewstate.go` (panelList interface ~line 135, the four list impls)
- Create: `internal/tui/mark.go`
- Create: `internal/tui/pairop_popup.go`
- Modify: `internal/tui/model.go` (fields ~line 40, routing ~line 140, keys ~line 167)
- Modify: `internal/tui/view.go` (render ~line 80, renderInterface footer/status ~line 142, renderPanel ~line 192, windowRows ~line 242)
- Modify: `internal/tui/tooltip.go:44` (windowRows signature)
- Create: `internal/tui/mark_test.go`

Context: `Model` is a value receiver; popups are pointer fields checked in a fixed routing order inside `Update` (modal → popup → repoPopup → settings → branchPopup → filterTyping → normal keys) and rendered in `render()`. `panelView(p)` returns `(rows, idx)` where display row n shows backing element `idx[n]`; `backingIndex(p)` resolves the selection. `startOp(op)` runs an engine operation asynchronously. Tests construct `Model` literals or use `newRepoDir(t)` + `driveOp(t, m, cmd)` (see `op_test.go`).

- [ ] **Step 1: Write the failing tests**

```go
package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/model"
)

func markModel() Model {
	return Model{
		branches:  []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}, {Name: "feat/b"}},
		commits:   []model.Commit{{Hash: "1111111", Subject: "one"}, {Hash: "2222222", Subject: "two"}},
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		focus:     panelBranches,
	}
}

func pressRune(t *testing.T, m Model, r string) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(r)})
	return updated.(Model)
}

func pressType(t *testing.T, m Model, kt tea.KeyType) Model {
	t.Helper()
	updated, _ := m.Update(tea.KeyMsg{Type: kt})
	return updated.(Model)
}

func TestMarkToggle(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	if m.mark == nil || m.mark.key != "main" || m.mark.panel != panelBranches {
		t.Fatalf("mark = %+v, want main on branches", m.mark)
	}
	m = pressRune(t, m, "m") // same row again: unmark
	if m.mark != nil {
		t.Fatal("second m on the marked row must unmark")
	}
}

func TestMarkMovesAcrossPanels(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m") // mark main on branches
	m.focus = panelCommits
	m = pressRune(t, m, "m")
	if m.mark == nil || m.mark.panel != panelCommits || m.mark.key != "1111111" {
		t.Fatalf("mark = %+v, want commit 1111111", m.mark)
	}
	if m.pairPopup != nil {
		t.Fatal("cross-panel m must move the mark, not open the popup")
	}
}

func TestMarkPairOpensPopupOnBranches(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m") // mark main
	m.sel[panelBranches] = 1 // feat/a
	m = pressRune(t, m, "m")
	if m.pairPopup == nil {
		t.Fatal("expected the pair-op popup")
	}
	if m.pairPopup.marked != "main" || m.pairPopup.selected != "feat/a" {
		t.Fatalf("popup pair = %s + %s", m.pairPopup.marked, m.pairPopup.selected)
	}
	if len(m.pairPopup.ops) != 2 {
		t.Fatalf("branches must register merge + rebase, got %d ops", len(m.pairPopup.ops))
	}
}

func TestMarkPairNoOpsPanel(t *testing.T) {
	m := markModel()
	m.focus = panelCommits
	m = pressRune(t, m, "m")
	m.sel[panelCommits] = 1
	m = pressRune(t, m, "m")
	if m.pairPopup != nil {
		t.Fatal("commits panel has no pair ops")
	}
	if !strings.Contains(m.statusMsg, "no pair operations") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestMarkSurvivesResortByIdentity(t *testing.T) {
	m := markModel()
	m.sel[panelBranches] = 1 // feat/a
	m = pressRune(t, m, "m")
	m.sortModes[panelBranches] = sortNameDesc // main, feat/b, feat/a
	if got := m.markDisplayIndex(panelBranches); got != 2 {
		t.Fatalf("markDisplayIndex = %d after resort, want 2 (identity, not index)", got)
	}
}

func TestDeadMarkRemarksInsteadOfPairing(t *testing.T) {
	m := markModel()
	m.sel[panelBranches] = 2 // feat/b
	m = pressRune(t, m, "m")
	// feat/b disappears (e.g. deleted elsewhere + reload)
	m.branches = []model.Branch{{Name: "main", IsHead: true}, {Name: "feat/a"}}
	m.sel[panelBranches] = 0
	m = pressRune(t, m, "m")
	if m.pairPopup != nil {
		t.Fatal("a dead mark must not open the popup")
	}
	if m.mark == nil || m.mark.key != "main" {
		t.Fatalf("mark = %+v, want re-marked main", m.mark)
	}
}

func TestEscClearsMarkBeforeFilter(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	m.filterPanel = panelBranches
	m.filterQuery = "fe"
	m = pressType(t, m, tea.KeyEsc)
	if m.mark != nil {
		t.Fatal("first esc must clear the mark")
	}
	if m.filterQuery != "fe" {
		t.Fatal("first esc must NOT clear the filter while a mark exists")
	}
	m = pressType(t, m, tea.KeyEsc)
	if m.filterQuery != "" {
		t.Fatal("second esc must clear the filter")
	}
}

func TestPairPopupDisabledRebaseEntry(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	m.sel[panelBranches] = 1
	m = pressRune(t, m, "m") // popup open
	m = pressRune(t, m, "j") // move to rebase
	m = pressType(t, m, tea.KeyEnter)
	if m.pairPopup == nil {
		t.Fatal("selecting a disabled entry must keep the popup open")
	}
	if !strings.Contains(m.statusMsg, "not implemented") {
		t.Fatalf("statusMsg = %q", m.statusMsg)
	}
}

func TestPairPopupEscKeepsMark(t *testing.T) {
	m := markModel()
	m = pressRune(t, m, "m")
	m.sel[panelBranches] = 1
	m = pressRune(t, m, "m")
	m = pressType(t, m, tea.KeyEsc)
	if m.pairPopup != nil {
		t.Fatal("esc must close the popup")
	}
	if m.mark == nil {
		t.Fatal("esc must keep the mark (user may pick another row)")
	}
}

func TestMarkedRowRendersDiamondAndStatusHint(t *testing.T) {
	m := markModel()
	m.width, m.height = 80, 24
	m.sel[panelBranches] = 1
	m = pressRune(t, m, "m")
	out := m.render()
	if !strings.Contains(out, "◆") {
		t.Fatal("marked row must render the ◆ prefix")
	}
	if !strings.Contains(out, "marked: feat/a") {
		t.Fatal("status line must show the mark hint")
	}
}

// Integration: enter on Merge dispatches SmartMerge and the merge really runs.
func TestPairPopupEnterRunsSmartMerge(t *testing.T) {
	dir, repo := newRepoDir(t)
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
	run("checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "feat change")
	run("checkout", "main")

	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.focus = panelBranches

	// find and mark feat, then select main and pair
	_, idx := m.panelView(panelBranches)
	l := m.listFor(panelBranches)
	for n, i := range idx {
		if l.Key(i) == "feat" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // mark feat
	for n, i := range idx {
		if l.Key(i) == "main" {
			m.sel[panelBranches] = n
		}
	}
	m = pressRune(t, m, "m") // popup: Merge feat into main first entry
	if m.pairPopup == nil {
		t.Fatal("popup expected")
	}
	updated, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = updated.(Model)
	if m.mark != nil || m.pairPopup != nil {
		t.Fatal("enter must clear both the popup and the mark")
	}
	m = driveOp(t, m, cmd)
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("merge did not run: feat.txt missing on main")
	}
}
```

- [ ] **Step 2: Run, verify compile failure** (`m.mark` undefined)

Run: `go test ./internal/tui/ -run 'TestMark|TestPairPopup|TestEscClears|TestDeadMark' -v`
Expected: compile error.

- [ ] **Step 3: Add `Key` to `panelList` (viewstate.go)**

In the `panelList` interface (after `Date(i int) int64`):

```go
	Key(i int) string  // stable identity of backing element i (mark survival)
```

And the four implementations, each next to its `Date` method:

```go
func (l branchList) Key(i int) string   { return l.items[i].Name }
func (l worktreeList) Key(i int) string { return l.items[i].Path }
func (l statusList) Key(i int) string   { return l.files[i].Path }
func (l commitList) Key(i int) string   { return l.items[i].Hash }
```

- [ ] **Step 4: Create `internal/tui/mark.go`**

```go
package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// markState identifies a marked row by stable identity (panelList.Key), not
// index, so it survives reloads, re-sorts, and filtering.
type markState struct {
	panel   panel
	key     string
	display string // human label for the status bar / popup (Key for now)
}

// pairOp is one two-argument operation a panel offers on (marked, selected).
type pairOp struct {
	label   func(marked, selected string) string
	build   func(marked, selected string) engine.Operation // nil when !enabled
	enabled bool
	note    string // shown for disabled entries
}

// pairOpsFor returns panel p's pair-operations. Only Branches has any; the
// labels spell out the direction so marked-vs-selected never carries
// implicit meaning.
func pairOpsFor(p panel) []pairOp {
	if p != panelBranches {
		return nil
	}
	return []pairOp{
		{
			label: func(marked, selected string) string { return "Merge " + marked + " into " + selected },
			build: func(marked, selected string) engine.Operation {
				return engine.SmartMerge{Source: marked, Target: selected}
			},
			enabled: true,
		},
		{
			label: func(marked, selected string) string { return "Rebase " + selected + " onto " + marked },
			note:  "not implemented yet",
		},
	}
}

// handleMarkKey implements the m-key state machine: mark, toggle off,
// move across panels, or pair with the marked row (opening the popup).
func (m Model) handleMarkKey() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(m.focus)
	if !ok {
		return m, nil
	}
	key := m.listFor(m.focus).Key(bi)
	// No mark, a mark in another panel, or a dead mark: (re-)mark here.
	if m.mark == nil || m.mark.panel != m.focus || !m.markAlive() {
		m.mark = &markState{panel: m.focus, key: key, display: key}
		return m, nil
	}
	if m.mark.key == key { // same row: toggle off
		m.mark = nil
		return m, nil
	}
	ops := pairOpsFor(m.focus)
	if len(ops) == 0 {
		m.statusMsg = "no pair operations for this panel"
		return m, nil
	}
	m.pairPopup = &pairOpPopup{marked: m.mark.display, selected: key, ops: ops}
	return m, nil
}

// markAlive reports whether the marked row still exists in its panel's
// backing list.
func (m Model) markAlive() bool {
	if m.mark == nil {
		return false
	}
	l := m.listFor(m.mark.panel)
	for i := 0; i < l.Len(); i++ {
		if l.Key(i) == m.mark.key {
			return true
		}
	}
	return false
}

// markDisplayIndex returns the display-row index of the mark in panel p, or
// -1 when p holds no living mark (or it is filtered out of view).
func (m Model) markDisplayIndex(p panel) int {
	if m.mark == nil || m.mark.panel != p {
		return -1
	}
	l := m.listFor(p)
	_, idx := m.panelView(p)
	for n, i := range idx {
		if l.Key(i) == m.mark.key {
			return n
		}
	}
	return -1
}
```

- [ ] **Step 5: Create `internal/tui/pairop_popup.go`**

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// pairOpPopup offers a panel's two-argument operations on (marked, selected).
type pairOpPopup struct {
	marked, selected string
	ops              []pairOp
	sel              int
}

// updatePairPopupKey handles one key while the pair-op popup is open. The
// popup swallows every key; ctrl+c still quits.
func (m Model) updatePairPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.pairPopup
	switch msg.String() {
	case "esc":
		m.pairPopup = nil // the mark survives: the user may pick another row
	case "up", "k":
		if p.sel > 0 {
			p.sel--
		}
	case "down", "j":
		if p.sel < len(p.ops)-1 {
			p.sel++
		}
	case "enter":
		op := p.ops[p.sel]
		if !op.enabled {
			m.statusMsg = op.label(p.marked, p.selected) + ": " + op.note
			return m, nil
		}
		marked, selected := p.marked, p.selected
		m.pairPopup = nil
		m.mark = nil
		return m.startOp(op.build(marked, selected))
	}
	return m, nil
}

// renderPairOpPopup draws the operation picker.
func (m Model) renderPairOpPopup() string {
	p := m.pairPopup
	var b strings.Builder
	b.WriteString(p.marked + " + " + p.selected + "\n\n")
	for i, op := range p.ops {
		line := op.label(p.marked, p.selected)
		if !op.enabled {
			line += "  (" + op.note + ")"
		}
		if i == p.sel {
			b.WriteString(selectedRow.Render("> " + line))
		} else {
			b.WriteString("  " + line)
		}
		b.WriteString("\n")
	}
	b.WriteString("\n[↑/↓] choose  [enter] run  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

- [ ] **Step 6: Wire `model.go`**

Add the fields after `pendingSwitchBranch` (keep the existing field comments):

```go
	mark      *markState   // the m-key mark; nil = none (see mark.go)
	pairPopup *pairOpPopup // two-row operation picker; nil = closed
```

Add the routing entry directly AFTER the `m.branchPopup != nil` check:

```go
		if m.pairPopup != nil {
			return m.updatePairPopupKey(msg)
		}
```

Add the `m` key case (next to the other action keys):

```go
		case "m":
			if !m.running && !m.loading {
				return m.handleMarkKey()
			}
```

Replace the existing `esc` case body (filter clearing) with mark-first
precedence — keep the existing filterPanel comment where it is:

```go
		case "esc":
			if m.mark != nil {
				m.mark = nil
				return m, nil
			}
			// filterPanel is intentionally left set — filterActive() gates on a
			// non-empty query, so the residue is inert.
			if m.filterQuery != "" {
				m.filterQuery = ""
			}
```

- [ ] **Step 7: Wire `view.go` (+ `tooltip.go`)**

1. `render()`: extend the tooltip-suppression condition and the overlay chain:

```go
	if m.popup == nil && m.repoPopup == nil && m.settings == nil && m.branchPopup == nil && m.pairPopup == nil {
```

and after the `branchPopup` overlay branch:

```go
	if m.pairPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderPairOpPopup(), w, h)
	}
```

2. `windowRows` also returns the window's start offset (needed to place the
mark inside the visible window):

```go
// windowRows returns at most n rows scrolled so sel stays visible, sel's
// index within the returned window, and the window's start offset.
func windowRows(rows []string, n, sel int) ([]string, int, int) {
	if n <= 0 {
		n = 1
	}
	if len(rows) <= n {
		return rows, sel, 0
	}
	start := sel - n/2
	if start < 0 {
		start = 0
	}
	if start+n > len(rows) {
		start = len(rows) - n
	}
	if start < 0 {
		start = 0
	}
	return rows[start : start+n], sel - start, start
}
```

Update BOTH callers: `renderPanel` (next item) and `tooltip.go:44`
(`_, selInWin, _ := windowRows(rows, rowsCap, sel)`). If any existing test
calls `windowRows` directly, add the third blank assignment there too.

3. `renderPanel` row loop — the marked row gets a `◆ ` prefix (it wins over
the `> ` focus prefix; reverse-video still shows focus):

```go
		win, selInWin, start := windowRows(rows, rowsCap, m.sel[p])
		markedInWin := -1
		if md := m.markDisplayIndex(p); md >= 0 {
			markedInWin = md - start
		}
		for i, row := range win {
			focused := i == selInWin && p == m.focus
			prefix := "  "
			if i == markedInWin {
				prefix = "◆ "
			} else if focused {
				prefix = "> "
			}
			line := padRight(truncate(prefix+row, innerW), innerW)
			if focused {
				line = selectedRow.Render(line)
			}
			lines = append(lines, line)
		}
```

4. `renderInterface` status line — prepend the mark hint:

```go
	statusLine := m.statusMsg
	if m.mark != nil && m.markAlive() {
		hint := "◆ marked: " + m.mark.display
		if statusLine != "" {
			statusLine = hint + " · " + statusLine
		} else {
			statusLine = hint
		}
	}
	if m.running {
		statusLine = "⏳ " + statusLine
	}
	statusLine = truncate(statusLine, g.w)
```

5. Footer — add `[m]ark` after `[w]orktree`:

```go
	footer := truncate("[p]ull [P]ush [s]witch [b]ranch [S]tash [u]ndo [w]orktree [m]ark [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [q] quit", g.w)
```

- [ ] **Step 8: Run the new tests, verify they pass**

Run: `go test ./internal/tui/ -run 'TestMark|TestPairPopup|TestEscClears|TestDeadMark' -v`
Expected: PASS (all twelve).

- [ ] **Step 9: Full package + gates, commit**

```bash
go test ./internal/tui/ && go vet ./... && gofmt -l internal cmd
git add internal/tui/
git commit -m "feat(tui): mark-and-pair mechanism with pair-op popup (merge enabled, rebase stub)"
```

---

### Task 4: CLI `gg merge`

**Files:**
- Create: `internal/cli/merge.go`
- Create: `internal/cli/merge_test.go`
- Modify: `internal/cli/cli.go` (the `switch cmd` ~line 48 and the `commands` map ~line 77)

Context: commands receive `(repo *repoT, args, stdin, stdout, stderr)`, parse flags with a `flag.FlagSet` (flags BEFORE positionals), build a `cliDecider{policy, in, out, interactive: stdinIsTerminal()}`, call `runOperation`, and map via `finish` (error → exit 1, summary → `✓ …` on stdout, exit 0). Test helpers in package: `newRepoDir(t)` (returns dir only — check `core_test.go` for its exact signature; `branch_test.go` uses `dir := newRepoDir(t)`), `gitRun(t, dir, args...)`.

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

// mergeFixture: main and feat with a non-conflicting extra file on feat.
func mergeFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat change")
	gitRun(t, dir, "checkout", "main")
	return dir
}

// conflictFixture: main and feat both edit shared.txt.
func conflictFixture(t *testing.T) string {
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
	return dir
}

func TestMergeIntoCurrent(t *testing.T) {
	dir := mergeFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "merged feat into main") {
		t.Fatalf("stdout: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing after merge")
	}
}

func TestMergeIntoExplicitTarget(t *testing.T) {
	dir := mergeFixture(t)
	gitRun(t, dir, "branch", "target")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--into", "target", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	// SmartMerge rung 3 ends on the target branch.
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "target" {
		t.Fatalf("on %q, want target", strings.TrimSpace(string(cur)))
	}
}

func TestMergeConflictUnansweredNonTTY(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestMergeConflictAbortFlag(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=abort", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort", got)
	}
}

func TestMergeConflictKeepFlag(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=keep", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run(); err != nil {
		t.Fatal("expected the merge left in progress")
	}
}

func TestMergeUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"merge"},                                   // missing source
		{"merge", "a", "b"},                         // too many positionals
		{"merge", "--on-conflict=bogus", "feat"},    // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
```

- [ ] **Step 2: Run, verify failure** (unknown command "merge", exit 2)

Run: `go test ./internal/cli/ -run 'TestMerge' -v`
Expected: FAIL (`unknown command "merge"` → exit 2 where 0/1 expected).

- [ ] **Step 3: Implement `internal/cli/merge.go` and route it**

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/engine"
)

// cmdMerge implements `gg merge [--into <target>] [--on-conflict=keep|abort]
// <source>`. Flags precede the positional source branch. --on-conflict
// pre-answers the merge-conflict fork; with neither flag nor TTY the
// conflict surfaces as exit 1 with the options on stderr.
func cmdMerge(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(stderr)
	into := fs.String("into", "", "target branch (default: the current branch)")
	onConflict := fs.String("on-conflict", "", "answer a merge conflict: keep|abort")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg merge [--into <target>] [--on-conflict=keep|abort] <source>")
		return 2
	}
	policy := map[string]string{}
	switch *onConflict {
	case "":
	case "keep":
		policy["merge-conflict"] = "keep-conflicts"
	case "abort":
		policy["merge-conflict"] = "abort"
	default:
		fmt.Fprintf(stderr, "merge: invalid --on-conflict %q (keep|abort)\n", *onConflict)
		return 2
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), repo,
		engine.SmartMerge{Source: fs.Arg(0), Target: *into}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

In `cli.go`, add to the `switch cmd` (after `case "switch":`):

```go
	case "merge":
		return cmdMerge(repo, rest, stdin, stdout, stderr)
```

and add `"merge": true,` to the `commands` map.

- [ ] **Step 4: Run the tests, verify they pass**

Run: `go test ./internal/cli/ -run 'TestMerge' -v`
Expected: PASS (all six).

- [ ] **Step 5: Full package + gates, commit**

```bash
go test ./internal/cli/ && go vet ./... && gofmt -l internal cmd
git add internal/cli/merge.go internal/cli/merge_test.go internal/cli/cli.go
git commit -m "feat(cli): gg merge with --into and --on-conflict policy"
```

---

### Task 5: e2e scenarios + contract row

**Files:**
- Create: `e2e/scenarios/s26_merge_ff.toml` … `e2e/scenarios/s32_merge_guards.toml` (7 files)
- Modify: `.claude/skills/writing-e2e-scenarios/SKILL.md` (contract table)

THE GOLDEN RULE: the expectations below are derived from SmartMerge's
contract (the spec + Task 2), not from running anything. If a scenario
fails, suspect the expectation derivation OR a real engine bug — investigate,
don't copy observed output. Harness facts that matter here: the injected
`.gg.toml` is committed by the FIRST `commit` step (so the first subject's
commit also carries it — subjects lists are exact and complete); commit dates
are frozen and strictly increasing in step order (newest-first log order
follows step order, latest step first); stdin is not a TTY.

- [ ] **Step 1: Add the contract row to the skill**

In `.claude/skills/writing-e2e-scenarios/SKILL.md`, add to the operation
contracts table after the `branch delete` row:

```markdown
| `merge [--into <t>] [--on-conflict=keep\|abort] <s>` | Merges `<s>` into `<t>` (default: current). Ends on `<t>` ONLY when `<t>` wasn't checked out anywhere (rung 3); if `<t>` lives in a linked worktree the merge lands THERE and you stay. Conflict → `merge-conflict` fork `["keep-conflicts","abort"]`: `abort` = exit 0 + tree restored; `keep` = exit 1 + `in_progress="merge"` + conflicted files; unanswered (no flag, no TTY) = exit 1 with the conflict LEFT IN PLACE (the decision fires after the conflict exists). Guards (same branch, missing branch, detached HEAD without `--into`) → exit 1, nothing happens. |
```

- [ ] **Step 2: Write the seven scenarios**

`e2e/scenarios/s26_merge_ff.toml`:

```toml
name = "merge: fast-forwards when the target hasn't diverged"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "feat.txt", content = "feat\n" },
  { commit = "feat change" },
  { switch = "main" },
]

[[run]]
cmd  = ["merge", "feat"]
exit = 0

[expect]
branch = "main"
clean  = true

[expect.files]
"feat.txt" = "feat\n"

[[expect.log]]
# Fast-forward: no merge commit, feat's commit is simply on main.
subjects = ["feat change", "base"]
```

`e2e/scenarios/s27_merge_true_merge.toml`:

```toml
name = "merge: diverged branches produce a merge commit"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "feat.txt", content = "feat\n" },
  { commit = "feat change" },
  { switch = "main" },
  { write = "main.txt", content = "main\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "feat"]
exit = 0

[expect]
branch = "main"
clean  = true

[expect.files]
"feat.txt" = "feat\n"
"main.txt" = "main\n"

[[expect.log]]
# Merge commit on top; "main change" was committed after "feat change"
# (later builder tick) so it sorts above it in date order.
subjects = [{ matches = "^Merge" }, "main change", "feat change", "base"]
```

`e2e/scenarios/s28_merge_conflict_abort.toml`:

```toml
name = "merge: conflict with --on-conflict=abort restores the tree (exit 0)"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "shared.txt", content = "feat\n" },
  { commit = "feat change" },
  { switch = "main" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "--on-conflict=abort", "feat"]
exit = 0

[expect]
branch      = "main"
clean       = true
in_progress = "none"

[expect.files]
"shared.txt" = "main\n"

[[expect.log]]
subjects = ["main change", "feat change", "base"]
```

`e2e/scenarios/s29_merge_conflict_keep.toml`:

```toml
name = "merge: conflict with --on-conflict=keep leaves the merge in progress (exit 1)"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "shared.txt", content = "feat\n" },
  { commit = "feat change" },
  { switch = "main" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "--on-conflict=keep", "feat"]
exit = 1

[expect]
branch      = "main"
in_progress = "merge"

[expect.status]
conflicted = ["shared.txt"]
```

`e2e/scenarios/s30_merge_conflict_unanswered.toml`:

```toml
name = "merge: unanswered conflict (no flag, no TTY) exits 1 with the merge left in place"

[input]
steps = [
  { write = "shared.txt", content = "base\n" },
  { commit = "base" },
  { branch = "feat" },
  { switch = "feat" },
  { write = "shared.txt", content = "feat\n" },
  { commit = "feat change" },
  { switch = "main" },
  { write = "shared.txt", content = "main\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "feat"]
exit = 1

[expect]
branch      = "main"
# The merge-conflict decision fires only AFTER the conflict exists; an
# unanswered decision errors out with the conflicted tree still in place.
in_progress = "merge"

[expect.status]
conflicted = ["shared.txt"]
```

`e2e/scenarios/s31_merge_into_worktree_branch.toml`:

```toml
name = "merge --into a branch checked out in a linked worktree: merges there, you stay"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
  { branch = "side" },
  { worktree = "wt-side", branch = "side" },
  { write = "main.txt", content = "m\n" },
  { commit = "main change" },
]

[[run]]
cmd  = ["merge", "--into", "side", "main"]
exit = 0

[expect]
branch    = "main"                   # the main worktree never switched
worktrees = ["wt-side"]

[[expect.log]]
branch   = "side"
# side was created at "base" and fast-forwards onto main's tip.
subjects = ["main change", "base"]

[expect.worktree."wt-side".files]
"main.txt" = "m\n"
```

`e2e/scenarios/s32_merge_guards.toml`:

```toml
name = "merge: guards fail fast (same branch, missing branch), nothing happens"

[input]
steps = [
  { write = "base.txt", content = "base\n" },
  { commit = "base" },
]

[[run]]
cmd  = ["merge", "--into", "main", "main"]
exit = 1

[[run]]
cmd  = ["merge", "no-such-branch"]
exit = 1

[expect]
branch      = "main"
clean       = true
in_progress = "none"

[[expect.log]]
subjects = ["base"]
```

- [ ] **Step 3: Run each scenario**

Run: `go test ./e2e -run 'TestScenarios/s26|TestScenarios/s27|TestScenarios/s28|TestScenarios/s29|TestScenarios/s30|TestScenarios/s31|TestScenarios/s32' -v`
Expected: PASS ×7. A failure means a wrong expectation derivation or a real
bug — investigate per the golden rule before touching the TOML. (Check the
exact `{ worktree = … }` input-step semantics against
`e2e/scenarios/s11_pull_in_worktree.toml` if s31 misbehaves.)

- [ ] **Step 4: Full e2e + commit**

```bash
go test ./e2e
git add e2e/scenarios/ .claude/skills/writing-e2e-scenarios/SKILL.md
git commit -m "test(e2e): merge scenarios s26-s32 + contract row"
```

---

### Task 6: Docs — agent skill v5, README, CHANGELOG, TUI-windows skill

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (Version)
- Regenerate: `.claude/skills/using-gg/SKILL.md` (drift-guard `TestDogfoodSkillCopyInSync` enforces byte-identity)
- Modify: `README.md`, `CHANGELOG.md`, `.claude/skills/adding-tui-windows/SKILL.md`

- [ ] **Step 1: Agent skill bullet + version bump**

In `internal/agentskill/using-gg.md`, add next to the existing command
bullets (match the surrounding bullet style exactly — read the file first):

```markdown
- `gg merge [--into <target>] [--on-conflict=keep|abort] <source>` — merge
  one branch into another (default target: current branch; worktree-aware,
  autostashes when it must switch). `--on-conflict` answers the conflict
  fork: `keep` leaves conflicts in the tree (exit 1), `abort` restores (exit 0).
```

In `internal/agentskill/agentskill.go`, bump `Version = 4` → `Version = 5`.

- [ ] **Step 2: Regenerate the dogfood copy and verify the drift guard**

```bash
go build ./cmd/gg && ./gg init --update
go test ./internal/agentskill/ -run TestDogfoodSkillCopyInSync -v
```

Expected: PASS, and `git status` shows `.claude/skills/using-gg/SKILL.md`
modified.

- [ ] **Step 3: README**

Read `README.md` first. Add `m` (mark / pair-operations) and the mark-`esc`
behavior to the TUI keys table, and a `gg merge` line to the CLI commands
section, matching the existing table/entry style:

- TUI: `m` — mark a row; `m` on another row of the same panel opens the
  pair-operation picker (Branches: merge, rebase-soon); `esc` clears the
  mark first, then the filter.
- CLI: `gg merge [--into <target>] [--on-conflict=keep|abort] <source>`.

- [ ] **Step 4: CHANGELOG**

Add at the top of `## [Unreleased]` → `### Added` in `CHANGELOG.md`:

```markdown
#### Mark-and-pair operations + SmartMerge
- TUI: `m` marks a row; `m` on a second row of the same panel opens a
  pair-operation picker (generic per-panel registry). Branches offer
  **Merge** (worktree-aware SmartMerge: merges in place, in the worktree
  that has the target checked out, or autostash+switch+stay) and a
  disabled Rebase placeholder. `esc` clears the mark before the filter.
- CLI: `gg merge [--into <target>] [--on-conflict=keep|abort] <source>`.
  New `merge-conflict` decision (`keep-conflicts`/`abort`) shared across
  frontends. Embedded using-gg agent skill v5.
```

- [ ] **Step 5: adding-tui-windows skill**

Read `.claude/skills/adding-tui-windows/SKILL.md`. Add a short subsection
(after the popup pattern, matching its heading style) documenting the
mark + pair-op pattern:

```markdown
### Pair-op popup (two-row operations)

For operations taking TWO rows of one panel: the `m` key marks a row by
stable identity (`panelList.Key`, not index — survives reload/sort/filter;
state in `Model.mark *markState`), a second `m` on another row opens
`pairOpPopup` listing the panel's registered `pairOp`s (`pairOpsFor(panel)`
in `mark.go`). Labels spell out the argument direction ("Merge A into B").
To give a panel pair-operations, register entries in `pairOpsFor` — the
mark mechanism, popup, and dispatch are already generic.
```

- [ ] **Step 6: Full gates + commit**

```bash
./test.sh
git add internal/agentskill/ .claude/skills/using-gg/ README.md CHANGELOG.md .claude/skills/adding-tui-windows/SKILL.md
git commit -m "docs: gg merge + mark/pair docs, agent skill v5"
```

---

### Final verification (after all tasks)

```bash
./test.sh race        # full staged suite with -race — the pre-merge gate
go build ./cmd/gg     # rebuild ./gg at the repo root
```

Then a manual smoke test in a scratch repo: `./gg merge --on-conflict=abort <branch>`, and the TUI `m` → `m` → popup flow.
