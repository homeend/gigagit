package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func cliWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref: %v", err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeRecycleCleanTarget(t *testing.T) {
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "recycled") {
		t.Fatalf("stdout = %q", out.String())
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

func TestWorktreeRecycleOnDirtyCommit(t *testing.T) {
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=commit", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	subj, _ := exec.Command("git", "-C", wt, "log", "-1", "--format=%s", "a").Output()
	if !strings.HasPrefix(string(subj), "Committed changes due to worktree recycle ") {
		t.Fatalf("subject on a = %q", subj)
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

// A relative path resolves against the main worktree root, not the process
// cwd (the e2e harness and shell wrappers rely on this).
func TestWorktreeRecycleRelativePath(t *testing.T) {
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "../wt-a", "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
}

func TestWorktreeRecycleDirtyWithoutFlagIsRefusedInAPipeline(t *testing.T) {
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("a dirty target with no --on-dirty must not succeed in a pipeline")
	}
	if !strings.Contains(errb.String(), "recycle.dirty") {
		t.Fatalf("stderr = %q, want the decision id", errb.String())
	}
	if got := headOf(t, wt); got != "a" {
		t.Fatalf("worktree HEAD = %q, want a (untouched)", got)
	}
	if _, err := os.Stat(filepath.Join(wt, "x.txt")); err != nil {
		t.Fatal("x.txt must survive")
	}
}

// Review Focus 5.
func TestWorktreeRecycleRejectsBadOnDirty(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=stash", "/nowhere", "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "--on-dirty") {
		t.Fatalf("stderr = %q", errb.String())
	}
}

func TestWorktreeRecycleUsage(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "recycle", "only-one-arg"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("exit = %d, want 2", code)
	}
}

// --on-dirty=shelve parks the work as one shelf set; a deletion the set
// cannot carry is recorded in the entry's note (gg note list --shelf).
func TestWorktreeRecycleOnDirtyShelve(t *testing.T) {
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	os.WriteFile(filepath.Join(wt, "x.txt"), []byte("x\n"), 0o644)
	os.Remove(filepath.Join(wt, "README.md"))
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=shelve", wt, "loose"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), `shelved as "WIP on a"`) {
		t.Fatalf("stdout = %q", out.String())
	}
	if got := headOf(t, wt); got != "loose" {
		t.Fatalf("worktree HEAD = %q, want loose", got)
	}
	if st, _ := exec.Command("git", "-C", wt, "status", "--porcelain").Output(); len(st) != 0 {
		t.Fatalf("worktree not clean:\n%s", st)
	}
	code, list, errList := runCLI(t, dir, "shelf", "list")
	if code != 0 || !strings.Contains(list, "WIP on a") {
		t.Fatalf("shelf list (exit %d): %s%s", code, list, errList)
	}
	id := strings.Fields(list)[0]
	code, notes, errNotes := runCLI(t, dir, "note", "list", "--shelf", id)
	if code != 0 || !strings.Contains(notes, "    Deleted (not in this set):") || !strings.Contains(notes, "      README.md") {
		t.Fatalf("note list --shelf %s (exit %d):\n%s%s", id, code, notes, errNotes)
	}
}

// cliRemoteFoo makes origin/foo a genuine remote-tracking branch one commit
// ahead of main, with no local foo.
func cliRemoteFoo(t *testing.T, dir string) {
	t.Helper()
	for _, a := range [][]string{
		{"config", "remote.origin.url", "file://" + dir},
		{"config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*"},
		{"commit", "--allow-empty", "-m", "remote work"},
		{"update-ref", "refs/remotes/origin/foo", "HEAD"},
		{"reset", "--hard", "HEAD~1"},
	} {
		c := exec.Command("git", append([]string{"-C", dir}, a...)...)
		c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", a, err, out)
		}
	}
}

func TestWorktreeRecycleRemoteBranch(t *testing.T) {
	dir := newCLIRepo(t)
	cliRemoteFoo(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "origin/foo"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errb.String())
	}
	if got := headOf(t, wt); got != "foo" {
		t.Fatalf("HEAD = %q, want foo (origin/ stripped)", got)
	}
	if !strings.Contains(out.String(), "from origin/foo") {
		t.Fatalf("stdout = %q", out.String())
	}
}

func TestWorktreeRecycleRemoteBranchAs(t *testing.T) {
	dir := newCLIRepo(t)
	cliRemoteFoo(t, dir)
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", "--as", "bar", wt, "origin/foo"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("code = %d, stderr = %s", code, errb.String())
	}
	if got := headOf(t, wt); got != "bar" {
		t.Fatalf("HEAD = %q, want bar", got)
	}
}

func TestWorktreeRecycleRemoteDivergedHints(t *testing.T) {
	dir := newCLIRepo(t)
	cliRemoteFoo(t, dir)
	// foo: a commit on main that origin/foo lacks — diverged.
	c := exec.Command("git", "-C", dir, "commit-tree", "-p", "main", "-m", "local", "main^{tree}")
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	sha, err := c.Output()
	if err != nil {
		t.Fatal(err)
	}
	exec.Command("git", "-C", dir, "branch", "-f", "foo", strings.TrimSpace(string(sha))).Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "recycle", wt, "origin/foo"}, strings.NewReader(""), &out, &errb, "")
	if code == 0 || !strings.Contains(errb.String(), "--as") {
		t.Fatalf("code = %d, stderr = %q; want a failure with the --as hint", code, errb.String())
	}
	if got := headOf(t, wt); got != "a" {
		t.Fatalf("HEAD = %q, want a (untouched)", got)
	}
}

func TestWorktreeRecycleAsNeedsRemoteBranch(t *testing.T) {
	dir := newCLIRepo(t)
	exec.Command("git", "-C", dir, "branch", "loose").Run()
	wt := cliWorktree(t, dir, "a", "wt-a")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "recycle", "--as", "x", wt, "loose"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("code = %d, want 2 (--as only renames a remote branch); stderr = %s", code, errb.String())
	}
}
