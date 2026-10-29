package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/model"
)

// A rebase paused on a conflict leaves its worktree detached in `git worktree
// list`; Worktrees still names the branch it is rewriting (both backends, a
// linked and the main worktree), so the Branches tab keeps the tree under it.
func TestRepoWorktreesNameTheBranchARebaseRewrites(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"--merge", "--apply"} {
		t.Run(backend, func(t *testing.T) {
			t.Parallel()
			dir, runner := newTestRepo(t)
			repo := &Repo{Runner: runner}
			write := func(d, body string) {
				t.Helper()
				if err := os.WriteFile(filepath.Join(d, "README.md"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			gittest.Run(t, dir, "branch", "feat")
			gittest.Run(t, dir, "branch", "other")
			write(dir, "main\n")
			gittest.Run(t, dir, "commit", "-qam", "main change")
			wt := filepath.Join(t.TempDir(), "feat")
			gittest.Run(t, dir, "worktree", "add", "-q", wt, "feat")
			write(wt, "feat\n")
			gittest.Run(t, wt, "commit", "-qam", "feat change")
			if out, err := exec.Command("git", "-C", wt, "rebase", backend, "main").CombinedOutput(); err == nil {
				t.Fatalf("rebase did not stop on the conflict: %s", out)
			}

			wts, err := repo.Worktrees(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var got *model.Worktree
			for _, w := range wts {
				if w.Path == filepath.Clean(wt) {
					got = &w
					if !w.Detached {
						t.Errorf("worktree not listed detached: %+v", w)
					}
					if w.HeldBranch() != "feat" {
						t.Errorf("HeldBranch() = %q, want feat", w.HeldBranch())
					}
				} else if w.Rebasing != "" || w.HeldBranch() != "main" {
					t.Errorf("main worktree = %+v (held %q), want on main, no rebase", w, w.HeldBranch())
				}
			}
			if got == nil || got.Branch != "" || got.Rebasing != "feat" {
				t.Fatalf("rebasing worktree = %+v, want Branch \"\" Rebasing feat (all: %+v)", got, wts)
			}
		})
	}
}

// The main worktree's own paused rebase reads from <repo>/.git.
func TestRepoWorktreesMainWorktreeRebasing(t *testing.T) {
	t.Parallel()
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gittest.Run(t, dir, "checkout", "-q", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "commit", "-qam", "feat change")
	gittest.Run(t, dir, "checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gittest.Run(t, dir, "commit", "-qam", "main change")
	if out, err := exec.Command("git", "-C", dir, "rebase", "main", "feat").CombinedOutput(); err == nil {
		t.Fatalf("rebase did not stop on the conflict: %s", out)
	}
	wts, err := repo.Worktrees(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wts) != 1 || wts[0].Rebasing != "feat" || wts[0].HeldBranch() != "feat" {
		t.Fatalf("worktrees = %+v, want the main tree rebasing feat", wts)
	}
}

// A rebase of a detached HEAD rewrites no branch: nothing is named.
func TestRebasingBranchInDetachedRebase(t *testing.T) {
	t.Parallel()
	gd := t.TempDir()
	if err := os.MkdirAll(filepath.Join(gd, "rebase-merge"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gd, "rebase-merge", "head-name"), []byte("detached HEAD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := RebasingBranchIn(gd); got != "" {
		t.Fatalf("RebasingBranchIn = %q, want \"\"", got)
	}
	if got := RebasingBranchIn(""); got != "" {
		t.Fatalf("RebasingBranchIn(\"\") = %q, want \"\"", got)
	}
}
