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
