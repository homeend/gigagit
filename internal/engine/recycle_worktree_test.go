package engine

import (
	"context"
	"errors"
	"os"
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

// Discard must drop STAGED changes too (a plain `restore --worktree` keeps
// them, and `git switch` would carry them onto the target branch), plus
// unstaged edits and untracked files.
func TestRecycleWorktreeDirtyDiscard(t *testing.T) {
	t.Parallel()
	_, deps, wt := recycleFixture(t)
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("staged edit\n"), 0o644)
	gitIn(t, wt, "add", "README.md")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("staged edit + unstaged edit\n"), 0o644)
	os.WriteFile(filepath.Join(wt, "staged-new.txt"), []byte("s\n"), 0o644)
	gitIn(t, wt, "add", "staged-new.txt")
	os.WriteFile(filepath.Join(wt, "new.txt"), []byte("n\n"), 0o644)
	deps.Decider = MapDecider{RecycleDirtyDecisionID: "discard"}
	res, err := RecycleWorktree{Dir: wt, Branch: "target", Now: fixedNow}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !strings.Contains(res.Summary, "; changes discarded") {
		t.Fatalf("summary = %q", res.Summary)
	}
	for _, f := range []string{"new.txt", "staged-new.txt"} {
		if _, err := os.Stat(filepath.Join(wt, f)); !os.IsNotExist(err) {
			t.Fatalf("%s must be gone after discard", f)
		}
	}
	if got := gitOut(t, wt, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree not clean after discard:\n%s", got)
	}
	if b, _ := os.ReadFile(filepath.Join(wt, "README.md")); string(b) != "hi\n" {
		t.Fatalf("README.md = %q, want the committed content", b)
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
	t.Run("the worktree gg runs in", func(t *testing.T) {
		t.Parallel()
		dir, deps, _ := recycleFixture(t)
		_, err := RecycleWorktree{Dir: dir, Branch: "target"}.Run(context.Background(), deps)
		if err == nil || !strings.Contains(err.Error(), "worktree you are in") {
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
