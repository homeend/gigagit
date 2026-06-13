package git

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestRebaseArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Rebase(context.Background(), "", "main"); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	want := []string{"rebase", "main"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRebaseArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Rebase(context.Background(), "/wt/x", "main"); err != nil {
		t.Fatalf("rebase: %v", err)
	}
	want := []string{"-C", "/wt/x", "rebase", "main"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRebaseAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RebaseAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("rebase abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "rebase", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

// Real-git: build a conflicting rebase, observe the in-progress probe, abort,
// observe clean — mirrors TestMergeConflictDetectAndAbortReal.
func TestRebaseConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "feat change")
	gitIn(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "main change")
	gitIn(t, dir, "checkout", "feat")

	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("no rebase should be in progress yet")
	}
	if err := repo.Rebase(context.Background(), "", "main"); err == nil {
		t.Fatal("conflicting rebase must fail")
	}
	in, err := repo.RebaseInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("RebaseInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if err := repo.RebaseAbort(context.Background(), ""); err != nil {
		t.Fatalf("rebase abort: %v", err)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the rebase state")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "feat\n" {
		t.Fatalf("f.txt = %q after abort, want %q (feat's pre-rebase tip)", got, "feat\n")
	}
}
