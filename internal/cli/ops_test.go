package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
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

func cloneBehind(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v2")
	gitIn(t, seed, "push", "origin", "main")
	return clone
}

func TestPullFastForward(t *testing.T) {
	clone := cloneBehind(t)
	code, out, errb := runCLI(t, clone, "pull")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pulled") {
		t.Fatalf("expected 'pulled' in output:\n%s", out)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 after pull", string(b))
	}
}

func TestSwitchCommand(t *testing.T) {
	dir := newRepoDir(t)
	gitIn(t, dir, "branch", "feature")
	code, out, errb := runCLI(t, dir, "switch", "feature")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "feature") {
		t.Fatalf("expected 'feature' in output:\n%s", out)
	}
}

func TestSwitchRequiresBranch(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "switch")
	if code == 0 {
		t.Fatal("switch without a branch should fail")
	}
}

func TestStashAndUndo(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	if code, _, errb := runCLI(t, dir, "stash"); code != 0 {
		t.Fatalf("stash exit = %d (stderr: %s)", code, errb)
	}
	gitIn(t, dir, "commit", "--allow-empty", "-m", "to-undo")
	code, out, errb := runCLI(t, dir, "undo")
	if code != 0 {
		t.Fatalf("undo exit = %d (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "undid") {
		t.Fatalf("expected 'undid' in output:\n%s", out)
	}
}
