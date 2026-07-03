package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAddPathThenCommitNewFile(t *testing.T) {
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
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644)
	code, _, errb := runCLI(t, dir, "add", "-A")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	t.Logf("Status output:\n%s", out)
	if code != 0 || !strings.Contains(out, "A  new.txt") {
		t.Fatalf("new.txt not staged:\n%s", out)
	}
}

func TestAddUsageErrors(t *testing.T) {
	dir := newRepoDir(t)
	if code, _, _ := runCLI(t, dir, "add"); code != 2 {
		t.Fatalf("bare add: exit=%d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "add", "-A", "x.txt"); code != 2 {
		t.Fatalf("add -A with path: exit=%d, want 2", code)
	}
}

func TestUnstage(t *testing.T) {
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
