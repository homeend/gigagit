package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// cherryPickFixture: a non-conflicting commit on feat (adds pick.txt), HEAD on
// main. Returns the dir and the commit SHA to pick.
func cherryPickFixture(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "pick.txt"), []byte("picked\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add pick.txt")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha = strings.TrimSpace(string(out))
	gitRun(t, dir, "checkout", "main")
	return dir, sha
}

func TestCherryPickCleanCLI(t *testing.T) {
	t.Parallel()
	dir, sha := cherryPickFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"cherry-pick", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "cherry-picked") {
		t.Fatalf("stdout: %q", out.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after cherry-pick")
	}
}

func cherryPickConflictFixtureCLI(t *testing.T) (dir, sha string) {
	t.Helper()
	dir = newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "feat change")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	sha = strings.TrimSpace(string(out))
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "commit", "-am", "main change")
	return dir, sha
}

func TestCherryPickConflictUnansweredNonTTY(t *testing.T) {
	t.Parallel()
	dir, sha := cherryPickConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"cherry-pick", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "keep-conflicts") || !strings.Contains(errb.String(), "abort") {
		t.Fatalf("stderr must list the options: %q", errb.String())
	}
}

func TestCherryPickConflictAbortFlag(t *testing.T) {
	t.Parallel()
	dir, sha := cherryPickConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"cherry-pick", "--on-conflict=abort", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	got, _ := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if string(got) != "main\n" {
		t.Fatalf("shared.txt = %q after abort, want main", got)
	}
}

func TestCherryPickConflictKeepFlag(t *testing.T) {
	t.Parallel()
	dir, sha := cherryPickConflictFixtureCLI(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"cherry-pick", "--on-conflict=keep", sha}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts kept)", code)
	}
	if err := exec.Command("git", "-C", dir, "rev-parse", "-q", "--verify", "CHERRY_PICK_HEAD").Run(); err != nil {
		t.Fatal("expected the cherry-pick left in progress")
	}
}

func TestCherryPickUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"cherry-pick"},                             // missing commit
		{"cherry-pick", "a", "b"},                   // too many positionals
		{"cherry-pick", "--on-conflict=bogus", "x"}, // invalid policy value
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
