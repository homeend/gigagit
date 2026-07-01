# Export a commit / file as a git patch — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a user export a commit — or one file's change within a commit — as a
`git format-patch` file (`.patch`, `git am`-able) through an editable-destination
dialog in the TUI and a `gg commit export-patch` CLI command.

**Architecture:** New git verbs (`FormatPatch`, `ParentCount`) → domain functions
(`CommitPatch`, `FilePatch`, `ExportDefaultDir`, merge guard) → a new
`engine.ExportFile` op that writes an absolute path outside the working tree →
TUI action rows + an editable-path popup, and a CLI sub-command. Mirrors the
shipped shelf-commit-temp-export feature's layering; TUI/CLI never import
`internal/git`.

**Tech Stack:** Go 1.26, Bubble Tea TUI, the project's `gitcmd`/`gitexec` verb
harness, `repogate` reservations, the `engine.Operation` contract.

## Global Constraints

- **Module:** `github.com/homeend/gigagit`, Go 1.26.
- **A git verb is one invocation.** Build argv with `gitcmd`, run via
  `r.Runner.Run`. Never shell out directly.
- **Frontends never import `internal/git`.** TUI/CLI reach git only through
  `internal/domain` (enforced by `internal/archtest`).
- **Operations never block on a human**; they `emit` events and `decide` via the
  `Decider` (option-lists only).
- **Format = `git format-patch -1 --binary --stdout`** (mailbox, `git am`-able,
  `--binary` keeps genuine binaries appliable). File extension `.patch`.
- **Merge commits are refused** with `ErrMergeCommitPatch` — this is a correctness
  guard: `format-patch -1` on a merge silently emits a *different* commit's patch
  (verified empirically during brainstorm).
- **Default destination = parent of the MAIN worktree root** (`/aaa/xxx/` for a
  repo at `/aaa/xxx/repo`), stable from a linked worktree.
- **TDD:** every task writes the failing test first. Run `go test ./...` (or the
  named package) green before committing. Commit at the end of each task.
- **Tests use a real `git`** in a `t.TempDir()` (helpers `newTestRepo`/`newRealRepo`/
  `gitRun`/`writeCommit`/`headHash`) or `gitexec.FakeRunner` for argv assertions.
- Build check: `go build ./cmd/gg`. Full gate before merge: `./test.sh race`.

---

## File Structure

- Create `internal/git/format_patch.go` — `FormatPatch` + `ParentCount` verbs.
- Create `internal/git/format_patch_test.go` — verb tests.
- Create `internal/engine/export_file.go` — `ExportFile` op.
- Create `internal/engine/export_file_test.go` — op tests.
- Create `internal/domain/patch.go` — `CommitPatch`, `FilePatch`,
  `ExportDefaultDir`, `ErrMergeCommitPatch`, `shortSHA`.
- Create `internal/domain/patch_test.go` — domain tests incl. merge guard + `git am` round-trip.
- Modify `internal/domain/export.go` — refactor `commitDirName` to reuse `shortSHA` (DRY).
- Create `internal/tui/export_patch.go` — msg, popup, resolve commands, action rows.
- Create `internal/tui/export_patch_test.go` — row gating + popup tests.
- Modify `internal/tui/action_menu.go` — wire the two rows.
- Modify `internal/tui/model.go` — handle `patchResolvedMsg`.
- Modify `internal/tui/source.go` — map `ExportFile` (and `ExportToDir`) to no refresh.
- Modify `internal/tui/help.go` — advertise the keys.
- Create `internal/cli/commit_export_patch.go` — the CLI sub-command.
- Modify `internal/cli/cli.go` — dispatch `export-patch` under `cmdCommit`.
- Create `internal/cli/commit_export_patch_test.go` — CLI test.
- Modify `CHANGELOG.md`, `README.md`, `CLAUDE.md`,
  `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go` (version).

---

## Task 1: `git.ParentCount` + `git.FormatPatch` verbs

**Files:**
- Create: `internal/git/format_patch.go`
- Test: `internal/git/format_patch_test.go`

**Interfaces:**
- Consumes: `gitcmd`, `gitexec` (existing verb harness); `*git.Repo`.
- Produces:
  - `func (r *Repo) ParentCount(ctx context.Context, rev string) (int, error)`
  - `func (r *Repo) FormatPatch(ctx context.Context, rev string, paths ...string) ([]byte, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/git/format_patch_test.go`:

```go
package git

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestParentCountArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-list (parent count)", gitexec.Result{Stdout: "abc def\n"})
	r := &Repo{Runner: f}
	n, err := r.ParentCount(context.Background(), "abc")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("parents = %d, want 1", n)
	}
	got := strings.Join(f.Calls[0].Argv, " ")
	if got != "rev-list --parents --max-count=1 abc" {
		t.Fatalf("argv = %q", got)
	}
}

func TestParentCountRealRepo(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}

	// root commit: 0 parents
	gitIn(t, dir, "commit", "--allow-empty", "-m", "root")
	if n, err := r.ParentCount(context.Background(), "HEAD"); err != nil || n != 0 {
		t.Fatalf("root ParentCount = %d, %v; want 0, nil", n, err)
	}
	// normal commit: 1 parent
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	if n, err := r.ParentCount(context.Background(), "HEAD"); err != nil || n != 1 {
		t.Fatalf("normal ParentCount = %d, %v; want 1, nil", n, err)
	}
	// merge commit: 2 parents
	gitIn(t, dir, "checkout", "-b", "topic", "HEAD~1")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "topic")
	gitIn(t, dir, "checkout", "-")           // back to the default branch
	gitIn(t, dir, "merge", "--no-ff", "topic", "-m", "merge topic")
	if n, err := r.ParentCount(context.Background(), "HEAD"); err != nil || n != 2 {
		t.Fatalf("merge ParentCount = %d, %v; want 2, nil", n, err)
	}
}

func TestFormatPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git format-patch", gitexec.Result{Stdout: "From ...\n"})
	r := &Repo{Runner: f}

	if _, err := r.FormatPatch(context.Background(), "abc123"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "format-patch -1 --binary --stdout abc123" {
		t.Fatalf("whole-commit argv = %q", got)
	}

	f2 := gitexec.NewFakeRunner()
	f2.SetResponse("git format-patch", gitexec.Result{Stdout: "From ...\n"})
	r2 := &Repo{Runner: f2}
	if _, err := r2.FormatPatch(context.Background(), "abc123", "dir/file.go"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f2.Calls[0].Argv, " "); got != "format-patch -1 --binary --stdout abc123 -- dir/file.go" {
		t.Fatalf("file-scoped argv = %q", got)
	}
}

func TestFormatPatchRealRepoScopesToPath(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "base")
	writeFileT(t, dir, "foo.go", "a\nb\nc\n")
	writeFileT(t, dir, "bar.txt", "x\n")
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add foo and bar")

	// whole-commit patch touches both files; a valid mailbox patch starts "From "
	whole, err := r.FormatPatch(context.Background(), "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(whole), "From ") {
		t.Fatalf("patch does not start with mailbox header: %q", string(whole)[:20])
	}
	if !strings.Contains(string(whole), "foo.go") || !strings.Contains(string(whole), "bar.txt") {
		t.Fatal("whole-commit patch should mention both files")
	}
	// path-scoped patch mentions only foo.go
	scoped, err := r.FormatPatch(context.Background(), "HEAD", "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(scoped), "foo.go") || strings.Contains(string(scoped), "bar.txt") {
		t.Fatalf("scoped patch should mention only foo.go:\n%s", scoped)
	}
}
```

