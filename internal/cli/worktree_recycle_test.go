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
	code := Run(dir, []string{"worktree", "recycle", "--on-dirty=shelve", "/nowhere", "loose"}, strings.NewReader(""), &out, &errb, "")
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
