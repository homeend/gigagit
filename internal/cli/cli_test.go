package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runCLI(t *testing.T, workdir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(workdir, args, &out, &errb)
	return code, out.String(), errb.String()
}

func TestStatusCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "main") {
		t.Fatalf("status output missing branch:\n%s", out)
	}
}

func TestCommitCommand(t *testing.T) {
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644)
	code, _, errb := runCLI(t, dir, "commit", "-m", "second", "--all")
	if code != 0 {
		t.Fatalf("exit = %d, want 0 (stderr: %s)", code, errb)
	}
	code, out, _ := runCLI(t, dir, "status")
	if code != 0 || !strings.Contains(out, "clean") {
		t.Fatalf("expected clean status after commit, got:\n%s", out)
	}
}

func TestCommitRequiresMessage(t *testing.T) {
	dir := newRepoDir(t)
	code, _, _ := runCLI(t, dir, "commit")
	if code == 0 {
		t.Fatal("commit without -m should fail")
	}
}

func TestUnknownCommand(t *testing.T) {
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "frobnicate")
	if code == 0 {
		t.Fatal("unknown command should return non-zero")
	}
	if !strings.Contains(errb, "unknown") {
		t.Fatalf("expected 'unknown' in stderr:\n%s", errb)
	}
}
