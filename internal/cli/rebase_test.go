package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestRebaseInteractiveRequiresPlan(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "-i", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb.String(), "--plan") {
		t.Fatalf("stderr %q should mention --plan", errb.String())
	}
}

func TestRebasePlanRequiresInteractive(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--plan", "/tmp/x.json", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (usage)", code)
	}
}

func TestRebaseInteractiveBadPlanFile(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "-i", "--plan", "/nonexistent/plan.json", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit %d, want 2 (unreadable plan)", code)
	}
}

// rebaseFixture: feat diverges from main with a disjoint file, main advances
// disjointly; ends on feat (the branch to rebase).
func rebaseFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "main.txt"), []byte("m\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main change")
	gitRun(t, dir, "checkout", "feat")
	return dir
}

func TestRebaseCurrentOntoBase(t *testing.T) {
	t.Parallel()
	dir := rebaseFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "rebased feat onto main") {
		t.Fatalf("stdout: %q", out.String())
	}
	// feat replayed onto main → main.txt present on feat now.
	if _, err := os.Stat(filepath.Join(dir, "main.txt")); err != nil {
		t.Fatal("main.txt missing after rebase")
	}
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "feat" {
		t.Fatalf("on %q, want feat", strings.TrimSpace(string(cur)))
	}
}

func TestRebaseExplicitBranchSwitchesAndStays(t *testing.T) {
	t.Parallel()
	dir := rebaseFixture(t)
	gitRun(t, dir, "checkout", "main") // now NOT on feat
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--branch", "feat", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	// SmartRebase rung 3 ends on the rebased branch.
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "feat" {
		t.Fatalf("on %q, want feat", strings.TrimSpace(string(cur)))
	}
}

// rebaseConflictFixture: feat and main both edit shared.txt; ends on feat.
func rebaseConflictFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "feat change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "main change")
	gitRun(t, dir, "checkout", "feat")
	return dir
}

func TestRebaseConflictUnansweredNonTTY(t *testing.T) {
	t.Parallel()
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestRebaseConflictAbortFlag(t *testing.T) {
	t.Parallel()
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--on-conflict=abort", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "feat\n" {
		t.Fatalf("shared.txt = %q after abort, want feat's pre-rebase version", got)
	}
}

func TestRebaseConflictKeepFlag(t *testing.T) {
	t.Parallel()
	dir := rebaseConflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"rebase", "--on-conflict=keep", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	// A rebase is left paused.
	if err := exec.Command("git", "-C", dir, "rebase", "--show-current-patch").Run(); err != nil {
		t.Fatal("expected the rebase left in progress")
	}
}

func TestRebaseUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"rebase"},           // missing newbase
		{"rebase", "a", "b"}, // too many positionals
		{"rebase", "--on-conflict=bogus", "main"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
