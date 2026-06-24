package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
)

func TestRevertArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git revert", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Revert(context.Background(), "", "abc123"); err != nil {
		t.Fatalf("revert: %v", err)
	}
	want := []string{"revert", "--no-edit", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRevertArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git revert", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Revert(context.Background(), "/wt/x", "abc123"); err != nil {
		t.Fatalf("revert: %v", err)
	}
	want := []string{"-C", "/wt/x", "revert", "--no-edit", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRevertAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git revert --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RevertAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "revert", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestRevertContinueArgvAndEnv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git revert --continue", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.RevertContinue(context.Background(), ""); err != nil {
		t.Fatalf("continue: %v", err)
	}
	want := []string{"revert", "--continue"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if !reflect.DeepEqual(f.Calls[0].Env, []string{"GIT_EDITOR=true"}) {
		t.Fatalf("env = %v, want [GIT_EDITOR=true]", f.Calls[0].Env)
	}
}

func TestRevertInProgressExitOneMeansNo(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse REVERT_HEAD", gitexec.Result{ExitCode: 1})
	f.SetError("git rev-parse REVERT_HEAD", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	in, err := r.RevertInProgress(context.Background(), "")
	if err != nil {
		t.Fatalf("RevertInProgress: %v", err)
	}
	if in {
		t.Fatal("exit 1 must mean 'no revert in progress'")
	}
}

// Real-git: a clean revert removes the change the picked commit introduced.
func TestRevertCleanReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("added\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add new.txt")
	target := gitOutIn(t, dir, "rev-parse", "HEAD")

	if err := repo.Revert(context.Background(), "", target); err != nil {
		t.Fatalf("revert: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("revert should have removed new.txt, stat err = %v", err)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("a clean revert must not leave REVERT_HEAD set")
	}
}

// Real-git: a conflicting revert sets REVERT_HEAD; abort clears it.
func TestRevertConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "to v2")
	target := gitOutIn(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v3\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "to v3") // now reverting "to v2" conflicts

	if err := repo.Revert(context.Background(), "", target); err == nil {
		t.Fatal("conflicting revert must fail")
	}
	in, err := repo.RevertInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("RevertInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if s, _ := repo.RevertHeadSummary(context.Background(), ""); s == "" {
		t.Fatal("RevertHeadSummary should name the reverted commit during a conflict")
	}
	if err := repo.RevertAbort(context.Background(), ""); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the revert state")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "v3\n" {
		t.Fatalf("f.txt = %q after abort, want v3", got)
	}
}

// Real-git: reverting a merge commit without -m is refused outright (no
// REVERT_HEAD), so the engine surfaces git's legible error.
func TestRevertMergeCommitRefusedReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "feat")
	gitIn(t, dir, "checkout", "main")
	gitIn(t, dir, "merge", "--no-ff", "feat", "-m", "merge feat")
	mergeSHA := gitOutIn(t, dir, "rev-parse", "HEAD")

	if err := repo.Revert(context.Background(), "", mergeSHA); err == nil {
		t.Fatal("reverting a merge commit without -m must fail")
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("a refused merge revert must not leave REVERT_HEAD set")
	}
}
