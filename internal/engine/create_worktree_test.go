package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// hasProgress reports whether a Progress event was emitted.
func hasProgress(events []Event) bool {
	for _, e := range events {
		if _, ok := e.(Progress); ok {
			return true
		}
	}
	return false
}

func TestCreateWorktreeRelativePathSucceeds(t *testing.T) {
	dir, repo := newRepo(t)
	ch := make(chan Event, 32)

	// "../wt-rel" is resolved against the repo top-level (dir), i.e. a sibling.
	op := CreateWorktree{StartPoint: "main", Branch: "feature/rel", Path: "../wt-rel"}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo, Events: ch})
	close(ch)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	wantPath := filepath.Clean(filepath.Join(dir, "..", "wt-rel"))
	if _, statErr := os.Stat(filepath.Join(wantPath, "README.md")); statErr != nil {
		t.Fatalf("worktree not created at %s: %v", wantPath, statErr)
	}
	if !hasProgress(drain(ch)) {
		t.Error("expected a Progress event")
	}
}

// When gg runs from inside a (possibly deeply-nested) linked worktree, a
// relative Path must still anchor on the MAIN worktree — not the current one —
// otherwise the new worktree nests under the current worktree (the real bug:
// a doubled ".worktrees" segment in the resolved path).
func TestCreateWorktreeRelativePathAnchorsOnMainWorktree(t *testing.T) {
	dir, repo := newRepo(t)

	// A linked worktree nested two levels below main, mirroring the field report.
	linkedPath := filepath.Join(dir, "nested", "wt-a")
	if err := repo.AddWorktree(context.Background(), linkedPath, "feature/a", "main", nil); err != nil {
		t.Fatalf("AddWorktree: %v", err)
	}
	// A repo rooted at the linked worktree — as if the user invoked gg there.
	linkedRepo := &git.Repo{Runner: gitexec.NewExecRunner("git", linkedPath, observ.NewRing(50))}

	op := CreateWorktree{StartPoint: "main", Branch: "feature/b", Path: "../wt-b"}
	res, err := op.Run(context.Background(), OpDeps{Repo: linkedRepo})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	wantMain := filepath.Clean(filepath.Join(dir, "..", "wt-b"))           // main-anchored (correct)
	wrongNested := filepath.Clean(filepath.Join(linkedPath, "..", "wt-b")) // current-worktree-anchored (the bug)
	if res.Path != wantMain {
		t.Fatalf("Result.Path = %q, want main-anchored %q (nested-anchored would be %q)", res.Path, wantMain, wrongNested)
	}
	if _, statErr := os.Stat(filepath.Join(wantMain, "README.md")); statErr != nil {
		t.Fatalf("worktree not created at main-anchored path %s: %v", wantMain, statErr)
	}
}

func TestCreateWorktreeInvalidBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	op := CreateWorktree{StartPoint: "main", Branch: "bad..name", Path: "../wt-bad"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "branch") {
		t.Fatalf("want invalid-branch error, got %v", err)
	}
}

func TestCreateWorktreeExistingPathErrors(t *testing.T) {
	dir, repo := newRepo(t)
	existing := filepath.Join(filepath.Dir(dir), "wt-exists")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "feature/x", Path: existing}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil || !strings.Contains(err.Error(), "exists") {
		t.Fatalf("want path-exists error, got %v", err)
	}
}

func TestCreateWorktreeDuplicateBranchErrors(t *testing.T) {
	_, repo := newRepo(t)
	if err := repo.CreateBranch(context.Background(), "dup", ""); err != nil {
		t.Fatal(err)
	}
	op := CreateWorktree{StartPoint: "main", Branch: "dup", Path: "../wt-dup"}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("creating a worktree on an existing branch should error")
	}
}

func TestCreateWorktreeMissingFieldsError(t *testing.T) {
	_, repo := newRepo(t)
	_, err := CreateWorktree{Branch: "x", Path: "../p"}.Run(context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("missing StartPoint should error")
	}
}

func TestCreateWorktreeResultCarriesAbsolutePath(t *testing.T) {
	dir, repo := newRepo(t)
	res, err := CreateWorktree{StartPoint: "main", Branch: "feature/p", Path: "../wt-p"}.
		Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}
	want := filepath.Clean(filepath.Join(dir, "..", "wt-p"))
	if res.Path != want {
		t.Fatalf("Result.Path = %q, want %q", res.Path, want)
	}
}

type fakeHookRunner struct {
	called bool
	spec   HookSpec
	lines  []string
	code   int
	err    error
}

