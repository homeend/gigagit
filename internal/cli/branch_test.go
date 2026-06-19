package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// gitRun runs git in dir, failing the test on error.
func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func cliBranchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+name).Run() == nil
}

func TestBranchCreate(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create", "feat/x"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !cliBranchExists(t, dir, "feat/x") {
		t.Fatal("branch not created")
	}
	if !strings.Contains(out.String(), "created branch feat/x") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestBranchRename(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "old")

	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "rename", "old", "renamed"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !cliBranchExists(t, dir, "renamed") {
		t.Fatal("branch not renamed")
	}
	if cliBranchExists(t, dir, "old") {
		t.Fatal("old branch still present")
	}
	if !strings.Contains(out.String(), "renamed branch old → renamed") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestBranchRenameUsage(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "rename", "only-one"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("want usage exit 2, got %d", code)
	}
}

func TestBranchCreateFromStartPoint(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "base")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "advance")

	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create", "feat/y", "base"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	tip, _ := exec.Command("git", "-C", dir, "rev-parse", "feat/y").Output()
	baseTip, _ := exec.Command("git", "-C", dir, "rev-parse", "base").Output()
	if string(tip) != string(baseTip) {
		t.Fatal("feat/y must point at base's tip")
	}
}

func TestBranchCreateUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "create"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("no-args exit = %d, want 2", code)
	}
	if code := Run(dir, []string{"branch"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("bare `gg branch` exit = %d, want 2", code)
	}
	if code := Run(dir, []string{"branch", "bogus"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("unknown sub exit = %d, want 2", code)
	}
}

func TestBranchDeleteMergedNeedsNoPrompt(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "merged")
	var out, errb bytes.Buffer
	// Non-interactive stdin: must still succeed — the confirm is pre-answered.
	if code := Run(dir, []string{"branch", "delete", "merged"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if cliBranchExists(t, dir, "merged") {
		t.Fatal("branch still exists")
	}
}

// unmerged creates a branch with a commit main doesn't have.
func unmerged(t *testing.T, dir, name string) {
	t.Helper()
	gitRun(t, dir, "switch", "-c", name)
	if err := os.WriteFile(filepath.Join(dir, name+".txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "unmerged work")
	gitRun(t, dir, "switch", "main")
}

func TestBranchDeleteUnmergedNonTTYExits1WithOptions(t *testing.T) {
	dir := newRepoDir(t)
	unmerged(t, dir, "risky")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"branch", "delete", "risky"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit = %d, want 1 (unanswered decision)", code)
	}
	if !strings.Contains(errb.String(), "branch-unmerged") || !strings.Contains(errb.String(), "force-delete") {
		t.Fatalf("stderr must name the decision and options: %s", errb.String())
	}
	if !cliBranchExists(t, dir, "risky") {
		t.Fatal("branch must survive an unanswered decision")
	}
}

func TestBranchDeleteUnmergedForce(t *testing.T) {
	dir := newRepoDir(t)
	unmerged(t, dir, "risky2")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "delete", "--force", "risky2"}, strings.NewReader(""), &out, &errb, ""); code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if cliBranchExists(t, dir, "risky2") {
		t.Fatal("unmerged branch should be force-deleted")
	}
}

func TestBranchDeleteCurrentBranchFails(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"branch", "delete", "main"}, strings.NewReader(""), &out, &errb, ""); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "checked-out branch") {
		t.Fatalf("stderr: %s", errb.String())
	}
}
