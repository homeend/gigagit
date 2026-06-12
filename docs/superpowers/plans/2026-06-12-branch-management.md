# Branch Create/Delete Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create and delete local branches from the TUI Branches panel (`b`/`B`/`d`) and a new `gg branch` CLI command, via two new engine operations.

**Architecture:** Two new engine `Operation`s (`CreateBranch`, `DeleteBranch`) over existing git verbs; the TUI adds a small input popup and panel-dispatches the `d` key; the CLI adds a `gg branch <create|delete>` subcommand using the flag-policy decider. `DeleteBranch` reuses the already-shipped `branch-unmerged` decision (ID + options verbatim) and adds an upfront `delete-branch` confirm that the CLI pre-answers.

**Tech Stack:** Go 1.26, Bubble Tea, system git via `internal/gitexec`. Spec: `docs/superpowers/specs/2026-06-12-branch-management-design.md`.

**Working branch:** `feat/branch-management` off `main`.

**Quality gates per task:** `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` (empty output). Run `go test ./... -race` once at the end of the plan. Commit messages end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: git verb — `CreateBranch` gains a start point

**Files:**
- Modify: `internal/git/mutate.go:25-30`
- Test: `internal/git/mutate_test.go`
- Modify (mechanical, call-site updates): `internal/engine/create_worktree_test.go:68`, `internal/engine/smart_switch_test.go:12,32,70`, `internal/engine/undo_test.go:36`

`Repo.CreateBranch` currently creates only at HEAD. Add an optional start point (`git branch <name> [<start>]` — still one invocation). All existing callers are tests; they pass `""`.

- [ ] **Step 1: Write the failing test**

Append to `internal/git/mutate_test.go` (the file already has `newTestRepo`; add the imports `"os/exec"` and `"strings"` to the import block):

```go
// revParse resolves ref to a full sha in dir, failing the test on error.
func revParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateBranchFromStartPoint(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Pin "base" at the initial commit, then advance main so HEAD != base.
	if err := repo.CreateBranch(context.Background(), "base", ""); err != nil {
		t.Fatalf("create base: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.Commit(context.Background(), "second", true); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if err := repo.CreateBranch(context.Background(), "from-base", "base"); err != nil {
		t.Fatalf("create from start point: %v", err)
	}
	if got, want := revParse(t, dir, "from-base"), revParse(t, dir, "base"); got != want {
		t.Fatalf("from-base = %s, want tip of base %s", got, want)
	}
	if revParse(t, dir, "from-base") == revParse(t, dir, "main") {
		t.Fatal("from-base must not point at advanced main")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestCreateBranchFromStartPoint`
