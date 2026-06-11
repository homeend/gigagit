# gigagit M1 — Plan 2C: Ref-only Undo — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a safe, reflog-backed "undo last commit" operation that moves the branch ref back one step with `--soft` (keeping the user's work staged) — the ref-only undo scoped for M1.

**Architecture:** Two thin `git.Repo` verbs (`LastReflogSubject`, `ResetSoft`) plus an `UndoLastCommit` engine `Operation`. The operation only acts when the last HEAD movement was a commit (checked via the reflog subject), so it never silently corrupts state. Working-tree-affecting undo is explicitly out of scope (see spec §6).

**Tech Stack:** Go 1.26, standard library + existing internal packages. Tests use real throwaway git repos (`newTestRepo` in `internal/git`, `newRepo` in `internal/engine`).

---

## Shared interfaces

```go
// internal/git
func (r *Repo) LastReflogSubject(ctx context.Context) (string, error) // e.g. "commit: msg", "checkout: moving from a to b"
func (r *Repo) ResetSoft(ctx context.Context, ref string) error       // git reset --soft <ref>

// internal/engine
type UndoLastCommit struct{}
```

---

## Task 1: Git verbs — LastReflogSubject, ResetSoft

**Files:**
- Create: `internal/git/reflog.go`
- Test: `internal/git/reflog_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/git/reflog_test.go`:
```go
package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastReflogSubjectIsCommit(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	subj, err := repo.LastReflogSubject(context.Background())
	if err != nil {
		t.Fatalf("last reflog subject: %v", err)
	}
	// The fixture's most recent HEAD movement is the initial commit.
	if !strings.HasPrefix(subj, "commit") {
		t.Fatalf("subject = %q, want it to start with 'commit'", subj)
	}
}

func TestResetSoftMovesRefKeepsWorkingTree(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// Second commit.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "second")

	before := revParse(t, dir, "HEAD")
	if err := repo.ResetSoft(context.Background(), "HEAD@{1}"); err != nil {
		t.Fatalf("reset soft: %v", err)
	}
	after := revParse(t, dir, "HEAD")
	if before == after {
		t.Fatal("HEAD did not move after reset --soft HEAD@{1}")
	}
	// b.txt must still exist (working tree untouched) and be staged.
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatalf("b.txt lost after soft reset: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if st.Counts().Staged == 0 {
		t.Fatalf("expected the undone commit's changes to be staged, got %+v", st.Counts())
	}
}
```

Note: `revParse(t, dir, ref)` and `gitIn(t, dir, args...)` are already defined in `sync_test.go` (same package) — reuse them directly; no new helpers needed. Test imports: `context`, `os`, `path/filepath`, `strings`, `testing`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/git/ -run 'TestLastReflogSubject|TestResetSoft'`
Expected: FAIL — undefined `LastReflogSubject`, `ResetSoft`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/git/reflog.go`:
```go
package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// LastReflogSubject returns the subject of the most recent HEAD reflog entry,
// e.g. "commit: add foo" or "checkout: moving from main to dev". Returns "" if
// there is no reflog.
func (r *Repo) LastReflogSubject(ctx context.Context) (string, error) {
	argv := gitcmd.New("reflog").Arg("-1", "--format=%gs").ToArgv()
	res, err := r.Runner.Run(ctx, "git reflog", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ResetSoft moves the current branch to ref, leaving the index and working tree
// unchanged (git reset --soft). The undone commit's changes remain staged.
func (r *Repo) ResetSoft(ctx context.Context, ref string) error {
	argv := gitcmd.New("reset").Arg("--soft", ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git reset --soft", argv)
	return err
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/git/ && go vet ./internal/git/ && gofmt -l internal/git`
Expected: PASS; gofmt clean.

- [ ] **Step 5: Commit**

```bash
git add internal/git/reflog.go internal/git/reflog_test.go
git commit -m "feat: add LastReflogSubject and ResetSoft verbs"
```

---

## Task 2: UndoLastCommit engine operation