If `writeFileT` does not already exist in the `git` test package, add this helper
at the top of `format_patch_test.go`:

```go
func writeFileT(t *testing.T, dir, rel, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, rel), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
```
(and add `"os"` + `"path/filepath"` imports). If a similar helper already exists
in the package's test files, use it and delete this one.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/git/ -run 'ParentCount|FormatPatch' -v`
Expected: FAIL — `r.ParentCount`/`r.FormatPatch` undefined.

- [ ] **Step 3: Write the verbs**

Create `internal/git/format_patch.go`:

```go
package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ParentCount returns how many parents rev has: 0 for the root commit, 1 for a
// normal commit, ≥2 for a merge. `git rev-list --parents --max-count=1 <rev>`
// prints "<rev> <p1> <p2>…"; the parent count is the field count minus one. One
// invocation. Used to refuse patch export of a merge commit (format-patch -1 on
// a merge silently emits a different commit's patch).
func (r *Repo) ParentCount(ctx context.Context, rev string) (int, error) {
	argv := gitcmd.New("rev-list").Arg("--parents", "--max-count=1", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list (parent count)", argv)
	if err != nil {
		return 0, err
	}
	fields := strings.Fields(strings.TrimSpace(res.Stdout))
	if len(fields) == 0 {
		return 0, fmt.Errorf("rev-list --parents %s: empty output", rev)
	}
	return len(fields) - 1, nil
}

// FormatPatch returns the mailbox-format patch for the single commit rev
// (`git format-patch -1 --binary --stdout <rev> [-- <paths…>]`). With paths the
// diff is scoped to those files while keeping the commit's From/Subject header;
// --binary keeps genuinely-binary changes appliable. The output is a git am-able
// patch. One invocation. Callers must reject merge commits first (see
// ParentCount): format-patch -1 skips a merge and emits the wrong commit.
func (r *Repo) FormatPatch(ctx context.Context, rev string, paths ...string) ([]byte, error) {
	b := gitcmd.New("format-patch").Arg("-1", "--binary", "--stdout", rev)
	if len(paths) > 0 {
		b = b.Arg("--")
		for _, p := range paths {
			b = b.Arg(p)
		}
	}
	res, err := r.Runner.Run(ctx, "git format-patch", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
```

> Note: confirm the exact `gitcmd` builder method names against an existing verb
> (e.g. `internal/git/show.go` uses `gitcmd.New("show").Arg(...).ToArgv()`). If
> `Arg` is variadic as shown there, the loop above can be `b.Arg(paths...)` after
> `b.Arg("--")`; keep whichever compiles.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'ParentCount|FormatPatch' -v`
Expected: PASS (all four tests).

- [ ] **Step 5: Commit**

```bash
git add internal/git/format_patch.go internal/git/format_patch_test.go
git commit -m "git: add FormatPatch and ParentCount verbs"
```

---

## Task 2: `engine.ExportFile` op

**Files:**
- Create: `internal/engine/export_file.go`
- Test: `internal/engine/export_file_test.go`

**Interfaces:**
- Consumes: `engine.OpDeps`, `engine.Result`, `repogate.Mode`, the shared
  `writeOverwrite`/`writeCancel` constants and `ErrWriteCancelled` (from
  `internal/engine/writefile.go`).
- Produces: `engine.ExportFile{ Path string; Data []byte }` — an `Operation` that
  writes `Data` to the absolute `Path` outside the working tree, asking to
  overwrite an existing *file* with different bytes.

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/export_file_test.go`:

```go
package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestExportFileWritesNestedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out", "a1b2c3d.patch")
	op := ExportFile{Path: path, Data: []byte("From abc\n")}
	res, err := op.Run(context.Background(), OpDeps{}) // absent → no decider needed
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed")
	}
	if got, _ := os.ReadFile(path); string(got) != "From abc\n" {
		t.Fatalf("content = %q", got)
	}
}

func TestExportFileIdenticalBytesNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("same"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("same")}
	res, err := op.Run(context.Background(), OpDeps{}) // identical → no decision asked
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if res.Changed {
		t.Fatal("identical bytes must be a no-op (Changed=false)")
	}
}

func TestExportFileExistingCancels(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("new")}
	_, err := op.Run(context.Background(), OpDeps{Decider: MapDecider{"overwrite": "cancel"}})
	if err != ErrWriteCancelled {
		t.Fatalf("err = %v, want ErrWriteCancelled", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "old" {
		t.Fatalf("cancel must leave the file untouched, got %q", got)
	}
}

func TestExportFileExistingOverwrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.patch")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	op := ExportFile{Path: path, Data: []byte("new")}
	res, err := op.Run(context.Background(), OpDeps{Decider: MapDecider{"overwrite": "overwrite"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected Changed after overwrite")
	}
	if got, _ := os.ReadFile(path); string(got) != "new" {
		t.Fatalf("content = %q, want new", got)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestExportFile -v`
