package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/rebaseplan"
)

// buildGG builds the gg binary once for use as the rebase sequence editor.
func buildGG(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "gg-test-bin")
	out, err := exec.Command("go", "build", "-o", bin, "github.com/gigagit/gg/cmd/gg").CombinedOutput()
	if err != nil {
		t.Fatalf("build gg: %v\n%s", err, out)
	}
	return bin
}

// shaOf returns the full sha of <rev> in dir.
func shaOf(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", rev, err)
	}
	return strings.TrimSpace(string(out))
}

// subjects returns the commit subjects over rangeSpec in dir, newest-first.
func subjects(t *testing.T, dir, rangeSpec string) []string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "log", "--pretty=%s", rangeSpec).Output()
	if err != nil {
		t.Fatalf("log %s: %v", rangeSpec, err)
	}
	var s []string
	for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if l != "" {
			s = append(s, l)
		}
	}
	return s
}

// threeCommitBranch builds main(initial) -> wip1 -> wip2 -> wip3 on "work".
func threeCommitBranch(t *testing.T) (string, *git.Repo) {
	t.Helper()
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	for _, n := range []string{"wip1", "wip2", "wip3"} {
		os.WriteFile(filepath.Join(dir, n+".txt"), []byte(n+"\n"), 0o644)
		gitE(t, dir, "add", ".")
		gitE(t, dir, "commit", "-m", n)
	}
	return dir, repo
}

func TestInteractiveRebaseRewordDropReorder(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	// oldest-first plan over main..work = [wip1, wip2, wip3]:
	// reword wip1 -> "wip1 reworded", drop wip2, keep wip3.
	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: shaOf(t, dir, "work~1"), Action: rebaseplan.Drop, Orig: "wip2"},
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	res, err := InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("interactive rebase: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	got := subjects(t, dir, "main..work") // newest-first
	want := []string{"wip3", "wip1 reworded"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("subjects = %v, want %v", got, want)
	}
}

func TestInteractiveRebaseOntoCommitSha(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	// Onto a raw commit SHA (wip2's parent = wip1 = work~2): range = [wip2, wip3];
	// drop wip2. This exercises the relaxed Onto (commit-ish, not a branch).
	onto := shaOf(t, dir, "work~2")
	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work~1"), Action: rebaseplan.Drop, Orig: "wip2"},
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	res, err := InteractiveRebase{Branch: "work", Onto: onto, Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("interactive rebase onto sha: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	got := subjects(t, dir, "main..work") // newest-first
	want := []string{"wip3", "wip1"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("subjects = %v, want %v (wip2 should be dropped)", got, want)
	}
}

func TestInteractiveRebaseOntoNoSuchCommit(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	_ = dir
	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	_, err := InteractiveRebase{Branch: "work", Onto: "definitely-not-a-ref", Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "no such commit") {
		t.Fatalf("err = %v, want a no-such-commit refusal", err)
	}
}

func TestInteractiveRebaseRefusesMergeCommits(t *testing.T) {
	gg := buildGG(t)
	dir, repo := newRepo(t)
	gitE(t, dir, "checkout", "-b", "work")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "a")
	gitE(t, dir, "checkout", "-b", "side", "main")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitE(t, dir, "add", ".")
	gitE(t, dir, "commit", "-m", "b")
	gitE(t, dir, "checkout", "work")
	gitE(t, dir, "merge", "--no-ff", "-m", "merge side", "side")

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "merge side"},
	}}
	_, err := InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "merge commits") {
		t.Fatalf("err = %v, want a merge-commits refusal", err)
	}
}

func TestInteractiveRebaseStashWrapPreservesWorkingTree(t *testing.T) {
	gg := buildGG(t)
	dir, repo := threeCommitBranch(t)
	// Dirty the tree: one staged new file, one unstaged edit to a committed file.
	os.WriteFile(filepath.Join(dir, "staged.txt"), []byte("s\n"), 0o644)
	gitE(t, dir, "add", "staged.txt")
	os.WriteFile(filepath.Join(dir, "wip1.txt"), []byte("edited\n"), 0o644) // unstaged

	plan := rebaseplan.Plan{Entries: []rebaseplan.Entry{
		{Sha: shaOf(t, dir, "work~2"), Action: rebaseplan.Reword, Orig: "wip1", NewMsg: "wip1 reworded"},
		{Sha: shaOf(t, dir, "work~1"), Action: rebaseplan.Pick, Orig: "wip2"},
		{Sha: shaOf(t, dir, "work"), Action: rebaseplan.Pick, Orig: "wip3"},
	}}
	if _, err := (InteractiveRebase{Branch: "work", Onto: "main", Plan: plan, GGBin: gg}).
		Run(context.Background(), OpDeps{Repo: repo}); err != nil {
		t.Fatalf("interactive rebase: %v", err)
	}
	if got := subjects(t, dir, "main..work"); got[len(got)-1] != "wip1 reworded" {
		t.Fatalf("oldest subject = %q, want 'wip1 reworded'", got[len(got)-1])
	}
	st, err := repo.Status(context.Background())
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var stagedNew, unstagedEdit bool
	for _, f := range st.Files {
		if f.Path == "staged.txt" && f.Staged != '.' && f.Staged != 0 {
			stagedNew = true
		}
		if f.Path == "wip1.txt" && f.Unstaged != '.' && f.Unstaged != 0 {
			unstagedEdit = true
		}
	}
	if !stagedNew {
		t.Error("staged.txt should be restored as staged")
	}
	if !unstagedEdit {
		t.Error("wip1.txt edit should be restored as unstaged")
	}
}
