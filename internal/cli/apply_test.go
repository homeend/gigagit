package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// applyGit runs git in dir and returns trimmed stdout.
func applyGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = dir
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func applyRun(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, args, strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

// usage errors: both flags, no positional, two positionals.
func TestCmdApplyUsageErrors(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	for _, args := range [][]string{
		{"apply", "--am", "--working", "x.patch"},
		{"apply"},
		{"apply", "a.patch", "b.patch"},
	} {
		if code, _, stderr := applyRun(t, dir, args...); code != 2 {
			t.Fatalf("gg %v = %d, want 2 (stderr: %s)", args, code, stderr)
		}
	}
}

// default mode applies to the working tree; --am recreates the commit.
func TestCmdApplyWorkingAndAm(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	// build a patch: commit, export, rewind
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "add f")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "extend f")
	patchData := applyGit(t, dir, "format-patch", "-1", "--binary", "--stdout", "HEAD")
	patch := filepath.Join(t.TempDir(), "extend.patch")
	os.WriteFile(patch, []byte(patchData+"\n"), 0o644)
	applyGit(t, dir, "reset", "--hard", "HEAD~1")

	// default = working tree: file changed, no commit
	if code, _, stderr := applyRun(t, dir, "apply", patch); code != 0 {
		t.Fatalf("apply = %d, stderr: %s", code, stderr)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("f.txt = %q", got)
	}
	if subj := applyGit(t, dir, "log", "-1", "--format=%s"); subj != "add f" {
		t.Fatalf("default mode must not commit; HEAD = %q", subj)
	}

	// --am on a rewound tree: commit recreated
	applyGit(t, dir, "checkout", "--", ".")
	code, stdout, stderr := applyRun(t, dir, "apply", "--am", patch)
	if code != 0 {
		t.Fatalf("apply --am = %d, stderr: %s", code, stderr)
	}
	if subj := applyGit(t, dir, "log", "-1", "--format=%s"); subj != "extend f" {
		t.Fatalf("--am should recreate the commit; HEAD = %q", subj)
	}
	if !strings.Contains(stdout, "applied") {
		t.Fatalf("stdout = %q, want an applied summary", stdout)
	}
}

// a relative patch path resolves against the workdir passed to Run, not the
// process cwd (which during `go test` is internal/cli/, not the repo) — the
// contract the in-process e2e harness depends on.
func TestCmdApplyRelativePathResolvesAgainstWorkdir(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "add f")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "extend f")
	patchData := applyGit(t, dir, "format-patch", "-1", "--binary", "--stdout", "HEAD")
	os.WriteFile(filepath.Join(dir, "extend.patch"), []byte(patchData+"\n"), 0o644)
	applyGit(t, dir, "reset", "--hard", "HEAD~1")

	if code, _, stderr := applyRun(t, dir, "apply", "extend.patch"); code != 0 {
		t.Fatalf("apply extend.patch = %d, stderr: %s", code, stderr)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "f.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("f.txt = %q", got)
	}
}

// a conflicting working-tree apply exits 1 (conflicts left in tree).
func TestCmdApplyConflictExit1(t *testing.T) {
	t.Parallel()
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("base\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "base")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("patched\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "patch side")
	patchData := applyGit(t, dir, "format-patch", "-1", "--binary", "--stdout", "HEAD")
	patch := filepath.Join(t.TempDir(), "c.patch")
	os.WriteFile(patch, []byte(patchData+"\n"), 0o644)
	applyGit(t, dir, "reset", "--hard", "HEAD~1")
	os.WriteFile(filepath.Join(dir, "f.txt"), []byte("conflicting local\n"), 0o644)
	applyGit(t, dir, "add", "f.txt")
	applyGit(t, dir, "commit", "-m", "local side")

	code, _, _ := applyRun(t, dir, "apply", patch)
	if code != 1 {
		t.Fatalf("conflicting apply = %d, want 1", code)
	}
	if unmerged := applyGit(t, dir, "diff", "--name-only", "--diff-filter=U"); unmerged != "f.txt" {
		t.Fatalf("unmerged = %q, want f.txt", unmerged)
	}
}
