package engine

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
)

func TestSmartRebaseGuards(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")

	cases := []struct {
		name string
		op   SmartRebase
		want string
	}{
		{"empty onto", SmartRebase{Branch: "feat"}, "Onto is required"},
		{"same branch", SmartRebase{Branch: "main", Onto: "main"}, "branch and base"},
		{"missing branch", SmartRebase{Branch: "nope", Onto: "main"}, "no such branch: nope"},
		{"missing onto", SmartRebase{Branch: "feat", Onto: "nope"}, "no such commit: nope"},
	}
	for _, tc := range cases {
		_, err := tc.op.Run(context.Background(), OpDeps{Repo: repo})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSmartRebaseDetachedHeadNeedsExplicitBranch(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "--detach")

	_, err := SmartRebase{Onto: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("err = %v, want detached HEAD guard", err)
	}
}

// divergedRepo: feat and main diverge from a common base with DISJOINT files
// (feat.txt vs main.txt) so a rebase replays cleanly. Leaves HEAD on main.
func divergedRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")
	gitE(t, dir, "checkout", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "feat change")
	gitE(t, dir, "checkout", "main")
	return dir, repo
}

func TestSmartRebaseCurrentBranchOntoBase(t *testing.T) {
	// rung 1: feat is current; rebase it onto main (disjoint files → clean).
	dir, repo := divergedRepo(t)
	gitE(t, dir, "checkout", "feat")

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "rebased feat onto main") {
		t.Fatalf("result = %+v", res)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s, want feat", got)
	}
	// feat replayed onto main → main.txt (main's change) now in feat's tree.
	if _, err := os.Stat(filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal("main.txt missing after rebase — feat was not replayed onto main")
	}
}

func TestSmartRebaseBranchInOtherWorktree(t *testing.T) {
	// rung 2: feat lives in a linked worktree; rebase happens there, we stay.
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	wt := filepath.Join(dir, "..", "feat-wt")
	gitE(t, dir, "worktree", "add", wt, "feat")
	// give feat a commit (in its worktree) and advance main disjointly
	os.WriteFile(filepath.Join(wt, "feat.txt"), []byte("f\n"), 0o644)
	gitE(t, wt, "add", ".")
	gitE(t, wt, "commit", "-m", "feat change")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if !strings.Contains(res.Summary, "in worktree") {
		t.Fatalf("summary = %q, want worktree mention", res.Summary)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("current branch %s changed, want main (we stay put)", got)
	}
	// feat replayed onto main → main.txt now present in feat's worktree.
	if _, err := os.Stat(filepath.Join(wt, "main.txt")); err != nil {
		t.Fatal("rebase did not land in the feat worktree")
	}
}

func TestSmartRebaseUncheckedOutBranchSwitchesAndStays(t *testing.T) {
	// rung 3: feat is not checked out anywhere; dirty main autostashes.
	dir, repo := divergedRepo(t) // ends on main, feat not checked out
	// dirty tracked file on main → autostash must carry it back to main? No:
	// we end on feat. The stash is popped on feat after a clean rebase.
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("rebase: %v", err)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s, want feat (rebase ends on Branch)", got)
	}
	if !strings.Contains(res.Summary, "rebased feat onto main") {
		t.Fatalf("summary = %q", res.Summary)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(got) != "dirty\n" {
		t.Fatal("autostashed change was not restored")
	}
	if out := gitOut(t, dir, "stash", "list"); out != "" {
		t.Fatalf("stash not popped: %q", out)
	}
}

// rebaseConflictRepo: feat and main both edit shared.txt → guaranteed rebase
// conflict; feat is the current branch.
func rebaseConflictRepo(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "base")
	gitE(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitE(t, dir, "commit", "-am", "feat change")
	gitE(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitE(t, dir, "commit", "-am", "main change")
	gitE(t, dir, "checkout", "feat")
	return dir, repo
}

func TestSmartRebaseConflictAbort(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"rebase-conflict": "abort"}})
	if err != nil {
		t.Fatalf("chosen abort must not be an error: %v", err)
	}
	if !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); in {
		t.Fatal("abort must clear the rebase state")
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "feat" {
		t.Fatalf("on %s after abort, want feat", got)
	}
}

func TestSmartRebaseConflictKeep(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	res, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"rebase-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("keep-conflicts must surface an error (CLI exit 1)")
	}
	if !strings.Contains(res.Summary, "conflict") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); !in {
		t.Fatal("rebase state was not kept")
	}
	_ = dir
}

func TestSmartRebaseConflictUndecidedLeavesRebaseState(t *testing.T) {
	dir, repo := rebaseConflictRepo(t)
	_, err := SmartRebase{Branch: "feat", Onto: "main"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("undecided conflict must error")
	}
	if in, _ := repo.RebaseInProgress(context.Background(), ""); !in {
		t.Fatal("expected rebase still in progress")
	}
	_ = dir
}

// A tag (a non-branch ref) is a valid rebase Onto — like InteractiveRebase,
// SmartRebase must accept any commit-ish, not only local branches. Also covers
// remote-tracking refs (origin/x).
func TestSmartRebaseOntoTag(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "tag", "base") // tag main's initial commit
	os.WriteFile(filepath.Join(dir, "m.txt"), []byte("m\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main work")

	// Rebasing main onto its ancestor tag is a no-op rebase; the point is that the
	// tag is ACCEPTED as Onto (no "no such branch" error).
	if _, err := (SmartRebase{Onto: "base"}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("rebase main onto tag base: %v", err)
	}
}
