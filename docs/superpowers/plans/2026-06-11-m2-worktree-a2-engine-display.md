# M2 Worktree A2 — Worktree Engine + Display Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make worktrees visible and creatable through the engine: a `git worktree add` verb, a streamed/cancellable `CreateWorktree` engine operation, a Worktrees panel in the TUI, and a has-worktree marker on branches.

**Architecture:** New thin git verbs in `internal/git/worktree.go` (`TopLevel`, `CheckRefFormatBranch`, `AddWorktree`). A new `engine.CreateWorktree` operation orchestrates them: validate the branch name → absolutize a repo-root-relative path → guard against an existing path → stream `git worktree add`. The TUI loads the worktree list (and the current worktree path) into the Model and renders a third left-hand panel plus a per-branch marker. No new dependencies.

**Tech Stack:** Go 1.26, existing `internal/{git,gitcmd,gitexec,engine,model,tui}`, Bubble Tea + lipgloss.

**Spec:** `docs/superpowers/specs/2026-06-11-worktree-management-design.md` §3 (engine boundary), §6 (default placement), §7 (TUI display — panels + icon), §8 (streamed/cancellable checkout), §10 (error cases). The `w`/`W` popup, create-and-switch, and shell integration are **A3, not A2.**

**Conventions (read before starting):**
- TDD red→green. After each task: `go test ./...`, `go vet ./...`, `gofmt -l internal cmd` clean.
- LF line endings only (`.gitattributes` enforces it; the drive is Windows-mounted — never reintroduce CRLF).
- Commit messages end with a `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>` trailer.
- Plain `fmt.Errorf` (with `%w` to wrap) — matches `internal/git`/`internal/engine`. No custom error types.
- Git verbs follow the existing builder pattern: `gitcmd.New("sub").Arg(...).ToArgv()` then `r.Runner.Run(ctx, "label", argv)` (or `.Stream(...)`). See `internal/git/sync.go`, `internal/git/mutate.go`.
- Engine ops follow `internal/engine/ops_basic.go`: emit a `Progress`, do the work, emit `Done`, return `Result`. Add a `var _ Operation = CreateWorktree{}` compile check.
- Engine tests build a **real** throwaway repo via the existing `newRepo(t)` helper in `internal/engine/ops_basic_test.go` (returns `(dir string, repo *git.Repo)`); event capture uses the existing `drain(ch chan Event)` helper.
- TUI tests use the existing `loadedModel(t)` helper (`internal/tui/nav_test.go`) and `newRepo(t)` (`internal/tui/...`).

---

## File Structure

**`internal/git/worktree.go` (new):** the three worktree-related verbs — `TopLevel`, `CheckRefFormatBranch`, `AddWorktree`. Test: `internal/git/worktree_verbs_test.go` (note: `worktree_parse.go`/`worktree_parse_test.go` already exist for the list parser — do not collide with those names).

**`internal/engine/create_worktree.go` (new):** the `CreateWorktree` operation. Test: `internal/engine/create_worktree_test.go`.

**`internal/tui/model.go`, `internal/tui/load.go`, `internal/tui/view.go` (modify):** load + store + render worktrees and the has-worktree marker. Tests extend `internal/tui/nav_test.go`/`fit_test.go` and add focused cases.

---

## Task 1: `git.TopLevel` verb

The repo root, used to absolutize repo-root-relative worktree paths and to mark the current worktree.

**Files:** Create `internal/git/worktree.go`; create `internal/git/worktree_verbs_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/git/worktree_verbs_test.go`

```go
package git

import (
	"context"
	"path/filepath"
	"testing"
)

func TestTopLevelReturnsRepoRoot(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	got, err := repo.TopLevel(context.Background())
	if err != nil {
		t.Fatalf("TopLevel: %v", err)
	}
	// git may resolve symlinks (e.g. /var -> /private/var on macOS); compare by
	// resolving both sides.
	wantResolved, _ := filepath.EvalSymlinks(dir)
	gotResolved, _ := filepath.EvalSymlinks(got)
	if gotResolved != wantResolved {
		t.Fatalf("TopLevel = %q, want %q", gotResolved, wantResolved)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestTopLevelReturnsRepoRoot -v`
Expected: FAIL — `repo.TopLevel undefined`.

