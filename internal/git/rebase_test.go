package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/gigagit/gg/internal/gitexec"
)

func TestRebaseInteractiveArgvAndEnv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rebase -i", gitexec.Result{})
	r := &Repo{Runner: f}
	env := []string{"GIT_SEQUENCE_EDITOR=/usr/bin/gg __rebase-seq /tmp/p.json"}
	if err := r.RebaseInteractive(context.Background(), "", "main", env); err != nil {
		t.Fatalf("rebase -i: %v", err)
	}
	if want := []string{"rebase", "-i", "main"}; !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
	if !reflect.DeepEqual(f.Calls[0].Env, env) {
		t.Fatalf("env = %v, want %v", f.Calls[0].Env, env)
	}
}

// Real-git: --merges counting over a range with and without a merge commit.
func TestHasMergeCommits(t *testing.T) {
	dir, runner := newTestRepo(t) // one commit "initial" on main
	repo := &Repo{Runner: runner}
	git := func(args ...string) {
		c := exec.Command("git", append([]string{"-C", dir}, args...)...)
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("checkout", "-b", "feature")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "feat a")
	has, err := repo.HasMergeCommits(context.Background(), "", "main", "feature")
	if err != nil {
		t.Fatalf("has-merge: %v", err)
	}
	if has {
		t.Fatal("linear range must report no merge commits")
	}
	git("checkout", "-b", "side", "main")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	git("add", ".")
	git("commit", "-m", "side b")
	git("checkout", "feature")
	git("merge", "--no-ff", "-m", "merge side", "side")
	has, err = repo.HasMergeCommits(context.Background(), "", "main", "feature")
	if err != nil {
		t.Fatalf("has-merge: %v", err)
	}
	if !has {
		t.Fatal("range with a merge commit must report true")
	}
}

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