Expected: COMPILE FAIL — `too many arguments in call to repo.CreateBranch` (the new signature doesn't exist yet; the existing two-arg call sites define the current shape).

- [ ] **Step 3: Change the verb signature**

In `internal/git/mutate.go` replace:

```go
// CreateBranch creates a new branch at HEAD without switching to it.
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("branch").Arg(name).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch", argv)
	return err
}
```

with:

```go
// CreateBranch creates a new branch without switching to it. An empty
// startPoint means HEAD.
func (r *Repo) CreateBranch(ctx context.Context, name, startPoint string) error {
	argv := gitcmd.New("branch").Arg(name).ArgIf(startPoint != "", startPoint).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch", argv)
	return err
}
```

- [ ] **Step 4: Update the existing call sites (all tests, all gain `""`)**

Each of these calls currently reads `repo.CreateBranch(<ctx>, "<name>")`; add a trailing `, ""` argument:

- `internal/git/mutate_test.go:30` → `repo.CreateBranch(context.Background(), "feature", "")`
- `internal/engine/create_worktree_test.go:68` → `repo.CreateBranch(context.Background(), "dup", "")`
- `internal/engine/smart_switch_test.go:12` → `repo.CreateBranch(context.Background(), "feature", "")`
- `internal/engine/smart_switch_test.go:32` → `repo.CreateBranch(context.Background(), "feature", "")`
- `internal/engine/smart_switch_test.go:70` → `repo.CreateBranch(ctx, "feature", "")`
- `internal/engine/undo_test.go:36` → `repo.CreateBranch(ctx, "feature", "")`

- [ ] **Step 5: Run the full suite to verify it passes**

Run: `go test ./internal/git/ ./internal/engine/`
Expected: PASS (including `TestCreateBranchFromStartPoint` and `TestCreateBranchAndSwitch`).

- [ ] **Step 6: Gate + commit**

```bash
go vet ./... && gofmt -l internal cmd   # must print nothing
git add internal/git/mutate.go internal/git/mutate_test.go internal/engine/create_worktree_test.go internal/engine/smart_switch_test.go internal/engine/undo_test.go
git commit -m "feat(git): CreateBranch verb accepts a start point

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: engine `CreateBranch` operation

**Files:**
- Create: `internal/engine/create_branch.go`
- Test: `internal/engine/create_branch_test.go`

No decisions in this op. Guard `Name`, validate with `CheckRefFormatBranch` (fail fast, the CreateWorktree precedent), then run the verb — git itself refuses an already-existing branch; wrap its error.

The test file reuses package-level helpers that already exist in `internal/engine`: `newRepo(t)` (ops_basic_test.go), `branchExists(t, dir, name)` and `gitIn(t, dir, args...)` (remove_worktree_test.go), `drain(ch)` (ops_basic_test.go).

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/create_branch_test.go`:

```go
package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// engineRevParse resolves ref to a full sha in dir.
func engineRevParse(t *testing.T, dir, ref string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", ref).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateBranchAtHead(t *testing.T) {
	dir, repo := newRepo(t)
	ch := make(chan Event, 16)
	res, err := CreateBranch{Name: "feat/x"}.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "feat/x") {
		t.Fatalf("result = %+v, want Changed with branch in summary", res)
	}
	if !branchExists(t, dir, "feat/x") {
		t.Fatal("branch not created")
	}
	var sawDone bool
	for _, e := range drain(ch) {
		if _, ok := e.(Done); ok {
			sawDone = true
		}
	}
	if !sawDone {
		t.Fatal("expected a Done event")
	}
}

func TestCreateBranchFromStartPoint(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "base")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "advance main")

	_, err := CreateBranch{Name: "feat/from-base", StartPoint: "base"}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateBranch: %v", err)
	}
	if got, want := engineRevParse(t, dir, "feat/from-base"), engineRevParse(t, dir, "base"); got != want {
		t.Fatalf("tip = %s, want %s (the start point)", got, want)
	}
}

func TestCreateBranchInvalidNameFailsFast(t *testing.T) {
	dir, repo := newRepo(t)
	res, err := CreateBranch{Name: "bad..name"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "invalid branch name") {
		t.Fatalf("want invalid-name error, got res=%+v err=%v", res, err)
	}
	if branchExists(t, dir, "bad..name") {
		t.Fatal("invalid branch must not be created")
	}
}

func TestCreateBranchExistingNameErrors(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "taken")
	res, err := CreateBranch{Name: "taken"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatalf("creating an existing branch must error, got %+v", res)
	}
}

func TestCreateBranchRequiresName(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Name is required") {
		t.Fatalf("want Name-required error, got %v", err)
	}
}
```

> Note: a `TestCreateBranchFromStartPoint` also exists in `internal/git` — different package, no clash.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestCreateBranch`
Expected: COMPILE FAIL — `undefined: CreateBranch`.

- [ ] **Step 3: Implement the operation**

Create `internal/engine/create_branch.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// CreateBranch creates a new local branch without switching to it.
type CreateBranch struct {
	Name       string // required
	StartPoint string // "" = HEAD
}

func (op CreateBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("create branch: Name is required")
	}

	// Validate up front so an illegal name fails with a clear message instead
	// of git's terser ref error.
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.Name); err != nil {
		return Result{}, fmt.Errorf("create branch: invalid branch name %q: %w", op.Name, err)
	}

	detail := op.Name
	if op.StartPoint != "" {
		detail += " from " + op.StartPoint
	}
	deps.emit(ctx, Progress{Step: "creating branch", Detail: detail})

	// An already-existing branch is refused by git itself; just wrap the error.
	if err := deps.Repo.CreateBranch(ctx, op.Name, op.StartPoint); err != nil {
		return Result{}, fmt.Errorf("create branch: %w", err)
	}

	res := Result{Summary: "created branch " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateBranch{}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestCreateBranch`
Expected: PASS (5 tests).

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/engine/create_branch.go internal/engine/create_branch_test.go
git commit -m "feat(engine): CreateBranch operation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: engine `DeleteBranch` operation

**Files:**
- Create: `internal/engine/delete_branch.go`
- Test: `internal/engine/delete_branch_test.go`

Shape mirrors `RemoveWorktree` (`internal/engine/remove_worktree.go`): guards fail fast before any decision; the safe command runs first; force is resolved reactively. Two decisions:
- `delete-branch` / `["delete", "abort"]` — upfront confirm (the TUI's single `d` keypress must not delete a ref unconfirmed; the CLI pre-answers it).
- `branch-unmerged` / `["force-delete", "keep"]` — **reuse the exact ID + options already shipped in RemoveWorktree** (cross-frontend decision API; esc in the TUI modal falls back to the last option = `keep`, which is safe).

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/delete_branch_test.go` (helpers `newRepo`, `gitIn`, `branchExists`, `addWorktree`, `MapDecider` all exist in the package):

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeleteBranchMergedDeletes(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "merged")

	res, err := DeleteBranch{Name: "merged"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "merged") {
		t.Fatalf("result = %+v, want Changed with branch in summary", res)
	}
	if branchExists(t, dir, "merged") {
		t.Fatal("branch still exists")
	}
}

func TestDeleteBranchConfirmAbortKeepsBranch(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "stay")

	res, err := DeleteBranch{Name: "stay"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "abort"}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if res.Changed {
		t.Fatal("abort must not change anything")
	}
	if !branchExists(t, dir, "stay") {
		t.Fatal("branch should survive an aborted delete")
	}
}

