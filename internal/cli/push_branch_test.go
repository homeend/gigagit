package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// pbGitOut runs git and returns trimmed stdout (the shared gitIn discards output).
func pbGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// cloneWithExtraBranch builds an origin+clone where `main` is pushed and a
// non-current `feature` branch exists locally (with a commit) but was never
// pushed. The clone is left checked out on `main`.
func cloneWithExtraBranch(t *testing.T) (clone, origin string) {
	t.Helper()
	root := t.TempDir()
	origin = filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone = filepath.Join(root, "clone")
	gitIn(t, root, "init", "--bare", origin)
	gitIn(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitIn(t, seed, "checkout", "-b", "main")
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "v1")
	gitIn(t, seed, "push", "-u", "origin", "main")

	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	gitIn(t, clone, "config", "user.name", "t")
	gitIn(t, clone, "config", "user.email", "t@t")

	gitIn(t, clone, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(clone, "g.txt"), []byte("feat\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "feature work")
	gitIn(t, clone, "checkout", "main") // back to main; feature is now non-current
	return clone, origin
}

// TestPushNamedNonCurrentBranch: `gg push <branch>` pushes the NAMED branch
// (sets upstream) without switching to it — the CLI counterpart of the TUI
// Branches-panel "Push <branch>" action.
func TestPushNamedNonCurrentBranch(t *testing.T) {
	t.Parallel()
	clone, origin := cloneWithExtraBranch(t)
	code, out, errb := runCLI(t, clone, "push", "feature")
	if code != 0 {
		t.Fatalf("push feature exit=%d (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pushed") {
		t.Fatalf("want 'pushed' in output, got %q", out)
	}
	// The push must NOT change the checked-out branch.
	if head := pbGitOut(t, clone, "symbolic-ref", "--short", "HEAD"); head != "main" {
		t.Fatalf("push <branch> must not switch; HEAD=%q want main", head)
	}
	heads := pbGitOut(t, clone, "ls-remote", "--heads", origin)
	if !strings.Contains(heads, "refs/heads/feature") {
		t.Fatalf("feature did not land on origin:\n%s", heads)
	}
	// And upstream tracking was set for feature.
	up := pbGitOut(t, clone, "for-each-ref", "--format=%(upstream:short)", "refs/heads/feature")
	if up != "origin/feature" {
		t.Fatalf("feature upstream = %q, want origin/feature", up)
	}
}

func TestPushTooManyArgs(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "push", "a", "b")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (usage)", code)
	}
	if !strings.Contains(errb, "too many arguments") {
		t.Fatalf("stderr = %q, want a 'too many arguments' message", errb)
	}
}
