package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// newCLIRepo makes a temp git repo with one commit on main and returns its dir.
func newCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		c := exec.Command("git", args...)
		c.Dir = dir
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-b", "main")
	os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644)
	run("add", ".")
	run("commit", "-m", "initial")
	return dir
}

func TestWorktreeList(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "list"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "main") {
		t.Fatalf("worktree list output missing main:\n%s", out.String())
	}
}

func TestWorktreeUnknownSub(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "bogus"}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("unknown worktree subcommand should be a non-zero exit")
	}
}

func TestWorktreeAddCreatesAndPrints(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[worktree]\nbranch_templates = []\ndefault_branch_template = \"issue/<user:id>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader("77\n"), &out, &errb, cwdFile)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "issue/77") {
		t.Fatalf("output missing branch issue/77:\n%s", out.String())
	}
	wt := filepath.Clean(filepath.Join(dir, "..", filepath.Base(dir)+".worktrees", "issue-77"))
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree not created at %s: %v", wt, err)
	}
	got, _ := os.ReadFile(cwdFile)
	if strings.TrimSpace(string(got)) != wt {
		t.Fatalf("cwd-file = %q, want %q", strings.TrimSpace(string(got)), wt)
	}
}

func TestWorktreeAddDefaultsToCurrentBranchNoUserFields(t *testing.T) {
	dir := newCLIRepo(t)
	// Default templates (no <user:>), no start-point arg -> uses current branch.
	os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[worktree]\ndefault_branch_template = \"wt/<parent-branch>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	// parent-branch resolved to the current branch (main); no prompt on stdout.
	if !strings.Contains(out.String(), "wt/main") {
		t.Fatalf("output missing wt/main (start-point should default to current branch):\n%s", out.String())
	}
	wt := filepath.Clean(filepath.Join(dir, "..", filepath.Base(dir)+".worktrees", "wt-main"))
	if _, err := os.Stat(filepath.Join(wt, "README.md")); err != nil {
		t.Fatalf("worktree not created at %s: %v", wt, err)
	}
}

func TestWorktreeAddResolveErrorCreatesNothing(t *testing.T) {
	dir := newCLIRepo(t)
	// Unknown token makes resolution fail before any git work.
	os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[worktree]\ndefault_branch_template = \"b-<bogus>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("a template resolve error should be a non-zero exit")
	}
	// No worktree container was created.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), filepath.Base(dir)+".worktrees")); err == nil {
		t.Fatal("no worktree should have been created on a resolve error")
	}
}

func TestWorktreeAddEOFWithoutNewline(t *testing.T) {
	dir := newCLIRepo(t)
	os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[worktree]\ndefault_branch_template = \"issue/<user:id>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	var out, errb bytes.Buffer
	// stdin supplies the value with NO trailing newline (piped input).
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader("42"), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "issue/42") {
		t.Fatalf("EOF-without-newline input not captured; output:\n%s", out.String())
	}
}

// addCLIWorktree creates a linked worktree of dir and returns its path.
func addCLIWorktree(t *testing.T, dir, branch, name string) string {
	t.Helper()
	wt := filepath.Join(filepath.Dir(dir), name)
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", branch, wt, "main")
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return wt
}

func TestWorktreeRemoveWorktreeOnly(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm1", "wt-cli-rm1")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", wt}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present: %v", err)
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/rm1").Run() != nil {
		t.Fatal("branch should be kept without --with-branch")
	}
}

func TestWorktreeRemoveWithBranch(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm2", "wt-cli-rm2")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", "--with-branch", wt}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/rm2").Run() == nil {
		t.Fatal("branch should be deleted with --with-branch")
	}
}

func TestWorktreeRemoveDirtyNeedsForce(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm3", "wt-cli-rm3")
	os.WriteFile(filepath.Join(wt, "README.md"), []byte("changed\n"), 0o644)

	var out, errb bytes.Buffer
	// Non-interactive because os.Stdin is not a TTY under `go test`, so the
	// worktree-dirty decision cannot be answered without --force.
	if code := Run(dir, []string{"worktree", "remove", wt}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("dirty removal without --force should fail non-interactively")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should still exist: %v", err)
	}
	var out2, errb2 bytes.Buffer
	if code := Run(dir, []string{"worktree", "remove", "--force", wt}, strings.NewReader(""), &out2, &errb2, ""); code != 0 {
		t.Fatalf("forced removal exit = %d, stderr=%s", code, errb2.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree not removed after --force: %v", err)
	}
}

func TestWorktreeRemoveUnknownPath(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", filepath.Join(dir, "nope")},
		strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("removing an unknown path should be a non-zero exit")
	}
	if !strings.Contains(errb.String(), "no worktree") {
		t.Fatalf("stderr should explain the unknown path: %s", errb.String())
	}
}
