# Import / Apply Patch Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg apply <patch-file>` (CLI) and a command-palette "Apply patch…" entry (TUI) that apply a patch file to the repo — working-tree mode (`git apply --3way`) or recreate-commits mode (`git am --3way`, atomic with rollback).

**Architecture:** New thin verbs in `internal/git` (one invocation each), one new `engine.ApplyPatch` operation with a mode decision for mailbox patches, a `gg apply` CLI verb routed through `runOne`, and a TUI palette entry + editable-path popup. No new domain queries (reuses `ExportDefaultDir`).

**Tech Stack:** Go 1.26, Bubble Tea (TUI), real-git tests in `t.TempDir()`, `gitexec.FakeRunner` for argv asserts.

**Spec:** `docs/superpowers/specs/2026-07-07-apply-patch-design.md` (same branch — read it first).

## Global Constraints

- A git verb is ONE invocation, argv built with `gitcmd`, run via `r.Runner.Run`.
- Operations never block on a human: forks go through `deps.decide` with option lists.
- Frontends run ops via `domain.Execute` (TUI: `m.startOp`; CLI: `runOperation`) — never assemble `OpDeps` directly.
- `internal/tui` and `internal/cli` never import `internal/git` (archtest-enforced).
- gg does NOT model a paused `git am`: `git.PausedOpIn` must keep returning `""` for a bare `rebase-apply` dir (existing test `TestPausedOpIn/"git-am is not modeled"` must keep passing).
- Work happens in the worktree `/mnt/t/others/gigagit/.claude/worktrees/apply-patch` on branch `feat/apply-patch`. All file paths below are relative to that worktree root. Use ABSOLUTE paths (worktree-prefixed) with Write/Edit tools.
- Run tests from the worktree root: `cd /mnt/t/others/gigagit/.claude/worktrees/apply-patch` first.
- TDD: write the failing test, watch it fail, implement, watch it pass, commit.

---

### Task 1: `internal/git` verbs — ApplyPatch, AmMailbox, AmAbort, AmInProgress, IsMailboxPatch

**Files:**
- Create: `internal/git/apply.go`
- Create: `internal/git/apply_test.go`

**Interfaces:**
- Consumes: `gitcmd.New(...).ArgIf(cond, ...).Arg(...)`, `r.Runner.Run`, `r.GitDir(ctx)` (exists in `internal/git/worktree.go:75`).
- Produces (Task 2 wires these into `engine.GitOps`):
  - `func (r *Repo) ApplyPatch(ctx context.Context, path string, threeWay bool) error`
  - `func (r *Repo) AmMailbox(ctx context.Context, path string, threeWay bool) error`
  - `func (r *Repo) AmAbort(ctx context.Context) error`
  - `func (r *Repo) AmInProgress(ctx context.Context) (bool, error)`
  - `func IsMailboxPatch(data []byte) bool` (package func, no Repo)

- [ ] **Step 1: Write the failing tests**

Create `internal/git/apply_test.go`:

```go
package git

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestIsMailboxPatch(t *testing.T) {
	cases := []struct {
		name string
		data string
		want bool
	}{
		{"format-patch head", "From 3f2a1b0c4d5e6f708192a3b4c5d6e7f801234567 Mon Sep 17 00:00:00 2001\nFrom: A U Thor <a@t>\n", true},
		{"plain git diff", "diff --git a/foo.go b/foo.go\nindex 000..111 100644\n", false},
		{"unified diff", "--- a/foo.go\n+++ b/foo.go\n@@ -1 +1 @@\n", false},
		{"leading blank lines", "\n\nFrom 3f2a1b0c Mon Sep 17 00:00:00 2001\n", true},
		{"empty", "", false},
		{"From: header alone is not the mbox sentinel", "From: A U Thor <a@t>\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IsMailboxPatch([]byte(c.data)); got != c.want {
				t.Fatalf("IsMailboxPatch = %v, want %v", got, c.want)
			}
		})
	}
}

func TestApplyPatchArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git apply", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.ApplyPatch(context.Background(), "/tmp/x.patch", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "apply --3way /tmp/x.patch" {
		t.Fatalf("argv = %q", got)
	}

	f2 := gitexec.NewFakeRunner()
	f2.SetResponse("git apply", gitexec.Result{})
	r2 := &Repo{Runner: f2}
	if err := r2.ApplyPatch(context.Background(), "/tmp/x.patch", false); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f2.Calls[0].Argv, " "); got != "apply /tmp/x.patch" {
		t.Fatalf("no-3way argv = %q", got)
	}
}

func TestAmMailboxAndAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git am", gitexec.Result{})
	f.SetResponse("git am --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.AmMailbox(context.Background(), "/tmp/x.patch", true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[0].Argv, " "); got != "am --3way /tmp/x.patch" {
		t.Fatalf("am argv = %q", got)
	}
	if err := r.AmAbort(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(f.Calls[1].Argv, " "); got != "am --abort" {
		t.Fatalf("abort argv = %q", got)
	}
}

// AmInProgress: false on a clean repo; true once rebase-apply/applying exists
// (the am marker); false for a bare rebase-apply dir (that shape belongs to a
// paused rebase using the apply backend — aborting am there would abort the
// user's REBASE).
func TestAmInProgress(t *testing.T) {
	dir, runner := newTestRepo(t)
	r := &Repo{Runner: runner}
	ctx := context.Background()

	if in, err := r.AmInProgress(ctx); err != nil || in {
		t.Fatalf("clean repo AmInProgress = %v, %v; want false, nil", in, err)
	}

	gd, err := r.GitDir(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mkdir(t, gd, "rebase-apply")
	if in, err := r.AmInProgress(ctx); err != nil || in {
		t.Fatalf("bare rebase-apply AmInProgress = %v, %v; want false, nil (rebase owns it)", in, err)
	}
	touch(t, gd, "rebase-apply", "applying")
	if in, err := r.AmInProgress(ctx); err != nil || !in {
		t.Fatalf("with applying marker AmInProgress = %v, %v; want true, nil", in, err)
	}
	_ = dir
}
```

