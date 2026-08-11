package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
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

// TestWorktreeAddFromLinkedWorktreeAnchorsOnMain reproduces the field report:
// running `gg worktree add` from inside a (nested) linked worktree must place
// the new worktree beside the MAIN repo using the MAIN repo's <repo> name — not
// nested under the current worktree with the current worktree's name (which
// doubled the ".worktrees" segment).
func TestWorktreeAddFromLinkedWorktreeAnchorsOnMain(t *testing.T) {
	dir := newCLIRepo(t)

	// A linked worktree nested two levels below main, as in the report.
	linked := filepath.Join(dir, "nested", "wt-a")
	c := exec.Command("git", "-C", dir, "worktree", "add", "-b", "feature/a", linked, "main")
	c.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	// Config is loaded from the invoking worktree's top level, so place it there.
	os.WriteFile(filepath.Join(linked, ".gg.toml"),
		[]byte("[worktree]\ndefault_branch_template = \"wt/<parent-branch>\"\npath_template = \"../<repo>.worktrees/<branch>\"\n"),
		0o644)

	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	// Invoke from the linked worktree (first arg = process working dir).
	code := Run(linked, []string{"worktree", "add", "main"}, strings.NewReader(""), &out, &errb, cwdFile)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}

	mainBase := filepath.Base(dir)
	want := filepath.Clean(filepath.Join(filepath.Dir(dir), mainBase+".worktrees", "wt-main"))
	wrong := filepath.Clean(filepath.Join(linked, "..", "wt-a.worktrees", "wt-main")) // the bug
	if _, err := os.Stat(filepath.Join(want, "README.md")); err != nil {
		t.Fatalf("worktree not created beside main at %s: %v\n(the bug would put it at %s)", want, err, wrong)
	}
	if got, _ := os.ReadFile(cwdFile); strings.TrimSpace(string(got)) != want {
		t.Fatalf("cwd-file = %q, want main-anchored %q", strings.TrimSpace(string(got)), want)
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

func TestWorktreeRemoveLockedNeedsForce(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm-lock", "wt-cli-lock")
	// Stand in for the "initializing" lock an interrupted `git worktree add`
	// leaves; even remove --force refuses it until unlocked.
	if out, err := exec.Command("git", "-C", dir, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock: %v\n%s", err, out)
	}

	var out, errb bytes.Buffer
	// Non-interactive: the worktree-locked decision can't be answered without --force.
	if code := Run(dir, []string{"worktree", "remove", wt}, strings.NewReader(""), &out, &errb, ""); code == 0 {
		t.Fatal("locked removal without --force should fail non-interactively")
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should still exist: %v", err)
	}

	var out2, errb2 bytes.Buffer
	if code := Run(dir, []string{"worktree", "remove", "--force", wt}, strings.NewReader(""), &out2, &errb2, ""); code != 0 {
		t.Fatalf("forced removal of locked worktree exit = %d, stderr=%s", code, errb2.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("locked worktree not removed after --force: %v", err)
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

// TestWorktreeRemoveRepoRelativePath verifies that a path relative to the repo
// top level (e.g. "../wt-name") matches a worktree even when the process
// working directory is NOT the repo directory (tests run from the package dir).
// This exercises the fromTop resolution added in cmdWorktreeRemove: without it
// filepath.Abs("../wt-name") resolves against the package dir and never matches.
func TestWorktreeRemoveRepoRelativePath(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rm5", "wt-cli-rm5")

	// Sanity: process cwd is NOT the repo dir (it's the package dir under the
	// source tree), so a plain filepath.Abs("../wt-cli-rm5") would not equal wt.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	naiveAbs := filepath.Clean(filepath.Join(cwd, "../wt-cli-rm5"))
	if naiveAbs == wt {
		t.Skip("process cwd happens to be the repo's parent — test precondition not met")
	}

	// Pass the path relative to the repo top level, exactly as a template like
	// "../<repo>.worktrees/<branch>" would produce.
	repoRelative := "../wt-cli-rm5"

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "remove", repoRelative}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("worktree dir still present after repo-relative remove: %v", err)
	}
	// Branch should be kept (no --with-branch).
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/feature/rm5").Run() != nil {
		t.Fatal("branch should be kept without --with-branch")
	}
}

// TestWorktreeRemoveFromLinkedWorktreeAnchorsOnMain is the remove-side mirror of
// the create fix: a relative target (as a template like
// "../<repo>.worktrees/<branch>" produces) resolves against the MAIN worktree,
// so `gg worktree remove <relative>` matches even when run from a *different*
// linked worktree — keeping create/remove round-tripping.
func TestWorktreeRemoveFromLinkedWorktreeAnchorsOnMain(t *testing.T) {
	dir := newCLIRepo(t)
	mainBase := filepath.Base(dir)

	// The worktree to remove, placed exactly where `gg worktree add` would put it.
	target := filepath.Join(filepath.Dir(dir), mainBase+".worktrees", "wt-target")
	gitRun(t, dir, "worktree", "add", "-b", "feature/target", target, "main")

	// A separate nested worktree we invoke gg from.
	from := filepath.Join(dir, "nested", "wt-from")
	gitRun(t, dir, "worktree", "add", "-b", "feature/from", from, "main")

	// Main-relative path, resolved against the main worktree despite running from `from`.
	rel := filepath.Join("..", mainBase+".worktrees", "wt-target")
	var out, errb bytes.Buffer
	code := Run(from, []string{"worktree", "remove", rel}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target worktree still present after relative remove from linked worktree: %v", err)
	}
}

func wtHeadCli(t *testing.T, wtPath string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", wtPath, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatalf("symbolic-ref in %s: %v", wtPath, err)
	}
	return strings.TrimSpace(string(out))
}

