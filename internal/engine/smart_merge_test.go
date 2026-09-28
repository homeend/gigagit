package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
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
	t.Parallel()
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")

	cases := []struct {
		name string
		op   SmartMerge
		want string
	}{
		{"empty source", SmartMerge{}, "Source is required"},
		{"same branch", SmartMerge{Source: "main", Target: "main"}, "source and target"},
		{"missing source", SmartMerge{Source: "nope"}, "no such commit: nope"},
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
	t.Parallel()
	dir, repo := newRepo(t)
	gitE(t, dir, "branch", "feat")
	gitE(t, dir, "checkout", "--detach")

	_, err := SmartMerge{Source: "feat"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "detached HEAD") {
		t.Fatalf("err = %v, want detached HEAD guard", err)
	}
}

func TestSmartMergeIntoCurrentBranch(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// A tag (a non-branch ref) is a valid merge Source — git merge <tag> works, and
// SmartMerge must not reject it as "no such branch". Also covers remote-tracking
// refs (origin/x), which fail the same local-branch check.
func TestSmartMergeSourceTag(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	branchWithCommit(t, dir, "feat", "feat.txt")
	gitE(t, dir, "tag", "v1", "feat") // tag the feature commit; HEAD stays on main

	res, err := SmartMerge{Source: "v1"}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("merge tag v1 into main: %v", err)
	}
	if !res.Changed {
		t.Fatal("expected the merge to change main")
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatalf("feat.txt missing after merging tag v1: %v", err)
	}
}

// A Message becomes the merge commit's message and implies --no-ff: the
// fixture fast-forwards without it (see TestSmartMergeIntoCurrentBranch),
// yet a real merge commit carrying the message must exist afterwards.
func TestSmartMergeMessageForcesMergeCommit(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	branchWithCommit(t, dir, "feat", "feat.txt")

	msg := "Merge feat: the feature\n\nBody line.\n\nTrailer: yes"
	res, err := SmartMerge{Source: "feat", Message: msg}.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil || !res.Changed {
		t.Fatalf("merge: %v, %+v", err, res)
	}
	if got := gitOut(t, dir, "log", "-1", "--format=%P"); len(strings.Fields(got)) != 2 {
		t.Fatalf("HEAD parents = %q, want a two-parent merge commit", got)
	}
	if got := gitOut(t, dir, "log", "-1", "--format=%B"); got != msg {
		t.Fatalf("merge message = %q, want %q", got, msg)
	}
}

// NoFF alone forces a merge commit with git's own message.
func TestSmartMergeNoFFKeepsGitsMessage(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	branchWithCommit(t, dir, "feat", "feat.txt")

	if _, err := (SmartMerge{Source: "feat", NoFF: true}).Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("merge: %v", err)
	}
	if got := gitOut(t, dir, "log", "-1", "--format=%P"); len(strings.Fields(got)) != 2 {
		t.Fatalf("HEAD parents = %q, want a two-parent merge commit", got)
	}
	if got := gitOut(t, dir, "log", "-1", "--format=%s"); !strings.HasPrefix(got, "Merge branch 'feat'") {
		t.Fatalf("subject = %q, want git's default", got)
	}
}

// git keeps -m in MERGE_MSG across a conflict, so a kept-then-resolved
// merge still commits with the caller's message.
func TestSmartMergeMessageSurvivesKeptConflict(t *testing.T) {
	t.Parallel()
	dir, repo := conflictRepo(t)
	_, err := SmartMerge{Source: "feat", Message: "Merge feat: resolved by hand"}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"merge-conflict": "keep-conflicts"}})
	if err == nil {
		t.Fatal("kept conflict must return an error")
	}
	b, rerr := os.ReadFile(filepath.Join(dir, ".git", "MERGE_MSG"))
	if rerr != nil || !strings.HasPrefix(string(b), "Merge feat: resolved by hand") {
		t.Fatalf("MERGE_MSG = %q, %v", b, rerr)
	}
}
