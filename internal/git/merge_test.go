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

func TestMergeArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Merge(context.Background(), "", "feat"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := []string{"merge", "--no-edit", "feat"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeArgvWithDir(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.Merge(context.Background(), "/wt/x", "feat"); err != nil {
		t.Fatalf("merge: %v", err)
	}
	want := []string{"-C", "/wt/x", "merge", "--no-edit", "feat"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeAbortArgv(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git merge --abort", gitexec.Result{})
	r := &Repo{Runner: f}
	if err := r.MergeAbort(context.Background(), "/wt/x"); err != nil {
		t.Fatalf("merge abort: %v", err)
	}
	want := []string{"-C", "/wt/x", "merge", "--abort"}
	if !reflect.DeepEqual(f.Calls[0].Argv, want) {
		t.Fatalf("argv = %v, want %v", f.Calls[0].Argv, want)
	}
}

func TestMergeInProgressExitOneMeansNo(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetResponse("git rev-parse MERGE_HEAD", gitexec.Result{ExitCode: 1})
	f.SetError("git rev-parse MERGE_HEAD", errors.New("exit status 1"))
	r := &Repo{Runner: f}
	in, err := r.MergeInProgress(context.Background(), "")
	if err != nil {
		t.Fatalf("MergeInProgress: %v", err)
	}
	if in {
		t.Fatal("exit 1 must mean 'no merge in progress'")
	}
}

// Real-git: build an actual conflict, observe MERGE_HEAD, abort, observe clean.
func TestMergeConflictDetectAndAbortReal(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "branch", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "main change")
	gitIn(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitIn(t, dir, "commit", "-am", "feat change")
	gitIn(t, dir, "checkout", "main")

	if in, _ := repo.MergeInProgress(context.Background(), ""); in {
		t.Fatal("no merge should be in progress yet")
	}
	if err := repo.Merge(context.Background(), "", "feat"); err == nil {
		t.Fatal("conflicting merge must fail")
	}
	in, err := repo.MergeInProgress(context.Background(), "")
	if err != nil || !in {
		t.Fatalf("MergeInProgress after conflict = (%v, %v), want (true, nil)", in, err)
	}
	if err := repo.MergeAbort(context.Background(), ""); err != nil {
		t.Fatalf("merge abort: %v", err)
	}
	if in, _ := repo.MergeInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the merge state")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "main\n" {
		t.Fatalf("f.txt = %q after abort, want %q", got, "main\n")
	}
}

func TestMergeFFOnly(t *testing.T) {
	dir, runner := newTestRepo(t)
	repo := &Repo{Runner: runner}
	ctx := context.Background()

	// base commit on main, then feat ahead by one commit; return to main.
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "base")
	gitIn(t, dir, "branch", "feat")
	gitIn(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "ahead")
	featTip := revParse(t, dir, "HEAD")
	gitIn(t, dir, "checkout", "main")

	// Fast-forward main to feat's tip.
	if err := repo.MergeFFOnly(ctx, "", featTip); err != nil {
		t.Fatalf("MergeFFOnly: %v", err)
	}
	if got := revParse(t, dir, "HEAD"); got != featTip {
		t.Fatalf("HEAD = %s, want %s (advanced)", got, featTip)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.txt")); err != nil {
		t.Fatalf("worktree not updated: %v", err)
	}

	// A non-descendant target must be refused (divergent commit on a new root).
	gitIn(t, dir, "checkout", "--orphan", "other")
	os.WriteFile(filepath.Join(dir, "z.txt"), []byte("z\n"), 0o644)
	gitIn(t, dir, "add", "z.txt")
	gitIn(t, dir, "commit", "-m", "orphan")
	other := revParse(t, dir, "HEAD")
	gitIn(t, dir, "checkout", "feat")
	if err := repo.MergeFFOnly(ctx, "", other); err == nil {
		t.Fatal("MergeFFOnly to a non-descendant must error")
	}
}