- [ ] **Step 3: Write minimal implementation** — `internal/git/worktree.go`

```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// TopLevel returns the absolute path of the current worktree's root
// (`git rev-parse --show-toplevel`).
func (r *Repo) TopLevel(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--show-toplevel").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (toplevel)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/git/ -run TestTopLevelReturnsRepoRoot -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): add TopLevel verb (rev-parse --show-toplevel)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 2: `git.CheckRefFormatBranch` verb

Validates a branch name so an illegal template result fails fast with a clear error, before the expensive checkout (spec §5, §10).

**Files:** Modify `internal/git/worktree.go`; modify `internal/git/worktree_verbs_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/git/worktree_verbs_test.go`

```go
func TestCheckRefFormatBranch(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	if err := repo.CheckRefFormatBranch(context.Background(), "feature/ok-1"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	if err := repo.CheckRefFormatBranch(context.Background(), "bad..name"); err == nil {
		t.Error("invalid name 'bad..name' should be rejected")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestCheckRefFormatBranch -v`
Expected: FAIL — `repo.CheckRefFormatBranch undefined`.

- [ ] **Step 3: Write minimal implementation** — append to `internal/git/worktree.go`

```go
// CheckRefFormatBranch reports whether name is a legal git branch name
// (`git check-ref-format --branch`). A non-zero exit (illegal name) is returned
// as an error.
func (r *Repo) CheckRefFormatBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("check-ref-format").Arg("--branch", name).ToArgv()
	_, err := r.Runner.Run(ctx, "git check-ref-format", argv)
	return err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/git/ -run TestCheckRefFormatBranch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): add CheckRefFormatBranch verb

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 3: `git.AddWorktree` streaming verb

`git worktree add -b <branch> <path> <start-point>`. Streamed so the long checkout's stdout lines reach a live log, and cancellable via the context (the `Stream` runner uses `exec.CommandContext`). This is the codebase's first use of `Runner.Stream`.

**Files:** Modify `internal/git/worktree.go`; modify `internal/git/worktree_verbs_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/git/worktree_verbs_test.go`

```go
import "os"      // add to the import block if not present
import "os/exec" // add to the import block if not present

func TestAddWorktreeCreatesDirAndBranch(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wtPath := filepath.Join(filepath.Dir(dir), "wt-feature")
	err := repo.AddWorktree(context.Background(), wtPath, "feature/x", "main", nil)
	if err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}

	// The worktree directory exists and contains the committed file.
	if _, statErr := os.Stat(filepath.Join(wtPath, "README.md")); statErr != nil {
		t.Fatalf("worktree checkout missing: %v", statErr)
	}
	// The new branch exists.
	cmd := exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/x")
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("new branch not created: %v\n%s", e, out)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/git/ -run TestAddWorktreeCreatesDirAndBranch -v`
Expected: FAIL — `repo.AddWorktree undefined`.

- [ ] **Step 3: Write minimal implementation** — append to `internal/git/worktree.go`

```go
// AddWorktree creates a new linked worktree at path on a new branch, based on
// startPoint (`git worktree add -b <branch> <path> <startPoint>`). Output lines
// are forwarded to onLine (nil is allowed) so a frontend can show progress; the
// checkout is cancellable via ctx.
func (r *Repo) AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("add", "-b", branch, path, startPoint).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree add", argv, onLine)
	return err
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/git/ -run TestAddWorktreeCreatesDirAndBranch -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/git
git add internal/git/worktree.go internal/git/worktree_verbs_test.go
git commit -m "feat(git): add streaming AddWorktree verb

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 4: `engine.CreateWorktree` operation

Orchestrates the verbs: validate the branch name, absolutize a repo-root-relative path, refuse an existing path, then stream `git worktree add`, emitting `Progress`/`GitLine`/`Done` (spec §3, §6, §8, §10).

**Files:** Create `internal/engine/create_worktree.go`; create `internal/engine/create_worktree_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/engine/create_worktree_test.go`

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// hasProgress reports whether a Progress event was emitted.
func hasProgress(events []Event) bool {
	for _, e := range events {
		if _, ok := e.(Progress); ok {
			return true
		}
	}
	return false
}

func TestCreateWorktreeRelativePathSucceeds(t *testing.T) {
	dir, repo := newRepo(t)
	ch := make(chan Event, 32)

	// "../wt-rel" is resolved against the repo top-level (dir), i.e. a sibling.
	op := CreateWorktree{StartPoint: "main", Branch: "feature/rel", Path: "../wt-rel"}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	wantPath := filepath.Clean(filepath.Join(dir, "..", "wt-rel"))
	if _, statErr := os.Stat(filepath.Join(wantPath, "README.md")); statErr != nil {
		t.Fatalf("worktree not created at %s: %v", wantPath, statErr)
	}
	if !hasProgress(drain(ch)) {
		t.Error("expected a Progress event")
	}
}

func TestCreateWorktreeInvalidBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	op := CreateWorktree{StartPoint: "main", Branch: "bad..name", Path: "../wt-bad"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("want invalid-branch error, got %v", err)
	}
}

func TestCreateWorktreeExistingPathErrors(t *testing.T) {
	dir, repo := newRepo(t)
	existing := filepath.Join(filepath.Dir(dir), "wt-exists")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "feature/x", Path: existing}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("want path-exists error, got %v", err)
	}
}

func TestCreateWorktreeDuplicateBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "dup"); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "dup", Path: "../wt-dup"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("creating a worktree on an existing branch should error")
	}
}

func TestCreateWorktreeMissingFieldsError(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateWorktree{Branch: "x", Path: "../p"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("missing StartPoint should error")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/engine/ -run TestCreateWorktree -v`
Expected: FAIL — `undefined: CreateWorktree`.

- [ ] **Step 3: Write minimal implementation** — `internal/engine/create_worktree.go`

```go
package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CreateWorktree creates a new linked worktree on a NEW branch (Branch) based on
// StartPoint, at Path. A relative Path is resolved against the repository root.
// The fields are fully resolved by the frontend (template resolution and any
// <user:> input happen there, not here — see spec §3).
type CreateWorktree struct {
	StartPoint string
	Branch     string
	Path       string
}

func (op CreateWorktree) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.StartPoint == "" || op.Branch == "" || op.Path == "" {
		return Result{}, fmt.Errorf("create worktree: StartPoint, Branch, and Path are required")
	}

	// Validate the branch name before the (potentially large) checkout so an
	// illegal template result fails fast with a clear message.
	if err := deps.Repo.CheckRefFormatBranch(ctx, op.Branch); err != nil {
		return Result{}, fmt.Errorf("create worktree: invalid branch name %q: %w", op.Branch, err)
	}

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

	deps.emit(ctx, Progress{Step: "creating worktree", Detail: op.Branch + " → " + abs})
	if err := deps.Repo.AddWorktree(ctx, abs, op.Branch, op.StartPoint, func(line string) {
		deps.emit(ctx, GitLine{Raw: line})
	}); err != nil {
		return Result{}, fmt.Errorf("create worktree: %w", err)
	}

	res := Result{Summary: "worktree created: " + abs, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// compile-time check that CreateWorktree satisfies Operation.
var _ Operation = CreateWorktree{}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/engine/ -run TestCreateWorktree -v`
Expected: PASS (all five subtests).

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/engine
git add internal/engine/create_worktree.go internal/engine/create_worktree_test.go
git commit -m "feat(engine): add CreateWorktree operation (validate, absolutize, stream)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 5: Load worktrees (and current worktree path) into the Model

The TUI snapshot gains the worktree list and the current worktree's path (to mark it). Reuses the worktree list loader (`repo.Worktrees`) and `TopLevel` from Task 1.

**Files:** Modify `internal/tui/load.go`; modify `internal/tui/model.go`; add a test to `internal/tui/load_test.go`.

- [ ] **Step 1: Write the failing test** — append to `internal/tui/load_test.go`

```go
func TestLoadIncludesWorktrees(t *testing.T) {
	m := loadedModel(t)
	if len(m.worktrees) < 1 {
		t.Fatalf("expected at least the main worktree, got %d", len(m.worktrees))
	}
	if m.currentWorktree == "" {
		t.Error("expected currentWorktree to be set")
	}
}
```

If `load_test.go` does not exist or lacks a package/import header, create it as:
```go
package tui

import "testing"
```
(then add the test function above).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run TestLoadIncludesWorktrees -v`
Expected: FAIL — `m.worktrees` / `m.currentWorktree` undefined.

- [ ] **Step 3a: Extend the Model** — in `internal/tui/model.go`, add two fields to the `Model` struct, right after the `commits []model.Commit` field:

```go
	commits  []model.Commit
	worktrees       []model.Worktree
	currentWorktree string
```

(Replace the existing `commits  []model.Commit` line with these three lines. `model.Worktree` is already imported via the `model` package.)

- [ ] **Step 3b: Store them in the dataLoadedMsg handler** — in `internal/tui/model.go`, inside `case dataLoadedMsg:` where it sets `m.status/m.branches/m.commits`, add:

```go
		if msg.err == nil {
			m.status = msg.status
			m.branches = msg.branches
			m.commits = msg.commits
			m.worktrees = msg.worktrees
			m.currentWorktree = msg.currentWorktree
		}
```

- [ ] **Step 3c: Extend the message and loader** — in `internal/tui/load.go`, add fields to `dataLoadedMsg`:

```go
type dataLoadedMsg struct {
	status          model.WorkingTreeStatus
	branches        []model.Branch
	commits         []model.Commit
	worktrees       []model.Worktree
	currentWorktree string
	err             error
}
```

and load them in `loadCmd`, after the commits load and before `return out`:

```go
		if out.commits, err = repo.Log(ctx, 50); err != nil {
			out.err = err
			return out
		}
		if out.worktrees, err = repo.Worktrees(ctx); err != nil {
			out.err = err
			return out
		}
		// TopLevel marks which listed worktree is the current one; a failure here
		// is non-fatal (the marker just won't show).
		if top, topErr := repo.TopLevel(ctx); topErr == nil {
			out.currentWorktree = top
		}
		return out
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run TestLoadIncludesWorktrees -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/load.go internal/tui/model.go internal/tui/load_test.go
git commit -m "feat(tui): load worktrees and current worktree path into the model

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 6: Worktrees panel + has-worktree marker

Add the `panelWorktrees` panel (Branches → Worktrees → Status order), wire it into navigation, render the three left panels, and mark branches that have a worktree with `◫`.

**Files:** Modify `internal/tui/model.go` (panel enum + `panelLen`), `internal/tui/view.go` (rows + render). Test: add to `internal/tui/view.go`'s test neighbor — create `internal/tui/worktree_view_test.go`.

- [ ] **Step 1: Write the failing test** — `internal/tui/worktree_view_test.go`

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestBranchRowsShowWorktreeMarker(t *testing.T) {
	m := Model{
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feature/x"},
			{Name: "lonely"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo.worktrees/x", Branch: "feature/x"},
		},
		sel: map[panel]int{},
	}
	rows := m.branchRows()
	// main and feature/x have worktrees -> marked; lonely does not.
	if !strings.Contains(rows[0], "◫") {
		t.Errorf("main should have worktree marker: %q", rows[0])
	}
	if !strings.Contains(rows[1], "◫") {
		t.Errorf("feature/x should have worktree marker: %q", rows[1])
	}
	if strings.Contains(rows[2], "◫") {
		t.Errorf("lonely should NOT have worktree marker: %q", rows[2])
	}
}

func TestWorktreeRowsFormatAndCurrentMarker(t *testing.T) {
	m := Model{
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo.worktrees/x", Branch: "feature/x"},
		},
		currentWorktree: "/repo",
		sel:             map[panel]int{},
	}
	rows := m.worktreeRows()
	if len(rows) != 2 {
		t.Fatalf("want 2 worktree rows, got %d: %v", len(rows), rows)
	}
	if !strings.Contains(rows[0], "main") || !strings.Contains(rows[0], "/repo") {
		t.Errorf("row should show branch and path: %q", rows[0])
	}
	// The current worktree is marked with '*'.
	if !strings.HasPrefix(rows[0], "* ") {
		t.Errorf("current worktree should be marked: %q", rows[0])
	}
	if strings.HasPrefix(rows[1], "* ") {
		t.Errorf("non-current worktree should not be marked: %q", rows[1])
	}
}

func TestPanelLenWorktrees(t *testing.T) {
	m := Model{worktrees: make([]model.Worktree, 3), sel: map[panel]int{}}
	if n := m.panelLen(panelWorktrees); n != 3 {
		t.Fatalf("panelLen(panelWorktrees) = %d, want 3", n)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/tui/ -run 'TestBranchRowsShowWorktreeMarker|TestWorktreeRowsFormatAndCurrentMarker|TestPanelLenWorktrees' -v`
Expected: FAIL — `panelWorktrees` / `m.worktreeRows` undefined.

- [ ] **Step 3a: Panel enum** — in `internal/tui/model.go`, change the panel constants to insert `panelWorktrees` between branches and status:

```go
const (
	panelBranches panel = iota
	panelWorktrees
	panelStatus
	panelCommits
	panelCount
)
```

- [ ] **Step 3b: `panelLen`** — in `internal/tui/model.go`, add a case to `panelLen`:

```go
func (m Model) panelLen(p panel) int {
	switch p {
	case panelBranches:
		return len(m.branches)
	case panelWorktrees:
		return len(m.worktrees)
	case panelStatus:
		return len(m.status.Files)
	case panelCommits:
		return len(m.commits)
	}
	return 0
}
```

- [ ] **Step 3c: `worktreeRows` + marker in `branchRows`** — in `internal/tui/view.go`, replace the existing `branchRows` and add `worktreeRows`:

```go
// worktreeBranchSet returns the set of branch names checked out in a worktree.
func (m Model) worktreeBranchSet() map[string]bool {
	set := make(map[string]bool, len(m.worktrees))
	for _, w := range m.worktrees {
		if w.Branch != "" {
			set[w.Branch] = true
		}
	}
	return set
}

func (m Model) branchRows() []string {
	hasWt := m.worktreeBranchSet()
	out := make([]string, 0, len(m.branches))
	for _, b := range m.branches {
		marker := "  "
		if b.IsHead {
			marker = "* "
		}
		row := marker + b.Name
		if hasWt[b.Name] {
			row += " ◫"
		}
		out = append(out, row)
	}
	return out
}

func (m Model) worktreeRows() []string {
	out := make([]string, 0, len(m.worktrees))
	for _, w := range m.worktrees {
		marker := "  "
		if w.Path == m.currentWorktree {
			marker = "* "
		}
		branch := w.Branch
		if branch == "" {
			branch = "(detached)"
		}
		out = append(out, marker+branch+"  "+w.Path)
	}
	return out
}
```

- [ ] **Step 3d: Render three left panels** — in `internal/tui/view.go`, replace the two-column branch of `render` (the block that builds `left`, `right`, `body` after `rightW := w - leftW`) with:

```go
	rightW := w - leftW

	var left string
	if bodyH >= 9 {
		// Three stacked left panels: Branches, Worktrees, Status. Each bordered
		// panel needs >=3 rows, so this layout requires bodyH >= 9.
		h1 := bodyH / 3
		h2 := bodyH / 3
		h3 := bodyH - h1 - h2
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), leftW, h1),
			m.renderPanel(panelWorktrees, "Worktrees", m.worktreeRows(), leftW, h2),
			m.renderPanel(panelStatus, "Status", m.statusRows(), leftW, h3),
		)
	} else {
		// Short terminal: fall back to two left panels (Branches over Status).
		branchesH := bodyH / 2
		statusH := bodyH - branchesH
		left = lipgloss.JoinVertical(lipgloss.Left,
			m.renderPanel(panelBranches, "Branches", m.branchRows(), leftW, branchesH),
			m.renderPanel(panelStatus, "Status", m.statusRows(), leftW, statusH),
		)
	}
	right := m.renderPanel(panelCommits, "Commits", m.commitRows(), rightW, bodyH)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, right)

	return strings.Join([]string{header, body, footer, statusLine}, "\n")
