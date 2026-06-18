# Discard Unstaged Changes (`d` / `D`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `d` (discard marked files / cursor row) and `D` (discard all unstaged changes) to the TUI Files panel, behind a confirmation modal, covering both tracked edits and new untracked files.

**Architecture:** A new frontend-agnostic `engine.Discard` operation runs two thin git verbs — `git restore --worktree` (revert tracked edits, keeping staged hunks) and `git clean -f -d` (remove untracked files/dirs). The TUI classifies the target file set by `model.FileKind`, confirms via the existing pre-op `decisionState` modal, and dispatches the op through `domain.Execute` via `startOp` (which reloads all panels on completion).

**Tech Stack:** Go 1.26, Bubble Tea TUI, `gitcmd` argv builder, `gitexec.FakeRunner` + real `git` in `t.TempDir()` for tests.

## Global Constraints

- A git verb is one invocation: build argv with `gitcmd`, run via `r.Runner.Run`. Never shell out directly.
- `internal/tui` must NOT import `internal/git` (archtest-guarded) — it reaches git only through `engine`/`domain`.
- New git verbs an op needs must be added to the `engine.GitOps` interface (`internal/engine/gitops.go`); `var _ GitOps = (*git.Repo)(nil)` proves the concrete repo satisfies it.
- Every new keybinding lands in BOTH `internal/tui/footer.go` and `internal/tui/help.go` (the `TestHelpFooterCoverage` drift guard enforces this).
- Footer availability predicates live in `internal/tui/avail.go` and are shared by the key dispatch and the footer; a footer `when` may be stricter than the dispatch gate, never looser.
- Conflicted (`KindUnmerged`) files are excluded from discard, consistent with `canShowFileDiff`/`canStageHunks`.
- `D` uses the whole-tree pathspec `:/` (not path enumeration) to avoid argv blowup on ~100GB monorepos.
- TDD: write the failing test first, watch it fail, implement minimally, watch it pass, commit.
- Run the staged gate before merge: `./test.sh race`.

---

### Task 1: Git verbs — `RestoreWorktree` + `CleanUntracked`

**Files:**
- Create: `internal/git/discard.go`
- Test: `internal/git/discard_test.go`

**Interfaces:**
- Consumes: `gitcmd.New(...).Arg(...).ArgIf(cond, ...).ToArgv()`; `r.Runner.Run(ctx, label, argv)`.
- Produces:
  - `func (r *Repo) RestoreWorktree(ctx context.Context, paths []string) error`
  - `func (r *Repo) CleanUntracked(ctx context.Context, paths []string) error`

- [ ] **Step 1: Write the failing argv + real-git tests**

Create `internal/git/discard_test.go`:

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

func TestRestoreWorktreeArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git restore --worktree", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RestoreWorktree(context.Background(), []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	want := []string{"restore", "--worktree", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRestoreWorktreeArgvAll(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git restore --worktree", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RestoreWorktree(context.Background(), []string{":/"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	want := []string{"restore", "--worktree", "--", ":/"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCleanUntrackedArgvPaths(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git clean -f -d", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CleanUntracked(context.Background(), []string{"new.txt"}); err != nil {
		t.Fatalf("clean: %v", err)
	}
	want := []string{"clean", "-f", "-d", "--", "new.txt"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCleanUntrackedArgvAll(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git clean -f -d", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CleanUntracked(context.Background(), nil); err != nil {
		t.Fatalf("clean: %v", err)
	}
	want := []string{"clean", "-f", "-d"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

// Real-git: restore reverts an unstaged edit but keeps a previously-staged hunk.
func TestRestoreWorktreeKeepsStaged(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("staged\n"), 0o644)
	gitOutIn(t, dir, "add", "README.md")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("unstaged\n"), 0o644)

	if err := repo.RestoreWorktree(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("restore: %v", err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(b) != "staged\n" {
		t.Fatalf("worktree = %q, want %q", b, "staged\n")
	}
}

// Real-git: clean removes a new untracked file AND a new untracked directory.
func TestCleanUntrackedRemovesFileAndDir(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "newdir"), 0o755)
	os.WriteFile(filepath.Join(dir, "newdir", "f.txt"), []byte("y\n"), 0o644)

	if err := repo.CleanUntracked(context.Background(), nil); err != nil {
		t.Fatalf("clean: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be removed, stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "newdir")); !os.IsNotExist(err) {
		t.Fatalf("newdir should be removed, stat err = %v", err)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'RestoreWorktree|CleanUntracked' -v`
Expected: FAIL — `r.RestoreWorktree` / `r.CleanUntracked` undefined (compile error).

- [ ] **Step 3: Implement the verbs**

Create `internal/git/discard.go`:

```go
package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// RestoreWorktree resets the given paths in the working tree to the index
// (git restore --worktree), discarding unstaged changes while keeping any
// staged hunks. Pass ":/" to restore the entire tree from the repo root.
func (r *Repo) RestoreWorktree(ctx context.Context, paths []string) error {
	b := gitcmd.New("restore").Arg("--worktree", "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git restore --worktree", b.ToArgv())
	return err
}

// CleanUntracked removes untracked files and directories (git clean -f -d).
// Empty paths cleans the whole working tree. The "--" guard is added only when
// paths are present so the all-paths call stays a bare `git clean -f -d`.
func (r *Repo) CleanUntracked(ctx context.Context, paths []string) error {
	b := gitcmd.New("clean").Arg("-f", "-d").ArgIf(len(paths) > 0, "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git clean -f -d", b.ToArgv())
	return err
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'RestoreWorktree|CleanUntracked' -v`
Expected: PASS (all six tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/discard.go internal/git/discard_test.go
git commit -m "feat(git): RestoreWorktree + CleanUntracked verbs

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 2: Engine op — `engine.Discard` + GitOps interface

**Files:**
- Create: `internal/engine/discard.go`
- Test: `internal/engine/discard_test.go`
- Modify: `internal/engine/gitops.go` (add the two verbs to the interface)

**Interfaces:**
- Consumes: `deps.Repo.RestoreWorktree(ctx, paths)`, `deps.Repo.CleanUntracked(ctx, paths)` (Task 1); `deps.emit(ctx, Progress{...})`, `Done{...}`; `Result{Summary, Changed}`.
- Produces: `type Discard struct { Restore []string; Remove []string; All bool }` satisfying `Operation`.

- [ ] **Step 1: Add the verbs to the GitOps interface**

In `internal/engine/gitops.go`, after the existing `StagePaths`/`UnstagePaths` lines (~line 60-61), add:

```go
	RestoreWorktree(ctx context.Context, paths []string) error
	CleanUntracked(ctx context.Context, paths []string) error
```

- [ ] **Step 2: Write the failing engine tests**

Create `internal/engine/discard_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Targeted: restore reverts a tracked edit; the file returns to HEAD content.
func TestDiscardRestoresTrackedEdit(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)

	res, err := Discard{Restore: []string{"README.md"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(readme)
	if string(b) != string(orig) {
		t.Fatalf("README.md = %q, want original %q", b, orig)
	}
}

// Targeted: remove deletes an untracked new file.
func TestDiscardRemovesUntracked(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	_, err := Discard{Remove: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone, stat err = %v", err)
	}
}

// All: discards every unstaged change — both a tracked edit and an untracked file.
func TestDiscardAll(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	res, err := Discard{All: true}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard all: %v", err)
	}
	if !res.Changed || res.Summary != "discarded" {
		t.Fatalf("result = %+v", res)
	}
	b, _ := os.ReadFile(readme)
	if string(b) != string(orig) {
		t.Fatalf("README.md not reverted: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone, stat err = %v", err)
	}
}

// Mixed targeted: restore + remove in one op.
func TestDiscardMixed(t *testing.T) {
	dir, repo := newRepo(t)
	readme := filepath.Join(dir, "README.md")
	orig, _ := os.ReadFile(readme)
	os.WriteFile(readme, []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	_, err := Discard{Restore: []string{"README.md"}, Remove: []string{"new.txt"}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("discard mixed: %v", err)
	}
	if b, _ := os.ReadFile(readme); string(b) != string(orig) {
		t.Fatalf("README.md not reverted: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be gone")
	}
}

// Partial failure: a clean error is surfaced, not swallowed. fakeRepo returns
// an error from CleanUntracked while RestoreWorktree succeeds.
func TestDiscardPartialFailureReturnsError(t *testing.T) {
	fr := &discardFakeRepo{cleanErr: true}
	_, err := Discard{Restore: []string{"a"}, Remove: []string{"b"}}.Run(context.Background(), OpDeps{Repo: fr})
	if err == nil {
		t.Fatal("expected error from clean failure")
	}
	if !strings.Contains(err.Error(), "clean") {
		t.Fatalf("error = %v, want clean", err)
	}
	if !fr.restoreCalled {
		t.Fatal("restore should still have been attempted")
	}
}
```

Add the minimal fake at the bottom of the same test file. It embeds a nil
`GitOps` so only the two methods under test need bodies (every other call would
panic, which these tests never make):

```go
// discardFakeRepo exercises Discard's error handling without a real repo.
type discardFakeRepo struct {
	GitOps // nil embed: only the two discard verbs are implemented
	restoreCalled bool
	cleanErr      bool
}

func (f *discardFakeRepo) RestoreWorktree(ctx context.Context, paths []string) error {
	f.restoreCalled = true
	return nil
}

func (f *discardFakeRepo) CleanUntracked(ctx context.Context, paths []string) error {
	if f.cleanErr {
		return errTestClean
	}
	return nil
}

var errTestClean = fmt.Errorf("boom")
```

Add `"fmt"` to the test file's imports.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestDiscard -v`
Expected: FAIL — `Discard` undefined (compile error).

- [ ] **Step 4: Implement the operation**

Create `internal/engine/discard.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
)

// Discard throws away unstaged working-tree changes. Restore holds tracked
// paths to reset from the index (git restore --worktree, keeping staged hunks);
// Remove holds untracked paths to delete (git clean). When All is set, both
// lists are ignored and the whole working tree is discarded via a repo-root
// pathspec, avoiding argv blowup on large monorepos. Default TreeWrite
// reservation (it touches the working tree).
type Discard struct {
	Restore []string
	Remove  []string
	All     bool
}

var _ Operation = Discard{}

func (op Discard) Run(ctx context.Context, deps OpDeps) (Result, error) {
	deps.emit(ctx, Progress{Step: "discarding"})

	restore, remove := op.Restore, op.Remove
	cleanWholeTree := false
	if op.All {
		restore = []string{":/"}
		remove = nil
		cleanWholeTree = true
	}

	// Run both steps even if the first errors, so we never leave a silent
	// half-discard; collect and join whatever failed.
	var errs []error
	if len(restore) > 0 {
		if err := deps.Repo.RestoreWorktree(ctx, restore); err != nil {
			errs = append(errs, fmt.Errorf("restore: %w", err))
		}
	}
	if cleanWholeTree || len(remove) > 0 {
		if err := deps.Repo.CleanUntracked(ctx, remove); err != nil {
			errs = append(errs, fmt.Errorf("clean: %w", err))
		}
	}
	if len(errs) > 0 {
		return Result{}, errors.Join(errs...)
	}

	res := Result{Summary: "discarded", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestDiscard -v`
Expected: PASS (all five tests).

- [ ] **Step 6: Verify the GitOps drift guard still compiles**

Run: `go build ./internal/engine/ ./internal/git/`
Expected: clean build (the `var _ GitOps = (*git.Repo)(nil)` assertion confirms `*git.Repo` now satisfies the expanded interface).

- [ ] **Step 7: Commit**

```bash
git add internal/engine/discard.go internal/engine/discard_test.go internal/engine/gitops.go
git commit -m "feat(engine): Discard op (restore tracked + clean untracked)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 3: TUI wiring — `d`/`D` keys, confirmation modal, footer, help

**Files:**
- Modify: `internal/tui/avail.go` (add `canDiscard`)
- Modify: `internal/tui/model.go` (`d` Files case, new `D` case, `discardTargets` + `discardPrompt` helpers)
- Modify: `internal/tui/footer.go` (two bindings)
- Modify: `internal/tui/help.go` (two help rows)
- Test: `internal/tui/discard_test.go`

**Interfaces:**
- Consumes: `engine.Discard{Restore, Remove, All}` (Task 2); `m.startOp(op)`; `m.backingIndex(panelFiles)`; `m.status.Files`, `m.status.Conflicts()`, `m.status.Counts()`; `m.fileMarks`; `decisionState{req, onResolve}`; `engine.DecisionRequest{ID, Prompt, Options}`.
- Produces:
  - `func (m Model) canDiscard() bool`
  - `func (m Model) discardTargets() (restore, remove []string, n int)`
  - `func discardPrompt(restore, remove []string, n int) string`

- [ ] **Step 1: Write the failing TUI tests**

Create `internal/tui/discard_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func fileStatus(path string, kind model.FileKind) model.FileStatus {
	return model.FileStatus{Path: path, Kind: kind, Unstaged: 'M'}
}

// canDiscard: true only on Files panel with at least one discardable row.
func TestCanDiscardGating(t *testing.T) {
	m := newTestModel(t)
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("a.go", model.KindTracked)}}
	m.focus = panelFiles
	if !m.canDiscard() {
		t.Fatal("want canDiscard on Files panel with a tracked file")
	}
	m.focus = panelStaged
	if m.canDiscard() {
		t.Fatal("canDiscard must be false on the Staged panel")
	}
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("c.go", model.KindUnmerged)}}
	if m.canDiscard() {
		t.Fatal("canDiscard must be false when the only row is conflicted")
	}
}

// discardTargets: marked set wins; untracked → remove, tracked → restore;
// unmerged dropped.
func TestDiscardTargetsMarked(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("new.txt", model.KindUntracked),
		fileStatus("conflict.go", model.KindUnmerged),
	}}
	m.fileMarks = map[string]bool{"edit.go": true, "new.txt": true, "conflict.go": true}
	restore, remove, n := m.discardTargets()
	if n != 2 {
		t.Fatalf("n = %d, want 2 (conflict dropped)", n)
	}
	if len(restore) != 1 || restore[0] != "edit.go" {
		t.Fatalf("restore = %v", restore)
	}
	if len(remove) != 1 || remove[0] != "new.txt" {
		t.Fatalf("remove = %v", remove)
	}
}

// discardTargets: no marks → cursor row only.
func TestDiscardTargetsCursor(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("new.txt", model.KindUntracked),
	}}
	m.sel[panelFiles] = 1 // cursor on new.txt
	restore, remove, n := m.discardTargets()
	if n != 1 || len(remove) != 1 || remove[0] != "new.txt" || len(restore) != 0 {
		t.Fatalf("restore=%v remove=%v n=%d", restore, remove, n)
	}
}

