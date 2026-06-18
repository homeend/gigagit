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

func TestPullDivergedRebaseViaOnConflict(t *testing.T) {
	clone := cloneBehind(t)
	// Diverge locally so ff-only fails and a decision is required.
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("l\n"), 0o644)
	gitIn(t, clone, "add", ".")
	gitIn(t, clone, "commit", "-m", "local")

	code, out, errb := runCLI(t, clone, "pull", "--on-conflict", "rebase")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "pulled") {
		t.Fatalf("expected 'pulled' in output:\n%s", out)
	}
	// Rebase applied the remote v2 change and kept the local commit.
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote change applied via rebase)", string(b))
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
}

func TestPullBackgroundRejectsOnConflict(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "pull", "--background", "--on-conflict", "rebase")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 for the rejected flag combo", code)
	}
	if !strings.Contains(errb, "on-conflict") {
		t.Fatalf("expected an --on-conflict error:\n%s", errb)
	}
}

func TestStashApplyPopDropCLI(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if code, _, errb := runCLI(t, dir, "stash", "-m", "wip"); code != 0 {
		t.Fatalf("stash: %d %s", code, errb)
	}
	// apply keeps the stash and restores the change
	if code, _, errb := runCLI(t, dir, "stash", "apply"); code != 0 {
		t.Fatalf("apply: %d %s", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "changed\n" {
		t.Fatalf("apply did not restore the change: %q", b)
	}
	if code, out, _ := runCLI(t, dir, "stash", "list"); code != 0 || !strings.Contains(out, "stash@{0}") {
		t.Fatalf("list: code=%d out=%q", code, out)
	}
	// drop removes it
	if code, _, errb := runCLI(t, dir, "stash", "drop", "stash@{0}"); code != 0 {
		t.Fatalf("drop: %d %s", code, errb)
	}
	if code, out, _ := runCLI(t, dir, "stash", "list"); code != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("stash list should be empty after drop: %q", out)
	}
}

func TestStashPopCLI(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	if code, _, _ := runCLI(t, dir, "stash", "-m", "wip"); code != 0 {
		t.Fatal("stash failed")
	}
	if code, _, errb := runCLI(t, dir, "stash", "pop"); code != 0 {
		t.Fatalf("pop: %d %s", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "changed\n" {
		t.Errorf("pop did not restore the change: %q", b)
	}
	if code, out, _ := runCLI(t, dir, "stash", "list"); code != 0 || strings.TrimSpace(out) != "" {
		t.Errorf("pop should remove the stash: %q", out)
	}
}

func TestStashApplyConflictCLI(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("stashed\n"), 0o644)
	if code, _, _ := runCLI(t, dir, "stash", "-m", "wip"); code != 0 {
		t.Fatal("stash failed")
	}
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("local\n"), 0o644)
	code, _, errb := runCLI(t, dir, "stash", "apply")
	if code == 0 {
		t.Fatal("apply over a conflicting local change must exit non-zero")
	}
	if !strings.Contains(errb, "overwritten") {
		t.Errorf("apply error should explain the conflict, got: %s", errb)
	}
}

func TestStashByPathCLI(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b\n"), 0o644)
	gitIn(t, dir, "add", ".")
	gitIn(t, dir, "commit", "-m", "ab")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a2\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("b2\n"), 0o644)
	if code, _, errb := runCLI(t, dir, "stash", "-m", "wip", "--", "a.txt"); code != 0 {
		t.Fatalf("stash by path: %d %s", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(b) != "a\n" {
		t.Errorf("a.txt should be reverted (stashed): %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "b.txt")); string(b) != "b2\n" {
		t.Errorf("b.txt should still be dirty: %q", b)
	}
}

// cloneWithRemoteFoo builds origin (main + a foo branch ahead) and clones it,
// returning the clone dir with refs/remotes/origin/foo present, no local foo.
func cloneWithRemoteFoo(t *testing.T) string {
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
	gitIn(t, seed, "commit", "-m", "c1")
	gitIn(t, seed, "push", "-u", "origin", "main")
	gitIn(t, seed, "checkout", "-b", "foo")
	os.WriteFile(filepath.Join(seed, "g.txt"), []byte("foo\n"), 0o644)
	gitIn(t, seed, "add", ".")
	gitIn(t, seed, "commit", "-m", "foo-c2")
	gitIn(t, seed, "push", "-u", "origin", "foo")
	gitIn(t, root, "clone", origin, clone)
	gitIn(t, clone, "checkout", "main")
	return clone
}

func TestCheckoutStayCreatesLocalTrackingBranch(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "checkout", "origin/foo")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if cur := strings.TrimSpace(runGit(t, clone, "symbolic-ref", "--short", "HEAD")); cur != "main" {
		t.Fatalf("HEAD = %q, want main (stay)", cur)
	}
	// rev-parse --verify fails the test (via gitIn) if local foo was not created.
	gitIn(t, clone, "rev-parse", "--verify", "refs/heads/foo")
}

func TestCheckoutSwitchChecksOutTheBranch(t *testing.T) {
	clone := cloneWithRemoteFoo(t)
	code, _, errb := runCLI(t, clone, "checkout", "origin/foo", "-s")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if cur := strings.TrimSpace(runGit(t, clone, "symbolic-ref", "--short", "HEAD")); cur != "foo" {
		t.Fatalf("HEAD = %q, want foo (switch)", cur)
	}
}

func TestCheckoutRequiresRemoteQualifiedRef(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "checkout"); code == 0 {
		t.Fatal("checkout without a ref should fail")
	}
	if code, _, _ := runCLI(t, dir, "checkout", "foo"); code == 0 {
		t.Fatal("checkout with a non-qualified ref should fail")
	}
}