Expected: FAIL — `ExportFile` undefined.

- [ ] **Step 3: Write the op**

Create `internal/engine/export_file.go`:

```go
package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/repogate"
)

// ExportFile writes Data to the absolute Path OUTSIDE the working tree (the
// export-a-patch primitive). It is the file-grained sibling of WriteFile: if
// Path already exists with different bytes it asks the Decider to overwrite or
// cancel; identical bytes are a silent no-op. Parent dirs are created. Writes via
// os directly (like ExportToDir), not deps.Repo.WriteWorktreeFile, because the
// destination is outside the repo. Read reservation: touches neither refs nor the
// working tree.
type ExportFile struct {
	Path string
	Data []byte
}

var _ Operation = ExportFile{}

func (op ExportFile) LockMode() repogate.Mode { return repogate.Read }

func (op ExportFile) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if existing, err := os.ReadFile(op.Path); err == nil {
		if bytes.Equal(existing, op.Data) {
			res := Result{Summary: "unchanged", Path: op.Path}
			deps.emit(ctx, Done{Result: res})
			return res, nil
		}
		choice, derr := deps.decide(ctx, DecisionRequest{
			ID:      "overwrite",
			Prompt:  "File exists: " + op.Path,
			Options: []string{writeOverwrite, writeCancel},
		})
		if derr != nil {
			return Result{}, derr
		}
		if choice.Option != writeOverwrite {
			return Result{}, ErrWriteCancelled
		}
	}
	if err := os.MkdirAll(filepath.Dir(op.Path), 0o755); err != nil {
		return Result{}, err
	}
	if err := os.WriteFile(op.Path, op.Data, 0o644); err != nil {
		return Result{}, err
	}
	res := Result{Summary: "wrote " + op.Path, Changed: true, Path: op.Path}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
```

> Note: `writeOverwrite`, `writeCancel`, and `ErrWriteCancelled` are declared in
> `internal/engine/writefile.go`; reuse them, don't redeclare. Confirm `Result`
> has a `Path` field (it does — `ExportToDir` sets it); if not, drop that field.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestExportFile -v`
Expected: PASS (all four).

- [ ] **Step 5: Commit**

```bash
git add internal/engine/export_file.go internal/engine/export_file_test.go
git commit -m "engine: add ExportFile op (write an absolute path outside the worktree)"
```

---

## Task 3: domain `CommitPatch` / `FilePatch` / `ExportDefaultDir` + merge guard

**Files:**
- Create: `internal/domain/patch.go`
- Test: `internal/domain/patch_test.go`
- Modify: `internal/domain/export.go` (refactor `commitDirName` to reuse `shortSHA`)

**Interfaces:**
- Consumes: `git.FormatPatch`, `git.ParentCount` (Task 1) via `s.repo`; the
  existing `query` Read-reservation helper; `s.Worktrees(ctx)`.
- Produces:
  - `var ErrMergeCommitPatch error`
  - `func (s *Service) CommitPatch(ctx context.Context, sha string) (patch []byte, defaultName string, err error)`
  - `func (s *Service) FilePatch(ctx context.Context, sha, path string) (patch []byte, defaultName string, err error)`
  - `func (s *Service) ExportDefaultDir(ctx context.Context) (string, error)`
  - `func shortSHA(sha string) string`

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/patch_test.go`:

```go
package domain

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitPatchAndFilePatch(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	gitRun(t, repoDir, "commit", "--allow-empty", "-m", "base")
	writeCommit(t, repoDir, "foo.go", "a\nb\nc\n", "add foo")
	// add a second file in the SAME commit so file-scoping is observable
	if err := os.WriteFile(filepath.Join(repoDir, "bar.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repoDir, "add", "bar.txt")
	gitRun(t, repoDir, "commit", "-m", "add bar")
	sha := headHash(t, repoDir)

	patch, name, err := svc.CommitPatch(ctx, sha)
	if err != nil {
		t.Fatalf("CommitPatch: %v", err)
	}
	if !strings.HasPrefix(string(patch), "From ") {
		t.Fatalf("not a mailbox patch: %q", string(patch)[:20])
	}
	if name != shortSHA(sha)+".patch" {
		t.Fatalf("commit defaultName = %q", name)
	}

	fpatch, fname, err := svc.FilePatch(ctx, sha, "bar.txt")
	if err != nil {
		t.Fatalf("FilePatch: %v", err)
	}
	if !strings.Contains(string(fpatch), "bar.txt") || strings.Contains(string(fpatch), "foo.go") {
		t.Fatalf("file patch should be scoped to bar.txt:\n%s", fpatch)
	}
	if fname != shortSHA(sha)+"-bar.txt.patch" {
		t.Fatalf("file defaultName = %q", fname)
	}
}

func TestCommitPatchRefusesMerge(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	writeCommit(t, repoDir, "a.txt", "1\n", "base")
	gitRun(t, repoDir, "checkout", "-b", "topic")
	writeCommit(t, repoDir, "a.txt", "2\n", "topic change")
	gitRun(t, repoDir, "checkout", "-")
	writeCommit(t, repoDir, "b.txt", "3\n", "main change")
	gitRun(t, repoDir, "merge", "--no-ff", "topic", "-m", "merge topic")
	mergeSHA := headHash(t, repoDir)

	if _, _, err := svc.CommitPatch(ctx, mergeSHA); !errors.Is(err, ErrMergeCommitPatch) {
		t.Fatalf("CommitPatch(merge) err = %v, want ErrMergeCommitPatch", err)
	}
	if _, _, err := svc.FilePatch(ctx, mergeSHA, "a.txt"); !errors.Is(err, ErrMergeCommitPatch) {
		t.Fatalf("FilePatch(merge) err = %v, want ErrMergeCommitPatch", err)
	}
}

func TestCommitPatchAmRoundTrip(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	ctx := context.Background()
	writeCommit(t, repoDir, "foo.go", "a\nb\nc\n", "base")
	writeCommit(t, repoDir, "foo.go", "a\nB\nc\n", "change foo")
	sha := headHash(t, repoDir)

	patch, _, err := svc.FilePatch(ctx, sha, "foo.go")
	if err != nil {
		t.Fatal(err)
	}
	// Apply onto a fresh repo seeded with the parent content.
	dst := t.TempDir()
	gitRun(t, dst, "init")
	gitRun(t, dst, "config", "user.email", "t@t")
	gitRun(t, dst, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dst, "foo.go"), []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dst, "add", "foo.go")
	gitRun(t, dst, "commit", "-m", "seed base")

	patchFile := filepath.Join(t.TempDir(), "p.patch")
	if err := os.WriteFile(patchFile, patch, 0o644); err != nil {
		t.Fatal(err)
	}
	am := exec.Command("git", "am", patchFile)
	am.Dir = dst
	am.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := am.CombinedOutput(); err != nil {
		t.Fatalf("git am failed: %v\n%s", err, out)
	}
	got, _ := os.ReadFile(filepath.Join(dst, "foo.go"))
	if string(got) != "a\nB\nc\n" {
		t.Fatalf("after am foo.go = %q, want a\\nB\\nc\\n", got)
	}
}

func TestExportDefaultDirIsParentOfRepo(t *testing.T) {
	repoDir, svc := newRealRepo(t)
	dir, err := svc.ExportDefaultDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Dir(filepath.Clean(repoDir)); dir != want {
		t.Fatalf("ExportDefaultDir = %q, want %q (parent of repo)", dir, want)
	}
}
```

