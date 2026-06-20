package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCherryPickGuardEmptyCommit(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CherryPick{}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "Commit is required") {
		t.Fatalf("err = %v, want 'Commit is required'", err)
	}
}

// A clean cherry-pick lands the picked commit's change on the current branch.
func TestCherryPickCleanApplies(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("from feat\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add new.txt")
	pick := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")

	res, err := CherryPick{Commit: pick}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "cherry-picked") {
		t.Fatalf("result = %+v", res)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "new.txt"))
	if string(got) != "from feat\n" {
		t.Fatalf("new.txt = %q on main, want applied", got)
	}
}

// A dirty tree is autostashed and restored across a clean cherry-pick.
func TestCherryPickAutostashRestores(t *testing.T) {
	dir, repo := newRepo(t)
	// seed a tracked file so we can dirty it
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("committed\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "wip base")

	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("from feat\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add new.txt")
	pick := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")

	// dirty the tree (uncommitted edit to wip.txt)
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("dirty edit\n"), 0o644)

	res, err := CherryPick{Commit: pick}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "new.txt")); string(got) != "from feat\n" {
		t.Fatalf("new.txt not applied: %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wip.txt")); string(got) != "dirty edit\n" {
		t.Fatalf("autostash not restored: wip.txt = %q", got)
	}
}

// setupCherryPickConflict leaves a conflicting commit on feat and HEAD on main.
func setupCherryPickConflict(t *testing.T, dir string) (pick string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "commit", "-am", "feat change")
	pick = gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitE(t, dir, "commit", "-am", "main change")
	return pick
}

func TestCherryPickConflictKeepLeavesState(t *testing.T) {
	dir, repo := newRepo(t)
	pick := setupCherryPickConflict(t, dir)

	res, err := CherryPick{Commit: pick}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("kept conflict must return an error")
	}
	if !res.Changed || !strings.Contains(res.Summary, "conflicts") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); !in {
		t.Fatal("keep-conflicts must leave CHERRY_PICK_HEAD set")
	}
}

func TestCherryPickConflictAbortRestores(t *testing.T) {
	dir, repo := newRepo(t)
	pick := setupCherryPickConflict(t, dir)

	res, err := CherryPick{Commit: pick}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the cherry-pick state")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "main\n" {
		t.Fatalf("f.txt = %q after abort, want main", got)
	}
}

// ContinueOp routes a resolved cherry-pick to `cherry-pick --continue`.
func TestContinueOpFinishesCherryPick(t *testing.T) {
	dir, repo := newRepo(t)
	pick := setupCherryPickConflict(t, dir)

	_, _ = CherryPick{Commit: pick}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "keep-conflicts"}})

	// resolve: keep theirs (the picked content) and stage
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("resolved\n"), 0o644)
	gitE(t, dir, "add", "f.txt")

	res, err := ContinueOp{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("continue: %v", err)
	}
	if !strings.Contains(res.Summary, "cherry-pick continued") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("continue must clear the cherry-pick state")
	}
}

// AbortOp routes an in-progress cherry-pick to `cherry-pick --abort`.
func TestAbortOpAbortsCherryPick(t *testing.T) {
	dir, repo := newRepo(t)
	pick := setupCherryPickConflict(t, dir)

	_, _ = CherryPick{Commit: pick}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "keep-conflicts"}})

	res, err := AbortOp{}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("abort op: %v", err)
	}
	if !strings.Contains(res.Summary, "cherry-pick aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("abort op must clear the cherry-pick state")
	}
}