func (h *fakeHookRunner) Run(_ context.Context, spec HookSpec, onLine func(string)) (int, error) {
	h.called = true
	h.spec = spec
	for _, l := range h.lines {
		onLine(l)
	}
	return h.code, h.err
}

func hookEnv(spec HookSpec, key string) string {
	for _, kv := range spec.Env {
		if strings.HasPrefix(kv, key+"=") {
			return kv[len(key)+1:]
		}
	}
	return ""
}

func TestCreateWorktreeRunsHook(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-hook")
	fh := &fakeHookRunner{lines: []string{"copied .env"}}
	ch := make(chan Event, 64)
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/h", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
	close(ch)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !fh.called {
		t.Fatal("hook not called")
	}
	if fh.spec.Dir != res.Path {
		t.Fatalf("hook Dir = %q, want %q", fh.spec.Dir, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_WORKTREE_PATH"); got != res.Path {
		t.Fatalf("GG_WORKTREE_PATH = %q, want %q", got, res.Path)
	}
	if got := hookEnv(fh.spec, "GG_BRANCH"); got != "f/h" {
		t.Fatalf("GG_BRANCH = %q, want f/h", got)
	}
	if hookEnv(fh.spec, "GG_MAIN_WORKTREE") == "" {
		t.Fatal("GG_MAIN_WORKTREE unset")
	}
	var sawLine bool
	for _, e := range drain(ch) {
		if g, ok := e.(GitLine); ok && g.Raw == "copied .env" {
			sawLine = true
		}
	}
	if !sawLine {
		t.Fatal("hook output not streamed as GitLine")
	}
}

func TestCreateWorktreeEmptyHookSkips(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-nohook")
	fh := &fakeHookRunner{}
	_, err := CreateWorktree{StartPoint: "main", Branch: "f/n", Path: wt}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh})
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("empty hook must not run")
	}
}

func TestCreateWorktreeHookFailureNonFatal(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-failhook")
	fh := &fakeHookRunner{code: 1}
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/f", Path: wt, PostCreateHook: "false"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh, Decider: MapDecider{HookDecisionID: "run"}})
	if err != nil {
		t.Fatalf("hook failure must not fail the op: %v", err)
	}
	if !res.Changed {
		t.Fatal("worktree should still count as created")
	}
	if !strings.Contains(res.Summary, "exit 1") {
		t.Fatalf("Summary should note hook failure, got %q", res.Summary)
	}
}

func TestCreateWorktreeHookSkippedWithoutApproval(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-skip")
	fh := &fakeHookRunner{}
	ch := make(chan Event, 64)
	res, err := CreateWorktree{StartPoint: "main", Branch: "f/s", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, Events: ch, HookRunner: fh, Decider: MapDecider{HookDecisionID: "skip"}})
	close(ch)
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("hook must not run when not approved")
	}
	if !res.Changed {
		t.Fatal("worktree should still be created")
	}
	var sawSkip bool
	for _, e := range drain(ch) {
		if g, ok := e.(GitLine); ok && g.Raw == "post-create hook skipped" {
			sawSkip = true
		}
	}
	if !sawSkip {
		t.Fatal("expected a 'post-create hook skipped' line")
	}
}

func TestCreateWorktreeHookSkippedWithNoDecider(t *testing.T) {
	dir, repo := newRepo(t)
	wt := filepath.Join(filepath.Dir(dir), "wt-nodecider")
	fh := &fakeHookRunner{}
	_, err := CreateWorktree{StartPoint: "main", Branch: "f/nd", Path: wt, PostCreateHook: "echo hi"}.Run(
		context.Background(), OpDeps{Repo: repo, HookRunner: fh}) // no Decider ⇒ safe skip
	if err != nil {
		t.Fatal(err)
	}
	if fh.called {
		t.Fatal("hook must not run when no decider can approve")
	}
}

// addCommit writes path=content and commits it in dir, returning nothing; the
// keep-mode tests need a second commit so HEAD has exactly one parent.
func addCommit(t *testing.T, dir, path, content, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("add", path)
	run("commit", "-m", msg)
}

// gitOut (already declared in smart_merge_test.go) runs git in dir and
// returns trimmed stdout.

