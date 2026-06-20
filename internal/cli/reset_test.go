package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// resetFixture: base then a second commit adding b.txt; HEAD on the second.
// Returns dir and the base SHA (an ancestor of HEAD).
func resetFixture(t *testing.T) (dir, base string) {
	t.Helper()
	dir = newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	base = strings.TrimSpace(string(out))
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add b.txt")
	return dir, base
}

func headSHA(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestResetDefaultMixedCLI(t *testing.T) {
	dir, base := resetFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"reset", base}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "reset (mixed)") {
		t.Fatalf("no-flag reset should default to mixed: %q", out.String())
	}
	if headSHA(t, dir) != base {
		t.Fatal("HEAD should be at base")
	}
}

func TestResetSoftCLI(t *testing.T) {
	dir, base := resetFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"reset", "--soft", base}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "reset (soft)") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestResetHardCLI(t *testing.T) {
	dir, base := resetFixture(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("dirty\n"), 0o644)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"reset", "--hard", base}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if _, err := os.Stat(filepath.Join(dir, "b.txt")); !os.IsNotExist(err) {
		t.Fatal("hard reset should remove b.txt")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(got) != "base\n" {
		t.Fatalf("a.txt = %q, hard reset should discard the dirty edit", got)
	}
}

// nonAncestorFixture: main and a side branch whose tip is not on main; HEAD on
// main. Returns dir, the side tip SHA, and main's tip SHA.
func nonAncestorFixture(t *testing.T) (dir, sideTip, mainTip string) {
	t.Helper()
	dir = newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("base\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "side")
	os.WriteFile(filepath.Join(dir, "s.txt"), []byte("s\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "side change")
	out, _ := exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
	sideTip = strings.TrimSpace(string(out))
	gitRun(t, dir, "checkout", "main")
	return dir, sideTip, headSHA(t, dir)
}

func TestResetNonAncestorUnforcedNonTTY(t *testing.T) {
	dir, sideTip, mainTip := nonAncestorFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"reset", sideTip}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit %d, want 1 (non-ancestor needs --force on non-TTY)", code)
	}
	if !strings.Contains(errb.String(), "proceed") || !strings.Contains(errb.String(), "cancel") {
		t.Fatalf("stderr must list the confirm options: %q", errb.String())
	}
	if headSHA(t, dir) != mainTip {
		t.Fatal("the refused reset must not move the branch")
	}
}

func TestResetNonAncestorForce(t *testing.T) {
	dir, sideTip, _ := nonAncestorFixture(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"reset", "--force", sideTip}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	if headSHA(t, dir) != sideTip {
		t.Fatal("--force reset should move the branch onto the side tip")
	}
}

func TestResetUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	for _, args := range [][]string{
		{"reset"},                          // missing commit
		{"reset", "a", "b"},                // too many positionals
		{"reset", "--soft", "--hard", "x"}, // two mode flags
	} {
		var out, errb bytes.Buffer
		if code := Run(dir, args, strings.NewReader(""), &out, &errb, ""); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}