Note: `newTestRepo(t)`, `gitIn`, `mkdir`, `touch` are existing helpers in this package (see `internal/git/conflict_test.go` for `mkdir`/`touch`, `internal/git/format_patch_test.go` for `newTestRepo`). If `mkdir`/`touch` take different parameters than `(t, dir, parts...)`, match their actual signatures — read `conflict_test.go:181-215` first.

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd /mnt/t/others/gigagit/.claude/worktrees/apply-patch && go test ./internal/git/ -run 'TestIsMailboxPatch|TestApplyPatchArgv|TestAmMailboxAndAbortArgv|TestAmInProgress' 2>&1 | tail -5`
Expected: compile error — `undefined: IsMailboxPatch`, `r.ApplyPatch undefined`, etc.

- [ ] **Step 3: Write the implementation**

Create `internal/git/apply.go`:

```go
package git

import (
	"bytes"
	"context"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// IsMailboxPatch reports whether data (the head of a patch file) is a git
// format-patch mailbox: its first non-empty line starts with "From " — the
// mbox From_ sentinel git mailsplit keys on. A plain `git diff` starts with
// "diff --git" (other unified diffs with "---") and is not a mailbox. Pure;
// no git invocation.
func IsMailboxPatch(data []byte) bool {
	for _, line := range bytes.Split(data, []byte("\n")) {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		return bytes.HasPrefix(line, []byte("From "))
	}
	return false
}

// ApplyPatch applies the unified diff at path to the working tree
// (`git apply [--3way] <path>`; no --index/--cached, so changes land
// unstaged). With threeWay a hunk that misses falls back to a 3-way merge and
// may leave conflict markers + unmerged index entries — git exits non-zero in
// that case TOO, so the error alone cannot distinguish applied-with-conflicts
// from applied-nothing; callers probe status (see engine.ApplyPatch). One
// invocation.
func (r *Repo) ApplyPatch(ctx context.Context, path string, threeWay bool) error {
	b := gitcmd.New("apply").ArgIf(threeWay, "--3way").Arg(path)
	_, err := r.Runner.Run(ctx, "git apply", b.ToArgv())
	return err
}

// AmMailbox applies a format-patch mailbox as real commits
// (`git am [--3way] <path>`), preserving each patch's author, date, and
// message. On conflict git am stops mid-way with rebase-apply/ state on disk;
// callers roll back via AmAbort (engine.ApplyPatch keeps am atomic — gg does
// not model a paused am). One invocation.
func (r *Repo) AmMailbox(ctx context.Context, path string, threeWay bool) error {
	b := gitcmd.New("am").ArgIf(threeWay, "--3way").Arg(path)
	_, err := r.Runner.Run(ctx, "git am", b.ToArgv())
	return err
}

// AmAbort rolls back an in-progress git am (`git am --abort`), restoring the
// branch to its pre-am state — including commits already made from earlier
// patches in a multi-patch mailbox. One invocation.
func (r *Repo) AmAbort(ctx context.Context) error {
	_, err := r.Runner.Run(ctx, "git am --abort", gitcmd.New("am").Arg("--abort").ToArgv())
	return err
}

// AmInProgress reports whether a git am is mid-flight: rebase-apply/applying
// exists — the am-specific marker. A paused REBASE on the apply backend has
// rebase-apply/rebasing instead, and a bare rebase-apply dir belongs to it,
// NOT to am: this guard is what keeps engine.ApplyPatch's rollback from
// running `git am --abort` on top of (and thereby aborting) a user's paused
// rebase. One git invocation (GitDir) + one stat. Deliberately NOT part of
// PausedOpIn — gg does not model paused am.
func (r *Repo) AmInProgress(ctx context.Context) (bool, error) {
	dir, err := r.GitDir(ctx)
	if err != nil {
		return false, err
	}
	_, statErr := os.Stat(filepath.Join(dir, "rebase-apply", "applying"))
	return statErr == nil, nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/git/ -run 'TestIsMailboxPatch|TestApplyPatchArgv|TestAmMailboxAndAbortArgv|TestAmInProgress' -v 2>&1 | tail -12`
Expected: all PASS.

Also run the whole package (the PausedOpIn "am is not modeled" test must still pass): `go test ./internal/git/ 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**

```bash
git add internal/git/apply.go internal/git/apply_test.go
git commit -m "feat(git): apply/am patch verbs + mailbox sniff

ApplyPatch (git apply --3way), AmMailbox (git am --3way), AmAbort,
AmInProgress (rebase-apply/applying marker — distinguishes a mid-flight
am from a paused rebase owning rebase-apply/), IsMailboxPatch."
```

---

### Task 2: `engine.ApplyPatch` operation

**Files:**
- Create: `internal/engine/apply_patch.go`
- Create: `internal/engine/apply_patch_test.go`
- Modify: `internal/engine/gitops.go` (add 4 methods to the `GitOps` interface)

**Interfaces:**
- Consumes (Task 1): `deps.Repo.ApplyPatch/AmMailbox/AmAbort/AmInProgress`, `git.IsMailboxPatch`; existing `deps.Repo.Status`, `deps.Repo.CommitLine`, `deps.decide`, `deps.emit`.
- Produces (Tasks 3–4 use these):
  - `engine.ApplyPatch{Path string, Mode ApplyPatchMode}` operation
  - `engine.ApplyModeAuto / ApplyModeWorkingTree / ApplyModeCommits`
  - `engine.ApplyModeDecisionID = "apply_patch.mode"`, options `"working-tree"` / `"commits"`
  - `engine.ErrNotMailbox`, `engine.ErrApplyCancelled`

- [ ] **Step 1: Add the verbs to the GitOps interface**

In `internal/engine/gitops.go`, after the `StageBlob` line (line 120), before `MergeContinue`, add:

```go
	ApplyPatch(ctx context.Context, path string, threeWay bool) error
	AmMailbox(ctx context.Context, path string, threeWay bool) error
	AmAbort(ctx context.Context) error
	AmInProgress(ctx context.Context) (bool, error)
```

Run: `go build ./...`
Expected: compiles (the `var _ GitOps = (*git.Repo)(nil)` proof passes because Task 1 added the methods).

- [ ] **Step 2: Write the failing tests**

Create `internal/engine/apply_patch_test.go`:

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
)

// gitOut runs git in dir and returns trimmed stdout (test helper).
func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeCommit writes content to name and commits it, returning the new HEAD sha.
func writeCommit(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitOut(t, dir, "add", name)
	gitOut(t, dir, "commit", "-m", msg)
	return gitOut(t, dir, "rev-parse", "HEAD")
}

// mailboxFor exports rev as a format-patch mailbox file and returns its path.
func mailboxFor(t *testing.T, dir, rev string) string {
	t.Helper()
	data := gitOut(t, dir, "format-patch", "-1", "--binary", "--stdout", rev)
	p := filepath.Join(t.TempDir(), "x.patch")
	if err := os.WriteFile(p, []byte(data+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestApplyPatchCommitsRoundTrip: export a commit, rewind, re-apply with
// ApplyModeCommits — the commit is recreated with its message preserved.
func TestApplyPatchCommitsRoundTrip(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply --am: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if subj := gitOut(t, dir, "log", "-1", "--format=%s"); subj != "extend foo" {
		t.Fatalf("HEAD subject = %q, want the recreated commit", subj)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "foo.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("foo.txt = %q", got)
	}
}

// TestApplyPatchWorkingTreeClean: the same patch in working-tree mode lands
// unstaged, no new commit.
func TestApplyPatchWorkingTreeClean(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")
	before := gitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply working: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree mode must not commit")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "foo.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("foo.txt = %q", got)
	}
	if staged := gitOut(t, dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("nothing should be staged, got %q", staged)
	}
}

// TestApplyPatchAmConflictAtomic: a conflicting mailbox in Commits mode rolls
// back completely — HEAD unchanged, no rebase-apply left, worktree clean.
func TestApplyPatchAmConflictAtomic(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "base\n", "base")
	sha := writeCommit(t, dir, "foo.txt", "patched\n", "patch side")
	patch := mailboxFor(t, dir, sha)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")
	// Diverge so the patch conflicts and 3-way cannot resolve it.
	writeCommit(t, dir, "foo.txt", "conflicting local\n", "local side")
	before := gitOut(t, dir, "rev-parse", "HEAD")

	_, err := ApplyPatch{Path: patch, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("conflicting am should error")
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD must be unchanged after am rollback")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "rebase-apply")); statErr == nil {
		t.Fatal("rebase-apply must be cleaned up (am --abort ran)")
	}
	if st := gitOut(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree must be clean after rollback, got %q", st)
	}
}

// TestApplyPatchWorkingTreeConflict: working-tree mode on a conflicting patch
// leaves 3-way conflict markers + unmerged entries, returns Result{Changed}
// AND an error (the SmartMerge keep-conflicts shape); HEAD unchanged.
func TestApplyPatchWorkingTreeConflict(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "base\n", "base")
	sha := writeCommit(t, dir, "foo.txt", "patched\n", "patch side")
	patch := mailboxFor(t, dir, sha)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")
	writeCommit(t, dir, "foo.txt", "conflicting local\n", "local side")
	before := gitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("conflicting apply should error (keep-conflicts shape)")
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed:true alongside the error", res)
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD must be unchanged")
	}
	if unmerged := gitOut(t, dir, "diff", "--name-only", "--diff-filter=U"); unmerged != "foo.txt" {
		t.Fatalf("unmerged = %q, want foo.txt", unmerged)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "foo.txt"))
	if !strings.Contains(string(content), "<<<<<<<") {
		t.Fatalf("expected conflict markers, got %q", content)
	}
}

