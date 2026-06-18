package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// discardConflictRepo builds a repo with one unresolved merge conflict on
// c.txt, returning the repo dir. Other helpers (newRepoDir, gitRun) are shared.
func discardConflictRepo(t *testing.T) string {
	t.Helper()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "feat")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("feat\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "feat")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "c.txt"), []byte("main\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main")
	// merge is expected to conflict (non-zero) — ignore the error.
	cmd := exec.Command("git", "-C", dir, "merge", "feat")
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	_ = cmd.Run()
	return dir
}

func TestDiscardAllYes(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "discard", "--all", "-y")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "hi\n" {
		t.Fatalf("README.md = %q, want reverted", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be removed, stat err = %v", err)
	}
}

func TestDiscardPathsYes(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "discard", "-y", "README.md", "new.txt")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "hi\n" {
		t.Fatalf("README.md = %q, want reverted", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("new.txt should be removed")
	}
}

func TestDiscardBareUsage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard")
	if code != 2 {
		t.Fatalf("bare discard exit = %d, want 2", code)
	}
}

func TestDiscardAllNoYesNonInteractive(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644)
	// runCLI feeds an empty, non-TTY stdin, so without -y this must refuse.
	code, _, _ := runCLI(t, dir, "discard", "--all")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (refused without --yes)", code)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "README.md")); string(b) != "dirty\n" {
		t.Fatalf("README.md changed despite refusal: %q", b)
	}
}

func TestDiscardAllWithPathsRejected(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard", "--all", "README.md")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (--all + paths)", code)
	}
}

func TestDiscardUnmatchedPath(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "discard", "-y", "nope.txt")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (unmatched path)", code)
	}
}

func TestDiscardConflictedPathRejected(t *testing.T) {
	dir := discardConflictRepo(t)
	code, _, _ := runCLI(t, dir, "discard", "-y", "c.txt")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (conflicted path)", code)
	}
}

func TestDiscardAllRefusesOnConflict(t *testing.T) {
	dir := discardConflictRepo(t)
	code, _, _ := runCLI(t, dir, "discard", "--all", "-y")
	if code != 2 {
		t.Fatalf("exit = %d, want 2 (conflict present)", code)
	}
}

func TestConfirmDiscard(t *testing.T) {
	var out strings.Builder
	if !confirmDiscard("? ", strings.NewReader("y\n"), &out) {
		t.Fatal(`"y" should confirm`)
	}
	if !confirmDiscard("? ", strings.NewReader("YES\n"), &out) {
		t.Fatal(`"YES" should confirm`)
	}
	if confirmDiscard("? ", strings.NewReader("n\n"), &out) {
		t.Fatal(`"n" should not confirm`)
	}
	if confirmDiscard("? ", strings.NewReader(""), &out) {
		t.Fatal(`empty should not confirm`)
	}
}
