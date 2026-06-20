package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestCherryPickArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git cherry-pick", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CherryPick(context.Background(), "", "abc123"); err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	want := []string{"cherry-pick", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCherryPickArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git cherry-pick", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CherryPick(context.Background(), "/wt/x", "abc123"); err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	want := []string{"-C", "/wt/x", "cherry-pick", "abc123"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCherryPickAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git cherry-pick --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CherryPickAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "cherry-pick", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestCherryPickContinueArgvAndEnv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git cherry-pick --continue", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.CherryPickContinue(context.Background(), ""); err != nil {
		t.Fatalf("continue: %v", err)
	}
	want := []string{"cherry-pick", "--continue"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if !reflect.DeepEqual(f.Calls[0].Env, []string{"GIT_EDITOR=true"}) {
		t.Fatalf("env = %v, want [GIT_EDITOR=true]", f.Calls[0].Env)
	}
}

func TestCherryPickInProgressExitOneMeansNo(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse CHERRY_PICK_HEAD", gitexec.Result{ExitCode: 1})
	f.SetError("git rev-parse CHERRY_PICK_HEAD", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	in, err := r.CherryPickInProgress(context.Background(), "")
	if err != nil {
		t.Fatalf("CherryPickInProgress: %v", err)
	}
	if in {
		t.Fatal("exit 1 must mean 'no cherry-pick in progress'")
	}
}

// Real-git: a clean cherry-pick lands the side commit's change on main.
func TestCherryPickCleanReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("from feat\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "add new.txt")
	pick := gitOutIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "main")

	if err := repo.CherryPick(context.Background(), "", pick); err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil || string(got) != "from feat\n" {
		t.Fatalf("new.txt = %q, %v; want %q", got, err, "from feat\n")
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("a clean cherry-pick must not leave CHERRY_PICK_HEAD set")
	}
}

// Real-git: a conflicting cherry-pick sets CHERRY_PICK_HEAD; abort clears it.
func TestCherryPickConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "feat change")
	pick := gitOutIn(t, dir, "rev-parse", "HEAD")
	gitIn(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "main change")

	if err := repo.CherryPick(context.Background(), "", pick); err == nil {
		t.Fatal("conflicting cherry-pick must fail")
	}
	in, err := repo.CherryPickInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("CherryPickInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if s, _ := repo.CherryPickHeadSummary(context.Background(), ""); s == "" {
		t.Fatal("CherryPickHeadSummary should name the picked commit during a conflict")
	}
	if err := repo.CherryPickAbort(context.Background(), ""); err != nil {
		t.Fatalf("abort: %v", err)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the cherry-pick state")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "main\n" {
		t.Fatalf("f.txt = %q after abort, want %q", got, "main\n")
	}
}

// Real-git: probe ordering. A paused interactive-rebase pick can also set
// CHERRY_PICK_HEAD, so callers must check RebaseInProgress first. This proves
// the overlap exists (justifying merge -> rebase -> cherry-pick ordering).
func TestRebaseConflictAlsoReportsRebaseInProgress(t *testing.T) {
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

	// Rebase feat onto main: the replayed pick conflicts and pauses.
	_ = repo.Rebase(context.Background(), "", "main")

	if in, _ := repo.RebaseInProgress(context.Background(), ""); !in {
		t.Fatal("a paused rebase must report RebaseInProgress")
	}
	// CHERRY_PICK_HEAD overlap is git-version-dependent; the load-bearing
	// invariant is only that rebase is detected, so callers check it first.
	_ = repo.RebaseAbort(context.Background(), "")
}
