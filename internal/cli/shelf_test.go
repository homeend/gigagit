package cli

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// shelfRepo is a real git temp repo with the shelf rooted in its own temp dir
// (hermetic: never touches the user's real state dir).
func shelfRepo(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return newRepoDir(t)
}

func TestShelfAddListRestoreRoundTrip(t *testing.T) {
	dir := shelfRepo(t)
	// README.md is committed as "hi\n" by newRepoDir; make an unstaged edit.
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	code, out, errb := runCLI(t, dir, "shelf", "add", "README.md")
	if code != 0 {
		t.Fatalf("add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)
	if id == "" {
		t.Fatalf("add printed no entry id")
	}

	code, out, _ = runCLI(t, dir, "shelf", "list")
	if code != 0 || !strings.Contains(out, "README.md") {
		t.Fatalf("list exit %d: %s", code, out)
	}

	// Delete the file entirely, then restore from the shelf to a new path.
	if err := os.Remove(filepath.Join(dir, "README.md")); err != nil {
		t.Fatal(err)
	}
	code, _, errb = runCLI(t, dir, "shelf", "restore", id, "restored.txt")
	if code != 0 {
		t.Fatalf("restore exit %d: %s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(dir, "restored.txt"))
	if err != nil || string(got) != "v2\n" {
		t.Fatalf("restored = %q err %v, want v2", got, err)
	}
}

func TestShelfRestoreRequiresDest(t *testing.T) {
	dir := shelfRepo(t)
	code, _, _ := runCLI(t, dir, "shelf", "restore", "some-id")
	if code != 2 {
		t.Fatalf("missing dest should exit 2, got %d", code)
	}
}

func TestShelfRestoreRefusesExistingWithoutForce(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, out, _ := runCLI(t, dir, "shelf", "add", "README.md")
	id := strings.TrimSpace(out)

	// README.md still exists and differs from the shelved bytes? It IS the
	// shelved bytes here, so restoring onto it is a no-op (identical). Use a
	// different existing file to force the differ branch.
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("different\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, _, _ := runCLI(t, dir, "shelf", "restore", id, "other.txt")
	if code != 2 {
		t.Fatalf("existing differing dest w/o --force should exit 2, got %d", code)
	}
	code, _, errb := runCLI(t, dir, "shelf", "restore", "--force", id, "other.txt")
	if code != 0 {
		t.Fatalf("--force should succeed, got %d: %s", code, errb)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "other.txt"))
	if string(got) != "v2\n" {
		t.Fatalf("after --force other.txt = %q, want v2", got)
	}
}

func TestShelfCommitThenExport(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add a")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	code, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit exit=%d stderr=%s", code, errb)
	}
	// stdout: "shelved commit as <id>\n"
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("shelf commit printed no output")
	}
	id := fields[len(fields)-1]
	if id == "" {
		t.Fatalf("shelf commit produced no entry id, stdout=%q", out)
	}

	outDir := filepath.Join(t.TempDir(), "out")
	code, _, errb = runCLI(t, dir, "shelf", "export", "--dir", outDir, id)
	if code != 0 {
		t.Fatalf("shelf export exit=%d stderr=%s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "src", "a.txt"))
	if err != nil || string(got) != "alpha\n" {
		t.Fatalf("exported a.txt = %q err=%v, want alpha\\n", got, err)
	}
}

func TestShelfExportRefusesExistingWithoutForce(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.MkdirAll(filepath.Join(dir, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "src", "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add a")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	_, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	fields := strings.Fields(out)
	if len(fields) == 0 {
		t.Fatalf("shelf commit printed no output, stderr=%s", errb)
	}
	id := fields[len(fields)-1]

	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	} // pre-exists -> triggers the overwrite/cancel decision

	code, _, _ := runCLI(t, dir, "shelf", "export", "--dir", outDir, id)
	if code != 2 {
		t.Fatalf("existing target dir w/o --force should exit 2, got %d", code)
	}
	code, _, errb = runCLI(t, dir, "shelf", "export", "--dir", outDir, "--force", id)
	if code != 0 {
		t.Fatalf("--force should succeed, got %d: %s", code, errb)
	}
	got, err := os.ReadFile(filepath.Join(outDir, "src", "a.txt"))
	if err != nil || string(got) != "alpha\n" {
		t.Fatalf("after --force a.txt = %q err=%v, want alpha\\n", got, err)
	}
}

func TestShelfExportUnknownEntry(t *testing.T) {
	dir := shelfRepo(t)
	code, _, errb := runCLI(t, dir, "shelf", "export", "no-such-id")
	if code != 1 {
		t.Fatalf("unknown entry export should exit 1, got %d: %s", code, errb)
	}
}

func TestShelfCommitUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	if code, _, _ := runCLI(t, dir, "shelf", "commit"); code != 2 {
		t.Fatalf("shelf commit without sha should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "export"); code != 2 {
		t.Fatalf("shelf export without id should exit 2, got %d", code)
	}
}

func TestShelfCommitName(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "add a")
	sha := runGit(t, dir, "rev-parse", "HEAD")

	if code, _, errb := runCLI(t, dir, "shelf", "commit", "--name", "my fix", sha); code != 0 {
		t.Fatalf("shelf commit --name exit=%d stderr=%s", code, errb)
	}

	code, out, errb := runCLI(t, dir, "shelf", "list")
	if code != 0 {
		t.Fatalf("shelf list exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "my fix") {
		t.Fatalf("shelf list did not show the label; stdout=%q", out)
	}
}

func TestShelfUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	if code, _, _ := runCLI(t, dir, "shelf"); code != 2 {
		t.Fatalf("bare shelf should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "bogus"); code != 2 {
		t.Fatalf("unknown subcommand should exit 2, got %d", code)
	}
	if code, _, _ := runCLI(t, dir, "shelf", "add"); code != 2 {
		t.Fatalf("add without paths should exit 2, got %d", code)
	}
}

// --- gg shelf cherry-pick ---

// shelfCherryPickFixture shelves a commit that adds pick.txt on a side
// branch, then returns HEAD to main. Returns the repo dir, the shelf entry
// id, and the commit sha.
func shelfCherryPickFixture(t *testing.T) (dir, id, sha string) {
	t.Helper()
	dir = shelfRepo(t)
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "pick.txt"), []byte("picked\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "add pick.txt")
	sha = revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	gitRun(t, dir, "checkout", "main")
	return dir, id, sha
}

func revParseHead(t *testing.T, dir string) string {
	t.Helper()
	cmd := exec.Command("git", "-C", dir, "rev-parse", "HEAD")
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

// shelveCommit runs `gg shelf commit <sha>` and returns the printed entry id.
func shelveCommit(t *testing.T, dir, sha string) string {
	t.Helper()
	code, out, errb := runCLI(t, dir, "shelf", "commit", sha)
	if code != 0 {
		t.Fatalf("shelf commit exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "shelved commit as "))
	if id == "" {
		t.Fatalf("could not parse entry id from %q", out)
	}
	return id
}

// gcAway makes sha unreachable and prunes it, then proves it is gone.
func gcAway(t *testing.T, dir, sha string) {
	t.Helper()
	gitRun(t, dir, "reflog", "expire", "--expire=all", "--all")
	gitRun(t, dir, "gc", "--prune=now")
	cat := exec.Command("git", "-C", dir, "cat-file", "-e", sha)
	cat.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if err := cat.Run(); err == nil {
		t.Fatalf("commit %s was not pruned; fixture does not exercise the gc'd lane", sha)
	}
}

func TestShelfCherryPickLiveLane(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "cherry-picked") {
		t.Fatalf("live lane must run engine.CherryPick (summary says cherry-picked); stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after cherry-pick")
	}
}

func TestShelfCherryPickPatchFlagForcesReplay(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", "--patch", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	// Both lanes mint a new sha; lane selection is observable in the op
	// summary — ApplyPatch says "applied …", CherryPick says "cherry-picked".
	if !strings.Contains(out, "applied") || strings.Contains(out, "cherry-picked") {
		t.Fatalf("--patch must force the ApplyPatch lane; stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after patch replay")
	}
	subj, err := exec.Command("git", "-C", dir, "log", "-1", "--format=%s").Output()
	if err != nil || strings.TrimSpace(string(subj)) != "add pick.txt" {
		t.Fatalf("replayed commit subject = %q err %v, want add pick.txt", subj, err)
	}
}

func TestShelfCherryPickGcdFallsBackToPatch(t *testing.T) {
	dir, id, sha := shelfCherryPickFixture(t)
	gitRun(t, dir, "branch", "-D", "feat")
	gcAway(t, dir, sha)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb)
	}
	if !strings.Contains(out, "applied") {
		t.Fatalf("gc'd commit must fall back to the patch lane; stdout: %q", out)
	}
	if _, err := os.Stat(filepath.Join(dir, "pick.txt")); err != nil {
		t.Fatal("pick.txt missing on main after gc'd-lane replay")
	}
}

// mergeShelfFixture shelves a MERGE commit — ShelfAddCommit stores it
// tar-only (format-patch cannot represent a merge), so the entry has no
// patch. Returns dir, entry id, merge sha, and main's pre-merge sha.
func mergeShelfFixture(t *testing.T) (dir, id, sha, preMerge string) {
	t.Helper()
	dir = shelfRepo(t)
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "side.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "side change")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "main.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "main change")
	preMerge = revParseHead(t, dir)
	gitRun(t, dir, "merge", "--no-ff", "-m", "merge side", "side")
	sha = revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	return dir, id, sha, preMerge
}

func TestShelfCherryPickPatchFlagOnPatchlessEntry(t *testing.T) {
	dir, id, _, _ := mergeShelfFixture(t)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", "--patch", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "has no stored patch") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickGcdWithoutPatch(t *testing.T) {
	dir, id, sha, preMerge := mergeShelfFixture(t)
	gitRun(t, dir, "update-ref", "refs/heads/main", preMerge)
	gitRun(t, dir, "checkout", "-f", "main")
	gitRun(t, dir, "branch", "-D", "side")
	gcAway(t, dir, sha)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "no longer exists and this entry has no stored patch") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickFileEntryRefused(t *testing.T) {
	dir := shelfRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, out, errb := runCLI(t, dir, "shelf", "add", "README.md")
	if code != 0 {
		t.Fatalf("shelf add exit %d: %s", code, errb)
	}
	id := strings.TrimSpace(out)
	code, _, errb = runCLI(t, dir, "shelf", "cherry-pick", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "not a shelved commit") {
		t.Fatalf("stderr: %q", errb)
	}
}

func TestShelfCherryPickUnknownID(t *testing.T) {
	dir := shelfRepo(t)
	code, _, errb := runCLI(t, dir, "shelf", "cherry-pick", "no-such-id")
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	if !strings.Contains(errb, "no entry") {
		t.Fatalf("stderr: %q", errb)
	}
}

// shelfConflictFixture shelves a commit that conflicts with main's HEAD.
func shelfConflictFixture(t *testing.T) (dir, id string) {
	t.Helper()
	dir = shelfRepo(t)
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-m", "base")
	gitRun(t, dir, "checkout", "-b", "feat")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("feat\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "feat change")
	sha := revParseHead(t, dir)
	id = shelveCommit(t, dir, sha)
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "shared.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "commit", "-am", "main change")
	return dir, id
}

func TestShelfCherryPickConflictKeep(t *testing.T) {
	dir, id := shelfConflictFixture(t)
	code, _, _ := runCLI(t, dir, "shelf", "cherry-pick", "--on-conflict=keep", id)
	if code != 1 {
		t.Fatalf("exit %d, want 1 (conflicts left in tree)", code)
	}
	got, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil || !strings.Contains(string(got), "<<<<<<<") {
		t.Fatalf("conflict markers missing (err %v): %q", err, got)
	}
}

func TestShelfCherryPickConflictAbort(t *testing.T) {
	dir, id := shelfConflictFixture(t)
	code, out, errb := runCLI(t, dir, "shelf", "cherry-pick", "--on-conflict=abort", id)
	// Parity with `gg cherry-pick --on-conflict=abort`: the abort is the
	// requested outcome and the rollback succeeded → exit 0, "aborted" summary.
	if code != 0 {
		t.Fatalf("exit %d, want 0 (stderr: %s)", code, errb)
	}
	if !strings.Contains(out, "aborted") {
		t.Fatalf("abort summary missing: %q", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "shared.txt"))
	if err != nil || string(got) != "main\n" {
		t.Fatalf("abort must leave the tree clean (err %v): %q", err, got)
	}
}

func TestShelfCherryPickUsageErrors(t *testing.T) {
	dir := shelfRepo(t)
	for _, args := range [][]string{
		{"shelf", "cherry-pick"},
		{"shelf", "cherry-pick", "a", "b"},
		{"shelf", "cherry-pick", "--on-conflict=bogus", "a"},
	} {
		if code, _, _ := runCLI(t, dir, args...); code != 2 {
			t.Fatalf("%v: exit %d, want 2", args, code)
		}
	}
}

func TestBatchDrivesShelfCherryPick(t *testing.T) {
	dir, id, _ := shelfCherryPickFixture(t)
	code, out, errb := runCLIStdin(t, dir, "shelf cherry-pick "+id+"\n", "batch")
	if code != 0 {
		t.Fatalf("batch exit %d: %s", code, errb)
	}
	if !strings.Contains(out, "#1 ok shelf cherry-pick") {
		t.Fatalf("batch framing missing: %q", out)
	}
}