> Note: `newRealRepo`, `gitRun`, `writeCommit`, and `headHash` already exist in
> the `domain` test package (see `internal/domain/export_test.go` and
> `compare_test.go`). If `newRealRepo` returns the git working-tree root as its
> first value, `ExportDefaultDir` must equal `filepath.Dir` of it. If that helper
> returns a symlinked temp path, compare with `filepath.Clean` on both sides (the
> test already does).

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/domain/ -run 'CommitPatch|FilePatch|ExportDefaultDir' -v`
Expected: FAIL — `svc.CommitPatch` / `ErrMergeCommitPatch` / `shortSHA` undefined.

- [ ] **Step 3: Write the domain layer**

Create `internal/domain/patch.go`:

```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrMergeCommitPatch is returned when a patch export targets a merge commit.
// git format-patch -1 does not error on a merge — it silently skips the merge
// and emits a DIFFERENT commit's patch — so callers must be refused up front.
var ErrMergeCommitPatch = errors.New("cannot export a merge commit as a patch")

// shortSHA abbreviates an object id to 7 chars for human-facing file names.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// CommitPatch returns the git am-able patch for sha's whole change set plus the
// default file name (<shortsha>.patch). Refuses a merge commit (ErrMergeCommitPatch).
func (s *Service) CommitPatch(ctx context.Context, sha string) ([]byte, string, error) {
	data, err := query(ctx, s, "commitpatch:"+sha, func(ctx context.Context) ([]byte, error) {
		if err := s.refuseMerge(ctx, sha); err != nil {
			return nil, err
		}
		return s.repo.FormatPatch(ctx, sha)
	})
	if err != nil {
		return nil, "", err
	}
	return data, shortSHA(sha) + ".patch", nil
}

// FilePatch returns the git am-able patch for a single file's change within sha
// plus the default file name (<shortsha>-<basename>.patch). Refuses a merge.
func (s *Service) FilePatch(ctx context.Context, sha, path string) ([]byte, string, error) {
	data, err := query(ctx, s, "filepatch:"+sha+":"+path, func(ctx context.Context) ([]byte, error) {
		if err := s.refuseMerge(ctx, sha); err != nil {
			return nil, err
		}
		return s.repo.FormatPatch(ctx, sha, path)
	})
	if err != nil {
		return nil, "", err
	}
	return data, shortSHA(sha) + "-" + filepath.Base(path) + ".patch", nil
}

// refuseMerge returns ErrMergeCommitPatch when sha has more than one parent.
func (s *Service) refuseMerge(ctx context.Context, sha string) error {
	n, err := s.repo.ParentCount(ctx, sha)
	if err != nil {
		return err
	}
	if n > 1 {
		return ErrMergeCommitPatch
	}
	return nil
}

