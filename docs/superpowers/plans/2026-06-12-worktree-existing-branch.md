# Worktree for an Existing Branch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `w` creates a worktree that checks out the selected existing branch; `W` keeps today's behavior (new branch from templates + worktree); CLI gains `gg worktree add --branch <name>`.

**Architecture:** New git verb `AddWorktreeForBranch` (`git worktree add <path> <branch>`), new engine op `CreateWorktreeForBranch{Branch, Path}` with existence/checked-out guards (a path-resolution helper is extracted from `CreateWorktree` and shared), an `existing bool` mode on the one `worktreePopup`, and a `--branch` flag on the CLI `add`. Agent skill bumps to v3.

**Tech Stack:** Go 1.26, Bubble Tea, system git via `internal/gitexec`. Spec: `docs/superpowers/specs/2026-06-12-worktree-existing-branch-design.md`.

**Working branch:** `feat/worktree-existing` off `main`.

**Quality gates per task:** `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` (empty output). `go test ./... -race` once at the end. Commit messages end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`.

---

### Task 1: git verb — `AddWorktreeForBranch`

**Files:**
- Modify: `internal/git/worktree.go` (insert after `AddWorktree`, ~line 41)
- Test: `internal/git/worktree_verbs_test.go`

- [ ] **Step 1: Write the failing test**

Append to `internal/git/worktree_verbs_test.go` (helpers `newTestRepo` and the imports `context`, `os/exec`, `path/filepath`, `strings`, `testing` are already present in the file):

```go
func TestAddWorktreeForBranchChecksOutExisting(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	if err := repo.CreateBranch(context.Background(), "existing/x", ""); err != nil {
		t.Fatalf("create branch: %v", err)
	}

	wtPath := filepath.Join(filepath.Dir(dir), "wt-existing")
	if err := repo.AddWorktreeForBranch(context.Background(), wtPath, "existing/x", nil); err != nil {
		t.Fatalf("AddWorktreeForBranch: %v", err)
	}

	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in new worktree: %v", err)
	}
	if got := strings.TrimSpace(string(out)); got != "existing/x" {
		t.Fatalf("worktree HEAD = %q, want existing/x", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run TestAddWorktreeForBranch`
Expected: COMPILE FAIL — `repo.AddWorktreeForBranch undefined`.

- [ ] **Step 3: Implement the verb**

In `internal/git/worktree.go`, insert after the existing `AddWorktree` function:

```go
// AddWorktreeForBranch creates a linked worktree at path that checks out the
// EXISTING branch (`git worktree add <path> <branch>`). Output lines are
// forwarded to onLine (nil is allowed); the checkout is cancellable via ctx.
// Callers must ensure branch exists locally — git would otherwise DWIM a
// remote-tracking branch into existence.
func (r *Repo) AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("add", path, branch).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree add", argv, onLine)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/`
Expected: PASS.

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd   # gofmt must print nothing
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): AddWorktreeForBranch verb (checkout existing branch)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 2: engine — path helper extraction + `CreateWorktreeForBranch` op

**Files:**
- Modify: `internal/engine/create_worktree.go` (extract `resolveNewWorktreePath`)
- Create: `internal/engine/create_worktree_for_branch.go`
- Test: `internal/engine/create_worktree_for_branch_test.go`

Package test helpers to reuse (do not redefine): `newRepo(t) (string, *git.Repo)`, `gitIn(t, dir, args...)`, `addWorktree(t, dir, branch, name)`, `drain(ch)`.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/create_worktree_for_branch_test.go`:

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

func wtHead(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestCreateWorktreeForBranchHappyPath(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "existing/y")

	wt := filepath.Join(filepath.Dir(dir), "wt-for-y")
	ch := make(chan Event, 32)
	res, err := CreateWorktreeForBranch{Branch: "existing/y", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateWorktreeForBranch: %v", err)
	}
	if !res.Changed || res.Path == "" {
		t.Fatalf("result = %+v, want Changed with Path set", res)
	}
	if got := wtHead(t, res.Path); got != "existing/y" {
		t.Fatalf("worktree HEAD = %q, want existing/y", got)
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

func TestCreateWorktreeForBranchRelativePathResolvesAgainstRoot(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "rel/z")

	res, err := CreateWorktreeForBranch{Branch: "rel/z", Path: "../wt-rel-z"}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktreeForBranch: %v", err)
	}
	want := filepath.Clean(filepath.Join(dir, "..", "wt-rel-z"))
	gotR, _ := filepath.EvalSymlinks(res.Path)
	wantR, _ := filepath.EvalSymlinks(want)
	if gotR != wantR {
		t.Fatalf("res.Path = %q, want %q", res.Path, want)
	}
}

func TestCreateWorktreeForBranchMissingBranchGuard(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-none")
	_, err := CreateWorktreeForBranch{Branch: "nope", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "no local branch") {
		t.Fatalf("want missing-branch guard, got %v", err)
	}
	if _, statErr := os.Stat(wt); !os.IsNotExist(statErr) {
		t.Fatal("no worktree dir may be created for a missing branch")
	}
}

func TestCreateWorktreeForBranchAlreadyCheckedOutGuard(t *testing.T) {
	dir, repo := newRepo(t)
	// "main" is checked out in the primary worktree.
	wt := filepath.Join(filepath.Dir(dir), "wt-dup-main")
	_, err := CreateWorktreeForBranch{Branch: "main", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("want already-checked-out guard, got %v", err)
	}

	// Same for a branch held by a linked worktree.
	addWorktree(t, dir, "feature/held", "wt-held")
	wt2 := filepath.Join(filepath.Dir(dir), "wt-held-2")
	_, err = CreateWorktreeForBranch{Branch: "feature/held", Path: wt2}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "already checked out") {
		t.Fatalf("want already-checked-out guard for linked worktree, got %v", err)
	}
}

func TestCreateWorktreeForBranchExistingPathGuard(t *testing.T) {
	dir, repo := newRepo(t)
	gitIn(t, dir, "branch", "pathy")
	taken := filepath.Join(filepath.Dir(dir), "wt-taken")
	if err := os.MkdirAll(taken, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err := CreateWorktreeForBranch{Branch: "pathy", Path: taken}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "path already exists") {
		t.Fatalf("want existing-path guard, got %v", err)
	}
}

func TestCreateWorktreeForBranchRequiresFields(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateWorktreeForBranch{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("want required-fields error, got %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestCreateWorktreeForBranch`
Expected: COMPILE FAIL — `undefined: CreateWorktreeForBranch`.

- [ ] **Step 3: Extract the shared path helper**

In `internal/engine/create_worktree.go`, replace the inline path-resolution block of `CreateWorktree.Run` (currently lines 31-45):

```go
	// Resolve a relative path against the repo root. The runner's working
	// directory may be a subdirectory of the repo, so a relative path must not
	// be left for git to interpret against its own cwd.
	abs := op.Path
	if !filepath.IsAbs(abs) {
		top, err := deps.Repo.TopLevel(ctx)
		if err != nil {
			return Result{}, err
		}
		abs = filepath.Clean(filepath.Join(top, op.Path))
	}

	if _, err := os.Stat(abs); err == nil {
		return Result{}, fmt.Errorf("create worktree: path already exists: %s", abs)
	}
```

with:

```go
	abs, err := resolveNewWorktreePath(ctx, deps, op.Path)
	if err != nil {
		return Result{}, err
	}
```

and append the helper at the bottom of the same file (above the `var _ Operation` line):

```go
// resolveNewWorktreePath resolves a possibly-relative worktree path against
// the repo root and refuses a path that already exists. The runner's working
// directory may be a subdirectory of the repo, so a relative path must not be
// left for git to interpret against its own cwd.
func resolveNewWorktreePath(ctx context.Context, deps OpDeps, path string) (string, error) {
	abs := path
	if !filepath.IsAbs(abs) {
		top, err := deps.Repo.TopLevel(ctx)
		if err != nil {
			return "", err
		}
		abs = filepath.Clean(filepath.Join(top, path))
	}
	if _, err := os.Stat(abs); err == nil {
		return "", fmt.Errorf("create worktree: path already exists: %s", abs)
	}
	return abs, nil
}
```

(The `os`/`filepath` imports in create_worktree.go stay — only the helper uses them now. If the compiler flags unused imports after the move, adjust accordingly.)

- [ ] **Step 4: Implement the operation**

Create `internal/engine/create_worktree_for_branch.go`:

```go
package engine

import (
	"context"
	"fmt"
)

// CreateWorktreeForBranch creates a linked worktree that checks out an
// EXISTING branch (no new branch). A relative Path resolves against the
// repository root.
type CreateWorktreeForBranch struct {
	Branch string
	Path   string
}

func (op CreateWorktreeForBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Branch == "" || op.Path == "" {
		return Result{}, fmt.Errorf("create worktree: Branch and Path are required")
	}

	// Guard: the branch must exist locally. Checking up front both gives a
	// clear message and forecloses git's remote-DWIM (a missing local branch
	// with a matching origin/<branch> would be silently created).
	branches, err := deps.Repo.Branches(ctx)
	if err != nil {
		return Result{}, err
	}
	found := false
	for _, b := range branches {
		if b.Name == op.Branch {
			found = true
			break
		}
	}
	if !found {
		return Result{}, fmt.Errorf("create worktree: no local branch %q", op.Branch)
	}

	// Guard: a branch can only be checked out in one worktree.
	wt, err := deps.Repo.WorktreeForBranch(ctx, op.Branch)
	if err != nil {
		return Result{}, err
	}
	if wt != nil {
		return Result{}, fmt.Errorf("create worktree: branch %s is already checked out in worktree %s", op.Branch, wt.Path)
	}

	abs, err := resolveNewWorktreePath(ctx, deps, op.Path)
	if err != nil {
		return Result{}, err
	}

	deps.emit(ctx, Progress{Step: "creating worktree", Detail: op.Branch + " → " + abs})
	if err := deps.Repo.AddWorktreeForBranch(ctx, abs, op.Branch, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	}); err != nil {
		return Result{}, fmt.Errorf("create worktree: %w", err)
	}

	res := Result{Summary: "worktree created: " + abs, Changed: true, Path: abs}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = CreateWorktreeForBranch{}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/`
Expected: PASS — the 6 new tests AND all existing `CreateWorktree` tests (the helper extraction must be behavior-identical).

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/engine/create_worktree.go internal/engine/create_worktree_for_branch.go internal/engine/create_worktree_for_branch_test.go
git commit -m "feat(engine): CreateWorktreeForBranch operation

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 3: TUI — `existing` popup mode; `w` = existing, `W` = new branch

**Files:**
- Modify: `internal/tui/worktree_popup.go` (mode field, fixed branch, labels/seqs, `e` inert, title/hints, `createOp`)
- Modify: `internal/tui/model.go` (the `"w"` case gains a sibling `"W"`; both pass the mode)
- Modify: `internal/tui/worktree_popup_test.go` (retarget popup-OPEN presses to `W` — they exercise the new-branch mode)
- Create: `internal/tui/worktree_existing_test.go`

**The retarget rule (do this precisely):** in `worktree_popup_test.go`, every `m.Update(keyMsg("w"))` that OPENS the popup (i.e., the first `keyMsg("w")` press in the test, while `m.popup` is still nil) becomes `m.Update(keyMsg("W"))`. Presses AFTER the popup is open are popup-internal actions and stay unchanged. The opening presses are at lines 30, 48, 62, 72, 116, 127, 138, 176, 194, 211, 262, 279, 296, 339, 371, 441; the internal ones (do NOT touch) are at 196 (`// create`), 216 (`// attempt create`), 281 (`// create AND switch`), 298 (`// plain create`), 443 (`// create AND switch`). Also rename `TestOpenPopupOnW` to `TestOpenNewBranchPopupOnShiftW`.

- [ ] **Step 1: Retarget the existing tests and add the failing new ones**

First apply the retarget rule above to `worktree_popup_test.go`.

Then create `internal/tui/worktree_existing_test.go`:

```go
package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/engine"
)

func TestWOpensExistingModePopup(t *testing.T) {
	m := modelWithConfig(t, "b/from-<parent-branch>", "../<repo>.worktrees/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if mm.popup == nil {
		t.Fatal("w should open the worktree popup")
	}
	if !mm.popup.existing {
		t.Fatal("w must open the popup in existing-branch mode")
	}
	// The branch is fixed to the selection — not resolved from the template.
	if mm.popup.previewBranch != mm.popup.startPoint {
		t.Fatalf("previewBranch = %q, want the fixed selection %q", mm.popup.previewBranch, mm.popup.startPoint)
	}
	if strings.HasPrefix(mm.popup.previewBranch, "b/from-") {
		t.Fatal("existing mode must not run the branch template")
	}
	// Path resolves with <branch> = the fixed branch.
	if !strings.Contains(mm.popup.previewPath, mm.popup.startPoint) {
		t.Fatalf("previewPath = %q, want it to contain %q", mm.popup.previewPath, mm.popup.startPoint)
	}
}

func TestExistingModeIgnoresBranchTemplateUserFields(t *testing.T) {
	// The branch template's <user:...> must NOT be prompted in existing mode;
	// only the path template's fields are.
	m := modelWithConfig(t, "<user:who>/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	mm := updated.(Model)
	if len(mm.popup.labels) != 0 {
		t.Fatalf("labels = %v, want none (branch template bypassed, path has no fields)", mm.popup.labels)
	}
	if mm.popup.state != stAction {
		t.Fatalf("state = %v, want stAction with no fields", mm.popup.state)
	}
}

func TestExistingModeEditKeyInert(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	updated, _ = m.Update(keyMsg("e"))
	m = updated.(Model)
	if m.popup.state == stEdit {
		t.Fatal("e must be inert in existing mode — the branch is the point")
	}
}

func TestExistingModeCreateOpAndSeqs(t *testing.T) {
	m := modelWithConfig(t, "issue/<seq:issue>", "../<repo>.worktrees/<seq:wt>-<branch>")
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)

	op, ok := m.popup.createOp().(engine.CreateWorktreeForBranch)
	if !ok {
		t.Fatalf("createOp = %T, want engine.CreateWorktreeForBranch", m.popup.createOp())
	}
	if op.Branch != m.popup.startPoint || op.Path != m.popup.previewPath {
		t.Fatalf("op {%q,%q} != {%q,%q}", op.Branch, op.Path, m.popup.startPoint, m.popup.previewPath)
	}
	// Only the PATH template's <seq> names are consumed in existing mode.
	if got := m.popup.consumedSeqNames(); len(got) != 1 || got[0] != "wt" {
		t.Fatalf("consumedSeqNames = %v, want [wt]", got)
	}
}

func TestExistingModeEndToEnd(t *testing.T) {
	dir, repo := newRepoDir(t)
	runGit(t, dir, "branch", "feature/have")

	m := New(repo)
	loaded, _ := m.Update(m.loadCmd()())
	m = loaded.(Model)
	m.cfg.Worktree.PathTemplate = "../<repo>.worktrees/<branch>"
	m.focus = panelBranches
	for vi := 0; vi < m.panelLen(panelBranches); vi++ {
		m.sel[panelBranches] = vi
		if bi, ok := m.backingIndex(panelBranches); ok && m.branches[bi].Name == "feature/have" {
			break
		}
	}

	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	if m.popup == nil || !m.popup.existing || m.popup.startPoint != "feature/have" {
		t.Fatalf("popup not in existing mode for feature/have: %+v", m.popup)
	}
	updated, cmd := m.Update(keyMsg("enter")) // create
	m = updated.(Model)
	m = driveOp(t, m, cmd)

	// The created worktree has the existing branch checked out.
	wt := filepath.Join(filepath.Dir(dir), filepath.Base(dir)+".worktrees", "feature-have")
	head := wtHeadTui(t, wt)
	if head != "feature/have" {
		t.Fatalf("worktree HEAD = %q, want feature/have", head)
	}
}

func TestExistingModeRenderTitleAndHints(t *testing.T) {
	m := modelWithConfig(t, "b/x", "wt/<branch>")
	m.width, m.height = 80, 24
	updated, _ := m.Update(keyMsg("w"))
	m = updated.(Model)
	out := m.View()
	if !strings.Contains(out, "Create worktree for") {
		t.Errorf("existing-mode title missing:\n%s", out)
	}
	if strings.Contains(out, "edit name") {
		t.Errorf("existing mode must not offer the edit-name hint:\n%s", out)
	}
}
```

And add this helper to the same new file:

```go
func wtHeadTui(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}
```

(add `"os/exec"` to the file's imports; if a same-purpose helper already exists in the tui package, reuse it instead.)

NOTE on the path assertion in `TestExistingModeEndToEnd`: the path template sanitizes `<branch>` (`feature/have` → `feature-have` in paths — the same sanitization `TestPopupEditMode` exercises via `my/b` → `my-b`). If the sanitization differs, derive the expected path from `m.popup.previewPath` captured before `enter` instead of hardcoding it.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestWOpens|TestExistingMode|TestOpenNewBranchPopupOnShiftW'`
Expected: COMPILE FAIL — `mm.popup.existing undefined`.

- [ ] **Step 3: Implement the popup mode**

In `internal/tui/worktree_popup.go`:

**3a.** Add the mode field to the struct (after `repoName string`):

```go
	existing bool // checkout the startPoint branch itself; no new branch
```

**3b.** Add a fixed-branch resolver method and use it in `recompute` — replace:

```go
func (p *worktreePopup) recompute() {
	fixed := p.branchOverride
	if p.state == stEdit {
		fixed = p.editBuf
	}
```

with:

```go
// fixedBranch returns the verbatim branch when one is fixed: the selection in
// existing mode, the live buffer while editing, or a confirmed hand-edit.
func (p *worktreePopup) fixedBranch() string {
	if p.existing {
		return p.startPoint
	}
	if p.state == stEdit {
		return p.editBuf
	}
	return p.branchOverride
}

func (p *worktreePopup) recompute() {
	fixed := p.fixedBranch()
```

**3c.** Make `openWorktreePopup` take the mode and bypass the branch template in existing mode — replace the signature and the template-derived parts:

```go
// openWorktreePopup builds a popup for the currently-selected branch. In
// existing mode the popup checks out that branch itself (no new branch): the
// branch template is bypassed and only the path template's fields/counters
// apply. Returns (model, false) if there is no branch to act on.
func (m Model) openWorktreePopup(existing bool) (Model, bool) {
	if len(m.branches) == 0 {
		return m, false
	}
	bt := m.cfg.Worktree.DefaultBranchTemplate
	pt := m.cfg.Worktree.PathTemplate

	tm := worktree.Templates{Branch: bt, Path: pt}
	if existing {
		tm = worktree.Templates{Path: pt} // branch template bypassed entirely
	}
	labels := tm.Labels()
	seqNames := tm.SeqNames()

	bi, ok := m.backingIndex(panelBranches)
	if !ok {
		return m, false
	}
	p := &worktreePopup{
		startPoint: m.branches[bi].Name,
		existing:   existing,
		branchTmpl: bt,
		pathTmpl:   pt,
		repoName:   worktree.RepoName(m.currentWorktree),
		labels:     labels,
		inputs:     map[string]string{},
		seqNames:   seqNames,
		seqs:       worktree.PeekSeqs(m.gitCommonDir, seqNames),
		seed:       rand.Uint64(),
		now:        time.Now(),
	}
	for _, l := range labels {
		p.inputs[l] = ""
	}
	if len(labels) > 0 {
		p.state = stInput
	} else {
		p.state = stAction
	}
	p.recompute()
	m.popup = p
	return m, true
}
```

**3d.** Make `e` inert in existing mode — in `updatePopupKey`'s `stAction` arm, change:

```go
		case "e":
			p.editBuf = p.previewBranch
			p.state = stEdit
			p.recompute()
```

to:

```go
		case "e":
			if p.existing {
				return m, nil // the branch IS the point of existing mode
			}
			p.editBuf = p.previewBranch
			p.state = stEdit
			p.recompute()
```

**3e.** `consumedSeqNames` — existing mode consumes only the path template's counters (same arm as a hand-edit):

```go
func (p *worktreePopup) consumedSeqNames() []string {
	if p.existing || p.branchOverride != "" {
		return worktree.Templates{Path: p.pathTmpl}.SeqNames()
	}
	return p.seqNames
}
```

(In existing mode `seqNames` was already built path-only in 3c, so both arms agree — keep the explicit check anyway for clarity.)

**3f.** `createOp` chooses the op by mode — replace:

```go
func (p *worktreePopup) createOp() engine.CreateWorktree {
	return engine.CreateWorktree{
		StartPoint: p.startPoint,
		Branch:     p.previewBranch,
		Path:       p.previewPath,
	}
}
```

with:

```go
// createOp builds the engine operation from the (already-resolved) preview, so
// the worktree that gets created is exactly what the preview showed.
func (p *worktreePopup) createOp() engine.Operation {
	if p.existing {
		return engine.CreateWorktreeForBranch{Branch: p.previewBranch, Path: p.previewPath}
	}
	return engine.CreateWorktree{
		StartPoint: p.startPoint,
		Branch:     p.previewBranch,
		Path:       p.previewPath,
	}
}
```

**Check the knock-on:** `worktree_popup_test.go`'s `TestCreateOpEqualsPreview` calls `m.popup.createOp()` and reads `.Branch/.Path/.StartPoint` — update it to type-assert: `op := m.popup.createOp().(engine.CreateWorktree)` (twice — also at the post-edit check near the end of that test).

**3g.** Title + hints in `renderWorktreePopup` — replace the title line:

```go
	b.WriteString("Create worktree from " + p.startPoint + "\n\n")
```

with:

```go
	title := "Create worktree from " + p.startPoint
	if p.existing {
		title = "Create worktree for " + p.startPoint
	}
	b.WriteString(title + "\n\n")
```

and the `stAction` hint:

```go
	default:
		b.WriteString("[w] create  [W] create & switch  [e] edit name  [esc] cancel")
```

with:

```go
	default:
		if p.existing {
			b.WriteString("[w] create  [W] create & switch  [esc] cancel")
		} else {
			b.WriteString("[w] create  [W] create & switch  [e] edit name  [esc] cancel")
		}
```

- [ ] **Step 4: Wire the keys**

In `internal/tui/model.go`, replace the `"w"` case:

```go
		case "w":
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(); ok {
					return mm, nil
				}
			}
```

with:

```go
		case "w": // worktree for the selected EXISTING branch
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(true); ok {
					return mm, nil
				}
			}
		case "W": // worktree on a NEW branch from the selected one
			if !m.running && !m.loading {
				if mm, ok := m.openWorktreePopup(false); ok {
					return mm, nil
				}
			}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/tui/`
Expected: PASS — the 6 new tests, all retargeted popup tests (now opening with `W`), and everything else.

- [ ] **Step 6: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/tui/worktree_popup.go internal/tui/worktree_popup_test.go internal/tui/worktree_existing_test.go internal/tui/model.go
git commit -m "feat(tui): w creates worktree for existing branch, W for a new one

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 4: CLI — `gg worktree add --branch <name>`

**Files:**
- Modify: `internal/cli/worktree.go` (`cmdWorktreeAdd`)
- Test: `internal/cli/worktree_test.go` (append)

- [ ] **Step 1: Write the failing tests**

Append to `internal/cli/worktree_test.go` (the cli package already has `newRepoDir(t) string` from core_test.go and `gitRun`/`cliBranchExists` from branch_test.go; check the file's existing imports and extend as needed — the tests below need `bytes`, `os/exec`, `path/filepath`, `strings`, `testing`):

```go
func wtHeadCli(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeAddBranchUsesExistingBranch(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "feature/have")
	// Config the path template so the destination is deterministic.
	cfgPath := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(cfgPath, []byte("[worktree]\npath_template = \"../wt-cli-<branch>\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--branch", "feature/have"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	wt := filepath.Join(filepath.Dir(dir), "wt-cli-feature-have")
	if got := wtHeadCli(t, wt); got != "feature/have" {
		t.Fatalf("worktree HEAD = %q, want feature/have", got)
	}
	if !strings.Contains(out.String(), "created worktree feature/have") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestWorktreeAddBranchRejectsStartPoint(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "x")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "add", "--branch", "x", "main"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("exit = %d, want 2 (start-point is meaningless with --branch)", code)
	}
}

func TestWorktreeAddBranchMissingBranchFails(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--branch", "ghost"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no local branch") {
		t.Fatalf("stderr: %s", errb.String())
	}
}
```

NOTE on the expected worktree path: the path template `../wt-cli-<branch>` resolves `<branch>` with path sanitization (`feature/have` → `feature-have`). If the first test's stat fails, print the actual created path (`git -C <dir> worktree list`) and align the assertion with the real sanitization rule rather than changing the template code. Also verify the `.gg.toml` key name for the path template by checking `internal/config` (grep for `path_template` / `PathTemplate` toml tags) — use the actual key.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestWorktreeAddBranch`
Expected: FAIL — `flag provided but not defined: -branch` style failure (exit 2 surfaces) or the start-point being misread; the happy-path test must fail.

- [ ] **Step 3: Implement the flag**

In `internal/cli/worktree.go`, rework `cmdWorktreeAdd` (currently it reads the start point straight from `args[0]`). New shape — flags first, then the two modes share the template/config plumbing:

```go
func cmdWorktreeAdd(repo *repoT, args []string, stdin io.Reader, stdout, stderr io.Writer, cwdFile string) int {
	fs := flag.NewFlagSet("worktree add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	forBranch := fs.String("branch", "", "create the worktree for this existing branch (no new branch)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	args = fs.Args()

	ctxBg := context.Background()

	if *forBranch != "" && len(args) > 0 {
		fmt.Fprintln(stderr, "worktree add: --branch and a start-point are mutually exclusive (the branch is the source)")
		return 2
	}

	// Start point: explicit arg, else the current branch. With --branch the
	// branch itself plays the <parent-branch> role for the path template.
	startPoint := *forBranch
	if startPoint == "" {
		if len(args) > 0 {
			startPoint = args[0]
		}
		if startPoint == "" {
			cur, err := repo.CurrentBranch(ctxBg)
			if err != nil || cur == "" {
				fmt.Fprintln(stderr, "worktree add: cannot determine current branch; pass a start-point")
				return 2
			}
			startPoint = cur
		}
	}

	top, err := repo.TopLevel(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	gitCommonDir, err := repo.GitCommonDir(ctxBg)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))
	if err != nil {
		fmt.Fprintln(stderr, "error: loading config:", err)
		return 1
	}

	tm := worktree.Templates{
		Branch: cfg.Worktree.DefaultBranchTemplate,
		Path:   cfg.Worktree.PathTemplate,
	}
	if *forBranch != "" {
		tm = worktree.Templates{Path: cfg.Worktree.PathTemplate} // branch template bypassed
	}

	// Prompt stdin for each <user:LABEL>. Prompts go to stderr so stdout stays
	// clean for scripting.
	inputs := map[string]string{}
	reader := bufio.NewReader(stdin)
	for _, label := range tm.Labels() {
		fmt.Fprintf(stderr, "%s: ", label)
		line, _ := reader.ReadString('\n')
		inputs[label] = strings.TrimRight(line, "\r\n")
	}

	seqNames := tm.SeqNames()
	ctx := template.Ctx{
		ParentBranch: startPoint,
		Repo:         worktree.RepoName(top),
		Seqs:         worktree.PeekSeqs(gitCommonDir, seqNames),
		Now:          time.Now,
		Rand:         rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())),
	}
	branch, path, err := worktree.Resolve(tm, *forBranch, inputs, ctx)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	var op engine.Operation = engine.CreateWorktree{StartPoint: startPoint, Branch: branch, Path: path}
	if *forBranch != "" {
		op = engine.CreateWorktreeForBranch{Branch: branch, Path: path}
	}
	res, err := runOperation(ctxBg, repo, op, cliDecider{}, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}

	// Consume the counters the templates used, now that creation succeeded.
	for _, name := range seqNames {
		_, _ = config.BumpSeq(gitCommonDir, name)
	}

	fmt.Fprintf(stdout, "✓ created worktree %s at %s\n", branch, res.Path)
	if cwdFile != "" && res.Path != "" {
		_ = os.WriteFile(cwdFile, []byte(res.Path), 0o644)
	}
	return 0
}
```

(This is the existing function body with four insertions: the FlagSet, the mutual-exclusion check, the `tm` bypass, and the op selection — `worktree.Resolve`'s `fixedBranch` parameter goes from the hardcoded `""` to `*forBranch`. Add `"flag"` to the file's imports; everything else is already imported. Keep `seqNames` from the bypassed `tm` so only path counters bump in --branch mode.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS — 3 new tests + every existing worktree test (plain `add` behavior unchanged).

- [ ] **Step 5: Gate + commit**

```bash
go test ./... && go vet ./... && gofmt -l internal cmd
git add internal/cli/worktree.go internal/cli/worktree_test.go
git commit -m "feat(cli): gg worktree add --branch for existing branches

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

### Task 5: Agent skill v3 + docs

**Files:**
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (`Version = 2` → `3`), `internal/agentskill/agentskill_test.go`
- Regenerate: `.claude/skills/using-gg/SKILL.md` (drift-guard test `TestDogfoodSkillCopyInSync` enforces sync)
- Modify: `CHANGELOG.md`, `README.md`

- [ ] **Step 1: Write the failing test**

In `internal/agentskill/agentskill_test.go`, `TestBodyCoversTheCLISurface` has an expected-substrings list — add `"--branch"` to it.

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agentskill/`
Expected: FAIL — body missing `--branch`.

- [ ] **Step 3: Update the embedded skill + version**

In `internal/agentskill/using-gg.md` the worktree bullet currently reads:

```markdown
- `gg worktree list` / `gg worktree add [<start-point>]` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields.
```

Replace it with:

```markdown
- `gg worktree list` / `gg worktree add [<start-point>]` /
  `gg worktree add --branch <name>` /
  `gg worktree remove [--with-branch] [--force] <path>` — linked worktrees;
  `add` resolves branch/path templates from `.gg.toml` and may prompt on stdin
  for `<user:...>` fields; `add --branch` checks out the EXISTING branch in
  the new worktree (no new branch; refuses a branch already checked out).
```

In `internal/agentskill/agentskill.go`, change `const Version = 2` to `const Version = 3`.

- [ ] **Step 4: Regenerate the committed dogfood skill**

```bash
go build -o ./gg ./cmd/gg
./gg init --update
go test ./internal/agentskill/   # TestDogfoodSkillCopyInSync must pass now
```

- [ ] **Step 5: Update CHANGELOG.md and README.md**

`CHANGELOG.md` — add under `[Unreleased]` → `### Added` (above the existing `#### Branch management` subsection, matching its style):

```markdown
#### Worktree for an existing branch
- `w` on the Branches panel now creates a worktree that checks out the
  **selected existing branch** (no new branch); `W` opens the previous
  template-driven popup (new branch + worktree). CLI:
  `gg worktree add --branch <name>`. Embedded using-gg agent skill v3.
```

`README.md`:
1. Replace the `w` key-table row (currently `` | `w` | create a worktree (popup); `W` create **and** switch into it | ``) with:
```markdown
| `w` | create a worktree **for the selected branch** (popup); `W` worktree on a **new** templated branch. Inside the popup: `w`/`enter` create, `W` create **and** switch |
```
2. In the CLI command block, after the `gg worktree add [<start-point>]` line, add:
```
gg worktree add --branch <name>
```

- [ ] **Step 6: Full gate + commit**

```bash
go test ./... -race && go vet ./... && gofmt -l internal cmd
git add internal/agentskill/using-gg.md internal/agentskill/agentskill.go internal/agentskill/agentskill_test.go .claude/skills/using-gg/SKILL.md CHANGELOG.md README.md
git commit -m "docs(agentskill): document worktree add --branch, bump skill to v3

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Final verification (after all tasks)

- [ ] `go test ./... -race && go vet ./... && gofmt -l internal cmd` — clean.
- [ ] Manual smoke: `go build -o ./gg ./cmd/gg`; in a scratch repo with a `.gg.toml` path template: `gg branch create t1 && gg worktree add --branch t1` → worktree HEAD is `t1`; `gg worktree add --branch t1` again fails with "already checked out".
- [ ] Dispatch the final code reviewer over the whole branch diff (`git diff main...HEAD`).
- [ ] Present finishing-a-development-branch options (the user merges manually).