```

(Delete the old `branchesH := bodyH / 2 ... return strings.Join(...)` lines that this replaces.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/tui/ -run 'TestBranchRowsShowWorktreeMarker|TestWorktreeRowsFormatAndCurrentMarker|TestPanelLenWorktrees' -v`
Expected: PASS. Then run the whole TUI package: `go test ./internal/tui/` — all prior tests still pass.

- [ ] **Step 5: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/model.go internal/tui/view.go internal/tui/worktree_view_test.go
git commit -m "feat(tui): worktrees panel and has-worktree branch marker

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 7: Fit-invariant test for the 3-panel left column

Guard that the new layout never overflows the terminal — including a medium terminal (3-panel path) crowded with worktrees.

**Files:** Modify `internal/tui/fit_test.go`.

- [ ] **Step 1: Write the failing-or-passing test** — append to `internal/tui/fit_test.go`

```go
// The 3-panel left column must also respect terminal bounds, even with many
// worktrees and a medium height where each left panel is near its 3-row floor.
func TestRenderThreePanelLeftFits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 60, 12 // bodyH = 9 -> three left panels of 3 rows each

	m.worktrees = make([]model.Worktree, 40)
	for i := range m.worktrees {
		m.worktrees[i] = model.Worktree{Path: "/very/long/path/to/worktree/number", Branch: "branch-name"}
	}
	m.branches = make([]model.Branch, 40)
	for i := range m.branches {
		m.branches[i] = model.Branch{Name: "branch-name"}
	}

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}
```