func TestWorktreeAddBranchUsesExistingBranch(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "feature/have")
	// Config the path template so the destination is deterministic.
	cfgPath := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(cfgPath, []byte("[worktree]\npath_template = \"../wt-cli-<branch>\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--branch", "feature/have"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	wt := filepath.Join(filepath.Dir(dir), "wt-cli-feature-have")
	if got := wtHeadCli(t, wt); got != "feature/have" {
		t.Fatalf("worktree HEAD = %q, want feature/have", got)
	}
	if !strings.Contains(out.String(), "created worktree feature/have") {
		t.Fatalf("stdout: %q", out.String())
	}
}

func TestWorktreeAddBranchRejectsStartPoint(t *testing.T) {
	dir := newRepoDir(t)
	gitRun(t, dir, "branch", "x")
	var out, errb bytes.Buffer
	if code := Run(dir, []string{"worktree", "add", "--branch", "x", "main"}, strings.NewReader(""), &out, &errb, ""); code != 2 {
		t.Fatalf("exit = %d, want 2 (start-point is meaningless with --branch)", code)
	}
}

func TestWorktreeAddBranchMissingBranchFails(t *testing.T) {
	dir := newRepoDir(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--branch", "ghost"}, strings.NewReader(""), &out, &errb, "")
	if code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no local branch") {
		t.Fatalf("stderr: %s", errb.String())
	}
}

func TestWorktreeAddRunsConfiguredHook(t *testing.T) {
	dir := newCLIRepo(t)
	cfgPath := filepath.Join(dir, ".gg.toml")
	// Static path/branch templates (no <user:> labels → no stdin prompting).
	if err := os.WriteFile(cfgPath,
		[]byte("[worktree]\ndefault_branch_template = \"hook-branch\"\npath_template = \"../wt-cli-hook\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--hook", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-hook", "hook-ran")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not run: %v", err)
	}
}

func TestWorktreeAddNoHookFlag(t *testing.T) {
	dir := newCLIRepo(t)
	cfgPath := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(cfgPath,
		[]byte("[worktree]\ndefault_branch_template = \"hook-branch\"\npath_template = \"../wt-cli-nohook\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--no-hook", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-nohook", "hook-ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("--no-hook must skip the hook")
	}
}

// TestWorktreeAddBranchRunsConfiguredHook covers the --branch path of
// CreateWorktreeForBranch: the post-create hook must fire when an existing
// branch is checked out into a new worktree.
func TestWorktreeAddBranchRunsConfiguredHook(t *testing.T) {
	dir := newCLIRepo(t)
	gitRun(t, dir, "branch", "hook-branch")
	cfgPath := filepath.Join(dir, ".gg.toml")
	// Static path template (no <user:> labels) to avoid stdin prompting.
	if err := os.WriteFile(cfgPath,
		[]byte("[worktree]\npath_template = \"../wt-cli-branch-hook\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--hook", "--branch", "hook-branch"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-branch-hook", "hook-ran")
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("hook did not run on --branch path: %v", err)
	}
}

// TestWorktreeAddBranchNoHookFlag covers --no-hook on the --branch path:
// the post-create hook must be suppressed.
func TestWorktreeAddBranchNoHookFlag(t *testing.T) {
	dir := newCLIRepo(t)
	gitRun(t, dir, "branch", "hook-branch")
	cfgPath := filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(cfgPath,
		[]byte("[worktree]\npath_template = \"../wt-cli-branch-nohook\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--branch", "hook-branch", "--no-hook"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d, stderr: %s", code, errb.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-branch-nohook", "hook-ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("--no-hook must skip the hook on the --branch path")
	}
}

// TestWorktreeAddHookSkippedNonInteractiveByDefault asserts that a non-interactive
// invocation (piped/no tty stdin, no --hook/--no-hook flag) skips the configured
// hook rather than running an unseen script silently in a pipeline.
func TestWorktreeAddHookSkippedNonInteractiveByDefault(t *testing.T) {
	dir := newCLIRepo(t)
	cfgPath := filepath.Join(dir, ".gg.toml")
	// Static branch/path templates so no <user:> prompting occurs.
	if err := os.WriteFile(cfgPath,
		[]byte("[worktree]\ndefault_branch_template = \"hook-skip-branch\"\npath_template = \"../wt-cli-default-skip\"\n"),
		0o644); err != nil {
		t.Fatal(err)
	}
	if err := config.SetWorktreePostCreateHook(cfgPath, "touch hook-ran\n"); err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	// No --hook/--no-hook; stdin is non-interactive (empty reader) ⇒ default skip.
	code := Run(dir, []string{"worktree", "add", "main"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	marker := filepath.Join(filepath.Dir(dir), "wt-cli-default-skip", "hook-ran")
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("non-interactive default must skip the hook")
	}
}

// TestWorktreeAddFromDefaultNameCapsShortSha covers the `gg worktree add
// --from <commit>` default-naming lane (no positional branch name, no
// --keep): the branch name is `<current-branch>_<short-sha>`, and the sha
// component must be capped to 7 chars to match the TUI's hash[:7]
// truncation (internal/tui/commit_scope.go), even though git's own %h can
// report a longer abbreviation. core.abbrev is forced high here so the test
// actually exercises the cap — a tiny repo's default abbrev already fits in
// 7 chars, which would pass trivially without the fix. This is the e2e
// harness's `branches` assertion (exact-set only, no regex support) can't
// pin: the default name embeds a commit sha that varies per run.
func TestWorktreeAddFromDefaultNameCapsShortSha(t *testing.T) {
	dir := newCLIRepo(t)
	c := exec.Command("git", "-C", dir, "config", "core.abbrev", "20")
	if out, err := c.CombinedOutput(); err != nil {
		t.Fatalf("git config core.abbrev: %v\n%s", err, out)
	}

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "add", "--from", "HEAD"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}

	re := regexp.MustCompile(`created worktree (\S+) at`)
	m := re.FindStringSubmatch(out.String())
	if m == nil {
		t.Fatalf("could not find created branch name in output:\n%s", out.String())
	}
	branch := m[1]
	shaRe := regexp.MustCompile(`^main_([0-9a-f]+)$`)
	sm := shaRe.FindStringSubmatch(branch)
	if sm == nil {
		t.Fatalf("branch name %q does not match main_<sha>", branch)
	}
	if len(sm[1]) != 7 {
		t.Fatalf("branch name sha suffix = %q (len %d), want 7 chars (capped like the TUI's hash[:7])", sm[1], len(sm[1]))
	}
}

func TestWorktreePruneCommand(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "prune"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "pruned") {
		t.Fatalf("out = %q", out.String())
	}
}

// TestWorktreeMoveToNewPath: `worktree move <old> <new>` relocates the
// worktree; the new path is listed by git afterward and the CLI prints a ✓
// summary line.
func TestWorktreeMoveToNewPath(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/mv1", "wt-cli-mv1")
	dest := filepath.Join(filepath.Dir(dir), "wt-cli-mv1-dest")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "move", wt, dest}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	if !strings.Contains(out.String(), "✓") {
		t.Fatalf("stdout missing ✓ line: %q", out.String())
	}
	listOut, err := exec.Command("git", "-C", dir, "worktree", "list").CombinedOutput()
	if err != nil {
		t.Fatalf("worktree list: %v\n%s", err, listOut)
	}
	if !strings.Contains(string(listOut), dest) {
		t.Fatalf("worktree list missing new path %s:\n%s", dest, listOut)
	}
	// wt is a path-prefix of dest ("...-mv1" vs "...-mv1-dest"), so match the
	// old path as its own listing entry (followed by whitespace before the
	// sha column) rather than a bare substring check.
	if strings.Contains(string(listOut), wt+" ") || strings.Contains(string(listOut), wt+"\t") {
		t.Fatalf("worktree list still shows old path %s:\n%s", wt, listOut)
	}
}

// TestWorktreeRenameByBranch: `worktree rename <branch> <new-name>` resolves
// the target by branch name and renames the directory in place (same parent).
func TestWorktreeRenameByBranch(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/rn1", "wt-cli-rn1")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "rename", "feature/rn1", "wt-cli-rn1-new"}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	want := filepath.Join(filepath.Dir(wt), "wt-cli-rn1-new")
	if _, err := os.Stat(want); err != nil {
		t.Fatalf("renamed worktree not found at %s: %v", want, err)
	}
	if _, err := os.Stat(wt); !os.IsNotExist(err) {
		t.Fatalf("old worktree path %s still present: %v", wt, err)
	}
}

// TestWorktreeRenamePathSeparatorRejected: a rename destination containing a
// path separator is rejected before any move is attempted.
func TestWorktreeRenamePathSeparatorRejected(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "x", "wt-cli-x")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "rename", "x", "new/name"}, strings.NewReader(""), &out, &errb, "")
	if code != 2 {
		t.Fatalf("exit = %d, want 2; stderr=%s", code, errb.String())
	}
	if !strings.Contains(errb.String(), "path separator") {
		t.Fatalf("stderr should explain the path-separator restriction: %s", errb.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should be untouched: %v", err)
	}
}

