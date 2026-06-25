package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestGitCommonDirIsAbsolute(t *testing.T) {
	_, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	got, err := repo.GitCommonDir(context.Background())
	if err != nil {
		t.Fatalf("GitCommonDir: %v", err)
	}
	if !filepath.IsAbs(got) {
		t.Fatalf("GitCommonDir = %q, want an absolute path", got)
	}
}

func TestRemoveWorktreeRemovesLinkedTree(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt := filepath.Join(filepath.Dir(dir), "wt-rm")
	if err := repo.AddWorktree(context.Background(), wt, "feature/rm", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if err := repo.RemoveWorktree(context.Background(), wt, false, nil); err != nil {
		t.Fatalf("RemoveWorktree: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
}

func TestUnlockWorktreeReleasesLock(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt := filepath.Join(filepath.Dir(dir), "wt-unlock")
	if err := repo.AddWorktree(context.Background(), wt, "feature/unlock", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	if out, err := exec.Command("git", "-C", dir, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock: %v\n%s", err, out)
	}
	// Locked: even a forced remove is refused.
	if err := repo.RemoveWorktree(context.Background(), wt, true, nil); err == nil {
		t.Fatal("removing a locked worktree should fail")
	}
	// Unlock, then the forced remove succeeds.
	if err := repo.UnlockWorktree(context.Background(), wt); err != nil {
		t.Fatalf("UnlockWorktree: %v", err)
	}
	if err := repo.RemoveWorktree(context.Background(), wt, true, nil); err != nil {
		t.Fatalf("RemoveWorktree after unlock: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
}

func TestRemoveWorktreeRefusesDirtyUntilForced(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	wt := filepath.Join(filepath.Dir(dir), "wt-dirty")
	if err := repo.AddWorktree(context.Background(), wt, "feature/d", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// Make the worktree dirty so a non-forced removal is refused.
	if err := os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := repo.RemoveWorktree(context.Background(), wt, false, nil); err == nil {
		t.Fatal("non-forced removal of a dirty worktree should fail")
	}
	if err := repo.RemoveWorktree(context.Background(), wt, true, nil); err != nil {
		t.Fatalf("forced removal: %v", err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after force: %v", err)
	}
}

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

func TestDeleteBranchRefusesUnmergedUntilForced(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	gitDo := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	// Create an unmerged branch: commit on it, then return to main.
	gitDo("checkout", "-b", "feature/unmerged")
	if err := os.WriteFile(filepath.Join(dir, "extra.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitDo("add", ".")
	gitDo("commit", "-m", "unmerged work")
	gitDo("checkout", "main")

	if err := repo.DeleteBranch(context.Background(), "feature/unmerged", false); err == nil {
		t.Fatal("safe delete of an unmerged branch should fail")
	}
	if err := repo.DeleteBranch(context.Background(), "feature/unmerged", true); err != nil {
		t.Fatalf("forced delete: %v", err)
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--list", "feature/unmerged").CombinedOutput()
	if strings.TrimSpace(string(out)) != "" {
		t.Fatalf("branch still present after force delete: %q", out)
	}
}