**Files:**
- Create: `internal/engine/undo.go`
- Test: `internal/engine/undo_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/engine/undo_test.go`:
```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUndoLastCommitMovesHeadBackAndStages(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	// Make a second commit to undo.
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	if err := repo.Commit(ctx, "second", true); err != nil {
		t.Fatalf("commit: %v", err)
	}

	res, err := UndoLastCommit{}.Run(ctx, OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("undo: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	// The undone commit's file change must now be staged, not lost.
	st, _ := repo.Status(ctx)
	if st.Counts().Staged == 0 {
		t.Fatalf("expected staged changes after undo, got %+v", st.Counts())
	}
}

func TestUndoLastCommitRefusesWhenLastOpNotCommit(t *testing.T) {
	dir, repo := newRepo(t)
	ctx := context.Background()

	// Make the last HEAD movement a checkout, not a commit.
	if err := repo.CreateBranch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Switch(ctx, "feature"); err != nil {
		t.Fatal(err)
	}
	_ = dir

	_, err := UndoLastCommit{}.Run(ctx, OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("expected undo to refuse when the last operation was not a commit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestUndoLastCommit`
Expected: FAIL — undefined `UndoLastCommit`.

- [ ] **Step 3: Write minimal implementation**

Create `internal/engine/undo.go`:
```go
package engine

import (
	"context"
	"fmt"
	"strings"
)

// UndoLastCommit reverts the most recent commit by moving the branch ref back
// one step (git reset --soft HEAD@{1}). The commit's changes are kept staged,
// never discarded. It refuses if the last HEAD movement was not a commit, so it
// never corrupts state by reversing an unrelated operation.
type UndoLastCommit struct{}

var _ Operation = UndoLastCommit{}

func (UndoLastCommit) Run(ctx context.Context, deps OpDeps) (Result, error) {
	subj, err := deps.Repo.LastReflogSubject(ctx)
	if err != nil {
		return Result{}, err
	}
	if !strings.HasPrefix(subj, "commit") {
		return Result{}, fmt.Errorf("undo: last operation was not a commit (%q); ref-only undo can only undo commits", subj)
	}
	deps.emit(ctx, Progress{Step: "undoing last commit", Detail: subj})
	if err := deps.Repo.ResetSoft(ctx, "HEAD@{1}"); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "undid last commit (changes kept staged)", Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/engine/ -run TestUndoLastCommit -v`
Expected: PASS (both tests).

- [ ] **Step 5: Run FULL suite + vet + fmt**

Run: `go build ./... && go test ./... && go vet ./... && gofmt -l internal cmd`
Expected: build OK; all PASS; vet clean; gofmt prints nothing.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/undo.go internal/engine/undo_test.go
git commit -m "feat: add UndoLastCommit operation (ref-only, soft reset)"
```

---

## Self-Review

**Spec coverage (§6 ref-only undo):** undo limited to a ref move (`reset --soft`), never touches the working tree, refuses when the last op wasn't a commit → Tasks 1–2. Working-tree/stash undo remains explicitly out of scope per §6.

**Placeholder scan:** none — all steps have complete code.

**Type consistency:** `LastReflogSubject`/`ResetSoft` signatures (Task 1) match the `UndoLastCommit` calls (Task 2); `UndoLastCommit{}` satisfies `Operation` (compile-time assertion); uses `deps.emit(ctx, …)` matching the Plan 2B emit signature.

**Deferred:** undoing branch switches / pushes and general reflog navigation — future enhancement; the TUI (Plan 3) will expose `UndoLastCommit` on a key.

---

## Plan sequence (M1)

1. Plan 1 — Foundation & read-only inspection ✅
2. Plan 2A — Engine contract & primitives ✅
3. Plan 2B — Smart operations (SmartPull, SmartSwitch) ✅
4. **Plan 2C — Ref-only Undo** (this document).
5. Plan 3 — TUI (Bubble Tea): wires status/branches/log panels, smart-sync keys, undo, and credential routing through the modal Decider. The first hand-usable surface.