- [ ] **Step 2: Run to verify**

Run: `go test ./internal/tui/ -run TestRenderThreePanelLeftFits -v`
Expected: PASS (the layout was built to satisfy this). If it FAILS on line count, the bug is in the Task 6 height math — fix it there (the three heights must sum to exactly `bodyH`).

- [ ] **Step 3: Run the existing fit tests too**

Run: `go test ./internal/tui/ -run TestRender -v`
Expected: PASS — `TestRenderNeverExceedsTerminal`, `TestRenderTinyTerminal`, and the new test.

- [ ] **Step 4: Commit**

```bash
gofmt -w internal/tui
git add internal/tui/fit_test.go
git commit -m "test(tui): fit-invariant for the 3-panel left column

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>"
```

---

## Task 8: Full-package verification

**Files:** none (verification only)

- [ ] **Step 1: Full suite** — `go test ./...` — Expected: all packages PASS.
- [ ] **Step 2: Race (the engine op + TUI bridge involve goroutines/channels)** — `go test -race ./internal/engine/ ./internal/tui/` — Expected: PASS, no race reports.
- [ ] **Step 3: Vet** — `go vet ./...` — Expected: no output.
- [ ] **Step 4: Format** — `gofmt -l internal cmd` — Expected: empty. If anything is listed, `gofmt -w internal cmd` and amend the relevant commit.