// TestWorktreeMoveUnknownTarget: an unresolvable target is a non-zero exit
// with a "no worktree at" message, matching `worktree remove`.
func TestWorktreeMoveUnknownTarget(t *testing.T) {
	dir := newCLIRepo(t)
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "move", filepath.Join(dir, "nope"), filepath.Join(dir, "..", "dest")},
		strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("moving an unknown target should be a non-zero exit")
	}
	if !strings.Contains(errb.String(), "no worktree at") {
		t.Fatalf("stderr should explain the unknown target: %s", errb.String())
	}
}

// TestWorktreeMoveLockedNeedsForce: a locked worktree, run non-interactively
// without --force, fails on the missing move-worktree-locked decision.
func TestWorktreeMoveLockedNeedsForce(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/mv-lock", "wt-cli-mv-lock")
	if out, err := exec.Command("git", "-C", dir, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock: %v\n%s", err, out)
	}
	dest := filepath.Join(filepath.Dir(dir), "wt-cli-mv-lock-dest")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "move", wt, dest}, strings.NewReader(""), &out, &errb, "")
	if code == 0 {
		t.Fatal("locked move without --force should fail non-interactively")
	}
	// Pin down that this is specifically the move-worktree-locked decision
	// going unanswered — not some other non-zero-exit failure. cliDecider
	// (internal/cli/core.go) reports this two ways depending on whether
	// os.Stdin reads as interactive: non-interactively it's "<id> needs a
	// decision (options: ...); rerun with the matching flag"; interactively
	// (reads an empty/invalid line from the supplied stdin reader) it's
	// "invalid choice %q for <id>". Both embed the decision ID, which is the
	// stable, distinguishing substring across environments — under `go test`
	// in this sandbox, os.Stdin.Stat() reports ModeCharDevice even when the
	// `go test` invocation's own stdin is redirected from /dev/null or a
	// pipe (verified: the same compiled test binary run directly reports it
	// correctly as non-interactive), so asserting the exact wording would be
	// environment-dependent.
	if !strings.Contains(errb.String(), "move-worktree-locked") {
		t.Fatalf("stderr should report the unanswered move-worktree-locked decision: %s", errb.String())
	}
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree should still exist at old path: %v", err)
	}
}

