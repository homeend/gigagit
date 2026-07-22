package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmdVersionsListAndRestore fabricates a version ref directly (raw
// update-ref, the shelf_test.go/versions_test.go convention) pointing main's
// "1753100000-merge" snapshot at the repo's first commit, then exercises the
// list and restore lanes: list shows the id + subject, "latest" resolves to
// the newest (only) row and rewinds HEAD, an unknown branch prints "(no
// versions)", an unknown id fails loud, and a too-many-args list call is a
// usage error.
func TestCmdVersionsListAndRestore(t *testing.T) {
	dir := newRepoDir(t) // main, one commit "initial", README.md = "hi\n"
	firstSha := runGit(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "second")

	gitRun(t, dir, "update-ref", "refs/gg/versions/main/1753100000-merge", firstSha)

	// gg versions
	code, out, errb := runCLI(t, dir, "versions")
	if code != 0 {
		t.Fatalf("versions exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "1753100000-merge") {
		t.Fatalf("versions output missing the id: %q", out)
	}
	if !strings.Contains(out, "initial") {
		t.Fatalf("versions output missing the first commit's subject: %q", out)
	}

	// gg versions restore main latest
	code, _, errb = runCLI(t, dir, "versions", "restore", "main", "latest")
	if code != 0 {
		t.Fatalf("restore latest exit %d: %s", code, errb)
	}
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != firstSha {
		t.Fatalf("HEAD = %s, want restored %s", head, firstSha)
	}

	// gg versions nosuch (no versions recorded for that branch name)
	code, out, errb = runCLI(t, dir, "versions", "nosuch")
	if code != 0 {
		t.Fatalf("versions nosuch exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "(no versions)") {
		t.Fatalf("versions nosuch output = %q, want (no versions)", out)
	}

	// gg versions restore main bogus-id
	code, _, errb = runCLI(t, dir, "versions", "restore", "main", "bogus-id")
	if code != 1 {
		t.Fatalf("restore bogus-id exit %d, want 1 (stderr: %s)", code, errb)
	}

	// gg versions a b c (usage)
	code, _, errb = runCLI(t, dir, "versions", "a", "b", "c")
	if code != 2 {
		t.Fatalf("versions a b c exit %d, want 2 (stderr: %s)", code, errb)
	}
}

// TestCmdVersionsRestoreDirtyRequiresDiscard covers the "restore-dirty"
// decision on the current-branch lane: a dirtied tracked file makes the
// restore fork a decision that, with empty non-interactive stdin, fails loud
// (exit 1, HEAD untouched); --discard pre-answers "proceed" and the restore
// succeeds, discarding the dirty change along with the hard reset.
func TestCmdVersionsRestoreDirtyRequiresDiscard(t *testing.T) {
	dir := newRepoDir(t)
	firstSha := runGit(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "second")
	secondSha := runGit(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "update-ref", "refs/gg/versions/main/1753100000-merge", firstSha)

	// Dirty the tree with an uncommitted tracked-file change.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, _, errb := runCLI(t, dir, "versions", "restore", "main", "latest")
	if code != 1 {
		t.Fatalf("dirty restore without --discard exit %d, want 1 (stderr: %s)", code, errb)
	}
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != secondSha {
		t.Fatalf("HEAD = %s, want unchanged %s after the failed decision", head, secondSha)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(got) != "dirty\n" {
		t.Fatalf("dirty change was not preserved after the refused restore: %q err=%v", got, err)
	}

	code, _, errb = runCLI(t, dir, "versions", "restore", "--discard", "main", "latest")
	if code != 0 {
		t.Fatalf("--discard restore exit %d, want 0 (stderr: %s)", code, errb)
	}
	if head := runGit(t, dir, "rev-parse", "HEAD"); head != firstSha {
		t.Fatalf("HEAD = %s, want restored %s", head, firstSha)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "README.md")); err != nil || string(got) != "hi\n" {
		t.Fatalf("README.md = %q err=%v, want restored content %q", got, err, "hi\n")
	}
}
