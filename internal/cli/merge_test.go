package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// mergeFixture: main and feat with a non-conflicting extra file on feat.
func mergeFixture(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "feat.txt"), []byte("f\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat change")
	gitRun(t, dir, "checkout", "main")
	return dir
}

// conflictFixture: main and feat both edit shared.txt.
func conflictFixture(t *testing.T) string {
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
	return dir
}

func TestMergeIntoCurrent(t *testing.T) {
	dir := mergeFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "merged feat into main") {
		t.Fatalf("stdout: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "feat.txt")); err != nil {
		t.Fatal("feat.txt missing after merge")
	}
}

func TestMergeIntoExplicitTarget(t *testing.T) {
	dir := mergeFixture(t)
	gitRun(t, dir, "branch", "target")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--into", "target", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	// SmartMerge rung 3 ends on the target branch.
	cur, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(cur)) != "target" {
		t.Fatalf("on %q, want target", strings.TrimSpace(string(cur)))
	}
}

func TestMergeConflictUnansweredNonTTY(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestMergeConflictAbortFlag(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=abort", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort", got)
	}
}

func TestMergeConflictKeepFlag(t *testing.T) {
	dir := conflictFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"merge", "--on-conflict=keep", "feat"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run(); err != nil {
		t.Fatal("expected the merge left in progress")
	}
}

func TestMergeUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"merge"},                                // missing source
		{"merge", "a", "b"},                      // too many positionals
		{"merge", "--on-conflict=bogus", "feat"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
