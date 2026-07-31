package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// run invokes the CLI in dir and returns (exit, stdout, stderr).
func runCompare(t *testing.T, dir string, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(dir, args, strings.NewReader(""), &out, &errb, "")
	return code, out.String(), errb.String()
}

func gitc(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// shelfCommitID shelves HEAD and returns the new entry's id, parsed from
// `gg shelf list` (commit entries render with their commit-<short>-<hash8> id).
func shelfCommitID(t *testing.T, dir, sha string) string {
	t.Helper()
	code, _, errb := runCompare(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit: exit %d, stderr %s", code, errb)
	}
	_, out, _ := runCompare(t, dir, "shelf", "list")
	m := regexp.MustCompile(`commit-[0-9a-f]+-[0-9a-f]{8}`).FindString(out)
	if m == "" {
		t.Fatalf("no commit entry id in shelf list output:\n%s", out)
	}
	return m
}

func headSha(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestCompareShelfEntryLive(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir()) // isolate the shelf store

	writeFile(t, dir, "f.txt", "old\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "A")
	shaA := headSha(t, dir)
	id := shelfCommitID(t, dir, shaA)
	writeFile(t, dir, "f.txt", "new\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "B")
	shaB := headSha(t, dir)

	// Live lane: entry sha still exists → plain tree compare, no stderr note.
	code, out, errb := runCompare(t, dir, "compare", "shelf:"+id, shaB)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errb)
	}
	if !strings.Contains(out, "M\tf.txt") {
		t.Errorf("stdout = %q, want M\\tf.txt line", out)
	}
	if strings.Contains(errb, "frozen") {
		t.Errorf("live compare must not print the frozen note, got %q", errb)
	}
}

func TestCompareShelfEntryFrozenAndPatch(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	writeFile(t, dir, "f.txt", "base\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "base")
	baseSha := headSha(t, dir)

	writeFile(t, dir, "f.txt", "doomed\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "doomed")
	doomedSha := headSha(t, dir)
	id := shelfCommitID(t, dir, doomedSha)

	// Erase the shelved commit: rewind, expire reflogs, gc.
	gitc(t, dir, "reset", "--hard", baseSha)
	gitc(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitc(t, dir, "gc", "--prune=now")

	// Frozen fallback: list lane.
	code, out, errb := runCompare(t, dir, "compare", "shelf:"+id, baseSha)
	if code != 0 {
		t.Fatalf("exit %d, stderr %s", code, errb)
	}
	if !strings.Contains(out, "M\tf.txt") {
		t.Errorf("stdout = %q, want M\\tf.txt", out)
	}
	if !strings.Contains(errb, "frozen compare") {
		t.Errorf("stderr = %q, want the frozen note", errb)
	}

	// Frozen fallback: --patch lane (flags precede positionals).
	code, out, _ = runCompare(t, dir, "compare", "--patch", "shelf:"+id, baseSha)
	if code != 0 {
		t.Fatalf("--patch exit %d", code)
	}
	for _, want := range []string{"--- a/f.txt", "+++ b/f.txt", "-doomed", "+base"} {
		if !strings.Contains(out, want) {
			t.Errorf("--patch stdout missing %q:\n%s", want, out)
		}
	}
}

func TestCompareSpecErrors(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Unknown shelf id → usage-level error (exit 2).
	code, _, _ := runCompare(t, dir, "compare", "shelf:nope", "HEAD")
	if code != 2 {
		t.Errorf("unknown shelf id: exit %d, want 2", code)
	}

	// A FILE shelf entry is not a commit entry → exit 2.
	writeFile(t, dir, "plain.txt", "x\n")
	code, _, _ = runCompare(t, dir, "shelf", "add", "plain.txt")
	if code != 0 {
		t.Fatal("shelf add failed")
	}
	_, list, _ := runCompare(t, dir, "shelf", "list")
	fileID := regexp.MustCompile(`unstaged-[0-9a-z-]+-[0-9a-f]{8}`).FindString(list)
	if fileID == "" {
		t.Fatalf("no file entry id in %q", list)
	}
	code, _, errb := runCompare(t, dir, "compare", "shelf:"+fileID, "HEAD")
	if code != 2 || !strings.Contains(errb, "not a commit") {
		t.Errorf("file entry: exit %d stderr %q, want 2 + 'not a commit'", code, errb)
	}
}
