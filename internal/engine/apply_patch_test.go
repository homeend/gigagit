package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// apGitOut runs git in dir and returns trimmed stdout (test helper). Named
// apGitOut, not gitOut, to avoid colliding with smart_merge_test.go's
// package-level gitOut (same package, different env: this one sets
// author/committer env so it can create commits).
func apGitOut(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// writeCommit writes content to name and commits it, returning the new HEAD sha.
func writeCommit(t *testing.T, dir, name, content, msg string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	apGitOut(t, dir, "add", name)
	apGitOut(t, dir, "commit", "-m", msg)
	return apGitOut(t, dir, "rev-parse", "HEAD")
}

// mailboxFor exports rev as a format-patch mailbox file and returns its path.
func mailboxFor(t *testing.T, dir, rev string) string {
	t.Helper()
	data := apGitOut(t, dir, "format-patch", "-1", "--binary", "--stdout", rev)
	p := filepath.Join(t.TempDir(), "x.patch")
	if err := os.WriteFile(p, []byte(data+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestApplyPatchCommitsRoundTrip: export a commit, rewind, re-apply with
// ApplyModeCommits — the commit is recreated with its message preserved.
func TestApplyPatchCommitsRoundTrip(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply --am: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if subj := apGitOut(t, dir, "log", "-1", "--format=%s"); subj != "extend foo" {
		t.Fatalf("HEAD subject = %q, want the recreated commit", subj)
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "foo.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("foo.txt = %q", got)
	}
}

// TestApplyPatchWorkingTreeClean: the same patch in working-tree mode lands
// unstaged, no new commit.
func TestApplyPatchWorkingTreeClean(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")
	before := apGitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply working: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree mode must not commit")
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "foo.txt")); string(got) != "one\ntwo\n" {
		t.Fatalf("foo.txt = %q", got)
	}
	if staged := apGitOut(t, dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("nothing should be staged, got %q", staged)
	}
}

// TestApplyPatchAmConflictAtomic: a conflicting mailbox in Commits mode rolls
// back completely — HEAD unchanged, no rebase-apply left, worktree clean.
func TestApplyPatchAmConflictAtomic(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "base\n", "base")
	sha := writeCommit(t, dir, "foo.txt", "patched\n", "patch side")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")
	// Diverge so the patch conflicts and 3-way cannot resolve it.
	writeCommit(t, dir, "foo.txt", "conflicting local\n", "local side")
	before := apGitOut(t, dir, "rev-parse", "HEAD")

	_, err := ApplyPatch{Path: patch, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("conflicting am should error")
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD must be unchanged after am rollback")
	}
	if _, statErr := os.Stat(filepath.Join(dir, ".git", "rebase-apply")); statErr == nil {
		t.Fatal("rebase-apply must be cleaned up (am --abort ran)")
	}
	if st := apGitOut(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree must be clean after rollback, got %q", st)
	}
}

// TestApplyPatchWorkingTreeConflict: working-tree mode on a conflicting patch
// leaves 3-way conflict markers + unmerged entries, returns Result{Changed}
// AND an error (the SmartMerge keep-conflicts shape); HEAD unchanged.
func TestApplyPatchWorkingTreeConflict(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "base\n", "base")
	sha := writeCommit(t, dir, "foo.txt", "patched\n", "patch side")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")
	writeCommit(t, dir, "foo.txt", "conflicting local\n", "local side")
	before := apGitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("conflicting apply should error (keep-conflicts shape)")
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed:true alongside the error", res)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD must be unchanged")
	}
	if unmerged := apGitOut(t, dir, "diff", "--name-only", "--diff-filter=U"); unmerged != "foo.txt" {
		t.Fatalf("unmerged = %q, want foo.txt", unmerged)
	}
	content, _ := os.ReadFile(filepath.Join(dir, "foo.txt"))
	if !strings.Contains(string(content), "<<<<<<<") {
		t.Fatalf("expected conflict markers, got %q", content)
	}
}

// TestApplyPatchAutoMailboxDecision: Auto + mailbox forks via
// apply_patch.mode; each answer routes to its mode; an unknown answer cancels.
func TestApplyPatchAutoMailboxDecision(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")

	// commits answer → real commit recreated
	if _, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "commits"}}); err != nil {
		t.Fatalf("auto→commits: %v", err)
	}
	if subj := apGitOut(t, dir, "log", "-1", "--format=%s"); subj != "extend foo" {
		t.Fatalf("HEAD subject = %q", subj)
	}

	// working-tree answer → no commit
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")
	before := apGitOut(t, dir, "rev-parse", "HEAD")
	if _, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "working-tree"}}); err != nil {
		t.Fatalf("auto→working-tree: %v", err)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree answer must not commit")
	}

	// unknown answer → cancelled, nothing ran
	apGitOut(t, dir, "reset", "--hard", "HEAD")
	apGitOut(t, dir, "checkout", "--", ".")
	_, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "bogus"}})
	if !errors.Is(err, ErrApplyCancelled) {
		t.Fatalf("err = %v, want ErrApplyCancelled", err)
	}
}

// TestApplyPatchAutoPlainDiffNoDecision: Auto + a plain diff applies to the
// working tree WITHOUT consulting the decider (no Decider in deps — a
// decision attempt would fail the op).
func TestApplyPatchAutoPlainDiffNoDecision(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	diff := apGitOut(t, dir, "diff", "HEAD~1", "HEAD")
	p := filepath.Join(t.TempDir(), "plain.diff")
	os.WriteFile(p, []byte(diff+"\n"), 0o644)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")

	res, err := ApplyPatch{Path: p, Mode: ApplyModeAuto}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("auto plain diff: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
}

// TestApplyPatchCommitsOnPlainDiff: --am semantics on a bare diff is a typed
// refusal (git am has no author/message to work with).
func TestApplyPatchCommitsOnPlainDiff(t *testing.T) {
	_, repo := newRepo(t)
	p := filepath.Join(t.TempDir(), "plain.diff")
	os.WriteFile(p, []byte("diff --git a/x b/x\n"), 0o644)
	_, err := ApplyPatch{Path: p, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if !errors.Is(err, ErrNotMailbox) {
		t.Fatalf("err = %v, want ErrNotMailbox", err)
	}
}

// TestApplyPatchMissingFile: a bad path errors before any git runs.
func TestApplyPatchMissingFile(t *testing.T) {
	_, repo := newRepo(t)
	_, err := ApplyPatch{Path: "/nonexistent/x.patch", Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("missing file should error")
	}
}