// ExportDefaultDir is the default directory a patch export writes into: the
// parent of the MAIN worktree root (e.g. /a/x/repo -> /a/x), stable even from a
// linked worktree. Mirrors TempExportBase's main-worktree anchor.
func (s *Service) ExportDefaultDir(ctx context.Context) (string, error) {
	wts, err := s.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	if len(wts) == 0 || wts[0].Path == "" {
		return "", fmt.Errorf("export: no main worktree")
	}
	return filepath.Dir(filepath.Clean(wts[0].Path)), nil
}
```

- [ ] **Step 4: DRY — refactor `commitDirName` to reuse `shortSHA`**

In `internal/domain/export.go`, replace:

```go
func commitDirName(sha string) string {
	if len(sha) > 7 {
		sha = sha[:7]
	}
	return "commit-" + sha
}
```

with:

```go
func commitDirName(sha string) string {
	return "commit-" + shortSHA(sha)
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/domain/ -run 'CommitPatch|FilePatch|ExportDefaultDir' -v`
Then the whole package (the refactor touches `commitDirName`):
Run: `go test ./internal/domain/`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/domain/patch.go internal/domain/patch_test.go internal/domain/export.go
git commit -m "domain: CommitPatch/FilePatch/ExportDefaultDir with merge guard"
```

---

## Task 4: TUI — commit-level "Export commit as patch"

**Files:**
- Create: `internal/tui/export_patch.go`
- Test: `internal/tui/export_patch_test.go`
- Modify: `internal/tui/action_menu.go` (wire `commitExportPatchRow` into `appendCommitContextRows`)
- Modify: `internal/tui/model.go` (handle `patchResolvedMsg`)
- Modify: `internal/tui/source.go` (map `ExportFile` to no refresh)

**Interfaces:**
- Consumes: `domain.CommitPatch`, `domain.ExportDefaultDir`, `engine.ExportFile`;
  the existing `textfield`, `viewField`, `popupContentWidth`, `popupInnerWidth`,
  `modalStyle`, `overlayCenter`, `clipToHeight`, `overlayDims`, `pushLayer`,
  `popLayer`, `startOp`, `newTextField`, `backingIndex`, `opsIdle` helpers.
- Produces (used by Task 5):
  - `type patchResolvedMsg struct { data []byte; defaultPath string; err error }`
  - `type exportPatchPopup struct { dest textfield; data []byte }`
  - `func (m Model) startExportCommitPatch(sha string) (Model, tea.Cmd)`
  - `func (m Model) commitExportPatchRow() (actionRow, bool)`

- [ ] **Step 1: Write the failing test**

Create `internal/tui/export_patch_test.go`:

```go
package tui

import (
	"strings"
	"testing"
)

func TestExportPatchPopupEnterStartsExport(t *testing.T) {
	p := &exportPatchPopup{data: []byte("From abc\n")}
	p.dest = newTextField("/tmp/repo-parent/a1b2c3d.patch")
	// Rendering must show the prefilled path and the key hints.
	m := Model{}
	m.width, m.height = 100, 30
	out := p.render(m, "")
	if !strings.Contains(out, "a1b2c3d.patch") {
		t.Fatalf("popup should show the default path:\n%s", out)
	}
	if !strings.Contains(out, "[enter]") || !strings.Contains(out, "[esc]") {
		t.Fatalf("popup should show key hints:\n%s", out)
	}
}

func TestCommitExportPatchRowHiddenForMerge(t *testing.T) {
	// A merge commit (len(Parents) > 1) must not offer the row.
	m := Model{focus: panelCommits}
	m.commits = []model.Commit{{Hash: "deadbeef", Parents: []string{"p1", "p2"}}}
	// Point the commit selection at index 0. (Use whatever selection setter the
	// panel uses; see commitShelfRow for the backingIndex pattern.)
	m = selectCommitAt(m, 0)
	if _, ok := m.commitExportPatchRow(); ok {
		t.Fatal("merge commit must not offer Export commit as patch")
	}
}
```

> Note: `selectCommitAt` is a stand-in for however existing row tests set the
> Commits selection. Look at an existing `*_test.go` that exercises
> `commitShelfRow`/`backingIndex` (grep `backingIndex(panelCommits)` in tests) and
> mirror its selection setup; if there is a helper, use it and delete
> `selectCommitAt`. If no such helper exists, construct the model so
> `m.backingIndex(panelCommits)` returns `(0, true)` the same way those tests do.
> Add `"github.com/homeend/gigagit/internal/model"` to the imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run 'ExportPatchPopup|CommitExportPatchRow' -v`
Expected: FAIL — `exportPatchPopup` / `commitExportPatchRow` undefined.

- [ ] **Step 3: Write the popup, resolve command, and row**

Create `internal/tui/export_patch.go`:

```go
package tui

import (
	"context"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// patchResolvedMsg carries a resolved patch (bytes + default destination path)
// back to the UI thread so the editable-destination popup can open prefilled.
// err is set when generating the patch failed (including ErrMergeCommitPatch).
type patchResolvedMsg struct {
	data        []byte
	defaultPath string
	err         error
}

// startExportCommitPatch resolves the whole-commit patch + default path
// off-thread (ExportDefaultDir + CommitPatch), then delivers patchResolvedMsg.
func (m Model) startExportCommitPatch(sha string) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		data, name, err := svc.CommitPatch(ctx, sha)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		return patchResolvedMsg{data: data, defaultPath: filepath.Join(dir, name)}
	}
}

// exportPatchPopup is the editable-destination confirmation shown after a patch
// has been generated. dest is prefilled with <defaultDir>/<name>; enter runs
// engine.ExportFile with the (possibly edited) full path. Mirrors tempExportPopup.
type exportPatchPopup struct {
	dest textfield
	data []byte
}

func (p *exportPatchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		path := strings.TrimSpace(p.dest.Value())
		if path == "" || len(p.data) == 0 {
			return m, nil
		}
		data := p.data
		m = m.popLayer()
		return m.startOp(engine.ExportFile{Path: path, Data: data})
	default:
		p.dest.HandleEditKey(msg)
	}
	return m, nil
}

func (p *exportPatchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Export as patch\n\n")
	b.WriteString(viewField("path: ", p.dest, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] path  [enter] write  [esc] cancel")
	box := modalStyle.Width(popupInnerWidth(w)).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}

// commitExportPatchRow offers "Export commit as patch" on the Commits panel (and
// the commit-list side of a files view). Pre-hidden for merge commits: their
// patch would be wrong (domain refuses them, but hiding avoids a dead row).
func (m Model) commitExportPatchRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	if len(c.Parents) > 1 {
		return actionRow{}, false // merge: format-patch would emit the wrong commit
	}
	sha := c.Hash
	return actionRow{
		id:    "commit-export-patch",
		label: "Export commit as patch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startExportCommitPatch(sha)
		},
	}, true
}
```

- [ ] **Step 4: Handle `patchResolvedMsg` in `model.go`**

In `internal/tui/model.go`, next to the `tempExportResolvedMsg` case (around
line 436), add:

```go
	case patchResolvedMsg:
		if msg.err != nil {
			m.statusMsg = "export patch: " + msg.err.Error()
			return m, nil
		}
		p := &exportPatchPopup{data: msg.data}
		p.dest = newTextField(msg.defaultPath)
		return m.pushLayer(p), nil
```

- [ ] **Step 5: Wire the row into `appendCommitContextRows`**

In `internal/tui/action_menu.go`, immediately after the `commitShelfRow` block
(lines 214-216), add:

```go
	if r, ok := m.commitExportPatchRow(); ok {
		out = append(out, r)
	}