// TestApplyPatchAutoMailboxDecision: Auto + mailbox forks via
// apply_patch.mode; each answer routes to its mode; an unknown answer cancels.
func TestApplyPatchAutoMailboxDecision(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")

	// commits answer → real commit recreated
	if _, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "commits"}}); err != nil {
		t.Fatalf("auto→commits: %v", err)
	}
	if subj := gitOut(t, dir, "log", "-1", "--format=%s"); subj != "extend foo" {
		t.Fatalf("HEAD subject = %q", subj)
	}

	// working-tree answer → no commit
	gitOut(t, dir, "reset", "--hard", "HEAD~1")
	before := gitOut(t, dir, "rev-parse", "HEAD")
	if _, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "working-tree"}}); err != nil {
		t.Fatalf("auto→working-tree: %v", err)
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree answer must not commit")
	}

	// unknown answer → cancelled, nothing ran
	gitOut(t, dir, "reset", "--hard", "HEAD")
	gitOut(t, dir, "checkout", "--", ".")
	_, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "bogus"}})
	if !errors.Is(err, ErrApplyCancelled) {
		t.Fatalf("err = %v, want ErrApplyCancelled", err)
	}
}

// TestApplyPatchAutoPlainDiffNoDecision: Auto + a plain diff applies to the
// working tree WITHOUT consulting the decider (no Decider in deps — a
// decision attempt would fail the op).
func TestApplyPatchAutoPlainDiffNoDecision(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	diff := gitOut(t, dir, "diff", "HEAD~1", "HEAD")
	p := filepath.Join(t.TempDir(), "plain.diff")
	os.WriteFile(p, []byte(diff+"\n"), 0o644)
	gitOut(t, dir, "reset", "--hard", "HEAD~1")

	res, err := ApplyPatch{Path: p, Mode: ApplyModeAuto}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("auto plain diff: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
}

// TestApplyPatchCommitsOnPlainDiff: --am semantics on a bare diff is a typed
// refusal (git am has no author/message to work with).
func TestApplyPatchCommitsOnPlainDiff(t *testing.T) {
	_, repo := newRepo(t)
	p := filepath.Join(t.TempDir(), "plain.diff")
	os.WriteFile(p, []byte("diff --git a/x b/x\n"), 0o644)
	_, err := ApplyPatch{Path: p, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if !errors.Is(err, ErrNotMailbox) {
		t.Fatalf("err = %v, want ErrNotMailbox", err)
	}
}

// TestApplyPatchMissingFile: a bad path errors before any git runs.
func TestApplyPatchMissingFile(t *testing.T) {
	_, repo := newRepo(t)
	_, err := ApplyPatch{Path: "/nonexistent/x.patch", Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("missing file should error")
	}
}
```

Note: `newRepo(t)` is the existing helper in `internal/engine/ops_basic_test.go:16`; `MapDecider` is the existing scripted decider (`MapDecider{"decision-id": "answer"}`, see `cherry_pick_test.go:96`). If `OpDeps` requires an `Events` channel for `deps.emit` (check how `deps.emit` handles a nil channel — look at `internal/engine/engine.go`), add `Events: make(chan Event, 32)` to each `OpDeps` literal and drain/ignore it; mirror what existing tests that omit Events do.

- [ ] **Step 3: Run tests to verify they fail**

Run: `go test ./internal/engine/ -run TestApplyPatch 2>&1 | tail -5`
Expected: compile error — `undefined: ApplyPatch`, `ApplyModeCommits`, etc.

- [ ] **Step 4: Write the implementation**

Create `internal/engine/apply_patch.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"

	"github.com/homeend/gigagit/internal/git"
)

// ApplyPatchMode selects how ApplyPatch lands the patch.
type ApplyPatchMode int

const (
	// ApplyModeAuto detects the format: a mailbox forks via the
	// apply_patch.mode decision, a plain diff goes to the working tree.
	ApplyModeAuto ApplyPatchMode = iota
	// ApplyModeWorkingTree runs `git apply --3way`: changes land unstaged;
	// conflicts land as markers + unmerged entries for the conflict process.
	ApplyModeWorkingTree
	// ApplyModeCommits runs `git am --3way`: the mailbox is replayed as real
	// commits (author/date/message preserved). Atomic: any failure rolls back
	// via `git am --abort`. Mailbox patches only.
	ApplyModeCommits
)

// ApplyModeDecisionID names the mode fork a mailbox patch raises under
// ApplyModeAuto. Options: applyOptWorkingTree (safe, first), applyOptCommits.
const ApplyModeDecisionID = "apply_patch.mode"

const (
	applyOptWorkingTree = "working-tree"
	applyOptCommits     = "commits"
)

var (
	// ErrNotMailbox: ApplyModeCommits needs a format-patch mailbox — git am
	// has no author/message to work with on a bare diff.
	ErrNotMailbox = errors.New("not a format-patch mailbox; apply it to the working tree instead")
	// ErrApplyCancelled: the mode decision was cancelled; nothing ran.
	ErrApplyCancelled = errors.New("apply cancelled")
)

// ApplyPatch imports a patch file from disk (the inverse of the
// export-as-patch flow). It reads the file head via os directly — the
// read-side analog of ExportFile's outside-the-tree precedent (the patch may
// live anywhere). gg does NOT model a paused git am: the Commits path is
// all-or-nothing (AmInProgress-guarded AmAbort on failure), and the
// working-tree path surfaces conflicts through the ordinary status →
// conflict-process wiring (the SmartMerge keep-conflicts Result+error shape).
// Default TreeWrite reservation: both paths mutate the tree and/or refs.
type ApplyPatch struct {
	Path string
	Mode ApplyPatchMode
}

var _ Operation = ApplyPatch{}

func (op ApplyPatch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	head, err := readHead(op.Path, 4096)
	if err != nil {
		return Result{}, fmt.Errorf("read patch: %w", err)
	}
	if len(head) == 0 {
		return Result{}, fmt.Errorf("empty patch file: %s", op.Path)
	}
	mailbox := git.IsMailboxPatch(head)
	base := filepath.Base(op.Path)

	mode := op.Mode
	if mode == ApplyModeAuto {
		if !mailbox {
			mode = ApplyModeWorkingTree
		} else {
			choice, derr := deps.decide(ctx, DecisionRequest{
				ID:      ApplyModeDecisionID,
				Prompt:  base + " is a format-patch mailbox — apply how?",
				Options: []string{applyOptWorkingTree, applyOptCommits},
			})
			if derr != nil {
				return Result{}, derr
			}
			switch choice.Option {
			case applyOptWorkingTree:
				mode = ApplyModeWorkingTree
			case applyOptCommits:
				mode = ApplyModeCommits
			default:
				return Result{}, ErrApplyCancelled
			}
		}
	}
	if mode == ApplyModeCommits && !mailbox {
		return Result{}, ErrNotMailbox
	}

	if mode == ApplyModeCommits {
		return op.runAm(ctx, deps, base)
	}
	return op.runApply(ctx, deps, base)
}

// runAm replays the mailbox as commits, atomically: on any failure a started
// am is rolled back (guarded by AmInProgress — a bare rebase-apply dir
// belongs to a paused REBASE, which must not be am-aborted).
func (op ApplyPatch) runAm(ctx context.Context, deps OpDeps, base string) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying", Detail: base + " (recreate commits)"})
	if amErr := deps.Repo.AmMailbox(ctx, op.Path, true); amErr != nil {
		if in, _ := deps.Repo.AmInProgress(ctx); in {
			if abortErr := deps.Repo.AmAbort(ctx); abortErr != nil {
				return Result{}, fmt.Errorf("patch does not apply cleanly (%v); git am --abort also failed: %w", amErr, abortErr)
			}
		}
		return Result{}, fmt.Errorf("patch does not apply cleanly; nothing changed: %w", amErr)
	}
	summary := "applied " + base + " as commits"
	// Name the resulting tip (the Commit op's read-back precedent;
	// best-effort — a failed read only costs the sha in the summary).
	if line, lerr := deps.Repo.CommitLine(ctx, "HEAD"); lerr == nil {
		summary = "applied " + base + ": now at " + line.Hash + " " + line.Subject
	}
	res := Result{Summary: summary, Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

// runApply lands the diff in the working tree. `git apply --3way` exits
// non-zero BOTH when it left conflict markers and when it applied nothing —
// unmerged index entries tell the two apart.
func (op ApplyPatch) runApply(ctx context.Context, deps OpDeps, base string) (Result, error) {
	deps.emit(ctx, Progress{Step: "applying", Detail: base + " (working tree)"})
	applyErr := deps.Repo.ApplyPatch(ctx, op.Path, true)
	if applyErr == nil {
		res := Result{Summary: "applied " + base + " to working tree", Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	st, stErr := deps.Repo.Status(ctx)
	if stErr == nil && st.Counts().Conflicted > 0 {
		// The SmartMerge keep-conflicts shape: Result AND error, so the TUI
		// refreshes (conflict process picks the files up) and the CLI exits 1.
		n := st.Counts().Conflicted
		return Result{Summary: "applied " + base + " with conflicts in " + strconv.Itoa(n) + " file(s) (left in tree)", Changed: true},
			fmt.Errorf("apply conflict: %s left %d file(s) unmerged — resolve and commit", base, n)
	}
	return Result{}, fmt.Errorf("patch does not apply; nothing changed: %w", applyErr)
}

// readHead returns up to n bytes from the start of the file at path.
func readHead(path string, n int) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	buf := make([]byte, n)
	read, err := io.ReadFull(f, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return buf[:read], nil
}
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/ -run TestApplyPatch -v 2>&1 | tail -25`
Expected: all 8 tests PASS. If `TestApplyPatchAmConflictAtomic` fails on the `rebase-apply` stat because git am refused to even start (no rollback needed), that is fine — the assert only checks the dir is ABSENT afterwards, which holds either way.

Then the full package: `go test ./internal/engine/ 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 6: Commit**

```bash
git add internal/engine/apply_patch.go internal/engine/apply_patch_test.go internal/engine/gitops.go
git commit -m "feat(engine): ApplyPatch op — git apply --3way / atomic git am --3way

Auto mode sniffs the mailbox sentinel and forks apply_patch.mode on a
mailbox; Commits mode is all-or-nothing (AmInProgress-guarded am --abort);
working-tree conflicts use the SmartMerge keep-conflicts Result+error shape."
```

---

### Task 3: CLI — `gg apply [--am | --working] <path>` + e2e scenario

**Files:**
- Create: `internal/cli/apply.go`
- Create: `internal/cli/apply_test.go`
- Modify: `internal/cli/cli.go` (runOne case + `commands` map)
- Create: `e2e/scenarios/s81_cli_apply.toml`

**Interfaces:**
- Consumes (Task 2): `engine.ApplyPatch{Path, Mode}`, `engine.ApplyModeWorkingTree`, `engine.ApplyModeCommits`; existing `runOperation`, `finish`, `cliDecider{}`.
- Produces: `cmdApply(svc *domain.Service, rest []string, stdout, stderr io.Writer) int`, dispatched from `runOne` under `case "apply":`.

- [ ] **Step 1: Write the failing CLI tests**

Existing conventions in this package: `newCLIRepo(t) string` (`internal/cli/worktree_test.go:15`) builds a temp repo with an initial commit on `main`; tests drive the FULL entry point `Run(dir, args, stdin, &out, &errb, "")` (see `TestWorktreeList` just below the helper). There is no shared `gitRun`/`gitOut` helper in the package — define local ones. Create `internal/cli/apply_test.go`:

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

// applyGit runs git in dir and returns trimmed stdout.
func applyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func applyRun(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, args, strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

// usage errors: both flags, no positional, two positionals.
func TestCmdApplyUsageErrors(t *testing.T) {
	dir := newCLIRepo(t)
	for _, args := range [][]string{
		{"apply", "--am", "--working", "x.patch"},
		{"apply"},
		{"apply", "a.patch", "b.patch"},
	} {
		if code, _, stderr := applyRun(t, dir, args...); code != 2 {
			t.Fatalf("gg %v = %d, want 2 (stderr: %s)", args, code, stderr)
		}
	}
}

// default mode applies to the working tree; --am recreates the commit.
func TestCmdApplyWorkingAndAm(t *testing.T) {
	dir := newCLIRepo(t)
	// build a patch: commit, export, rewind
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "add f")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "extend f")
	patchData := applyGit(t, dir, "format-patch", "-1", "--binary", "--stdout", "HEAD")
	patch := filepath.Join(t.TempDir(), "extend.patch")
	os.WriteFile(patch, []byte(patchData+"\n"), 0o644)
	applyGit(t, dir, "reset", "--hard", "HEAD~1")

	// default = working tree: file changed, no commit
	if code, _, stderr := applyRun(t, dir, "apply", patch); code != 0 {
		t.Fatalf("apply = %d, stderr: %s", code, stderr)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("f.txt = %q", got)
	}
	if subj := applyGit(t, dir, "log", "-1", "--format=%s"); subj != "add f" {
		t.Fatalf("default mode must not commit; HEAD = %q", subj)
	}

	// --am on a rewound tree: commit recreated
	applyGit(t, dir, "checkout", "--", ".")
	code, stdout, stderr := applyRun(t, dir, "apply", "--am", patch)
	if code != 0 {
		t.Fatalf("apply --am = %d, stderr: %s", code, stderr)
	}
	if subj := applyGit(t, dir, "log", "-1", "--format=%s"); subj != "extend f" {
		t.Fatalf("--am should recreate the commit; HEAD = %q", subj)
	}
	if !strings.Contains(stdout, "applied") {
		t.Fatalf("stdout = %q, want an applied summary", stdout)
	}
}

// a conflicting working-tree apply exits 1 (conflicts left in tree).
func TestCmdApplyConflictExit1(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("patched\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "patch side")
	patchData := applyGit(t, dir, "format-patch", "-1", "--binary", "--stdout", "HEAD")
	patch := filepath.Join(t.TempDir(), "c.patch")
	os.WriteFile(patch, []byte(patchData+"\n"), 0o644)
	applyGit(t, dir, "reset", "--hard", "HEAD~1")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("conflicting local\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "local side")

	code, _, _ := applyRun(t, dir, "apply", patch)
	if code != 1 {
		t.Fatalf("conflicting apply = %d, want 1", code)
	}
	if unmerged := applyGit(t, dir, "diff", "--name-only", "--diff-filter=U"); unmerged != "f.txt" {
		t.Fatalf("unmerged = %q, want f.txt", unmerged)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/cli/ -run TestCmdApply 2>&1 | tail -5`
Expected: compile error — `undefined: cmdApply`.

- [ ] **Step 3: Implement `cmdApply` + dispatch**

Create `internal/cli/apply.go`:

```go
package cli

import (
	"context"
	"flag"
	"fmt"
	"io"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// cmdApply implements `gg apply [--am | --working] <path>`. Flags precede the
// positional (the gg review convention). The CLI always passes an explicit
// mode — engine.ApplyModeAuto (and its mailbox decision) is TUI-only, so
// `gg apply` never forks mid-run. Default (no flag) = working-tree mode for
// any patch format: safe, non-committing. --am recreates commits from a
// format-patch mailbox (typed refusal on a plain diff). Exit 0 = applied
// cleanly; 1 = failure OR applied-with-conflicts (conflicts left in tree, the
// `gg merge --on-conflict=keep` convention); 2 = usage.
func cmdApply(svc *domain.Service, rest []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply", flag.ContinueOnError)
	fs.SetOutput(stderr)
	am := fs.Bool("am", false, "recreate commits from a format-patch mailbox (git am)")
	working := fs.Bool("working", false, "apply to the working tree (the default)")
	if err := fs.Parse(rest); err != nil {
		return 2
	}
	if (*am && *working) || fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg apply [--am | --working] <path>")
		return 2
	}
	mode := engine.ApplyModeWorkingTree
	if *am {
		mode = engine.ApplyModeCommits
	}
	res, err := runOperation(context.Background(), svc,
		engine.ApplyPatch{Path: fs.Arg(0), Mode: mode}, cliDecider{}, stderr)
	return finish(res, err, stdout, stderr)
}
```

In `internal/cli/cli.go`:
1. Add to `runOne`'s switch (after `case "unstage":`):

```go
	case "apply":
		return cmdApply(svc, rest, stdout, stderr)
```

2. Add `"apply": true,` to the `commands` map (there is a test asserting every runOne case appears in the map — `cli_test.go:88`).

Check `cliDecider{}`'s zero-value construction against `cmdStash` (`internal/cli/ops.go:237`) — if the type needs a stdin field for interactive prompts, mirror how a never-forking command constructs it.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/ 2>&1 | tail -3`
Expected: `ok` (including the runOne↔commands-map consistency test).

- [ ] **Step 5: Write the e2e scenario**

Read the `writing-e2e-scenarios` project skill (`.claude/skills/writing-e2e-scenarios/SKILL.md`) first — it defines the schema and operation contracts. Then create `e2e/scenarios/s81_cli_apply.toml`. The scenario needs a patch file on disk before `gg apply` runs; if the `[input]` steps schema has no way to produce one (check the skill — steps are `write`/`commit`/…), use `gg commit export-patch` as the producing run:

```toml
name = "cli: gg apply round-trips an exported patch (working tree and --am)"

[input]
steps = [
  { write = "f.txt", content = "one\n" },
  { commit = "add f" },
  { write = "f.txt", content = "one\ntwo\n" },
  { commit = "extend f" },
]

# Export HEAD as a patch next to the repo, then rewind.
[[run]]
cmd  = ["commit", "export-patch", "HEAD", "--out", "extend.patch"]
exit = 0

[[run]]
cmd  = ["reset", "--hard", "--force", "HEAD~1"]
exit = 0

# Default mode: working tree — applied, nothing committed.
[[run]]
cmd  = ["apply", "extend.patch"]
exit = 0
stdout_contains = ["applied"]

[[run]]
cmd  = ["discard", "--yes", "--all"]
exit = 0

# --am: the commit is recreated.
[[run]]
cmd             = ["apply", "--am", "extend.patch"]
exit            = 0
stdout_contains = ["applied", "extend f"]

[[run]]
cmd             = ["log", "-n", "1"]
exit            = 0
stdout_contains = ["extend f"]

[expect]
branch = "main"
clean  = true
```

Adjust to the real schema after reading the skill: in particular (a) whether `--out extend.patch` writes relative to the repo or elsewhere — `gg commit export-patch` defaults to the PARENT of the repo, so an explicit relative `--out` may need a path the harness allows; (b) whether `reset --hard` needs `--force` here (`HEAD~1` IS on the branch, so likely not — drop it if the run fails); (c) the exact default branch name the harness produces. Run the harness (Step 6) and iterate until green.

- [ ] **Step 6: Run the e2e suite**

Run: `go test ./e2e/ -run TestScenarios 2>&1 | tail -5` (check the actual e2e test entrypoint name via `grep -rn "func Test" e2e/*.go` and use that)
Expected: PASS including s81.

- [ ] **Step 7: Commit**

```bash
git add internal/cli/apply.go internal/cli/apply_test.go internal/cli/cli.go e2e/scenarios/s81_cli_apply.toml
git commit -m "feat(cli): gg apply [--am|--working] <path>

Working-tree by default (git apply --3way, non-committing); --am
recreates commits from a mailbox. Never forks mid-run: the CLI always
passes an explicit mode. Routed through runOne so gg batch drives it."
```

---

### Task 4: TUI — palette entry "Apply patch…" + path popup + opAffectedSources

**Files:**
- Create: `internal/tui/apply_patch.go`
- Create: `internal/tui/apply_patch_test.go`
- Modify: `internal/tui/command_palette.go:28-32` (add the palette entry)
- Modify: `internal/tui/model.go:~545` (handle `applyPatchDirMsg`, beside the `patchResolvedMsg` case)
- Modify: `internal/tui/source.go:256-296` (opAffectedSources mapping)

**Interfaces:**
- Consumes (Task 2): `engine.ApplyPatch{Path, Mode: engine.ApplyModeAuto}`; existing `m.svc.ExportDefaultDir`, `m.startOp`, `m.pushLayer`/`popLayer`, `newTextField`, `viewField`, `popupMax`, `modalStyle`, `overlayCenter`, `clipToHeight`, `popupResolveWidth`, `popupInnerWidth`, `popupContentWidth`.
- Produces: `Model.openApplyPatchPopup() (Model, tea.Cmd)` (the palette `run` handler), `applyPatchPopup` layer, `applyPatchDirMsg`.

- [ ] **Step 1: Write the failing tests**

Existing conventions in this package: `loadedModel(t) Model` (`internal/tui/nav_test.go:11`) builds a loaded model over a real temp repo; `send(m Model, msg tea.Msg) (Model, tea.Cmd)` (`internal/tui/goto_commit_popup_test.go:31`) drives Update; `keyType(t tea.KeyType) tea.KeyMsg` (`internal/tui/irebase_view_test.go:129`) builds key messages. Create `internal/tui/apply_patch_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// The palette contains an "Apply patch…" entry.
func TestPaletteHasApplyPatch(t *testing.T) {
	found := false
	for _, c := range paletteCommands() {
		if c.label == "Apply patch…" {
			found = true
		}
	}
	if !found {
		t.Fatal("palette should list Apply patch…")
	}
}

// applyPatchDirMsg opens the popup prefilled with <dir>/; enter with a typed
// path closes the popup and dispatches the op (a non-nil tea.Cmd).
func TestApplyPatchPopupDispatchesOp(t *testing.T) {
	m := loadedModel(t)

	m, _ = send(m, applyPatchDirMsg{dir: "/exports"})
	p, ok := m.topLayer().(*applyPatchPopup)
	if !ok {
		t.Fatalf("top layer = %T, want *applyPatchPopup", m.topLayer())
	}
	if got := p.path.Value(); !strings.HasPrefix(got, "/exports") {
		t.Fatalf("prefill = %q, want /exports/ prefix", got)
	}

	for _, r := range "x.patch" {
		m, _ = send(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	var cmd tea.Cmd
	m, cmd = send(m, keyType(tea.KeyEnter))
	if cmd == nil {
		t.Fatal("enter should dispatch the op")
	}
	if _, still := m.topLayer().(*applyPatchPopup); still {
		t.Fatal("popup should close on dispatch")
	}
}

// enter on an empty path is a no-op (popup stays, nothing dispatched).
func TestApplyPatchPopupEmptyPathNoop(t *testing.T) {
	m := loadedModel(t)
	m, _ = send(m, applyPatchDirMsg{err: errTest})
	p, ok := m.topLayer().(*applyPatchPopup)
	if !ok || p.path.Value() != "" {
		t.Fatalf("resolve error should still open the popup with empty prefill (layer %T)", m.topLayer())
	}
	var cmd tea.Cmd
	m, cmd = send(m, keyType(tea.KeyEnter))
	if cmd != nil {
		t.Fatal("enter on empty path must not dispatch")
	}
	if _, still := m.topLayer().(*applyPatchPopup); !still {
		t.Fatal("popup should stay open")
	}
}

// esc closes without dispatching.
func TestApplyPatchPopupEscCancels(t *testing.T) {
	m := loadedModel(t)
	m, _ = send(m, applyPatchDirMsg{dir: "/exports"})
	m, _ = send(m, keyType(tea.KeyEsc))
	if _, still := m.topLayer().(*applyPatchPopup); still {
		t.Fatal("esc should close the popup")
	}
}
```

`errTest`: grep the package for an existing sentinel test error (`errors.New` in a shared test file); if none is shared, declare `var errTest = errors.New("boom")` locally in this file (and import `errors`). If `send` returns `(Model, tea.Cmd)` with different semantics than shown, mirror its call sites in `goto_commit_popup_test.go`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestPaletteHasApplyPatch|TestApplyPatchPopup' 2>&1 | tail -5`
Expected: compile error — `undefined: applyPatchDirMsg`, `applyPatchPopup`.

- [ ] **Step 3: Implement the popup + palette entry + wiring**

Create `internal/tui/apply_patch.go`:

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// applyPatchDirMsg carries the resolved default patch directory for the
// apply-patch popup's path prefill (the patchResolvedMsg pattern, read side).
// A resolve error is not fatal — the popup opens with an empty prefill.
type applyPatchDirMsg struct {
	dir string
	err error
}

// openApplyPatchPopup resolves the default export dir off-thread (the same
// directory export-as-patch writes into — the natural place a patch lives),
// then opens the editable-path popup via applyPatchDirMsg.
func (m Model) openApplyPatchPopup() (Model, tea.Cmd) {
	svc := m.svc
	return m, func() tea.Msg {
		dir, err := svc.ExportDefaultDir(context.Background())
		return applyPatchDirMsg{dir: dir, err: err}
	}
}

// applyPatchPopup is the editable-path prompt behind the palette's
// "Apply patch…": enter dispatches engine.ApplyPatch in Auto mode (a mailbox
// forks the working-tree/commits decision via the standard modal Decider).
// Mirrors exportPatchPopup.
type applyPatchPopup struct {
	popupMax
	path textfield
}

func (p *applyPatchPopup) update(m Model, msg tea.KeyMsg) (Model, tea.Cmd) {
	if msg.Type == tea.KeyCtrlC {
		return m, tea.Quit
	}
	switch msg.Type {
	case tea.KeyEsc:
		m = m.popLayer()
	case tea.KeyEnter:
		path := strings.TrimSpace(p.path.Value())
		if path == "" {
			return m, nil
		}
		m = m.popLayer()
		return m.startOp(engine.ApplyPatch{Path: path, Mode: engine.ApplyModeAuto})
	default:
		p.path.HandleEditKey(msg)
	}
	return m, nil
}

func (p *applyPatchPopup) render(m Model, below string) string {
	w, h := m.overlayDims()
	var b strings.Builder
	b.WriteString("Apply patch\n\n")
	b.WriteString(viewField("path: ", p.path, true, popupContentWidth(w)) + "\n\n")
	b.WriteString("[type] path  [enter] apply  [esc] cancel")
	box := modalStyle.Width(popupResolveWidth(w, p.maximized, popupInnerWidth(w))).Render(b.String()) + "\n"
	return overlayCenter(clipToHeight(below, h), box, w, h)
}
```

In `internal/tui/command_palette.go`, extend `paletteCommands()`:

```go
func paletteCommands() []paletteCommand {
	return []paletteCommand{
		{label: "Show commit", keyHint: "#", run: Model.openGotoCommitPopup},
		{label: "Apply patch…", keyHint: "", run: Model.openApplyPatchPopup},
	}
}
```

In `internal/tui/model.go`, beside the existing `case patchResolvedMsg:` (line ~553), add:

```go
	case applyPatchDirMsg:
		p := &applyPatchPopup{}
		prefill := ""
		if msg.err == nil && msg.dir != "" {
			prefill = msg.dir + string(os.PathSeparator)
		}
		p.path = newTextField(prefill)
		return m.pushLayer(p), nil
```

(add `"os"` to model.go's imports if absent).

In `internal/tui/source.go`, add to the `opAffectedSources` switch (after the `engine.SmartMerge, engine.SmartRebase` case):

```go
	case engine.ApplyPatch:
		// Commits mode moves the branch tip and adds commits; working-tree
		// mode changes status (possibly to conflicted). One op covers both,
		// so refresh the union.
		return []sourceKey{srcStatus, srcFeed, srcBranches}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/tui/ -run 'TestPaletteHasApplyPatch|TestApplyPatchPopup' -v 2>&1 | tail -8`
Expected: PASS.

Then the whole package (palette tests index-sensitive tests may exist): `go test ./internal/tui/ 2>&1 | tail -3`
Expected: `ok`. If a palette test asserts the entry COUNT or a fixed selection index, update it to the new two-entry registry.

- [ ] **Step 5: Commit**

```bash
git add internal/tui/apply_patch.go internal/tui/apply_patch_test.go internal/tui/command_palette.go internal/tui/model.go internal/tui/source.go
git commit -m "feat(tui): Apply patch… palette entry + path popup

Prefills the export dir, dispatches engine.ApplyPatch in Auto mode; a
mailbox forks working-tree/commits via the standard modal Decider.
opAffectedSources: status+feed+branches."
```

---

### Task 5: Docs, agent skill, full verification

**Files:**
- Modify: `CHANGELOG.md` (new entry at top)
- Modify: `README.md` (CLI verb list + palette mention — match how `gg review` is documented)
- Modify: `CLAUDE.md` (package map: engine op, git verbs, CLI verb, TUI palette entry)
- Modify: `internal/agentskill/using-gg.md` (document `gg apply`; bump the `<!-- gg:using-gg:vNN -->` marker)
- Modify: `internal/agentskill/agentskill.go` (or wherever `Version` lives — grep `agentskill.Version`) — bump by 1

**Interfaces:** none produced; documentation of Tasks 1–4.

- [ ] **Step 1: CHANGELOG entry**

Read the top of `CHANGELOG.md` for the entry format and add (adapting the heading style to match):

```markdown
- **Import/apply a patch** — `gg apply [--am | --working] <path>` and a TUI
  command-palette "Apply patch…" entry apply a patch file: working-tree mode
  (`git apply --3way`; conflicts land as normal markers for the conflict
  process) or recreate-commits mode (`git am --3way`, atomic — any failure
  rolls back, nothing half-applied). Round-trips `gg commit export-patch`.
```

- [ ] **Step 2: README + CLAUDE.md**

- README: add `gg apply` beside the other CLI verbs (mirror the one-paragraph style used for `gg review`), and mention the palette entry wherever the command palette is described.
- CLAUDE.md package map: extend the `engine` row (ApplyPatch op — atomic am, keep-conflicts apply, `apply_patch.mode` decision, `ErrNotMailbox`/`ErrApplyCancelled`), the `git` row (`ApplyPatch`/`AmMailbox`/`AmAbort`/`AmInProgress` + `IsMailboxPatch`, the rebase-apply/applying vs rebasing distinction), the `cli` row (`gg apply [--am|--working] <path>`, never forks, exit 1 on conflicts-left), and the `tui` row (palette "Apply patch…" + `applyPatchPopup`, opAffectedSources union).

- [ ] **Step 3: using-gg.md + version bump**

In `internal/agentskill/using-gg.md`, add after the `gg commit export-patch` block:

```markdown
- `gg apply [--am | --working] <path>` — apply a patch file. Default =
  working-tree mode (`git apply --3way`): the diff lands as unstaged changes,
  nothing committed; conflicts stay in the tree as markers (exit 1) for you
  to resolve and commit. `--am` recreates commits from a `git format-patch`
  mailbox (author/date/message preserved) and is atomic: a conflicting
  mailbox is rolled back completely (exit 1, nothing changed). `--am` on a
  plain diff is refused. Round-trips `gg commit export-patch`.
```

Bump the `<!-- gg:using-gg:vNN -->` marker at the top of the file AND the `Version` constant in the agentskill package (grep for it) — they must match; there is a test asserting this.

- [ ] **Step 4: Full verification**

```bash
cd /mnt/t/others/gigagit/.claude/worktrees/apply-patch
./test.sh 2>&1 | tail -15
```
Expected: vet+gofmt clean, unit green, e2e green.

Then the race run (required before merge):
```bash
./test.sh race 2>&1 | tail -8
```
Expected: green.

Refresh installed skill copies:
```bash
go build -o ./gg ./cmd/gg && ./gg init --update
```

- [ ] **Step 5: Commit**

```bash
git add CHANGELOG.md README.md CLAUDE.md internal/agentskill/
git commit -m "docs: document gg apply / Apply patch… (import a patch)"
```

---

## Self-review notes (already applied)

- Spec's "am-conflict → no `rebase-apply` left" is asserted in `TestApplyPatchAmConflictAtomic`.
- Spec's Windows-safe behavior needs no extra work here (no filenames are generated).
- The `PausedOpIn` non-regression is covered by the existing `TestPausedOpIn` table, re-run in Task 1 Step 4.
- Types are consistent across tasks: `ApplyPatch{Path string, Mode ApplyPatchMode}`, mode constants, and error variables are defined once in Task 2 and only referenced afterwards.
