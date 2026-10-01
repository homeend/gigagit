package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gittest"
)

func TestHeadAtBranch(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	if got := HeadAt(dir); got != "main" {
		t.Fatalf("HeadAt = %q, want main", got)
	}
}

func TestHeadAtDetached(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha := strings.TrimSpace(string(out))
	gittest.Run(t, dir, "checkout", "--detach", sha)
	got := HeadAt(dir)
	if len(got) != 7 || !strings.HasPrefix(sha, got) {
		t.Fatalf("HeadAt detached = %q, want the 7-char prefix of %s", got, sha)
	}
}

func TestHeadAtLinkedWorktree(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "hello\n")
	gittest.Run(t, dir, "branch", "side")
	wt := filepath.Join(t.TempDir(), "wt")
	gittest.Run(t, dir, "worktree", "add", wt, "side")
	if got := HeadAt(wt); got != "side" {
		t.Fatalf("HeadAt linked worktree = %q, want side", got)
	}
}

func TestHeadAtNotARepo(t *testing.T) {
	t.Parallel()
	if got := HeadAt(t.TempDir()); got != "" {
		t.Fatalf("HeadAt on a plain dir = %q, want empty", got)
	}
	if got := HeadAt(filepath.Join(t.TempDir(), "missing")); got != "" {
		t.Fatalf("HeadAt on a missing dir = %q, want empty", got)
	}
	// A .git file whose gitdir points nowhere is unreadable, not an error.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /nowhere/at/all\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := HeadAt(dir); got != "" {
		t.Fatalf("HeadAt with a dangling gitdir = %q, want empty", got)
	}
}

func TestGitDirAtMainAndLinked(t *testing.T) {
	t.Parallel()
	dir := gittest.BasicRepo(t, "x\n")
	if got := GitDirAt(dir); got != filepath.Join(dir, ".git") {
		t.Fatalf("main GitDirAt = %q", got)
	}
	wt := filepath.Join(t.TempDir(), "wt")
	if out, err := exec.Command("git", "-C", dir, "worktree", "add", "-b", "b", wt).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	got := GitDirAt(wt)
	if filepath.Base(filepath.Dir(got)) != "worktrees" {
		t.Fatalf("linked GitDirAt = %q, want <common>/worktrees/<name>", got)
	}
	if GitDirAt(t.TempDir()) != "" {
		t.Fatal("non-repo must be empty")
	}
}