```

- [ ] **Step 6: Map `ExportFile` to no post-op refresh**

In `internal/tui/source.go`, inside `opAffectedSources`, add a case (group it with
`ExportToDir`, which also dirties nothing outside the tree):

```go
	case engine.ExportFile, engine.ExportToDir:
		return []sourceKey{} // writes outside the working tree; refresh nothing
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'ExportPatchPopup|CommitExportPatchRow' -v`
Then build: `go build ./cmd/gg`
Expected: PASS, build OK.

- [ ] **Step 8: Commit**

```bash
git add internal/tui/export_patch.go internal/tui/export_patch_test.go \
        internal/tui/action_menu.go internal/tui/model.go internal/tui/source.go
git commit -m "tui: Export commit as patch (Commits panel . menu)"
```

---

## Task 5: TUI — file-level "Export this file's diff as patch" (diff view)

**Files:**
- Create: adds to `internal/tui/export_patch.go`
- Test: adds to `internal/tui/export_patch_test.go`
- Modify: `internal/tui/action_menu.go` (wire into the `inContentWindow` branch)

**Interfaces:**
- Consumes: `patchResolvedMsg`, `exportPatchPopup` (Task 4); `domain.FilePatch`,
  `domain.ExportDefaultDir`; `m.diffLayer()`, `m.inCompareMode()`, the `diffView`
  fields `rev` and `title`.
- Produces:
  - `func (m Model) startExportFilePatch(sha, path string) (Model, tea.Cmd)`
  - `func (m Model) exportFilePatchRow() (actionRow, bool)`

- [ ] **Step 1: Write the failing test**

Add to `internal/tui/export_patch_test.go`:

```go
func TestExportFilePatchRowOnlyForCommitDiff(t *testing.T) {
	// A commit-vs-parent file diff (rev set, not compare mode) offers the row.
	m := Model{}
	m.filesMode = filesModeCommit
	dv := &diffView{title: "src/foo.go", rev: "abc123"}
	m = m.pushLayer(dv) // however diffView is installed; see openDiffForFileLine
	if _, ok := m.exportFilePatchRow(); !ok {
		t.Fatal("commit file diff should offer Export this file's diff as patch")
	}
	// Compare-mode diff (rev set but comparing two endpoints) must NOT offer it.
	m2 := Model{}
	m2.filesMode = filesModeCompare
	dv2 := &diffView{title: "src/foo.go", rev: "abc123"}
	m2 = m2.pushLayer(dv2)
	if _, ok := m2.exportFilePatchRow(); ok {
		t.Fatal("compare-mode diff must not offer file patch export")
	}
	// A working-tree diff (rev == "") must NOT offer it.
	m3 := Model{}
	dv3 := &diffView{title: "src/foo.go", rev: ""}
	m3 = m3.pushLayer(dv3)
	if _, ok := m3.exportFilePatchRow(); ok {
		t.Fatal("working-tree diff must not offer file patch export")
	}
}
```

> Note: `filesModeCommit` and `filesModeCompare` are declared in
> `internal/tui/files_view.go`. `m.diffLayer()` must return `dv` after it is
> installed — confirm how the diff view is set (it is a Model field / layer;
> `openDiffForFileLine` in `files_view.go:545` shows the exact construction). If
> `pushLayer` is not how the diff view is stored, set `m` so `m.diffLayer()`
> returns `dv` (assign the field the same way `openDiffForFileLine` does). Also
> ensure `m.opsIdle()` is true for a zero-value Model (it should be — no running
> op); if the row gates on `opsIdle()` and the zero Model isn't idle, set whatever
> field makes it idle, mirroring existing diff-view row tests.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/tui/ -run TestExportFilePatchRowOnlyForCommitDiff -v`
Expected: FAIL — `exportFilePatchRow` undefined.

- [ ] **Step 3: Add the resolve command and the row**

Append to `internal/tui/export_patch.go`:

```go
// startExportFilePatch resolves a single file's patch within sha off-thread
// (ExportDefaultDir + FilePatch), then delivers patchResolvedMsg (reused).
func (m Model) startExportFilePatch(sha, path string) (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		ctx := context.Background()
		data, name, err := svc.FilePatch(ctx, sha, path)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		dir, err := svc.ExportDefaultDir(ctx)
		if err != nil {
			return patchResolvedMsg{err: err}
		}
		return patchResolvedMsg{data: data, defaultPath: filepath.Join(dir, name)}
	}
}

// exportFilePatchRow offers "Export this file's diff as patch" inside the diff
// view, but ONLY when it shows a commit-vs-parent file diff: dv.rev is the
// commit, and NOT compare mode (compare-mode diffs also set dv.rev, but the patch
// would be commit-vs-parent, not the compared endpoints). A merge dv.rev is
// caught by the domain guard (surfaced as a status message).
func (m Model) exportFilePatchRow() (actionRow, bool) {
	if !m.opsIdle() {
		return actionRow{}, false
	}
	dv := m.diffLayer()
	if dv == nil || dv.rev == "" || m.inCompareMode() {
		return actionRow{}, false
	}
	sha, path := dv.rev, dv.title
	return actionRow{
		id:    "file-export-patch",
		label: "Export this file's diff as patch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startExportFilePatch(sha, path)
		},
	}, true
}
```

- [ ] **Step 4: Wire the row into the `inContentWindow` branch**

In `internal/tui/action_menu.go`, inside `availableActions`, in the
`if m.inContentWindow()` branch, after the `onStackFile`/`frontIsFilesView`
if-blocks and before the `shelfAddRow` block (around line 69), add:

```go
		if r, ok := m.exportFilePatchRow(); ok {
			rows = append(rows, r)
		}
```

(The row self-gates to a commit file diff being front, so its position among the
surface rows is safe.)

- [ ] **Step 5: Run the tests to verify they pass**

Run: `go test ./internal/tui/ -run 'ExportPatch|ExportFilePatchRow|CommitExportPatchRow' -v`
Then build: `go build ./cmd/gg`
Expected: PASS, build OK.

- [ ] **Step 6: Commit**

```bash
git add internal/tui/export_patch.go internal/tui/export_patch_test.go internal/tui/action_menu.go
git commit -m "tui: Export this file's diff as patch (diff view . menu)"
```

---

## Task 6: TUI — advertise the keys in help

