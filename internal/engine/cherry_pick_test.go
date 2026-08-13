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

	res, err := CherryPick{Commits: []string{pick}}.Run(context.Background(), OpDeps{Repo: repo})
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

	res, err := CherryPick{Commits: []string{pick}}.Run(context.Background(), OpDeps{Repo: repo})
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

	res, err := CherryPick{Commits: []string{pick}}.Run(context.Background(),
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

	res, err := CherryPick{Commits: []string{pick}}.Run(context.Background(),
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

// Cherry-picking a commit already on the branch leaves CHERRY_PICK_HEAD set with
// a clean tree; the op must auto-abort and return a legible error (never trap).
func TestCherryPickAlreadyAppliedAutoAborts(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add g")
	pick := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	// apply it once cleanly
	if _, err := (CherryPick{Commits: []string{pick}}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("first pick: %v", err)
	}
	// apply the SAME commit again → already present
	_, err := CherryPick{Commits: []string{pick}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "keep-conflicts"}})
	if err == nil || !strings.Contains(err.Error(), "already on this branch") {
		t.Fatalf("err = %v, want 'already on this branch'", err)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("an already-applied pick must not leave CHERRY_PICK_HEAD set")
	}
}

// The autostash is restored after the abort path (dirty tree + abort).
func TestCherryPickAbortRestoresAutostash(t *testing.T) {
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "commit", "-am", "feat change")
	pick := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitE(t, dir, "commit", "-am", "main change")

	// dirty an unrelated tracked file so autostash kicks in
	os.WriteFile(filepath.Join(dir, "wip.txt"), []byte("dirty\n"), 0o644)
	gitE(t, dir, "add", "wip.txt") // staged-new so it's part of the stash

	res, err := CherryPick{Commits: []string{pick}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path: %v", err)
	}
	if res.Changed {
		t.Fatalf("abort should report no change: %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "wip.txt")); string(got) != "dirty\n" {
		t.Fatalf("autostash not restored after abort: wip.txt = %q", got)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "main\n" {
		t.Fatalf("f.txt = %q after abort, want main", got)
	}
}

// Several commits in one op land in the given (oldest-first) order.
func TestCherryPickMultipleCleanApplies(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add one")
	first := gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "two.txt"), []byte("two\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add two")
	second := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")

	res, err := CherryPick{Commits: []string{first, second}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "cherry-picked 2 commits") {
		t.Fatalf("result = %+v", res)
	}
	// Newest-first log order proves the picks applied oldest→newest.
	if got := gitOut(t, dir, "log", "--format=%s", "-2"); got != "add two\nadd one" {
		t.Fatalf("log order = %q, want add two / add one", got)
	}
}

// setupTwoPickSecondConflicts leaves commits A (clean) and B (conflicting with
// main) on feat and HEAD on main; returns them oldest-first.
func setupTwoPickSecondConflicts(t *testing.T, dir string) (clean, conflicting string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "ok.txt"), []byte("ok\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "clean pick")
	clean = gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "commit", "-am", "feat change")
	conflicting = gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main\n"), 0o644)
	gitE(t, dir, "commit", "-am", "main change")
	return clean, conflicting
}

// Aborting a mid-sequence conflict rewinds the WHOLE sequence: commits applied
// before the conflict are rewound too (all-or-nothing).
func TestCherryPickMultipleConflictAbortRewindsAll(t *testing.T) {
	dir, repo := newRepo(t)
	clean, conflicting := setupTwoPickSecondConflicts(t, dir)
	tip := gitOut(t, dir, "rev-parse", "HEAD")

	res, err := CherryPick{Commits: []string{clean, conflicting}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "abort"}})
	if err != nil {
		t.Fatalf("abort path: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("result = %+v", res)
	}
	if got := gitOut(t, dir, "rev-parse", "HEAD"); got != tip {
		t.Fatalf("HEAD = %s after abort, want pre-op tip %s (nothing applied)", got, tip)
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err == nil {
		t.Fatal("abort must rewind the already-applied first pick (ok.txt present)")
	}
}

// keep-conflicts on a mid-sequence conflict leaves the sequencer paused so the
// resume lane (`cherry-pick --continue`) can finish the remaining picks.
func TestCherryPickMultipleConflictKeepLeavesSequence(t *testing.T) {
	dir, repo := newRepo(t)
	clean, conflicting := setupTwoPickSecondConflicts(t, dir)

	res, err := CherryPick{Commits: []string{clean, conflicting}}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"cherry-pick-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("kept conflict must return an error")
	}
	if !res.Changed || !strings.Contains(res.Summary, "conflicts") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); !in {
		t.Fatal("keep-conflicts must leave the sequence paused")
	}
	// The clean first pick already landed.
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("first pick should be applied before the conflict: %v", err)
	}
}

// An already-applied commit inside a multi-pick is skipped and the rest land.
func TestCherryPickMultipleSkipsAlreadyApplied(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add one")
	first := gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "two.txt"), []byte("two\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add two")
	second := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	// Land the first commit's change ahead of time → it becomes an empty pick.
	gitE(t, dir, "cherry-pick", first)

	res, err := CherryPick{Commits: []string{first, second}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "cherry-picked 1 of 2 commits") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "two.txt")); err != nil {
		t.Fatalf("second pick should land: %v", err)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("skip path must end with a clean sequencer state")
	}
}

// When EVERY commit in a multi-pick is already applied, the op ends cleanly
// with nothing applied (no error, unlike the single-commit path).
func TestCherryPickMultipleAllAlreadyApplied(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "one.txt"), []byte("one\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add one")
	first := gitOut(t, dir, "rev-parse", "HEAD")
	os.WriteFile(filepath.Join(dir, "two.txt"), []byte("two\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "add two")
	second := gitOut(t, dir, "rev-parse", "HEAD")
	gitE(t, dir, "checkout", "main")
	gitE(t, dir, "cherry-pick", first, second)

	res, err := CherryPick{Commits: []string{first, second}}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("cherry-pick: %v", err)
	}
	if res.Changed || !strings.Contains(res.Summary, "nothing to apply") {
		t.Fatalf("result = %+v", res)
	}
	if in, _ := repo.CherryPickInProgress(context.Background(), ""); in {
		t.Fatal("all-skipped path must end with a clean sequencer state")
	}
}

// ContinueOp routes a resolved cherry-pick to `cherry-pick --continue`.
func TestContinueOpFinishesCherryPick(t *testing.T) {
	dir, repo := newRepo(t)
	pick := setupCherryPickConflict(t, dir)

	_, _ = CherryPick{Commits: []string{pick}}.Run(context.Background(),
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

	_, _ = CherryPick{Commits: []string{pick}}.Run(context.Background(),
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
