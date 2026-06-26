package cli

import (
	"strings"
	"testing"
)

func prefixRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newRepoDir(t)
}

func TestPrefixAddListRemove(t *testing.T) {
	dir := prefixRepo(t)

	if code, _, errb := runCLI(t, dir, "prefix", "add", "--global", "feat/"); code != 0 {
		t.Fatalf("add exit %d: %s", code, errb)
	}

	code, out, errb := runCLI(t, dir, "prefix", "ls")
	if code != 0 {
		t.Fatalf("ls exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "feat/") || !strings.Contains(out, "global") {
		t.Fatalf("ls out = %q", out)
	}

	if code, _, errb := runCLI(t, dir, "prefix", "rm", "--global", "feat/"); code != 0 {
		t.Fatalf("rm exit %d: %s", code, errb)
	}
	_, out, _ = runCLI(t, dir, "prefix", "ls")
	if strings.Contains(out, "feat/") {
		t.Fatalf("still listed after rm: %q", out)
	}
}

func TestPrefixAddRejectsBranchToken(t *testing.T) {
	dir := prefixRepo(t)
	if code, _, _ := runCLI(t, dir, "prefix", "add", "x-<branch>"); code == 0 {
		t.Fatalf("want non-zero exit for <branch>")
	}
}

func TestPrefixUsageErrors(t *testing.T) {
	dir := prefixRepo(t)
	if code, _, _ := runCLI(t, dir, "prefix"); code != 2 {
		t.Fatalf("bare prefix should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "prefix", "add"); code != 2 {
		t.Fatalf("add without value should exit 2, got %d", code)
	}
}