No commit needed if everything is already committed.

---

## Self-Review Notes (plan author)

- **Spec coverage:** §3 engine boundary (frontend passes resolved `{StartPoint,Branch,Path}`; op never prompts) → Task 4; §6 default placement / relative-path-against-root + container created by git → Task 4 (`filepath.Join(top, ...)`; `git worktree add` creates leading dirs); §7 TUI display (3 left panels Branches/Worktrees/Status, has-worktree `◫`, worktree list as `<branch>  <path>` with current marked) → Tasks 5–7; §8 streamed + cancellable checkout → Tasks 3–4 (`Runner.Stream` + `exec.CommandContext` cancellation; `GitLine` forwarding); §10 error cases (illegal branch name → `CheckRefFormatBranch`; path exists → `os.Stat` guard; branch already exists / start-point checked out elsewhere → git's wrapped error) → Task 4.
- **Deferred to A3 (correctly):** the `w`/`W` popup, live preview, `<user:>` collection, template+config wiring, create-and-switch re-root, `--cwd-file`/`gg shell-init`. A2 builds only the engine op and the read-only display.
- **Known limitation (documented):** `Runner.Stream` captures stdout only, so `git worktree add`'s stderr progress is not line-streamed; the op still emits `Progress`/`Done` and is cancellable, satisfying the non-blocking invariant. Richer stderr progress is a future runner enhancement, not A2 scope.
- **Type consistency:** `CreateWorktree{StartPoint,Branch,Path}` matches the spec §3 fields and the A3 caller; `AddWorktree(ctx, path, branch, startPoint, onLine)`, `TopLevel(ctx) (string,error)`, `CheckRefFormatBranch(ctx, name) error` are referenced consistently across Tasks 1–4; `panelWorktrees`, `worktreeRows`, `worktreeBranchSet`, `m.worktrees`, `m.currentWorktree` are consistent across Tasks 5–7.