// unmergedBranch creates a branch with a commit main doesn't have, then
// returns to main, so `git branch -d` refuses to delete it.
func unmergedBranch(t *testing.T, dir, name string) {
	t.Helper()
	gitIn(t, dir, "switch", "-c", name)
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "unmerged work")
	gitIn(t, dir, "switch", "main")
}

func TestDeleteBranchUnmergedForceDeletes(t *testing.T) {
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "risky")

	res, err := DeleteBranch{Name: "risky"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "force-delete",
		}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if branchExists(t, dir, "risky") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestDeleteBranchUnmergedKeepKeeps(t *testing.T) {
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "precious")

	res, err := DeleteBranch{Name: "precious"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "keep",
		}})
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	if res.Changed {
		t.Fatal("keeping the branch must not report Changed")
	}
	if !branchExists(t, dir, "precious") {
		t.Fatal("branch should be kept")
	}
	if !strings.Contains(res.Summary, "kept") {
		t.Fatalf("summary should mention the branch was kept: %q", res.Summary)
	}
}

func TestDeleteBranchGuardsCurrentBranch(t *testing.T) {
	_, repo := newRepo(t)
	_, err := DeleteBranch{Name: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err == nil || !strings.Contains(err.Error(), "checked-out branch") {
		t.Fatalf("want current-branch guard error, got %v", err)
	}
}

func TestDeleteBranchGuardsWorktreeBranch(t *testing.T) {
	dir, repo := newRepo(t)
	addWorktree(t, dir, "feature/wt", "wt-branchdel")

	_, err := DeleteBranch{Name: "feature/wt"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"delete-branch": "delete"}})
	if err == nil || !strings.Contains(err.Error(), "checked out in worktree") {
		t.Fatalf("want worktree guard error, got %v", err)
	}
	if !branchExists(t, dir, "feature/wt") {
		t.Fatal("guarded branch must survive")
	}
}

func TestDeleteBranchRequiresName(t *testing.T) {
	_, repo := newRepo(t)
	_, err := DeleteBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Name is required") {
		t.Fatalf("want Name-required error, got %v", err)
	}
}

