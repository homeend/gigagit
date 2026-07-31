package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/rebaseplan"
)

// fourCommitBranch builds main(initial) -> a -> b -> c -> d on "work", each
// commit touching a distinct file so reorders don't conflict.
func fourCommitBranch(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	for _, n := range []string{"a", "b", "c", "d"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte(n+"\n"), 0o644)
		gitE(t, dir, "add", ".")
		gitE(t, dir, "commit", "-m", n)
	}
	return dir, repo
}

// runEdit drives the full single-commit edit path the TUI uses: derive Onto via
// rebaseplan.OntoFor (the REAL derivation), load the range, build the plan, run
// InteractiveRebase. Returns the op result + error.
func runEdit(t *testing.T, dir string, repo *git.Repo, gg, targetRev string, e rebaseplan.Edit, deps OpDeps) (Result, error) {
	t.Helper()
	sha := shaOf(t, dir, targetRev)
	onto := rebaseplan.OntoFor(sha, e)
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range %s..work: %v", onto, err)
	}
	plan, err := rebaseplan.BuildSingleEdit(commits, sha, e)
	if err != nil {
		t.Fatalf("buildSingleEdit: %v", err)
	}
	deps.Repo = repo
	return InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}.Run(context.Background(), deps)
}

func TestSingleCommitDrop(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	if _, err := runEdit(t, dir, repo, gg, "work~1", rebaseplan.EditDrop, OpDeps{}); err != nil { // drop c
		t.Fatalf("drop: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"d", "b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after drop c: %v, want %v", got, want)
	}
}

func TestSingleCommitDropNewest(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Drop d (the branch tip). The range d~1..work holds ONLY d, so the todo
	// must carry an explicit `drop` line — an empty todo makes git abort with
	// "error: nothing to do" (the bug this test pins).
	if _, err := runEdit(t, dir, repo, gg, "work", rebaseplan.EditDrop, OpDeps{}); err != nil {
		t.Fatalf("drop newest: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"c", "b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after drop d: %v, want %v", got, want)
	}
}

func TestMultiCommitDropEntireRange(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Drop c and d — the two newest, so the selection IS the whole rebase
	// range (onto = c^): every entry is a Drop and the todo has no picks.
	onto := shaOf(t, dir, "work~1") + "^" // c's parent
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	c, d := shaOf(t, dir, "work~1"), shaOf(t, dir, "work")
	plan, err := rebaseplan.BuildDrop(commits, []string{c, d})
	if err != nil {
		t.Fatalf("buildDrop: %v", err)
	}
	if _, err := (InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"b", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after drop c,d: %v, want %v", got, want)
	}
}

func TestMultiCommitDrop(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Drop b and d (non-adjacent) in one rebase. Base onto the oldest target's
	// parent (b^ == a), the same onto the TUI derives from the oldest selected.
	onto := shaOf(t, dir, "work~2") + "^" // b's parent
	commits, err := repo.LogRangeMessages(context.Background(), onto, "work")
	if err != nil {
		t.Fatalf("range: %v", err)
	}
	b, d := shaOf(t, dir, "work~2"), shaOf(t, dir, "work")
	plan, err := rebaseplan.BuildDrop(commits, []string{d, b}) // order-independent
	if err != nil {
		t.Fatalf("buildDrop: %v", err)
	}
	if _, err := (InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("drop: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"c", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after drop b,d: %v, want %v", got, want)
	}
}

func TestSingleCommitMoveDown(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Move c down (older): swap c with b. ~2 derivation is the load-bearing case.
	if _, err := runEdit(t, dir, repo, gg, "work~1", rebaseplan.EditMoveDown, OpDeps{}); err != nil {
		t.Fatalf("move down: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"d", "b", "c", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after move c down: %v, want %v", got, want)
	}
}

func TestSingleCommitMoveUp(t *testing.T) {
	gg := buildGG(t)
	dir, repo := fourCommitBranch(t)
	// Move b up (newer): swap b with c.
	if _, err := runEdit(t, dir, repo, gg, "work~2", rebaseplan.EditMoveUp, OpDeps{}); err != nil {
		t.Fatalf("move up: %v", err)
	}
	got := subjects(t, dir, "main..work") // newest-first
	if want := []string{"d", "b", "c", "a"}; strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("after move b up: %v, want %v", got, want)
	}
}

func TestSingleCommitEditConflictPauses(t *testing.T) {
	gg := buildGG(t)
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	// a and b both touch the SAME file/line, so reordering them conflicts.
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "a")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "b")

	// Move a up (swap a,b) → conflict; keep-conflicts → paused.
	res, err := runEdit(t, dir, repo, gg, "work~1", rebaseplan.EditMoveUp,
		OpDeps{Decider: MapDecider{"rebase-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("expected a paused-on-conflict error")
	}
	if !res.Changed || !strings.Contains(res.Summary, "paused") {
		t.Fatalf("result = %+v, want a paused summary", res)
	}
	inRebase, perr := repo.RebaseInProgress(context.Background(), "")
	if perr != nil {
		t.Fatalf("rebase-in-progress probe: %v", perr)
	}
	if !inRebase {
		t.Fatal("expected a rebase to be left in progress (no silent corruption)")
	}
}
