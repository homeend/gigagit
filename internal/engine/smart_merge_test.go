package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
)

// gitE runs a raw git command in dir (mirrors the run closure in newRepo).
func gitE(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func gitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git %v: %v", args, err)
	}
	return strings.TrimSpace(string(out))
}

// branchWithCommit creates branch off main with one extra commit, returns to main.
func branchWithCommit(t *testing.T, dir, branch, file string) {
	t.Helper()
	gitE(t, dir, "checkout", "-b", branch)
	os.WriteFile(filepath.Join(dir, file), []byte(branch+"\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", branch+" change")
	gitE(t, dir, "checkout", "main")
}

func TestSmartMergeGuards(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")

	cases := []struct {
		name string
		op   SmartMerge
		want string
	}{
		{"empty source", SmartMerge{}, "Source is required"},
		{"same branch", SmartMerge{Source: "main", Target: "main"}, "source and target"},
		{"missing source", SmartMerge{Source: "nope"}, "no such branch: nope"},
		{"missing target", SmartMerge{Source: "feat", Target: "nope"}, "no such branch: nope"},
	}
	for _, tc := range cases {
		_, err := tc.op.Run(context.Background(), OpDeps{Repo: repo})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want containing %q", tc.name, err, tc.want)
		}
	}
}

func TestSmartMergeDetachedHeadNeedsExplicitTarget(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "--detach")

	_, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("err = %v, want detached HEAD guard", err)
	}
}

func TestSmartMergeIntoCurrentBranch(t *testing.T) {
	dir, repo := newRepo(t)
	branchWithCommit(t, dir, "feat", "feat.txt")

	res, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !res.Changed || !strings.Contains(res.Summary, "merged feat into main") {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing after merge")
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("on %s, want main", got)
	}
}

func TestSmartMergeIntoBranchInOtherWorktree(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "side")
	wt := filepath.Join(dir, "..", "side-wt")
	gitE(t, dir, "worktree", "add", wt, "side")
	// advance main so there is something to merge into side
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "main change")

	res, err := SmartMerge{Source: "main", Target: "side"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if !strings.Contains(res.Summary, "in worktree") {
		t.Fatalf("summary = %q, want worktree mention", res.Summary)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "main" {
		t.Fatalf("current branch %s changed, want main (we stay put)", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "new.txt")); err != nil {
		t.Fatal("merge did not land in the side worktree")
	}
}

func TestSmartMergeIntoUncheckedOutBranchSwitchesAndStays(t *testing.T) {
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "target")
	branchWithCommit(t, dir, "feat", "feat.txt")
	// dirty file on main → autostash must carry it to target and pop it back
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)

	res, err := SmartMerge{Source: "feat", Target: "target"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := gitOut(t, dir, "branch", "--show-current"); got != "target" {
		t.Fatalf("on %s, want target (merge ends on Target)", got)
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing on target after merge")
	}
	got, _ := os.ReadFile(filepath.Join(dir, "README.md"))
	if string(got) != "dirty\n" {
		t.Fatal("autostashed change was not restored")
	}
	if !strings.Contains(res.Summary, "merged feat into target") {
		t.Fatalf("summary = %q", res.Summary)
	}
	if out := gitOut(t, dir, "stash", "list"); out != "" {
		t.Fatalf("stash not popped: %q", out)
	}
}

// conflictRepo: main and feat both edit shared.txt → guaranteed conflict.
func conflictRepo(t *testing.T) (string, *git.Repo) {
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
	return dir, repo
}

func TestSmartMergeConflictAbort(t *testing.T) {
	dir, repo := conflictRepo(t)
	res, err := SmartMerge{Source: "feat"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"merge-conflict": "abort"}})
	if err != nil {
		t.Fatalf("chosen abort must not be an error: %v", err)
	}
	if !strings.Contains(res.Summary, "aborted") {
		t.Fatalf("summary = %q", res.Summary)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort, want main's version", got)
	}
}

func TestSmartMergeConflictKeep(t *testing.T) {
	dir, repo := conflictRepo(t)
	res, err := SmartMerge{Source: "feat"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"merge-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("keep-conflicts must surface an error (CLI exit 1)")
	}
	if !strings.Contains(res.Summary, "conflicts") {
		t.Fatalf("summary = %q", res.Summary)
	}
	// MERGE_HEAD must still exist (merge left in progress)
	if gitOut(t, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == "" {
		t.Fatal("merge state was not kept")
	}
}

func TestSmartMergeConflictUndecidedLeavesMergeState(t *testing.T) {
	dir, repo := conflictRepo(t)
	_, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("undecided conflict must error")
	}
	// The decision fires only after the conflict exists: state stays.
	if gitOut(t, dir, "rev-parse", "-q", "--verify", "MERGE_HEAD") == "" {
		t.Fatal("expected merge still in progress")
	}
}
