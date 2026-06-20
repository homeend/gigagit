package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevertGuardEmptyCommit(t *testing.T) {
	_, repo := newRepo(t)
	_, err := Revert{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Commit is required") {
		t.Fatalf("err = %v, want 'Commit is required'", err)
	}
}

// A clean revert removes the change the target commit introduced.
func TestRevertCleanUndoes(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("added\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add new.txt")
	target := gitOut(t, dir, "rev-parse", "HEAD")

	res, err := Revert{Commit: target}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "reverted") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("revert should have removed new.txt, stat err = %v", err)
	}
}

// A dirty tree is autostashed and restored across a clean revert.
func TestRevertAutostashRestores(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("committed\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "wip base")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("added\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add new.txt")
	target := gitOut(t, dir, "rev-parse", "HEAD")

	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("dirty edit\n"), 0o644)

	res, err := Revert{Commit: target}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("revert: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("revert should have removed new.txt")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wip.txt")); string(got) != "dirty edit\n" {
		t.Fatalf("autostash not restored: wip.txt = %q", got)
	}
}

// setupRevertConflict leaves HEAD at "to v3"; reverting "to v2" (returned)
// conflicts.
func setupRevertConflict(t *testing.T, dir string) (target string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644)
	gitE(t, dir, "commit", "-am", "to v2")
	target = gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v3\n"), 0o644)
	gitE(t, dir, "commit", "-am", "to v3")
	return target
}

func TestRevertConflictKeepLeavesState(t *testing.T) {
	dir, repo := newRepo(t)
	target := setupRevertConflict(t, dir)

	res, err := Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("kept conflict must return an error")
	}
	if !res.Changed || !strings.Contains(res.Summary, "conflicts") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); !in {
		t.Fatal("keep-conflicts must leave REVERT_HEAD set")
	}
}

func TestRevertConflictAbortRestores(t *testing.T) {
	dir, repo := newRepo(t)
	target := setupRevertConflict(t, dir)

	res, err := Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the revert state")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "v3\n" {
		t.Fatalf("f.txt = %q after abort, want v3", got)
	}
}

// Reverting a commit whose change is already undone: git refuses with a clean
// tree (no REVERT_HEAD), so the op returns a legible error and never traps.
func TestRevertAlreadyUndone(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add a")
	target := gitOut(t, dir, "rev-parse", "HEAD")

	if _, err := (Revert{Commit: target}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("first revert: %v", err)
	}
	// revert the SAME commit again — the change is already undone
	_, err := Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("reverting an already-undone commit must return an error")
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("must not leave REVERT_HEAD set (no trap)")
	}
}

// The autostash is restored after the abort path (dirty tree + abort).
func TestRevertAbortRestoresAutostash(t *testing.T) {
	dir, repo := newRepo(t)
	target := setupRevertConflict(t, dir)
	// dirty an unrelated tracked file so autostash kicks in
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("dirty\n"), 0o644)
	gitE(t, dir, "add", "wip.txt")

	res, err := Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path: %v", err)
	}
	if res.Changed {
		t.Fatalf("abort should report no change: %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wip.txt")); string(got) != "dirty\n" {
		t.Fatalf("autostash not restored after abort: wip.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "v3\n" {
		t.Fatalf("f.txt = %q after abort, want v3", got)
	}
}

// Reverting a merge commit without -m is refused outright with a legible error.
func TestRevertMergeCommitRefused(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("1\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat")
	gitE(t, dir, "checkout", "main")
	gitE(t, dir, "merge", "--no-ff", "feat", "-m", "merge feat")
	mergeSHA := gitOut(t, dir, "rev-parse", "HEAD")

	_, err := Revert{Commit: mergeSHA}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "merge") {
		t.Fatalf("err = %v, want git's merge-needs-mainline error", err)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("a refused merge revert must not leave REVERT_HEAD set")
	}
}

// ContinueOp routes a resolved revert to `revert --continue`.
func TestContinueOpFinishesRevert(t *testing.T) {
	dir, repo := newRepo(t)
	target := setupRevertConflict(t, dir)
	_, _ = Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "keep-conflicts"}})

	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644)
	gitE(t, dir, "add", "f.txt")

	res, err := ContinueOp{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if !strings.Contains(res.Summary, "revert continued") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("continue must clear the revert state")
	}
}

// AbortOp routes an in-progress revert to `revert --abort`.
func TestAbortOpAbortsRevert(t *testing.T) {
	dir, repo := newRepo(t)
	target := setupRevertConflict(t, dir)
	_, _ = Revert{Commit: target}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"revert-conflict": "keep-conflicts"}})

	res, err := AbortOp{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("abort op: %v", err)
	}
	if !strings.Contains(res.Summary, "revert aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RevertInProgress(context.Background(), ""); in {
		t.Fatal("abort op must clear the revert state")
	}
}
