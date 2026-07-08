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

// apGitAllowFail runs git in dir like apGitOut, but does NOT fail the test on
// a non-zero exit — for a call expected to fail (e.g. a `git am` that pauses
// on conflict), mirroring conflict_test.go's newConflictRepo `run` helper.
func apGitAllowFail(t *testing.T, dir string, args ...string) (string, error) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
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

// TestApplyPatchWorkingTreeDriftFallbackUnstaged: a plain `git apply` misses
// because local HEAD drifted on a context line near the hunk (not the hunk's
// own line), so the --3way retry resolves it CLEANLY (no conflict markers,
// no unmerged entries) — but --3way implies --index, so without the fix the
// clean resolution would land staged, violating ApplyModeWorkingTree's
// "lands unstaged" contract. Also pins the surgical-unstage property: a
// pre-existing staged change to an UNRELATED file must survive still staged.
func TestApplyPatchWorkingTreeDriftFallbackUnstaged(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "file.txt", "one\ntwo\nthree\nfour\nfive\n", "base")
	sha := writeCommit(t, dir, "file.txt", "one\ntwo\nTHREE\nfour\nfive\n", "change three")
	patchBody := apGitOut(t, dir, "diff", sha+"~1", sha, "--", "file.txt")
	p := filepath.Join(t.TempDir(), "drift.patch")
	if err := os.WriteFile(p, []byte(patchBody+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apGitOut(t, dir, "reset", "--hard", sha+"~1")

	// Local drift: a committed edit to a DIFFERENT line of the same file,
	// close enough to sit inside the hunk's context window. This makes the
	// plain context-matching apply fail (the context text no longer matches
	// verbatim) while --3way's blob-level merge still resolves cleanly,
	// since the local edit and the patch's edit don't overlap.
	writeCommit(t, dir, "file.txt", "ONE\ntwo\nthree\nfour\nfive\n", "local drift on an unrelated line")

	// A pre-existing staged change to a DIFFERENT file must survive the
	// apply still staged — pins that the unstage is scoped to the patch's
	// own paths, not a blanket unstage-everything.
	if err := os.WriteFile(filepath.Join(dir, "other.txt"), []byte("pre-existing staged content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apGitOut(t, dir, "add", "other.txt")

	before := apGitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: p, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply working (drift fallback): %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree mode must not commit")
	}
	want := "ONE\ntwo\nTHREE\nfour\nfive\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "file.txt")); string(got) != want {
		t.Fatalf("file.txt = %q, want %q (both edits present)", got, want)
	}
	if staged := apGitOut(t, dir, "diff", "--cached", "--name-only"); staged != "other.txt" {
		t.Fatalf("staged = %q, want exactly %q (file.txt unstaged, other.txt still staged)", staged, "other.txt")
	}
	if unmerged := apGitOut(t, dir, "diff", "--name-only", "--diff-filter=U"); unmerged != "" {
		t.Fatalf("unmerged = %q, want none (clean 3-way resolution)", unmerged)
	}
}

// TestApplyPatchWorkingTreeDriftFallbackUnstagedMailbox: the same drift
// scenario as TestApplyPatchWorkingTreeDriftFallbackUnstaged, but with a
// format-patch MAILBOX (mailboxFor) instead of a plain diff — the shape
// ApplyModeWorkingTree also accepts (see TestApplyPatchWorkingTreeClean).
// PatchPaths runs `git apply --numstat -z` on whatever file the op was
// given, so this pins that the From/Subject/diffstat mailbox preamble
// doesn't confuse it into reporting zero paths (which would turn a
// successful mailbox apply into a false "applied but left staged" error).
func TestApplyPatchWorkingTreeDriftFallbackUnstagedMailbox(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "file.txt", "one\ntwo\nthree\nfour\nfive\n", "base")
	sha := writeCommit(t, dir, "file.txt", "one\ntwo\nTHREE\nfour\nfive\n", "change three")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", sha+"~1")

	writeCommit(t, dir, "file.txt", "ONE\ntwo\nthree\nfour\nfive\n", "local drift on an unrelated line")
	before := apGitOut(t, dir, "rev-parse", "HEAD")

	res, err := ApplyPatch{Path: patch, Mode: ApplyModeWorkingTree}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatalf("apply working (mailbox drift fallback): %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("working-tree mode must not commit")
	}
	want := "ONE\ntwo\nTHREE\nfour\nfive\n"
	if got, _ := os.ReadFile(filepath.Join(dir, "file.txt")); string(got) != want {
		t.Fatalf("file.txt = %q, want %q (both edits present)", got, want)
	}
	if staged := apGitOut(t, dir, "diff", "--cached", "--name-only"); staged != "" {
		t.Fatalf("staged = %q, want empty (nothing left staged)", staged)
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

// TestApplyPatchRefusesPreExistingAm pins Finding 2: a USER'S own paused raw
// `git am` (started outside gg, e.g. mid-conflict-resolution) must survive an
// ApplyPatch{Mode: ApplyModeCommits} call untouched. Before the fix, the op
// would call AmMailbox (which fails immediately — "previous rebase directory
// still exists"), see AmInProgress true, and run `git am --abort` — silently
// destroying the user's paused am and any partial resolution edits, then
// report "nothing changed". The fix probes AmInProgress BEFORE calling
// AmMailbox and refuses outright, so the rollback logic never touches an am
// it didn't start.
func TestApplyPatchRefusesPreExistingAm(t *testing.T) {
	dir, repo := newRepo(t)
	// Build a linear history: base -> change one.txt (conflicting patch) ->
	// add two.txt (an unrelated, later, DIFFERENT patch).
	writeCommit(t, dir, "one.txt", "one\n", "add one")
	confSha := writeCommit(t, dir, "one.txt", "one\nchanged\n", "change one")
	conflictingMailbox := mailboxFor(t, dir, confSha)
	otherSha := writeCommit(t, dir, "two.txt", "two\n", "add two")
	differentMailbox := mailboxFor(t, dir, otherSha)

	// Reset to base, then diverge so the conflicting mailbox cannot 3-way
	// resolve when raw `git am` is run below.
	apGitOut(t, dir, "reset", "--hard", confSha+"~1")
	writeCommit(t, dir, "one.txt", "one\nCONFLICT\n", "local side")

	// The user pauses a raw `git am --3way` outside gg (expected to fail and
	// leave .git/rebase-apply on disk — apGitAllowFail tolerates the
	// non-zero exit instead of failing the test).
	if _, amErr := apGitAllowFail(t, dir, "am", "--3way", conflictingMailbox); amErr == nil {
		t.Fatal("setup: expected the raw git am to conflict and pause")
	}
	rebaseApplyDir := filepath.Join(dir, ".git", "rebase-apply")
	if _, statErr := os.Stat(rebaseApplyDir); statErr != nil {
		t.Fatalf("setup: expected a paused rebase-apply dir, stat error: %v", statErr)
	}

	_, err := ApplyPatch{Path: differentMailbox, Mode: ApplyModeCommits}.Run(
		context.Background(), OpDeps{Repo: repo})
	if err == nil {
		t.Fatal("ApplyPatch must refuse while a git am is already in progress")
	}
	if _, statErr := os.Stat(rebaseApplyDir); statErr != nil {
		t.Fatalf("the user's paused am must survive untouched, but rebase-apply is gone: %v", statErr)
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

// TestApplyPatchAutoMailboxAbortAnswer: the "abort" answer (what the TUI's
// abortOption maps esc to, since it's the LAST option in
// ApplyModeDecisionID's options list) must cancel — ErrApplyCancelled, HEAD
// unchanged, worktree clean — NOT silently run git am. This pins Finding 1:
// before the fix, "abort" wasn't a recognized option name, esc mapped to the
// (then-)last option "commits", and the modal's esc key ran `git am` instead
// of cancelling.
func TestApplyPatchAutoMailboxAbortAnswer(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")
	before := apGitOut(t, dir, "rev-parse", "HEAD")

	_, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{ApplyModeDecisionID: "abort"}})
	if !errors.Is(err, ErrApplyCancelled) {
		t.Fatalf("err = %v, want ErrApplyCancelled", err)
	}
	if got := apGitOut(t, dir, "rev-parse", "HEAD"); got != before {
		t.Fatal("HEAD must be unchanged after an abort answer")
	}
	if st := apGitOut(t, dir, "status", "--porcelain"); st != "" {
		t.Fatalf("worktree must be clean after an abort answer, got %q", st)
	}
}

// TestApplyPatchAutoMailboxDecisionOptionsIncludeAbort pins the options list
// itself: abortOption (internal/tui) picks the option named "abort" if
// present, so the decision request must offer it explicitly rather than
// relying on it happening to be last.
func TestApplyPatchAutoMailboxDecisionOptionsIncludeAbort(t *testing.T) {
	dir, repo := newRepo(t)
	writeCommit(t, dir, "foo.txt", "one\n", "add foo")
	sha := writeCommit(t, dir, "foo.txt", "one\ntwo\n", "extend foo")
	patch := mailboxFor(t, dir, sha)
	apGitOut(t, dir, "reset", "--hard", "HEAD~1")

	var seen DecisionRequest
	dec := DeciderFunc(func(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
		seen = req
		return DecisionResponse{Option: "abort"}, nil
	})
	if _, err := (ApplyPatch{Path: patch, Mode: ApplyModeAuto}).Run(context.Background(),
		OpDeps{Repo: repo, Decider: dec}); !errors.Is(err, ErrApplyCancelled) {
		t.Fatalf("err = %v, want ErrApplyCancelled", err)
	}
	found := false
	for _, o := range seen.Options {
		if o == "abort" {
			found = true
		}
	}
	if !found {
		t.Fatalf("decision options = %v, want \"abort\" present", seen.Options)
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