**Files:**
- Modify: `internal/tui/help.go`

**Interfaces:**
- Consumes: the two new `.`-menu actions (Tasks 4–5). No new symbols produced.

- [ ] **Step 1: Locate the Commits and diff-view help sections**

Run: `grep -n "Shelf this commit\|Bookmark this commit\|Commits\|diff view\|\\. menu" internal/tui/help.go`
Read the surrounding lines to match the existing entry format (the file lists
`.`-menu actions per surface).

- [ ] **Step 2: Add the two help lines**

In the Commits-panel section, next to the "Shelf this commit" / "Bookmark this
commit" entries, add a line describing **"Export commit as patch"** via the `.`
menu. In the diff-view section, add a line for **"Export this file's diff as
patch"** via the `.` menu. Match the exact column/format of the neighboring
entries in that file (copy an adjacent line and edit its text — do not invent a
new format).

- [ ] **Step 3: Build and eyeball**

Run: `go build ./cmd/gg`
Expected: build OK. (No unit test asserts help text; the neighboring entries are
the format oracle.)

- [ ] **Step 4: Commit**

```bash
git add internal/tui/help.go
git commit -m "tui: advertise Export-as-patch actions in help"
```

---

## Task 7: CLI — `gg commit export-patch`

**Files:**
- Create: `internal/cli/commit_export_patch.go`
- Modify: `internal/cli/cli.go` (dispatch under `cmdCommit`)
- Test: `internal/cli/commit_export_patch_test.go`

**Interfaces:**
- Consumes: `domain.CommitPatch`, `domain.FilePatch`, `domain.ExportDefaultDir`,
  `engine.ExportFile`, `engine.ErrWriteCancelled`; the CLI helpers `runOperation`,
  `finish`, `cliDecider{policy}`.
- Produces: `func cmdCommitExportPatch(svc *domain.Service, args []string, stdout, stderr io.Writer) int`

- [ ] **Step 1: Write the failing test**

Create `internal/cli/commit_export_patch_test.go`:

```go
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitExportPatchWritesFile(t *testing.T) {
	repoDir, svc := newRealRepoCLI(t) // see note below
	writeCommitCLI(t, repoDir, "foo.go", "a\nb\nc\n", "base")
	writeCommitCLI(t, repoDir, "foo.go", "a\nB\nc\n", "change foo")
	sha := headHashCLI(t, repoDir)

	out := filepath.Join(t.TempDir(), "my.patch")
	var so, se bytes.Buffer
	code := cmdCommitExportPatch(svc, []string{sha, "--out", out}, &so, &se)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, se.String())
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if !strings.HasPrefix(string(b), "From ") {
		t.Fatalf("not a mailbox patch: %q", string(b)[:20])
	}
}

func TestCommitExportPatchRefusesMerge(t *testing.T) {
	repoDir, svc := newRealRepoCLI(t)
	writeCommitCLI(t, repoDir, "a.txt", "1\n", "base")
	runGitCLI(t, repoDir, "checkout", "-b", "topic")
	writeCommitCLI(t, repoDir, "a.txt", "2\n", "topic")
	runGitCLI(t, repoDir, "checkout", "-")
	writeCommitCLI(t, repoDir, "b.txt", "3\n", "main")
	runGitCLI(t, repoDir, "merge", "--no-ff", "topic", "-m", "merge")
	sha := headHashCLI(t, repoDir)

	var so, se bytes.Buffer
	code := cmdCommitExportPatch(svc, []string{sha, "--out", filepath.Join(t.TempDir(), "x.patch")}, &so, &se)
	if code == 0 {
		t.Fatal("merge export should exit non-zero")
	}
	if !strings.Contains(se.String(), "merge commit") {
		t.Fatalf("stderr should explain the merge refusal: %q", se.String())
	}
}
```

