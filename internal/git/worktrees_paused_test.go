package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func pausedRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// pausedFixture has three worktrees: main, a linked "side" on branch side, and
// a branch "other" whose k.txt conflicts with both.
func pausedFixture(t *testing.T) (*Repo, string, string) {
	t.Helper()
	dir, runner := newTestRepo(t)
	write := func(d, name, body string) {
		if err := os.WriteFile(filepath.Join(d, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(dir, "k.txt", "base\n")
	pausedRun(t, dir, "add", ".")
	pausedRun(t, dir, "commit", "-qm", "base")
	pausedRun(t, dir, "branch", "other")
	pausedRun(t, dir, "branch", "side")
	side := filepath.Join(t.TempDir(), "side")
	pausedRun(t, dir, "worktree", "add", "-q", side, "side")
	write(side, "k.txt", "side\n")
	pausedRun(t, side, "commit", "-qam", "side")
	pausedRun(t, dir, "checkout", "-q", "other")
	write(dir, "k.txt", "other\n")
	pausedRun(t, dir, "commit", "-qam", "other")
	return &Repo{Runner: runner}, dir, side
}

func wtAt(t *testing.T, r *Repo, path string) model.Worktree {
	t.Helper()
	wts, err := r.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range wts {
		if model.SamePath(w.Path, path) {
			return w
		}
	}
	t.Fatalf("no worktree at %s in %+v", path, wts)
	return model.Worktree{}
}

func TestWorktreesReportAPausedOp(t *testing.T) {
	t.Parallel()
	r, dir, side := pausedFixture(t)

	if w := wtAt(t, r, side); w.Paused != "" || w.Conflicted {
		t.Fatalf("idle worktree: %+v", w)
	}

	// A merge stopped on a conflict, in another worktree than the one asked from.
	cmd := exec.Command("git", "merge", "other")
	cmd.Dir = side
	_ = cmd.Run() // exits 1: the conflict
	w := wtAt(t, r, side)
	if w.Paused != "merge" || !w.Conflicted {
		t.Errorf("conflicted merge: %+v", w)
	}
	if w := wtAt(t, r, dir); w.Paused != "" {
		t.Errorf("a worktree with nothing paused says %q", w.Paused)
	}

	// Conflicts resolved, the merge not yet committed: still in progress.
	pausedRun(t, side, "add", "k.txt")
	if w := wtAt(t, r, side); w.Paused != "merge" || w.Conflicted {
		t.Errorf("resolved merge: %+v", w)
	}
	pausedRun(t, side, "merge", "--abort")

	// A rebase stopped on a conflict.
	cmd = exec.Command("git", "rebase", "other")
	cmd.Dir = side
	_ = cmd.Run()
	w = wtAt(t, r, side)
	if w.Paused != "rebase" || !w.Conflicted || w.Rebasing != "side" {
		t.Errorf("conflicted rebase: %+v", w)
	}
}

func TestPausedNoticeIDIsAPlainIDAndMovesWithTheConflict(t *testing.T) {
	t.Parallel()
	a := model.Worktree{Path: "/r/a", Paused: "rebase"}
	b := a
	b.Conflicted = true
	if a.PausedNoticeID() == b.PausedNoticeID() {
		t.Error("a conflict must be a new notice")
	}
	if a.PausedNoticeID() != (model.Worktree{Path: "/r/a/", Paused: "rebase"}).PausedNoticeID() {
		t.Error("the id must follow the checkout key, not the spelling")
	}
}
