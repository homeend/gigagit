package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// headSHA (current HEAD commit hash) is defined in reset_test.go; reused here.

// TestCommitExportPatchWritesFile exercises the sha-before-flags order — the
// case a flag.FlagSet would mis-parse (it stops at the first positional), which
// is why cmdCommitExportPatch hand-parses instead.
func TestCommitExportPatchWritesFile(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add foo.go")
	sha := headSHA(t, dir)

	out := filepath.Join(t.TempDir(), "my.patch")
	code, _, errb := runCLI(t, dir, "commit", "export-patch", sha, "--out", out)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if !strings.HasPrefix(string(b), "From ") {
		n := len(b)
		if n > 20 {
			n = 20
		}
		t.Fatalf("not a mailbox patch: %q", string(b[:n]))
	}
}

// TestCommitExportPatchFlagsBeforeSha exercises the opposite argument order
// (flags first, positional last) to confirm the parsing is order-independent.
func TestCommitExportPatchFlagsBeforeSha(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add foo.go")
	sha := headSHA(t, dir)

	out := filepath.Join(t.TempDir(), "my.patch")
	code, _, errb := runCLI(t, dir, "commit", "export-patch", "--out", out, sha)
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("expected patch file: %v", err)
	}
}

func TestCommitExportPatchRefusesMerge(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	gitRun(t, dir, "checkout", "-b", "topic")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("2\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "topic change")
	gitRun(t, dir, "checkout", "main")
	os.WriteFile(filepath.Join(dir, "b.txt"), []byte("3\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main change")
	gitRun(t, dir, "merge", "--no-ff", "topic", "-m", "merge")
	sha := headSHA(t, dir)

	code, _, errb := runCLI(t, dir, "commit", "export-patch", sha, "--out", filepath.Join(t.TempDir(), "x.patch"))
	if code == 0 {
		t.Fatal("merge export should exit non-zero")
	}
	if !strings.Contains(errb, "merge commit") {
		t.Fatalf("stderr should explain the merge refusal: %q", errb)
	}
}

func TestCommitExportPatchFileScoped(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "bar.go"), []byte("package bar\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add two files")
	sha := headSHA(t, dir)

	out := filepath.Join(t.TempDir(), "foo.patch")
	code, _, errb := runCLI(t, dir, "commit", "export-patch", sha, "--out", out, "--", "foo.go")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("read patch: %v", err)
	}
	if !strings.Contains(string(b), "diff --git a/foo.go b/foo.go") {
		t.Fatalf("patch should diff foo.go:\n%s", b)
	}
	if strings.Contains(string(b), "diff --git a/bar.go b/bar.go") {
		t.Fatalf("file-scoped patch should NOT diff bar.go:\n%s", b)
	}
}

func TestCommitExportPatchOverwriteRequiresForce(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	os.WriteFile(filepath.Join(dir, "foo.go"), []byte("package foo\n"), 0o644)
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add foo.go")
	sha := headSHA(t, dir)

	out := filepath.Join(t.TempDir(), "my.patch")
	if err := os.WriteFile(out, []byte("existing content"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errb := runCLI(t, dir, "commit", "export-patch", sha, "--out", out)
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (cancel); stderr=%s", code, errb)
	}
	if !strings.Contains(errb, "already exists") {
		t.Fatalf("stderr should mention already exists: %q", errb)
	}

	code, _, errb = runCLI(t, dir, "commit", "export-patch", sha, "--out", out, "--force")
	if code != 0 {
		t.Fatalf("--force exit=%d stderr=%s", code, errb)
	}
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "From ") {
		t.Fatalf("expected overwritten patch content, got: %q", string(b))
	}
}

func TestCommitExportPatchMissingShaUsage(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	code, _, errb := runCLI(t, dir, "commit", "export-patch")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
	if !strings.Contains(errb, "usage") {
		t.Fatalf("expected usage message: %q", errb)
	}
}

func TestCommitExportPatchUnknownFlag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	sha := headSHA(t, dir)
	code, _, errb := runCLI(t, dir, "commit", "export-patch", sha, "--bogus")
	if code != 2 {
		t.Fatalf("exit=%d, want 2", code)
	}
	if !strings.Contains(errb, "--bogus") {
		t.Fatalf("expected the unknown flag echoed: %q", errb)
	}
}
