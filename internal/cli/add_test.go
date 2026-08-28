package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddPathThenCommitNewFile(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)

	code, _, errb := runCLI(t, dir, "add", "new.txt")
	if code != 0 {
		t.Fatalf("add exit=%d stderr=%s", code, errb)
	}
	// the previously-impossible flow: commit a brand-new file via gg only
	code, _, errb = runCLI(t, dir, "commit", "-m", "add new file")
	if code != 0 {
		t.Fatalf("commit exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "clean") {
		t.Fatalf("expected clean tree:\n%s", out)
	}
}

func TestAddAllFlag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	code, _, errb := runCLI(t, dir, "add", "-A")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "A  new.txt") {
		t.Fatalf("new.txt not staged:\n%s", out)
	}
}

func TestAddUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "add"); code != 2 {
		t.Fatalf("bare add: exit=%d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "add", "-A", "x.txt"); code != 2 {
		t.Fatalf("add -A with path: exit=%d, want 2", code)
	}
}

func TestUnstage(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	runCLI(t, dir, "add", "new.txt")

	code, _, errb := runCLI(t, dir, "unstage", "new.txt")
	if code != 0 {
		t.Fatalf("unstage exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "?? new.txt") {
		t.Fatalf("new.txt should be untracked again:\n%s", out)
	}
	if code, _, _ := runCLI(t, dir, "unstage"); code != 2 {
		t.Fatal("bare unstage must be usage error")
	}
}

// writeIgnoredCLI plants a .gitignore-excluded docs/specs/a.md.
func writeIgnoredCLI(t *testing.T, dir string) {
	t.Helper()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("docs/specs\n"), 0o644)
	os.MkdirAll(filepath.Join(dir, "docs", "specs"), 0o755)
	os.WriteFile(filepath.Join(dir, "docs", "specs", "a.md"), []byte("x\n"), 0o644)
}

func TestAddForceStagesIgnored(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	writeIgnoredCLI(t, dir)

	code, _, errb := runCLI(t, dir, "add", "-f", "docs/specs/a.md")
	if code != 0 {
		t.Fatalf("add -f exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "A  docs/specs/a.md") {
		t.Fatalf("docs/specs/a.md not staged:\n%s", out)
	}
}

// Without -f a non-interactive add of an ignored path fails with git's
// refusal — same contract as before the stage.ignored fork existed.
func TestAddIgnoredWithoutForceFails(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	writeIgnoredCLI(t, dir)

	code, _, errb := runCLI(t, dir, "add", "docs/specs/a.md")
	if code == 0 {
		t.Fatal("add of ignored path without -f should fail non-interactively")
	}
	if !strings.Contains(errb, "ignored by one of your .gitignore files") {
		t.Fatalf("stderr should carry git's refusal:\n%s", errb)
	}
}