> Note: reuse whatever the CLI test package already uses to build a `*domain.Service`
> over a real repo. Grep the CLI tests for an existing helper (e.g. how
> `shelf_test.go` / `compare_test.go` in `internal/cli` construct a service + repo)
> and use it; the `newRealRepoCLI`/`writeCommitCLI`/`headHashCLI`/`runGitCLI` names
> above are placeholders for those existing helpers. If the CLI package has no such
> helper, prefer adding an **e2e scenario** instead (see the writing-e2e-scenarios
> skill) and drop this unit test.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/cli/ -run TestCommitExportPatch -v`
Expected: FAIL — `cmdCommitExportPatch` undefined.

- [ ] **Step 3: Write the command**

Create `internal/cli/commit_export_patch.go`:

```go
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdCommitExportPatch implements `gg commit export-patch <sha> [--out <path>]
// [--force] [-- <file>]`. Without a file it exports the whole commit; with a file
// it exports just that file's change. Merge commits are refused.
func cmdCommitExportPatch(svc *domain.Service, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("commit export-patch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	out := fs.String("out", "", "output path (default: <repo-parent>/<name>.patch)")
	force := fs.Bool("force", false, "overwrite an existing file")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	// Optional file scope after a `--` separator: <sha> [-- <file>].
	sha, file := "", ""
	for i := 0; i < len(rest); i++ {
		if rest[i] == "--" {
			if i+1 < len(rest) {
				file = rest[i+1]
			}
			break
		}
		if sha == "" {
			sha = rest[i]
		} else {
			fmt.Fprintln(stderr, "commit export-patch: too many arguments")
			return 2
		}
	}
	if sha == "" {
		fmt.Fprintln(stderr, "usage: gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]")
		return 2
	}

	ctx := context.Background()
	var (
		data []byte
		name string
		err  error
	)
	if file == "" {
		data, name, err = svc.CommitPatch(ctx, sha)
	} else {
		data, name, err = svc.FilePatch(ctx, sha, file)
	}
	if err != nil {
		fmt.Fprintf(stderr, "commit export-patch: %v\n", err)
		return 1
	}

	target := *out
	if target == "" {
		dir, derr := svc.ExportDefaultDir(ctx)
		if derr != nil {
			fmt.Fprintf(stderr, "commit export-patch: %v\n", derr)
			return 1
		}
		target = filepath.Join(dir, name)
	}

	// Answer the engine's Overwrite/Cancel fork from policy: --force = overwrite,
	// otherwise cancel (an existing file then refuses).
	policy := map[string]string{"overwrite": "cancel"}
	if *force {
		policy["overwrite"] = "overwrite"
	}
	dec := cliDecider{policy: policy}
	res, err := runOperation(ctx, svc, engine.ExportFile{Path: target, Data: data}, dec, stderr)
	if errors.Is(err, engine.ErrWriteCancelled) {
		fmt.Fprintf(stderr, "commit export-patch: %s already exists; pass --force to overwrite\n", target)
		return 2
	}
	if err == nil && res.Changed {
		fmt.Fprintf(stdout, "wrote %s\n", target)
	}
	return finish(res, err, stdout, stderr)
}
```

> Note: match `runOperation`'s and `finish`'s exact signatures from
> `internal/cli/commit_reword.go` and `internal/cli/shelf.go`. If `runOperation`
> takes a `stdin`/decider in a different position, adjust. The `cliDecider{policy}`
> shape is copied verbatim from `shelfExport`.

- [ ] **Step 4: Dispatch it under `cmdCommit`**

In `internal/cli/cli.go`, in `cmdCommit` (right after the existing `reword`
sub-dispatch at line 153), add:

```go
	if len(args) > 0 && args[0] == "export-patch" {
		return cmdCommitExportPatch(svc, args[1:], stdout, stderr)
	}
```

- [ ] **Step 5: Run the tests + build**

Run: `go test ./internal/cli/ -run TestCommitExportPatch -v`
Run: `go build ./cmd/gg`
Expected: PASS, build OK.

- [ ] **Step 6: Commit**

```bash
git add internal/cli/commit_export_patch.go internal/cli/cli.go internal/cli/commit_export_patch_test.go
git commit -m "cli: gg commit export-patch (whole-commit + file-scoped)"
```

---

## Task 8: Docs + agent skill

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `CLAUDE.md`,
  `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go`

**Interfaces:** none (documentation).

- [ ] **Step 1: CHANGELOG**

Add an entry under the current unreleased section describing: export a commit —
or a single file's change within a commit — as a `git format-patch` file
(`.patch`, `git am`-able) via the Commits panel `.` menu, the diff-view `.` menu,
and `gg commit export-patch`; default destination is the parent of the repo;
merge commits are refused.

- [ ] **Step 2: README**

In the user-facing feature/CLI list, add: the two `.`-menu actions and the
`gg commit export-patch <sha> [--out <path>] [--force] [-- <file>]` command.

- [ ] **Step 3: CLAUDE.md**

Update the package map: `git.FormatPatch`/`git.ParentCount` verbs (git row);
`engine.ExportFile` (engine row — note it is the file-grained sibling of
`WriteFile` and the second op to write outside the working tree, after
`ExportToDir`); domain `CommitPatch`/`FilePatch`/`ExportDefaultDir` +
`ErrMergeCommitPatch` (domain row); and the merge-guard rationale.

- [ ] **Step 4: Agent skill + version bump**

In `internal/agentskill/using-gg.md`, add a line documenting
`gg commit export-patch <commit> [--out <path>] [--force] [-- <file>]`. Then bump
`agentskill.Version` in `internal/agentskill/agentskill.go` (find the current
value: `grep -n "Version" internal/agentskill/agentskill.go`).

- [ ] **Step 5: Verify the agentskill embed test**

Run: `go test ./internal/agentskill/`
Expected: PASS (the embedded copy + version marker stay consistent).

- [ ] **Step 6: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/using-gg.md internal/agentskill/agentskill.go
git commit -m "docs: export commit/file as a git patch (CHANGELOG/README/CLAUDE/agentskill)"
```

---

## Final verification (before requesting merge)

- [ ] **Full race gate**

Run: `./test.sh race`
Expected: vet+gofmt clean, all unit tests + e2e pass with `-race`.

- [ ] **Manual smoke (optional but recommended)**

Build: `go build -o ./gg ./cmd/gg`
- In the TUI: Commits panel → `.` → "Export commit as patch" → edit path → enter;
  confirm `<parent>/<shortsha>.patch` exists and `git am`-applies elsewhere.
- Drill into a commit's file → open diff → `.` → "Export this file's diff as
  patch"; confirm scoped patch.
- Try it on a merge commit: the row is absent in the Commits panel; via CLI
  `gg commit export-patch <merge-sha>` prints the merge refusal and exits non-zero.

---

## Self-Review (completed by plan author)

**Spec coverage:**
- format-patch format + `--binary` → Task 1 (verb), asserted in Task 3 (`git am`).
- File-scoped patch keeps header → Task 1 real-repo test + Task 3.
- v1 scope = commit-vs-parent only → Task 5 gate `dv.rev != "" && !inCompareMode()`.
- Default = parent of main worktree → Task 3 `ExportDefaultDir` + test.
- Editable full-path dialog → Task 4 `exportPatchPopup`.
- Merge guard (load-bearing) → Task 3 (`ErrMergeCommitPatch`) + Task 4 pre-hide +
  Task 7 CLI refusal; tested in Tasks 3 & 7.
- `engine.ExportFile` (not `ExportToDir`; file-keyed overwrite; identical=no-op) → Task 2.
- No post-op refresh → Task 4 Step 6 (`opAffectedSources`).
- CLI parity → Task 7.
- Docs + agentskill version bump → Task 8.

**Placeholder scan:** The only deliberately-deferred bits are references to
existing test helpers whose exact names must be confirmed at execution time
(`selectCommitAt`, `newRealRepoCLI`, `writeFileT`), each flagged with a Note and a
grep to find the real helper. All production code is complete.

**Type consistency:** `patchResolvedMsg{data, defaultPath, err}` and
`exportPatchPopup{dest, data}` are defined in Task 4 and reused unchanged in
Tasks 5 & 7. `CommitPatch`/`FilePatch` return `([]byte, string, error)`
consistently across domain, TUI, and CLI. `ExportFile{Path, Data}` and
`ErrWriteCancelled` are consistent across engine, TUI, CLI.
