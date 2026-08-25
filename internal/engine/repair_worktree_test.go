package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepairWorktreeRebindsMovedWorktree(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	parent := t.TempDir()
	wt := filepath.Join(parent, "alpha")
	gitE(t, dir, "worktree", "add", wt, "-b", "tmp-branch")
	moved := filepath.Join(parent, "beta")
	if err := os.Rename(wt, moved); err != nil {
		t.Fatal(err)
	}
	list := func() string {
		out, err := exec.Command("git", "-C", dir, "worktree", "list", "--porcelain").CombinedOutput()
		if err != nil {
			t.Fatalf("worktree list: %v\n%s", err, out)
		}
		return string(out)
	}
	// Broken: the admin gitdir record still names the vanished old path —
	// the same stale-absolute-link state a cross-environment worktree is in.
	if out := list(); !strings.Contains(out, "alpha") {
		t.Fatalf("precondition: admin record should still name the old path\n%s", out)
	}

	res, err := RepairWorktree{Path: moved}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("repair: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if want := "repaired worktree link: " + moved; res.Summary != want {
		t.Fatalf("summary = %q, want %q", res.Summary, want)
	}
	out := list()
	if !strings.Contains(out, "beta") || strings.Contains(out, "alpha") {
		t.Fatalf("after repair, worktree list should name the new path only\n%s", out)
	}
	// The repaired worktree actually works again.
	if o, err := exec.Command("git", "-C", moved, "rev-parse", "--is-inside-work-tree").CombinedOutput(); err != nil || strings.TrimSpace(string(o)) != "true" {
		t.Fatalf("repaired worktree unusable: %v\n%s", err, o)
	}
}

func TestRepairWorktreeErrorPassesThrough(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	// A path that is not a worktree: git worktree repair reports an error.
	res, err := RepairWorktree{Path: filepath.Join(t.TempDir(), "nope")}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatalf("expected an error, got %+v", res)
	}
	if res.Changed {
		t.Fatalf("failed repair must not report Changed: %+v", res)
	}
}