func TestCreateWorktreeKeepStaged(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	parent := gitOut(t, dir, "rev-parse", "HEAD^")

	op := CreateWorktree{StartPoint: head, Branch: "redo/x", Path: "../wt-keep-staged", Keep: KeepStaged}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree keep-staged: %v", err)
	}
	// The new branch must land on the PARENT, with the commit's diff staged.
	if got := gitOut(t, res.Path, "rev-parse", "HEAD"); got != parent {
		t.Fatalf("worktree HEAD = %s, want parent %s", got, parent)
	}
	status := gitOut(t, res.Path, "status", "--porcelain")
	if !strings.Contains(status, "A  a.txt") && !strings.Contains(status, "M  a.txt") {
		t.Fatalf("a.txt not staged in the new worktree; status:\n%s", status)
	}
	if !strings.Contains(res.Summary, "commit's changes staged") {
		t.Fatalf("Summary = %q, want the staged suffix", res.Summary)
	}
}

func TestCreateWorktreeKeepUnstaged(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")
	parent := gitOut(t, dir, "rev-parse", "HEAD^")

	op := CreateWorktree{StartPoint: head, Branch: "redo/y", Path: "../wt-keep-unstaged", Keep: KeepUnstaged}
	res, err := op.Run(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("CreateWorktree keep-unstaged: %v", err)
	}
	if got := gitOut(t, res.Path, "rev-parse", "HEAD"); got != parent {
		t.Fatalf("worktree HEAD = %s, want parent %s", got, parent)
	}
	// --mixed: nothing staged, a.txt untracked-or-modified in the working tree.
	status := gitOut(t, res.Path, "status", "--porcelain")
	if strings.Contains(status, "A  a.txt") || strings.Contains(status, "M  a.txt") {
		t.Fatalf("a.txt unexpectedly staged; status:\n%s", status)
	}
	if !strings.Contains(status, "a.txt") {
		t.Fatalf("a.txt missing from the new worktree's status:\n%s", status)
	}
}

func TestCreateWorktreeKeepRefusesRootCommit(t *testing.T) {
	dir, repo := newRepo(t) // exactly one commit — HEAD is the root
	op := CreateWorktree{StartPoint: "HEAD", Branch: "redo/root", Path: "../wt-keep-root", Keep: KeepStaged}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	var wantErr WorktreeKeepParentError
	if !errors.As(err, &wantErr) || wantErr.Parents != 0 {
		t.Fatalf("err = %v, want WorktreeKeepParentError{Parents: 0}", err)
	}
	// Refusal happens BEFORE anything is created.
	if _, statErr := os.Stat(filepath.Join(dir, "..", "wt-keep-root")); statErr == nil {
		t.Fatal("worktree directory must not exist after the refusal")
	}
	if out := gitOut(t, dir, "branch", "--list", "redo/root"); out != "" {
		t.Fatalf("branch must not exist after the refusal, got %q", out)
	}
}

func TestCreateWorktreeKeepRefusesMergeCommit(t *testing.T) {
	dir, repo := newRepo(t)
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "-b", "side")
	addCommit(t, dir, "s.txt", "side\n", "side work")
	run("checkout", "main")
	addCommit(t, dir, "m.txt", "main\n", "main work")
	run("merge", "--no-ff", "-m", "merge side", "side")

	op := CreateWorktree{StartPoint: "HEAD", Branch: "redo/merge", Path: "../wt-keep-merge", Keep: KeepUnstaged}
	_, err := op.Run(context.Background(), OpDeps{Repo: repo})
	var wantErr WorktreeKeepParentError
	if !errors.As(err, &wantErr) || wantErr.Parents != 2 {
		t.Fatalf("err = %v, want WorktreeKeepParentError{Parents: 2}", err)
	}
}

func TestCreateWorktreeKeepHookSeesResetState(t *testing.T) {
	dir, repo := newRepo(t)
	addCommit(t, dir, "a.txt", "two\n", "second")
	head := gitOut(t, dir, "rev-parse", "HEAD")

	// The hook snapshots status at hook time; if it ran before the reset the
	// tree would be clean and the file would be empty.
	op := CreateWorktree{StartPoint: head, Branch: "redo/hook", Path: "../wt-keep-hook",
		Keep: KeepStaged, PostCreateHook: "git status --porcelain > hook-status.txt"}
	res, err := op.Run(context.Background(), opDepsApprovingHook(repo)) // same deps shape TestCreateWorktreeForBranchRunsHook uses
	if err != nil {
		t.Fatalf("CreateWorktree keep+hook: %v", err)
	}
	out, rerr := os.ReadFile(filepath.Join(res.Path, "hook-status.txt"))
	if rerr != nil {
		t.Fatalf("hook did not run: %v", rerr)
	}
	if !strings.Contains(string(out), "a.txt") {
		t.Fatalf("hook ran before the reset — status at hook time:\n%s", out)
	}
}
