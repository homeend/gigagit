package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// stampVersionsFormat writes the format-2 marker ref directly at git's
// well-known empty-tree object. Both fixtures below fabricate a version ref
// with raw update-ref rather than going through the real writer (which
// stamps the marker itself on first write), so the versions feature would
// otherwise resolve the store as unmarked format 1 and gate `gg versions`
// off entirely (internal/cli cannot import internal/git — archtest — so this
// builds the ref path from the domain constants instead of git.MetaRef).
func stampVersionsFormat(t *testing.T, dir string) {
	t.Helper()
	ref := fmt.Sprintf("refs/gg/meta/%s/%d", domain.StoreVersions, domain.VersionsFormat)
	gitRun(t, dir, "update-ref", ref, "4b825dc642cb6eb9a060e54bf8d69288fbee4904")
}

// TestCmdVersionsListAndRestore fabricates a version ref directly (raw
// update-ref, the shelf_test.go/versions_test.go convention) pointing main's
// "1753100000-merge" snapshot at the repo's first commit, then exercises the
// list and restore lanes: list shows the id + subject, "latest" resolves to
// the newest (only) row and rewinds HEAD, an unknown branch prints "(no
// versions)", an unknown id fails loud, and a too-many-args list call is a
// usage error.
func TestCmdVersionsListAndRestore(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t) // main, one commit "initial", README.md = "hi\n"
	firstSha := runGit(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "second")

	gitRun(t, dir, "update-ref", "refs/gg/versions/main/1753100000-merge", firstSha)
	stampVersionsFormat(t, dir)

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
	t.Parallel()
	dir := newRepoDir(t)
	firstSha := runGit(t, dir, "rev-parse", "HEAD")

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "second")
	secondSha := runGit(t, dir, "rev-parse", "HEAD")

	gitRun(t, dir, "update-ref", "refs/gg/versions/main/1753100000-merge", firstSha)
	stampVersionsFormat(t, dir)

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

// A two-branch version row is followed by its preview link on an indented
// continuation line; a one-branch row (no Base/Ours) is not.
func TestCmdVersionsPrintsThePreviewLinkUnderTwoBranchRows(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	gitRun(t, dir, "update-ref", "refs/gg/versions/feat/1753100001-amend", ours) // one-branch: no preview
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "versions", "feat")
	if code != 0 {
		t.Fatalf("versions exit %d: %s", code, errb)
	}
	want := "\n  gg:///" // the sandbox has no remote: local form, indented
	if !strings.Contains(out, want) || !strings.Contains(out, "@"+base+".."+ours+"?version=1753100000-rebase") {
		t.Fatalf("missing the continuation link line:\n%s", out)
	}
	if strings.Contains(out, "?version=1753100001-amend") {
		t.Fatalf("a one-branch row must not get a link:\n%s", out)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 || !strings.HasPrefix(lines[0], "1753100001-amend ") || !strings.HasPrefix(lines[1], "1753100000-rebase ") || !strings.HasPrefix(lines[2], "  gg://") {
		t.Fatalf("rows must stay `<id> …` first, link indented under its row:\n%s", out)
	}
}

// A checkout whose path holds a character the grammar cannot carry has no
// link form: the rows print, the link line is simply absent.
func TestCmdVersionsNoLinkFormPrintsRowsOnly(t *testing.T) {
	t.Parallel()
	parent := t.TempDir()
	dir := filepath.Join(parent, "odd?name")
	if err := os.Rename(newRepoDir(t), dir); err != nil {
		t.Skip("cannot create a '?' path here: " + err.Error())
	}
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	stampVersionsFormat(t, dir)
	code, out, errb := runCLI(t, dir, "versions", "feat")
	if code != 0 {
		t.Fatalf("versions exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "1753100000-rebase") || strings.Contains(out, "gg://") {
		t.Fatalf("want the row without a link line:\n%s", out)
	}
}

// The printed link drives gg diff and gg link resolve --json carries the hint.
func TestVersionLinkRoundTripsThroughDiffAndResolve(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	stampVersionsFormat(t, dir)
	_, out, _ := runCLI(t, dir, "versions", "feat")
	var link string
	for _, ln := range strings.Split(out, "\n") {
		if strings.HasPrefix(ln, "  gg://") {
			link = strings.TrimSpace(ln)
		}
	}
	if link == "" {
		t.Fatalf("no link line in:\n%s", out)
	}
	if code, dout, errb := runCLI(t, dir, "diff", link); code != 0 || !strings.Contains(dout, "f3.txt") {
		t.Fatalf("diff <link> exit %d out=%q err=%s", code, dout, errb)
	}
	code, rout, errb := runCLI(t, dir, "link", "resolve", "--json", link)
	if code != 0 || !strings.Contains(rout, `"hint_kind":"version"`) || !strings.Contains(rout, `"hint_id":"1753100000-rebase"`) {
		t.Fatalf("resolve --json exit %d out=%q err=%s", code, rout, errb)
	}
}

// `gg link --version <branch> <id|latest>` prints the version's preview link —
// the line `gg versions` prints under the row; a one-branch record has none,
// an unknown id is an error, and the flag takes no other target or hint.
func TestCmdLinkVersion(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t)
	base, ours, other, _ := buildResurrectionFixture(t, dir, "feat", "feat")
	fabricateVersion(t, dir, "feat", "rebase", 1753100000, base, ours, other)
	stampVersionsFormat(t, dir)

	code, out, errb := runCLI(t, dir, "link", "--version", "feat", "1753100000-rebase")
	if code != 0 {
		t.Fatalf("link --version exit %d: %s", code, errb)
	}
	want := "@" + base + ".." + ours + "?version=1753100000-rebase\n"
	if !strings.HasPrefix(out, "gg:///") || !strings.HasSuffix(out, want) {
		t.Fatalf("out = %q, want the local-form preview link ending %q", out, want)
	}
	_, list, _ := runCLI(t, dir, "versions", "feat")
	if !strings.Contains(list, "  "+strings.TrimSpace(out)+"\n") {
		t.Fatalf("link --version must print what `gg versions` prints:\n%s\nvs\n%s", out, list)
	}

	gitRun(t, dir, "update-ref", "refs/gg/versions/feat/1753100001-amend", ours) // one-branch, newest
	if code, _, errb := runCLI(t, dir, "link", "--version", "feat", "latest"); code != 1 || !strings.Contains(errb, "records no preview") {
		t.Fatalf("one-branch latest: exit %d, %q", code, errb)
	}
	if code, _, errb := runCLI(t, dir, "link", "--version", "feat", "1-nope"); code != 1 || !strings.Contains(errb, "no version") {
		t.Fatalf("unknown id: exit %d, %q", code, errb)
	}
	if code, _, _ := runCLI(t, dir, "link", "--version", "feat"); code != 2 {
		t.Fatalf("missing id: exit %d, want 2", code)
	}
	if code, _, _ := runCLI(t, dir, "link", "--version", "feat", "--rev", "HEAD", "1753100000-rebase"); code != 2 {
		t.Fatalf("--version with --rev: exit %d, want 2", code)
	}
}
