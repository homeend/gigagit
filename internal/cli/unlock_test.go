package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func runUnlock(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, append([]string{"unlock"}, args...), strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

func TestUnlockCleanRepo(t *testing.T) {
	dir := newCLIRepo(t)
	code, out, errb := runUnlock(t, dir)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	if !strings.Contains(out, "no git locks") {
		t.Fatalf("stdout = %q", out)
	}
}

// Without --yes nothing is removed and the exit code is non-zero, so a script
// can use `gg unlock` as a precondition check.
func TestUnlockListsWithoutRemoving(t *testing.T) {
	dir := newCLIRepo(t)
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runUnlock(t, dir)
	if code != 1 {
		t.Fatalf("locks present should exit 1, got %d", code)
	}
	if !strings.Contains(out, "index.lock") {
		t.Fatalf("stdout should list the lock, got %q", out)
	}
	if !strings.Contains(errb, "--yes") {
		t.Fatalf("stderr should say how to remove it, got %q", errb)
	}
	if _, err := os.Stat(lock); err != nil {
		t.Fatal("the lock must NOT be removed without --yes")
	}
}

func TestUnlockRemovesWithYes(t *testing.T) {
	dir := newCLIRepo(t)
	lock := filepath.Join(dir, ".git", "index.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errb := runUnlock(t, dir, "--yes")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb)
	}
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("index.lock should be gone")
	}
	// The real point: the repo works again.
	var out2, err2 bytes.Buffer
	if c := Run(dir, []string{"status"}, strings.NewReader(""), &out2, &err2, ""); c != 0 {
		t.Fatalf("status after unlock failed: %d %s", c, err2.String())
	}
}

func TestUnlockRejectsPositionalArgs(t *testing.T) {
	dir := newCLIRepo(t)
	if code, _, _ := runUnlock(t, dir, "index.lock"); code != 2 {
		t.Fatalf("usage error should exit 2, got %d", code)
	}
}

// gg batch drives every command through runOne, so unlock must be reachable
// there too.
func TestUnlockRunsUnderBatch(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"batch"}, strings.NewReader("unlock\n"), &out, &errb, "")
	if code != 0 {
		t.Fatalf("batch exit = %d, stderr=%s out=%s", code, errb.String(), out.String())
	}
	if !strings.Contains(out.String(), "no git locks") {
		t.Fatalf("batch stdout = %q", out.String())
	}
}
