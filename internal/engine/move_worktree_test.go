package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMoveWorktreeHappyMove(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/move", "wt-move")
	dest := filepath.Join(filepath.Dir(dir), "moved-elsewhere")

	res, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if res.Path != dest {
		t.Fatalf("result.Path = %q, want %q", res.Path, dest)
	}
	if worktreeListed(t, dir, wt) {
		t.Fatal("old path should no longer be listed")
	}
	if !worktreeListed(t, dir, dest) {
		t.Fatal("dest should be listed")
	}
}

func TestMoveWorktreeMissingFields(t *testing.T) {
	t.Parallel()
	_, repo := newRepo(t)
	_, err := MoveWorktree{}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "Path and Dest are required") {
		t.Fatalf("want missing-fields error, got %v", err)
	}
}

func TestMoveWorktreeRefusesMainWorktree(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	dest := filepath.Join(filepath.Dir(dir), "main-dest")
	_, err := MoveWorktree{Path: dir, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "main worktree") {
		t.Fatalf("want main-worktree guard error, got %v", err)
	}
}

func TestMoveWorktreeDestExists(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/destexists", "wt-destexists")
	dest := filepath.Join(filepath.Dir(dir), "wt-destexists-dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("want dest-exists error, got %v", err)
	}
}

func TestMoveWorktreeDestParentMissing(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/noparent", "wt-noparent")
	parent := filepath.Join(filepath.Dir(dir), "nonexistent-parent")
	dest := filepath.Join(parent, "wt-noparent-dest")

	_, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), parent) {
		t.Fatalf("want error naming missing parent %q, got %v", parent, err)
	}
}

func TestMoveWorktreeDestInsideSource(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/inside", "wt-inside")
	dest := filepath.Join(wt, "sub")

	_, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "inside the worktree") {
		t.Fatalf("want dest-inside-source error, got %v", err)
	}
}

func TestMoveWorktreeLockedUnlockAndMove(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/lockedmove", "wt-lockedmove")
	gitIn(t, dir, "worktree", "lock", wt)
	dest := filepath.Join(filepath.Dir(dir), "wt-lockedmove-dest")

	res, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"move-worktree-locked": "unlock-and-move"}})
	if err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if !worktreeListed(t, dir, dest) {
		t.Fatal("dest should be listed after unlock-and-move")
	}
}

func TestMoveWorktreeLockedAbort(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	wt := addWorktree(t, dir, "feature/lockedabort", "wt-lockedabort")
	gitIn(t, dir, "worktree", "lock", wt)
	dest := filepath.Join(filepath.Dir(dir), "wt-lockedabort-dest")

	res, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"move-worktree-locked": "abort"}})
	if err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	if res.Changed {
		t.Fatal("aborting the lock prompt should leave the worktree unmoved")
	}
	if !worktreeListed(t, dir, wt) {
		t.Fatal("old path should still be listed after abort")
	}
	gitIn(t, dir, "worktree", "unlock", wt) // let t.TempDir cleanup proceed
}

func TestMoveWorktreeDoneOnSuccessOnly(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)

	// Case 1: happy move emits exactly one Done.
	wt := addWorktree(t, dir, "feature/donecheck", "wt-donecheck")
	dest := filepath.Join(filepath.Dir(dir), "wt-donecheck-dest")
	ch := make(chan Event, 32)
	_, err := MoveWorktree{Path: wt, Dest: dest}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch, Decider: MapDecider{}})
	close(ch)
	if err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	doneCount := 0
	for _, e := range drain(ch) {
		if _, ok := e.(Done); ok {
			doneCount++
		}
	}
	if doneCount != 1 {
		t.Fatalf("happy move Done events = %d, want 1", doneCount)
	}

	// Case 2: locked-then-abort emits no Done.
	wt2 := addWorktree(t, dir, "feature/donecheck2", "wt-donecheck2")
	gitIn(t, dir, "worktree", "lock", wt2)
	dest2 := filepath.Join(filepath.Dir(dir), "wt-donecheck2-dest")
	ch2 := make(chan Event, 32)
	_, err = MoveWorktree{Path: wt2, Dest: dest2}.Run(
		context.Background(),
		OpDeps{Repo: repo, Events: ch2, Decider: MapDecider{"move-worktree-locked": "abort"}})
	close(ch2)
	if err != nil {
		t.Fatalf("MoveWorktree: %v", err)
	}
	for _, e := range drain(ch2) {
		if _, ok := e.(Done); ok {
			t.Fatal("abort path should not emit Done")
		}
	}
	gitIn(t, dir, "worktree", "unlock", wt2) // let t.TempDir cleanup proceed
}
