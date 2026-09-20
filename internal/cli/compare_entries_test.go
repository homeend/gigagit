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

	// Frozen fallback: --patch lane (flags precede positionals). This lane
	// re-derives both sets from the endpoints and renders per member, so it
	// answers — the --patch refusal must NOT swallow it.
	code, out, _ = runCompare(t, dir, "compare", "--patch", "shelf:"+id, baseSha)
	if code != 0 {
		t.Fatalf("--patch exit %d", code)
	}
	for _, want := range []string{"--- a/f.txt", "+++ b/f.txt", "-doomed", "+base"} {
		if !strings.Contains(out, want) {
			t.Errorf("--patch stdout missing %q:\n%s", want, out)
		}
	}

	// The SAME entry spelled as a link. Two things are being pinned here:
	//
	//  1. `gg://<checkout>@<gone sha>?shelf=<id>` falls back to the frozen tar
	//     exactly as `shelf:<id>` does — before, this spelling leaked git's
	//     raw "fatal: bad object".
	//  2. Adding a /<path> NARROWS the set. ComparePatch re-derives its sets
	//     from the endpoints and would widen it back, so domain renders a
	//     narrowed set per member instead — it used to be refused.
	linkBase := "gg://" + filepath.ToSlash(dir)
	code, out, errb = runCompare(t, dir, "compare", linkBase+"@"+doomedSha+"?shelf="+id, baseSha)
	if code != 0 {
		t.Fatalf("link spelling of the frozen entry: exit %d, stderr %q", code, errb)
	}
	if !strings.Contains(out, "M\tf.txt") {
		t.Errorf("link spelling stdout = %q, want M\\tf.txt", out)
	}

	code, out, errb = runCompare(t, dir, "compare", "--patch",
		linkBase+"/f.txt@"+doomedSha+"?shelf="+id, baseSha)
	if code != 0 {
		t.Fatalf("--patch of a NARROWED shelf link: exit %d (stdout %q stderr %q)", code, out, errb)
	}
	if !strings.Contains(out, "+++ b/f.txt") || !strings.Contains(out, "-doomed\n+base") {
		t.Errorf("stdout = %q, want the frozen f.txt against the base", out)
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

// TestNoInvalidComparePairRemains is what TestCompareInvalidPairMessages
// became. It used to pin the two distinct invalid-pair refusals — "order
// endpoints oldest→newest…" for a reversed live pair and "a frozen shelf entry
// pairs only with a commit or another shelf entry" for a frozen side against
// @staged/@worktree. Both are GONE: validComparePair has been deleted and
// domain.CompareSets is total over the bounded/unbounded 2×2, so the two
// comparisons it screened out now ANSWER. This test keeps the same two
// fixtures and asserts the opposite, so the removal cannot be quietly undone.
func TestNoInvalidComparePairRemains(t *testing.T) {
	dir := newRepoDir(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())

	// Plain reverse pair: a comparison now, with no ordering lecture. The
	// working tree is clean here, so the answer is simply no rows.
	code, out, errb := runCompare(t, dir, "compare", "@worktree", "HEAD")
	if code != 0 {
		t.Fatalf("reverse pair: exit %d, want 0; stderr %q", code, errb)
	}
	if out != "" {
		t.Errorf("reverse pair on a clean tree: stdout = %q, want no rows", out)
	}
	if strings.Contains(errb, "not the reverse") || strings.Contains(errb, "oldest") {
		t.Errorf("reverse pair: the deleted ordering refusal is still printed: %q", errb)
	}

	// Shelf side paired with @staged: the shelf-specific explanation. The
	// entry's commit must be gone (a frozen model.EndpointShelf) — a live
	// shelf entry resolves to an ordinary EndpointCommit and would fall
	// through to the plain live-pair message instead.
	writeFile(t, dir, "f.txt", "base\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "base")
	baseSha := headSha(t, dir)

	writeFile(t, dir, "f.txt", "doomed\n")
	gitc(t, dir, "add", ".")
	gitc(t, dir, "commit", "-m", "doomed")
	doomedSha := headSha(t, dir)
	id := shelfCommitID(t, dir, doomedSha)

	gitc(t, dir, "reset", "--hard", baseSha)
	gitc(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitc(t, dir, "gc", "--prune=now")

	// The frozen shelf side against the INDEX is the comparison the old
	// refusal blocked, and it answers a real question: the shelf froze
	// f.txt = "doomed\n" while the index (reset to baseSha) holds "base\n",
	// so the projection of the shelf's one member onto the index is a
	// modification.
	code, out, errb = runCompare(t, dir, "compare", "shelf:"+id, "@staged")
	if code != 0 {
		t.Fatalf("shelf vs @staged: exit %d, want 0; stderr %q", code, errb)
	}
	if out != "M\tf.txt\n" {
		t.Errorf("shelf vs @staged: stdout = %q, want exactly \"M\\tf.txt\\n\"", out)
	}
	if strings.Contains(errb, "pairs only with") {
		t.Errorf("shelf vs @staged: the deleted shelf refusal is still printed: %q", errb)
	}
	// The frozen-fallback notice still goes to stderr, so stdout stays
	// parseable — that part of the contract is untouched.
	if !strings.Contains(errb, "frozen compare") {
		t.Errorf("shelf vs @staged: stderr should still carry the frozen notice: %q", errb)
	}
}