// TestWorktreeMoveLockedWithForce: --force answers move-worktree-locked with
// unlock-and-move, and the move succeeds.
func TestWorktreeMoveLockedWithForce(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/mv-lock2", "wt-cli-mv-lock2")
	if out, err := exec.Command("git", "-C", dir, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock: %v\n%s", err, out)
	}
	dest := filepath.Join(filepath.Dir(dir), "wt-cli-mv-lock2-dest")

	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "move", "--force", wt, dest}, strings.NewReader(""), &out, &errb, "")
	if code != 0 {
		t.Fatalf("forced move exit = %d, stderr=%s", code, errb.String())
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("worktree not moved after --force: %v", err)
	}
}

// TestWorktreeMoveUpdatesCwdFile: moving the worktree that contains the
// process's current directory writes the moved-equivalent path into
// cwdFile, mirroring the `worktree add` cwd-file contract.
func TestWorktreeMoveUpdatesCwdFile(t *testing.T) {
	dir := newCLIRepo(t)
	wt := addCLIWorktree(t, dir, "feature/mv-cwd", "wt-cli-mv-cwd")
	sub := filepath.Join(wt, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(filepath.Dir(dir), "wt-cli-mv-cwd-dest")

	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(sub); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.Chdir(origCwd) }()

	cwdFile := filepath.Join(t.TempDir(), "cwd")
	var out, errb bytes.Buffer
	code := Run(dir, []string{"worktree", "move", wt, dest}, strings.NewReader(""), &out, &errb, cwdFile)
	if code != 0 {
		t.Fatalf("exit = %d, stderr=%s", code, errb.String())
	}
	got, _ := os.ReadFile(cwdFile)
	want := filepath.Join(dest, "sub")
	if strings.TrimSpace(string(got)) != want {
		t.Fatalf("cwd-file = %q, want %q", strings.TrimSpace(string(got)), want)
	}
}
