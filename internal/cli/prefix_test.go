package cli

import (
	"path/filepath"
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

func prefixID(t *testing.T, dir, value string, global bool) string {
	t.Helper()
	args := []string{"prefix", "add", value}
	if global {
		args = append(args, "--global")
	}
	if code, _, errb := runCLI(t, dir, args...); code != 0 {
		t.Fatalf("add %q: %d %s", value, code, errb)
	}
	_, out, _ := runCLI(t, dir, "prefix", "ls")
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Split(line, "\t"); len(f) == 3 && f[2] == value {
			return f[0]
		}
	}
	t.Fatalf("%q not listed:\n%s", value, out)
	return ""
}

func TestPrefixResolveFillsLabels(t *testing.T) {
	dir := prefixRepo(t)
	id := prefixID(t, dir, "me/MTHR-<user:issue-number>", false)
	code, out, errb := runCLI(t, dir, "prefix", "resolve", id, "--set", "issue-number=1234")
	if code != 0 || out != "me/MTHR-1234\n" {
		t.Fatalf("resolve = %d %q %q", code, out, errb)
	}
	code, _, errb = runCLI(t, dir, "prefix", "resolve", id)
	if code != 2 || !strings.Contains(errb, "--set issue-number=") {
		t.Fatalf("missing label = %d %q", code, errb)
	}
	code, _, errb = runCLI(t, dir, "prefix", "resolve", "nope")
	if code != 2 || !strings.Contains(errb, "gg prefix ls") {
		t.Fatalf("unknown id = %d %q", code, errb)
	}
	code, _, _ = runCLI(t, dir, "prefix", "resolve", id, "--set", "no-equals")
	if code != 2 {
		t.Fatalf("a --set without = must be a usage error, got %d", code)
	}
}

func TestPrefixResolveParentAndTemplate(t *testing.T) {
	dir := prefixRepo(t)
	code, out, errb := runCLI(t, dir, "prefix", "resolve", "--template", "<parent-branch>_BCK_<repo>")
	if code != 0 || out != "main_BCK_"+filepath.Base(dir)+"\n" {
		t.Fatalf("template = %d %q %q", code, out, errb)
	}
	code, out, _ = runCLI(t, dir, "prefix", "resolve", "--template", "<parent-branch>/x", "--parent", "release")
	if code != 0 || out != "release/x\n" {
		t.Fatalf("--parent = %d %q", code, out)
	}
	if code, _, _ := runCLI(t, dir, "prefix", "resolve", "--template", "a", "x"); code != 2 {
		t.Fatalf("an id with --template must be a usage error, got %d", code)
	}
}

func TestPrefixResolveSeqPeekAndBump(t *testing.T) {
	dir := prefixRepo(t)
	id := prefixID(t, dir, "fix-<seq:fix:3>-", false)
	for i := 0; i < 2; i++ {
		if code, out, errb := runCLI(t, dir, "prefix", "resolve", id); code != 0 || out != "fix-001-\n" {
			t.Fatalf("peek %d = %d %q %q", i, code, out, errb)
		}
	}
	if code, out, _ := runCLI(t, dir, "prefix", "resolve", "--bump", id); code != 0 || out != "fix-001-\n" {
		t.Fatalf("bump = %d %q", code, out)
	}
	if code, out, _ := runCLI(t, dir, "prefix", "resolve", id); code != 0 || out != "fix-002-\n" {
		t.Fatalf("after bump = %d %q", code, out)
	}
}