func TestDeleteBranchEmitsBothDecisions(t *testing.T) {
	dir, repo := newRepo(t)
	unmergedBranch(t, dir, "forked")

	ch := make(chan Event, 32)
	_, err := DeleteBranch{Name: "forked"}.Run(context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{
			"delete-branch":   "delete",
			"branch-unmerged": "keep",
		}})
	close(ch)
	if err != nil {
		t.Fatalf("DeleteBranch: %v", err)
	}
	got := map[string][]string{}
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok {
			got[d.Request.ID] = d.Request.Options
		}
	}
	if strings.Join(got["delete-branch"], ",") != "delete,abort" {
		t.Fatalf("delete-branch options = %v", got["delete-branch"])
	}
	if strings.Join(got["branch-unmerged"], ",") != "force-delete,keep" {
		t.Fatalf("branch-unmerged options = %v (must match RemoveWorktree's)", got["branch-unmerged"])
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestDeleteBranch`
Expected: COMPILE FAIL — `undefined: DeleteBranch`.

- [ ] **Step 3: Implement the operation**

Create `internal/engine/delete_branch.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// DeleteBranch deletes a local branch. Force is resolved reactively via the
// Decider — only when git refuses the safe `branch -d`.
type DeleteBranch struct {
	Name string // required
}

func (op DeleteBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" {
		return Result{}, fmt.Errorf("delete branch: Name is required")
	}

	// Guards: fail fast with a clear message before asking anything. git would
	// refuse both cases anyway, but only after a pointless confirm prompt.
	cur, err := deps.Repo.CurrentBranch(ctx)
	if err != nil {
		return Result{}, err
	}
	if cur == op.Name {
		return Result{}, fmt.Errorf("delete branch: cannot delete the checked-out branch %s — switch away first", op.Name)
	}
	wts, err := deps.Repo.Worktrees(ctx)
	if err != nil {
		return Result{}, err
	}
	for _, wt := range wts {
		if wt.Branch == op.Name {
			return Result{}, fmt.Errorf("delete branch: %s is checked out in worktree %s — remove the worktree first", op.Name, wt.Path)
		}
	}

	// Decision 1: confirm. A single TUI keypress must not delete a ref
	// unconfirmed; the CLI pre-answers this (the command is the confirmation).
	confirm, err := deps.decide(ctx, DecisionRequest{
		ID:      "delete-branch",
		Prompt:  "Delete branch " + op.Name + "?",
		Options: []string{"delete", "abort"},
	})
	if err != nil {
		return Result{}, err
	}
	if confirm.Option != "delete" {
		return Result{Summary: "cancelled", Changed: false}, nil
	}

	deps.emit(ctx, Progress{Step: "deleting branch", Detail: op.Name})

	// Safe delete first; force only via the same branch-unmerged fork
	// RemoveWorktree ships (one decision shape across frontends).
	if err := deps.Repo.DeleteBranch(ctx, op.Name, false); err != nil {
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "branch-unmerged",
			Prompt:  "Branch " + op.Name + " is not fully merged; force-delete discards its unmerged commits.",
			Options: []string{"force-delete", "keep"},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != "force-delete" {
			return Result{Summary: "kept branch " + op.Name, Changed: false}, nil
		}
		if err := deps.Repo.DeleteBranch(ctx, op.Name, true); err != nil {
			return Result{}, fmt.Errorf("delete branch (force): %w", err)
		}
	}

	res := Result{Summary: "deleted branch " + op.Name, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = DeleteBranch{}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestDeleteBranch`
Expected: PASS (8 tests).

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/engine/delete_branch.go internal/engine/delete_branch_test.go
git commit -m "feat(engine): DeleteBranch operation with confirm and unmerged fork

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: TUI — create-branch popup (`b` / `B`)

**Files:**
- Create: `internal/tui/branch_popup.go`
- Modify: `internal/tui/model.go` (struct fields ~line 32-39, popup routing ~line 129-137, key cases ~line 192, `opFinishedMsg` ~line 286-308)
- Modify: `internal/tui/view.go` (`render()` ~line 74-86, footer ~line 121)
- Test: `internal/tui/branch_popup_test.go`

The popup follows the standard contract (pointer field on Model, swallows all keys, `overlayCenter`). `B` chains `SmartSwitch` after a successful create via a new `pendingSwitchBranch` field — the `pendingSwitch` worktree precedent at model.go:299.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/branch_popup_test.go` (helpers `newRepoDir(t) (string, *git.Repo)`, `runGit`, `keyMsg` already exist in the tui test package — see `worktree_delete_test.go` for the drive-the-op loop this borrows from):

```go
package tui

import (
	"os/exec"
	"testing"

	"github.com/gigagit/gg/internal/engine"
)

func tuiBranchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+name).Run() == nil
}

// loadModel builds a Model over dir with panel data loaded.
func loadModel(t *testing.T, repo *gitRepoT) Model {
	t.Helper()
	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	return loaded.(Model)
}

func TestBKeyOpensBranchPopupWithSelectedStartPoint(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	m.sel[panelBranches] = 0

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	if m.branchPopup == nil {
		t.Fatal("b should open the branch popup")
	}
	if m.branchPopup.startPoint != m.branches[0].Name {
		t.Fatalf("startPoint = %q, want selected branch %q", m.branchPopup.startPoint, m.branches[0].Name)
	}
	if m.branchPopup.switchAfter {
		t.Fatal("b must not set switchAfter")
	}
}

func TestBKeyInertOnOtherPanels(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelCommits

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	if m.branchPopup != nil {
		t.Fatal("b must be inert outside the Branches panel")
	}
}

func TestBranchPopupTypeEnterCreatesBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	for _, r := range "feat/new" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if m.branchPopup.name != "feat/new" {
		t.Fatalf("typed name = %q", m.branchPopup.name)
	}

	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.branchPopup != nil {
		t.Fatal("enter should close the popup")
	}
	for i := 0; i < 100 && m.running; i++ {
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if !tuiBranchExists(t, dir, "feat/new") {
		t.Fatal("branch not created")
	}
}

func TestBranchPopupEnterOnEmptyNameDoesNothing(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.branchPopup == nil {
		t.Fatal("enter with an empty name must keep the popup open")
	}
	if m.running {
		t.Fatal("no op may start for an empty name")
	}
}

func TestBranchPopupEscClosesWithoutOp(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.branchPopup != nil || m.running {
		t.Fatal("esc must close the popup without starting an op")
	}
}

func TestShiftBChainsSmartSwitchAfterCreate(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches

	updated, _ := m.Update(keyMsg("B"))
	m = updated.(Model)
	if m.branchPopup == nil || !m.branchPopup.switchAfter {
		t.Fatal("B should open the popup with switchAfter set")
	}
	for _, r := range "feat/go" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)

	// Drive create + the chained SmartSwitch to completion. SmartSwitch on a
	// clean tree needs no decisions.
	for i := 0; i < 200 && (m.running || cmd != nil); i++ {
		if cmd == nil {
			break
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "feat/go\n" {
		t.Fatalf("HEAD = %q, want feat/go (SmartSwitch should have chained)", got)
	}
	if m.pendingSwitchBranch != "" {
		t.Fatal("pendingSwitchBranch must be cleared")
	}
}

func TestBranchPopupSwallowsActionKeys(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("q")) // would quit outside the popup
	m = u.(Model)
	if m.branchPopup == nil {
		t.Fatal("popup must swallow ordinary keys")
	}
	if m.branchPopup.name != "q" {
		t.Fatalf("typed rune should land in the name, got %q", m.branchPopup.name)
	}
}

// engine.SmartSwitch must be reachable for the chain (compile-time reminder).
var _ = engine.SmartSwitch{}
```

Also define the small alias used above. At the top of the same test file (after imports) add:

```go
type gitRepoT = git.Repo
```

and add `"github.com/gigagit/gg/internal/git"` to the imports. (`loadModel` takes `*gitRepoT` so the helper reads cleanly.)

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestBKey|TestBranchPopup|TestShiftB'`
Expected: COMPILE FAIL — `m.branchPopup undefined`.

- [ ] **Step 3: Implement the popup**

Create `internal/tui/branch_popup.go`:

```go
package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

// branchPopup holds the in-flight create-branch dialog.
type branchPopup struct {
	startPoint  string // selected branch the new one is based on
	name        string // typed branch name
	switchAfter bool   // B: smart-switch to the branch after creating it
}

// openBranchPopup builds the popup for the currently-selected branch. Returns
// (model, false) when no branch row is selected.
func (m Model) openBranchPopup(switchAfter bool) (Model, bool) {
	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	m.branchPopup = &branchPopup{startPoint: m.branches[bi].Name, switchAfter: switchAfter}
	return m, true
}

// updateBranchPopupKey handles one key while the popup is open. The popup
// swallows every key; ctrl+c still quits so the user is never trapped.
func (m Model) updateBranchPopupKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	p := m.branchPopup
	switch msg.Type {
	case tea.KeyEsc:
		m.branchPopup = nil
	case tea.KeyEnter:
		if p.name == "" {
			return m, nil
		}
		op := engine.CreateBranch{Name: p.name, StartPoint: p.startPoint}
		if p.switchAfter {
			m.pendingSwitchBranch = p.name
		}
		m.branchPopup = nil
		return m.startOp(op)
	case tea.KeyBackspace, tea.KeyCtrlH:
		if r := []rune(p.name); len(r) > 0 {
			p.name = string(r[:len(r)-1])
		}
	case tea.KeySpace:
		// Branch names cannot contain spaces; the engine would reject it with
		// a clear message, but not inserting one avoids the round trip.
	case tea.KeyRunes:
		p.name += string(msg.Runes)
	}
	return m, nil
}

// renderBranchPopup draws the create-branch dialog.
func (m Model) renderBranchPopup() string {
	p := m.branchPopup
	var b strings.Builder
	title := "Create branch from " + p.startPoint
	if p.switchAfter {
		title = "Create + switch branch from " + p.startPoint
	}
	b.WriteString(title + "\n\n")
	b.WriteString("name: " + p.name + "\n\n")
	b.WriteString("[type] name  [enter] create  [esc] cancel")
	w, _ := m.overlayDims()
	return modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
}
```

- [ ] **Step 4: Wire the Model**

In `internal/tui/model.go`:

**4a.** Add two fields to the `Model` struct, after `pendingSwitch bool` (line ~38):

```go
	branchPopup         *branchPopup
	pendingSwitchBranch string // branch to SmartSwitch to after a successful op (B = create-and-switch)
```

**4b.** Add the routing entry after the `m.settings != nil` block (line ~135-137), keeping precedence modal → worktree popup → repo popup → settings → branch popup → filter:

```go
		if m.branchPopup != nil {
			return m.updateBranchPopupKey(msg)
		}
```

**4c.** Add the key cases next to the existing `"w"` case (line ~192):

```go
		case "b":
			if !m.running && !m.loading && m.focus == panelBranches {
				if mm, ok := m.openBranchPopup(false); ok {
					return mm, nil
				}
			}
		case "B":
			if !m.running && !m.loading && m.focus == panelBranches {
				if mm, ok := m.openBranchPopup(true); ok {
					return mm, nil
				}
			}
```

**4d.** Chain the switch in the `opFinishedMsg` case (currently model.go:286-308). Replace the whole case body with:

```go
	case opFinishedMsg:
		m.running = false
		m.opMsgs = nil
		switchTo := ""
		chainSwitch := ""
		if msg.err != nil {
			m.statusMsg = "error: " + msg.err.Error()
		} else {
			if msg.res.Summary != "" {
				m.statusMsg = msg.res.Summary
			}
			for _, name := range m.pendingSeqBump {
				_, _ = config.BumpSeq(m.gitCommonDir, name)
			}
			if m.pendingSwitch && msg.res.Path != "" {
				switchTo = msg.res.Path
			}
			chainSwitch = m.pendingSwitchBranch
		}
		m.pendingSeqBump = nil
		m.pendingSwitch = false
		m.pendingSwitchBranch = "" // cleared before the chained op starts, so it cannot re-fire
		if switchTo != "" {
			return m.reRoot(switchTo)
		}
		if chainSwitch != "" {
			return m.startOp(engine.SmartSwitch{Branch: chainSwitch})
		}
		return m, m.loadCmd()
```

- [ ] **Step 5: Wire the view**

In `internal/tui/view.go`:

**5a.** In `render()` add a branch-popup case after the `m.settings != nil` block (line ~82-85):

```go
	if m.branchPopup != nil {
		w, h := m.overlayDims()
		return overlayCenter(bg, m.renderBranchPopup(), w, h)
	}
```

**5b.** In the footer (line ~121) add `[b]ranch` after `[s]witch`:

```go
	footer := truncate("[p]ull [P]ush [s]witch [b]ranch [S]tash [u]ndo [w]orktree [d]elete [o]rder [/]filter [R]epo [,] settings  •  [tab] focus  [r] reload  [q] quit", g.w)
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS — the new tests and all existing TUI tests (routing untouched for other popups).

- [ ] **Step 7: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/tui/branch_popup.go internal/tui/branch_popup_test.go internal/tui/model.go internal/tui/view.go
git commit -m "feat(tui): create-branch popup on b/B with SmartSwitch chaining

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: TUI — `d` deletes the selected branch on the Branches panel

**Files:**
- Modify: `internal/tui/model.go` (the `"d"` case, line ~198-204)
- Test: `internal/tui/branch_delete_test.go`

`d` becomes panel-dispatched: Worktrees → `RemoveWorktree` (unchanged), Branches → `DeleteBranch`. Decisions arrive as ordinary modals.

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/branch_delete_test.go`:

```go
package tui

import (
	"testing"
)

// TestDKeyDeletesBranchThroughConfirmModal presses d on a branch row, answers
// the delete-branch confirm with "delete", and asserts the branch is gone.
func TestDKeyDeletesBranchThroughConfirmModal(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/doomed")

	m := loadModel(t, repo)
	m.focus = panelBranches
	// Select the feat/doomed row (panel order may vary with sort modes).
	found := false
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/doomed" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("feat/doomed not in the panel: %+v", m.branches)
	}

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)

	answered := false
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			if m.modal.req.ID != "delete-branch" {
				t.Fatalf("unexpected decision %q", m.modal.req.ID)
			}
			// selection 0 == "delete"
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
	if !answered {
		t.Fatal("expected a delete-branch confirm modal")
	}
	if tuiBranchExists(t, dir, "feat/doomed") {
		t.Fatal("branch still exists after confirmed delete")
	}
}

// TestDKeyOnBranchesEscKeepsBranch answers the confirm with esc (= abort).
func TestDKeyOnBranchesEscKeepsBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feat/safe")

	m := loadModel(t, repo)
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feat/safe" {
			break
		}
	}

	updated, cmd := m.Update(keyMsg("d"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		if m.modal != nil {
			u, _ := m.Update(keyMsg("esc")) // abortOption -> "abort"
			m = u.(Model)
			continue
		}
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if !tuiBranchExists(t, dir, "feat/safe") {
		t.Fatal("esc on the confirm must keep the branch")
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run TestDKey`
Expected: `TestDKeyDeletesBranchThroughConfirmModal` FAILS — `d` is currently inert on the Branches panel, so no op starts and the branch survives ("expected a delete-branch confirm modal"). `TestDeleteKeyRemovesWorktreeThroughModal` (existing) still passes.

- [ ] **Step 3: Panel-dispatch the d key**

In `internal/tui/model.go` replace the `"d"` case (line ~198-204):

```go
		case "d":
			if !m.running && !m.loading && m.focus == panelWorktrees {
				if bi, ok := m.backingIndex(panelWorktrees); ok {
					wt := m.worktrees[bi]
					return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
				}
			}
```

with:

```go
		case "d":
			if !m.running && !m.loading {
				switch m.focus {
				case panelWorktrees:
					if bi, ok := m.backingIndex(panelWorktrees); ok {
						wt := m.worktrees[bi]
						return m.startOp(engine.RemoveWorktree{Path: wt.Path, Branch: wt.Branch})
					}
				case panelBranches:
					if bi, ok := m.backingIndex(panelBranches); ok {
						return m.startOp(engine.DeleteBranch{Name: m.branches[bi].Name})
					}
				}
			}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS — both new tests plus the existing worktree-delete tests (the Worktrees arm is byte-identical behavior).

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/tui/model.go internal/tui/branch_delete_test.go
git commit -m "feat(tui): d on the Branches panel deletes the selected branch

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 6: CLI — `gg branch create|delete`

**Files:**
- Create: `internal/cli/branch.go`
- Modify: `internal/cli/cli.go` (dispatch switch line ~48-72, `commands` map line ~75-79)
- Modify: `cmd/gg/main.go:42` (help string)
- Test: `internal/cli/branch_test.go`

Mirrors `gg worktree` (`internal/cli/worktree.go`): a sub-dispatcher, flag-policy decider, `finish` for exit codes. Flags precede positionals (the `worktree remove` convention), so the surface is `gg branch delete [--force] <name>`. The `delete-branch` confirm is always pre-answered — typing the command is the confirmation.

- [ ] **Step 1: Write the failing tests**

Create `internal/cli/branch_test.go` (`newRepoDir(t) string` exists in `core_test.go`):

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

// gitRun runs git in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func cliBranchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+name).Run() == nil
}

func TestBranchCreate(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create", "feat/x"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !cliBranchExists(t, dir, "feat/x") {
		t.Fatal("branch not created")
	}
	if !strings.Contains(out.String(), "created branch feat/x") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestBranchCreateFromStartPoint(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "base")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "advance")

	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create", "feat/y", "base"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	tip, _ := exec.Command("git", "-C", dir, "rev-parse", "feat/y").Output()
	baseTip, _ := exec.Command("git", "-C", dir, "rev-parse", "base").Output()
	if string(tip) != string(baseTip) {
		t.Fatal("feat/y must point at base's tip")
	}
}

func TestBranchCreateUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("no-args exit = %d, want 2", code)
	}
	if code := Run(dir, []string{"branch"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("bare `gg branch` exit = %d, want 2", code)
	}
	if code := Run(dir, []string{"branch", "bogus"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("unknown sub exit = %d, want 2", code)
	}
}

func TestBranchDeleteMergedNeedsNoPrompt(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "merged")
	var out, errb bytes.Buffer
	// Non-interactive stdin: must still succeed — the confirm is pre-answered.
	if code := Run(dir, []string{"branch", "delete", "merged"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if cliBranchExists(t, dir, "merged") {
		t.Fatal("branch still exists")
	}
}

// unmerged creates a branch with a commit main doesn't have.
func unmerged(t *testing.T, dir, name string) {
	t.Helper()
	gitRun(t, dir, "switch", "-c", name)
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "unmerged work")
	gitRun(t, dir, "switch", "main")
}

func TestBranchDeleteUnmergedNonTTYExits1WithOptions(t *testing.T) {
	dir := newRepoDir(t)
	unmerged(t, dir, "risky")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"branch", "delete", "risky"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (unanswered decision)", code)
	}
	if !strings.Contains(errb.String(), "branch-unmerged") || !strings.Contains(errb.String(), "force-delete") {
		t.Fatalf("stderr must name the decision and options: %s", errb.String())
	}
	if !cliBranchExists(t, dir, "risky") {
		t.Fatal("branch must survive an unanswered decision")
	}
}

func TestBranchDeleteUnmergedForce(t *testing.T) {
	dir := newRepoDir(t)
	unmerged(t, dir, "risky2")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "delete", "--force", "risky2"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if cliBranchExists(t, dir, "risky2") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestBranchDeleteCurrentBranchFails(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "delete", "main"}, strings.NewReader(""), &out, &errb, ""); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "checked-out branch") {
		t.Fatalf("stderr: %s", errb.String())
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestBranch`
Expected: FAIL — `unknown command "branch"` makes every test exit 2 (and the create/delete assertions fail).

- [ ] **Step 3: Implement the command**

Create `internal/cli/branch.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/gigagit/gg/internal/engine"
)

// cmdBranch dispatches `gg branch <sub>`.
func cmdBranch(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg branch <create|delete> [args]")
		return 2
	}
	switch args[0] {
	case "create":
		return cmdBranchCreate(repo, args[1:], stdout, stderr)
	case "delete":
		return cmdBranchDelete(repo, args[1:], stdin, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "branch: unknown subcommand %q (use create or delete)\n", args[0])
		return 2
	}
}

// cmdBranchCreate implements `gg branch create <name> [<start-point>]`.
func cmdBranchCreate(repo *repoT, args []string, stdout, stderr io.Writer) int {
	if len(args) < 1 || len(args) > 2 || args[0] == "" {
		fmt.Fprintln(stderr, "usage: gg branch create <name> [<start-point>]")
		return 2
	}
	start := ""
	if len(args) == 2 {
		start = args[1]
	}
	res, err := runOperation(context.Background(), repo,
		engine.CreateBranch{Name: args[0], StartPoint: start}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}

// cmdBranchDelete implements `gg branch delete [--force] <name>`. Flags must
// precede the name. The delete-branch confirm is always pre-answered — typing
// the command is the confirmation; --force also pre-answers the unmerged fork.
func cmdBranchDelete(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("branch delete", flag.ContinueOnError)
	fs.SetOutput(stderr)
	force := fs.Bool("force", false, "delete even when not fully merged (git branch -D)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 || fs.Arg(0) == "" {
		fmt.Fprintln(stderr, "usage: gg branch delete [--force] <name>")
		return 2
	}
	policy := map[string]string{"delete-branch": "delete"}
	if *force {
		policy["branch-unmerged"] = "force-delete"
	}
	dec := cliDecider{policy: policy, in: stdin, out: stderr, interactive: stdinIsTerminal()}
	res, err := runOperation(context.Background(), repo,
		engine.DeleteBranch{Name: fs.Arg(0)}, dec, stderr)
	return finish(res, err, stdout, stderr)
}
```

- [ ] **Step 4: Register the command**

In `internal/cli/cli.go`:

**4a.** Add a case to the dispatch switch in `Run`, after `case "switch":` (line ~57-58):

```go
	case "branch":
		return cmdBranch(repo, rest, stdin, stdout, stderr)
```

**4b.** Add `"branch": true` to the `commands` map (line ~75-79):

```go
var commands = map[string]bool{
	"status": true, "commit": true, "pull": true, "push": true,
	"switch": true, "branch": true, "stash": true, "undo": true, "worktree": true,
	"inspect": true, "repo": true, "init": true,
}
```

**4c.** In `cmd/gg/main.go:42` add `branch` to the help line:

```go
		fmt.Fprintln(os.Stderr, "commands: status commit pull push switch branch stash undo worktree repo init inspect (run `gg` with no arguments for the TUI)")
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS (8 new tests + existing).

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/cli/branch.go internal/cli/branch_test.go internal/cli/cli.go cmd/gg/main.go
git commit -m "feat(cli): gg branch create|delete

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 7: Agent skill v2 + docs

**Files:**
- Modify: `internal/agentskill/using-gg.md` (command list)
- Modify: `internal/agentskill/agentskill.go:19` (`Version = 2`)
- Test: `internal/agentskill/agentskill_test.go` (assert the new command is documented)
- Regenerate: `.claude/skills/using-gg/SKILL.md` (via the freshly built binary)
- Modify: `CHANGELOG.md`, `README.md`

This is the first exercise of the update convention: CLI surface changed → update the embedded skill, bump the version, refresh installed copies. **The skill must never document a flag the CLI doesn't register** — every flag below was verified against `internal/cli/branch.go` in Task 6 (`--force` only; flags precede the name).

- [ ] **Step 1: Write the failing test**

In `internal/agentskill/agentskill_test.go`, find the test that checks `Body()` contains the command names (it iterates a list of expected substrings around line 20-35) and add `"gg branch create"` and `"gg branch delete"` to that expected-substrings list.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/agentskill/`
Expected: FAIL — body does not yet mention `gg branch create`.

- [ ] **Step 3: Update the embedded skill + version**

In `internal/agentskill/using-gg.md`, insert after the `gg switch <branch>` bullet (line ~17-18):

```markdown
- `gg branch create <name> [<start-point>]` — create a branch (no switch);
  start point defaults to HEAD.
- `gg branch delete [--force] <name>` — delete a branch; refuses the
  checked-out branch and branches checked out in a worktree. An unmerged
  branch is a `branch-unmerged` fork (`force-delete`/`keep`): pass `--force`
  to pre-answer it.
```

In the "rule that matters for agents" section, extend the pre-answer bullet (line ~37-38) to mention the new flag:

```markdown
- Pre-answer decisions with the matching flag: `--on-conflict` for pull
  divergence; `--with-branch` / `--force` for worktree removal; `--force`
  for unmerged branch deletion.
```

In `internal/agentskill/agentskill.go:19` change:

```go
const Version = 2
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/agentskill/ ./internal/agentinit/`
Expected: PASS — the agentskill tests assert against `Version` dynamically (`fmt.Sprintf("gg:using-gg:v%d", Version)`), so the bump propagates; agentinit fixtures stamp whatever the current version is.

- [ ] **Step 5: Regenerate the committed dogfood skill**

```bash
go build -o ./gg ./cmd/gg
./gg init --update
git diff --stat .claude/skills/using-gg/SKILL.md   # must show changes (v1 → v2 + new bullets)
```

Expected: `./gg init --update` reports refreshing `.claude/skills/using-gg/SKILL.md` (and any other targets installed on this machine — only the repo-local one is committed).

- [ ] **Step 6: Update CHANGELOG.md and README.md**

`CHANGELOG.md` — add under `[Unreleased]` (create the subsection if absent, Keep-a-Changelog style):

```markdown
### Added
- Branch management: `b` (create-branch popup) / `B` (create and smart-switch)
  / `d` (delete with confirm + unmerged force fork) on the TUI Branches panel,
  and `gg branch create <name> [<start>]` / `gg branch delete [--force] <name>`
  in the CLI. The embedded using-gg agent skill is now v2.
```

`README.md` — in the TUI key table (line ~39-44) add after the `s` row and extend the `d` row:

```markdown
| `b` | create a branch off the selected one (popup); `B` create **and** switch to it |
| `d` | on the Worktrees panel: delete the selected worktree; on the Branches panel: delete the selected branch |
```

(Replace the existing `d` row — `| `d` | on the Worktrees panel: delete the selected worktree |` — with the extended one.)

In the CLI command block (line ~66-75) add after `gg switch <branch>`:

```
gg branch create <name> [<start-point>]
gg branch delete [--force] <name>
```

- [ ] **Step 7: Full gate + commit**

```bash
go test ./... -race && go vet ./... && gofmt -l internal cmd
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go internal/agentskill/agentskill_test.go .claude/skills/using-gg/SKILL.md CHANGELOG.md README.md
git commit -m "docs(agentskill): document gg branch, bump skill to v2

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `go test ./... -race` — full suite, race-clean.
- [ ] `go vet ./... && gofmt -l internal cmd` — clean.
- [ ] Manual smoke: `go build -o ./gg ./cmd/gg`, then in a scratch repo: `gg branch create t1 && gg branch delete t1` round-trips; `./gg` TUI shows `[b]ranch` in the footer.
- [ ] Dispatch the final code reviewer over the whole branch diff (`git diff main...HEAD`).
- [ ] Present finishing-a-development-branch options (the user merges manually).
