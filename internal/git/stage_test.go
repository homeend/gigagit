package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
)

func TestStagePathsArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git add", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.StagePaths(context.Background(), []string{"a.go", "b.go"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	want := []string{"add", "--", "a.go", "b.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestUnstagePathsArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git restore --staged", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.UnstagePaths(context.Background(), []string{"a.go"}); err != nil {
		t.Fatalf("unstage: %v", err)
	}
	want := []string{"restore", "--staged", "--", "a.go"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

// Real-git: stage an unstaged modification, then unstage it, asserting the
// porcelain transitions via the existing Status verb.
func TestStageUnstageReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	// README.md exists from newTestRepo; modify it (unstaged).
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)

	if err := repo.StagePaths(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("stage: %v", err)
	}
	st, _ := repo.Status(context.Background())
	if findStaged(st, "README.md") == '.' {
		t.Fatal("README.md should be staged after StagePaths")
	}

	if err := repo.UnstagePaths(context.Background(), []string{"README.md"}); err != nil {
		t.Fatalf("unstage: %v", err)
	}
	st, _ = repo.Status(context.Background())
	if findStaged(st, "README.md") != '.' {
		t.Fatal("README.md should be unstaged after UnstagePaths")
	}
}

// findStaged returns the Staged porcelain byte for path ('.' if absent/clean).
func findStaged(st model.WorkingTreeStatus, path string) byte {
	for _, f := range st.Files {
		if f.Path == path {
			if f.Staged == 0 {
				return '.'
			}
			return f.Staged
		}
	}
	return '.'
}
