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
