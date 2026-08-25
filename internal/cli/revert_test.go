package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// revertFixture: a commit that adds pick.txt on top of base; HEAD on main.
// Returns the dir and the SHA to revert.
func revertFixture(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "base.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "add.txt"), []byte("added\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add add.txt")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return dir, strings.TrimSpace(string(out))
}

func TestRevertCleanCLI(t *testing.T) {
	t.Parallel()
	dir, sha := revertFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"revert", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "reverted") {
		t.Fatalf("stdout: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "add.txt")); !os.IsNotExist(err) {
		t.Fatal("add.txt should be gone after revert")
	}
}

func revertConflictFixtureCLI(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v2\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "to v2")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha = strings.TrimSpace(string(out))
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("v3\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "to v3")
	return dir, sha
}

func TestRevertConflictUnansweredNonTTY(t *testing.T) {
	t.Parallel()
	dir, sha := revertConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"revert", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestRevertConflictAbortFlag(t *testing.T) {
	t.Parallel()
	dir, sha := revertConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"revert", "--on-conflict=abort", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "f.txt"))
	if string(got) != "v3\n" {
		t.Fatalf("f.txt = %q after abort, want v3", got)
	}
}

func TestRevertConflictKeepFlag(t *testing.T) {
	t.Parallel()
	dir, sha := revertConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"revert", "--on-conflict=keep", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "REVERT_HEAD").Run(); err != nil {
		t.Fatal("expected the revert left in progress")
	}
}

func TestRevertUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"revert"},                             // missing commit
		{"revert", "a", "b"},                   // too many positionals
		{"revert", "--on-conflict=bogus", "x"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