// d on a conflicted cursor row with no marks: empty target → no-op + statusMsg.
func TestDiscardKeyEmptyTargetNoOp(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("c.go", model.KindUnmerged)}}
	// canDiscard is false here, so the dispatch should not open a modal.
	nm, _ := m.handleKey(keyMsg("d"))
	out := nm.(Model)
	if out.modal != nil {
		t.Fatal("no modal expected for an all-conflicted Files panel")
	}
}

// D refuses while a conflict exists.
func TestDiscardAllRefusesOnConflict(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		fileStatus("edit.go", model.KindTracked),
		fileStatus("c.go", model.KindUnmerged),
	}}
	nm, _ := m.handleKey(keyMsg("D"))
	out := nm.(Model)
	if out.modal != nil {
		t.Fatal("D must not open a modal while conflicts exist")
	}
	if !strings.Contains(out.statusMsg, "conflict") {
		t.Fatalf("statusMsg = %q, want a conflict refusal", out.statusMsg)
	}
}

// d opens the confirm modal with the Discard/Cancel options.
func TestDiscardKeyOpensModal(t *testing.T) {
	m := newTestModel(t)
	m.focus = panelFiles
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{fileStatus("edit.go", model.KindTracked)}}
	nm, _ := m.handleKey(keyMsg("d"))
	out := nm.(Model)
	if out.modal == nil {
		t.Fatal("d should open the confirm modal")
	}
	if out.modal.req.ID != "discard" {
		t.Fatalf("modal ID = %q, want discard", out.modal.req.ID)
	}
	if got := out.modal.req.Options; len(got) != 2 || got[0] != "Discard" || got[1] != "Cancel" {
		t.Fatalf("options = %v", got)
	}
	if !strings.Contains(out.modal.req.Prompt, "edit.go") {
		t.Fatalf("prompt = %q, want the filename", out.modal.req.Prompt)
	}
}
```

> **Note on test helpers:** this file uses `newTestModel(t)`, `m.handleKey(...)`, and `keyMsg("d")`. Before writing, grep the existing tui tests (`grep -n 'func newTestModel\|func keyMsg\|handleKey\|sendKey' internal/tui/*_test.go`) and match whatever the established helpers are named (e.g. some suites build the model inline and call `m.Update(tea.KeyMsg{...})`). Use the real helper names; do not invent ones that don't exist. The assertions above stay the same regardless of the entry-point helper.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run Discard -v`
Expected: FAIL — `canDiscard` / `discardTargets` undefined (compile error).

- [ ] **Step 3: Add `canDiscard` to `avail.go`**

Append to `internal/tui/avail.go`:

```go
// canDiscard gates d/D on the Files panel: at least one discardable
// (non-conflicted) working-tree row exists and no op is running. Conflicted
// files are excluded (they are the x editor's job), matching canShowFileDiff.
func (m Model) canDiscard() bool {
	if m.focus != panelFiles || !m.opsIdle() {
		return false
	}
	for _, f := range m.status.Files {
		if f.Kind != model.KindUnmerged {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: Add the helpers + dispatch to `model.go`**

Add the helpers near `handleStageKey` (after line ~881 in `internal/tui/model.go`):

```go
// discardTargets resolves what d should discard: the marked file set if any,
// otherwise the cursor row. Conflicted (unmerged) files are dropped. Untracked
// paths go to Remove (git clean); every other tracked path goes to Restore
// (git restore --worktree). n is the total number of targeted paths.
func (m Model) discardTargets() (restore, remove []string, n int) {
	var files []model.FileStatus
	if len(m.fileMarks) > 0 {
		for _, f := range m.status.Files {
			if m.fileMarks[f.Path] {
				files = append(files, f)
			}
		}
	} else if bi, ok := m.backingIndex(panelFiles); ok {
		files = []model.FileStatus{m.status.Files[bi]}
	}
	for _, f := range files {
		switch f.Kind {
		case model.KindUnmerged:
			continue
		case model.KindUntracked:
			remove = append(remove, f.Path)
		default:
			restore = append(restore, f.Path)
		}
	}
	return restore, remove, len(restore) + len(remove)
}

// discardPrompt is the confirmation text for a targeted (d) discard.
func discardPrompt(restore, remove []string, n int) string {
	if n == 1 {
		all := append(append([]string{}, restore...), remove...)
		return "Discard changes to " + all[0] + "? This cannot be undone."
	}
	return fmt.Sprintf("Discard changes to %d files? This cannot be undone.", n)
}
```

Confirm `fmt` is already imported in `model.go` (it is — used elsewhere).

In the existing `case "d":` block (the `switch m.focus` at ~line 524), add a
`panelFiles` arm alongside `panelWorktrees`/`panelBranches`:

```go
		case panelFiles:
			if !m.canDiscard() {
				return m, nil
			}
			restore, remove, n := m.discardTargets()
			if n == 0 {
				m.statusMsg = "nothing to discard (resolve conflicts first)"
				return m, nil
			}
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "discard",
					Prompt:  discardPrompt(restore, remove, n),
					Options: []string{"Discard", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Discard" {
						m.fileMarks = nil
						return m.startOp(engine.Discard{Restore: restore, Remove: remove})
					}
					return m, nil
				},
			}
			return m, nil
```

Add a new top-level `case "D":` next to `case "d":`:

```go
		case "D":
			if m.focus != panelFiles || !m.opsIdle() {
				return m, nil
			}
			if len(m.status.Conflicts()) > 0 {
				m.statusMsg = "resolve conflicts before discarding all"
				return m, nil
			}
			c := m.status.Counts()
			if c.Unstaged == 0 && c.Untracked == 0 {
				m.statusMsg = "nothing to discard"
				return m, nil
			}
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "discard-all",
					Prompt:  "Discard ALL unstaged changes? This cannot be undone.",
					Options: []string{"Discard", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Discard" {
						m.fileMarks = nil
						return m.startOp(engine.Discard{All: true})
					}
					return m, nil
				},
			}
			return m, nil
```

- [ ] **Step 5: Add footer bindings (`footer.go`) and help rows (`help.go`)**

In `internal/tui/footer.go`, add to `contextBindings` (near the other Files-panel rows, after `mark-file`):

```go
	{"discard", "d", "[d]iscard", func(m Model) bool { return m.focus == panelFiles && m.canDiscard() }, scopeRow},
	{"discard-all", "D", "[D] discard all", func(m Model) bool { return m.focus == panelFiles && m.canDiscard() }, scopeWindow},
```

In `internal/tui/help.go`, under the `h("Files panel")` group, add:

```go
		r("d", "discard the marked files (or the cursor row): revert edits, delete new files (confirm)"),
		r("D", "discard ALL unstaged changes: revert every edit + delete every new file (confirm)"),
```

- [ ] **Step 6: Run the targeted + drift-guard tests**

Run: `go test ./internal/tui/ -run 'Discard|HelpFooter' -v`
Expected: PASS — discard behaviour tests green, and `TestHelpFooterCoverage` still passes (the new `d`/`D` footer keys now have matching help rows).

- [ ] **Step 7: Run the full tui package**

Run: `go test ./internal/tui/`
Expected: PASS (no regressions in existing key-dispatch / footer tests).

- [ ] **Step 8: Commit**

```bash
git add internal/tui/avail.go internal/tui/model.go internal/tui/footer.go internal/tui/help.go internal/tui/discard_test.go
git commit -m "feat(tui): d/D discard unstaged changes with confirm modal

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

### Task 4: Docs + full gate

**Files:**
- Modify: `CHANGELOG.md`
- Modify: `README.md` (only if it lists Files-panel keybindings — check first)

**Interfaces:** none (documentation + verification only).

- [ ] **Step 1: Update CHANGELOG.md**

Add an entry under the current unreleased/in-progress section:

```markdown
- **Discard unstaged changes**: `d` discards the marked files (or the cursor
  row) in the Files panel — reverting tracked edits (keeping any staged hunks)
  and deleting new untracked files; `D` discards all unstaged changes. Both
  prompt for confirmation. Conflicted files are excluded; `D` refuses while a
  conflict exists.
```

- [ ] **Step 2: Update README.md if it documents keybindings**

Run: `grep -n 'Files panel\|\[space\] stage\|stage hunks' README.md`
- If a Files-panel keybinding list exists, add `d` / `D` rows mirroring the help text from Task 3.
- If README has no such list, skip (note it in the commit message).

- [ ] **Step 3: Run the full staged gate with race**

Run: `./test.sh race`
Expected: vet + gofmt clean, all unit tests PASS (incl. the new git/engine/tui tests), e2e PASS.

- [ ] **Step 4: Commit**

```bash
git add CHANGELOG.md README.md
git commit -m "docs: changelog + readme for discard unstaged (d/D)

Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>
Claude-Session: https://claude.ai/code/session_0151SXf6HykjK298evgdKkro"
```

---

## Self-Review

**Spec coverage:**
- `d` = marked / cursor row → Task 3 (`discardTargets`, `d` dispatch). ✓
- `D` = all unstaged → Task 3 (`D` dispatch, `Discard{All:true}`). ✓
- Confirmation modal → Task 3 (`decisionState`, Discard/Cancel). ✓
- Tracked edit → `restore --worktree`; new file → `clean -f -d` → Tasks 1+2. ✓
- Partial-staging keeps staged hunks → Task 1 `TestRestoreWorktreeKeepsStaged`. ✓
- Conflicted excluded from `d`; `D` refuses on conflict; empty target no-op → Task 3 (`canDiscard`, `discardTargets` drop, `D` guard, `n==0` guard). ✓
- Whole-tree `:/` for `D` (no enumeration) → Task 2 `All` branch. ✓
- Partial-failure surfaces the error → Task 2 `TestDiscardPartialFailureReturnsError`. ✓
- `Result{Changed:true}` reloads panels → uses `startOp`, whose `opFinishedMsg` path calls `m.loadCmd()`. ✓
- Footer + help (both) → Task 3 Step 5. ✓
- TUI never imports `internal/git` → only `engine.Discard` referenced. ✓
- TUI-only (no CLI) → no `internal/cli` changes. ✓

**Placeholder scan:** No TBD/TODO; every code step shows full code; the one
flagged uncertainty (tui test helper names) is called out explicitly with a
grep to resolve it, not left vague.

**Type consistency:** `RestoreWorktree(ctx, []string)` / `CleanUntracked(ctx,
[]string)` identical across Tasks 1, 2 (interface), and the fake. `engine.Discard{Restore,
Remove, All}` field names consistent across Tasks 2 and 3. `canDiscard`,
`discardTargets (restore, remove, n)`, `discardPrompt(restore, remove, n)`
signatures match between definition (Task 3 Steps 3-4) and tests (Step 1).
