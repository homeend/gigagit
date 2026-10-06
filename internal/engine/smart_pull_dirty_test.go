package engine

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// dirtyPullDeps wires the seams the pull.dirty answers use: RepoAt for a
// worktree's own view, ShelveStaged recording where it was called and what
// the index held then.
type shelveCall struct{ dir, branch, staged string }

func dirtyPullDeps(t *testing.T, repo *git.Repo, answer string, ch chan Event) (OpDeps, *shelveCall) {
	t.Helper()
	call := &shelveCall{}
	deps := OpDeps{
		Repo:    repo,
		RepoAt:  func(d string) GitOps { return repo.InDir(d) },
		Events:  ch,
		Decider: MapDecider{PullDirtyDecisionID: answer},
		ShelveStaged: func(ctx context.Context, dir, branch string) (model.ShelfEntry, error) {
			call.dir, call.branch = dir, branch
			call.staged = gitOut(t, dir, "diff", "--cached", "--name-only")
			return model.ShelfEntry{ID: "e1", Label: "WIP on " + branch}, nil
		},
	}
	return deps, call
}

func dirtyDecision(t *testing.T, ch chan Event) *DecisionRequest {
	t.Helper()
	close(ch)
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == PullDirtyDecisionID {
			return &d.Request
		}
	}
	return nil
}

// An edit to a file the pull changes: git refuses, the question lists the
// worktree's changes, discard clears them and the pull runs again.
func TestSmartPullDirtyDiscardRetries(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644)
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "discard", ch)
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	req := dirtyDecision(t, ch)
	if req == nil {
		t.Fatal("expected the pull.dirty decision")
	}
	if strings.Join(req.Options, ",") != "shelve,discard,abort" {
		t.Fatalf("options = %v", req.Options)
	}
	if !strings.HasSuffix(req.Prompt, ":\n\n.M f.txt") {
		t.Fatalf("prompt = %q", req.Prompt)
	}
	if !res.Changed || res.Summary != "pulled main; changes discarded" {
		t.Fatalf("result = %+v", res)
	}
	if got := readFile(t, filepath.Join(clone, "f.txt")); got != "v2\n" {
		t.Fatalf("f.txt = %q, want the pulled v2", got)
	}
}

// Shelve stores every change (untracked too) through the seam, then pulls.
func TestSmartPullDirtyShelve(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644)
	os.WriteFile(filepath.Join(clone, "other.txt"), []byte("o\n"), 0o644)
	ch := make(chan Event, 64)
	deps, call := dirtyPullDeps(t, repo, "shelve", ch)
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !samePath(call.dir, clone) || call.branch != "main" {
		t.Fatalf("seam got (%q, %q), want (%q, main)", call.dir, call.branch, clone)
	}
	if !strings.Contains(call.staged, "f.txt") || !strings.Contains(call.staged, "other.txt") {
		t.Fatalf("the index at the seam call must hold every change, got:\n%s", call.staged)
	}
	if !res.Changed || res.Summary != `pulled main; shelved as "WIP on main"` {
		t.Fatalf("result = %+v", res)
	}
	if got := gitOut(t, clone, "status", "--porcelain"); got != "" {
		t.Fatalf("worktree not clean after shelve + pull:\n%s", got)
	}
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not pulled")
	}
}

// Abort touches nothing: no error, the edit stays, the branch stays.
func TestSmartPullDirtyAbort(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644)
	before := revAt(t, clone, "main")
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "abort", ch)
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("abort must not be an error: %v", err)
	}
	if res.Changed || res.Summary != "pull cancelled" {
		t.Fatalf("result = %+v", res)
	}
	if got := readFile(t, filepath.Join(clone, "f.txt")); got != "local\n" {
		t.Fatalf("f.txt = %q, the edit must survive an abort", got)
	}
	if revAt(t, clone, "main") != before {
		t.Fatal("main moved on abort")
	}
	if dirtyDecision(t, ch) == nil {
		t.Fatal("expected the pull.dirty decision")
	}
}

// Dirt the pull does not touch never asks: git pulls over it.
func TestSmartPullUnrelatedDirtNoPrompt(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "other.txt"), []byte("o\n"), 0o644)
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "discard", ch)
	if _, err := (SmartPull{Intent: PullAndStay}).Run(context.Background(), deps); err != nil {
		t.Fatalf("pull: %v", err)
	}
	if dirtyDecision(t, ch) != nil {
		t.Fatal("dirt the pull does not touch must not prompt")
	}
	if got := readFile(t, filepath.Join(clone, "other.txt")); got != "o\n" {
		t.Fatalf("other.txt = %q, untouched dirt must stay", got)
	}
}

// worktreeBehindWithIncomingFile: dev is checked out in its own worktree and
// origin/dev adds n.txt; the worktree holds an untracked n.txt of its own.
func worktreeBehindWithIncomingFile(t *testing.T) (repo *git.Repo, wtPath string) {
	t.Helper()
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")
	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev1")
	gitAt(t, seed, "push", "-u", "origin", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")
	wtPath = filepath.Join(root, "wt-dev")
	gitAt(t, clone, "worktree", "add", wtPath, "dev")
	os.WriteFile(filepath.Join(seed, "n.txt"), []byte("remote\n"), 0o644)
	gitAt(t, seed, "add", "n.txt")
	gitAt(t, seed, "commit", "-m", "dev2")
	gitAt(t, seed, "push", "origin", "dev")
	os.WriteFile(filepath.Join(wtPath, "n.txt"), []byte("mine\n"), 0o644)
	return repo, wtPath
}

// The other worktree's untracked file blocks the pull there: the question
// lists THAT worktree's files and the answer acts on it.
func TestSmartPullWorktreeDirtyDiscard(t *testing.T) {
	t.Parallel()
	repo, wtPath := worktreeBehindWithIncomingFile(t)
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "discard", ch)
	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	req := dirtyDecision(t, ch)
	if req == nil {
		t.Fatal("expected the pull.dirty decision")
	}
	if !strings.HasSuffix(req.Prompt, ":\n\n?? n.txt") || !strings.Contains(req.Prompt, wtPath) {
		t.Fatalf("prompt = %q", req.Prompt)
	}
	if !res.Changed || !strings.HasSuffix(res.Summary, "; changes discarded") {
		t.Fatalf("result = %+v", res)
	}
	if got := readFile(t, filepath.Join(wtPath, "n.txt")); got != "remote\n" {
		t.Fatalf("n.txt = %q, want the pulled file", got)
	}
}

func TestSmartPullWorktreeDirtyShelve(t *testing.T) {
	t.Parallel()
	repo, wtPath := worktreeBehindWithIncomingFile(t)
	ch := make(chan Event, 64)
	deps, call := dirtyPullDeps(t, repo, "shelve", ch)
	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if !samePath(call.dir, wtPath) || call.branch != "dev" || call.staged != "n.txt" {
		t.Fatalf("seam got %+v, want (%q, dev, n.txt)", *call, wtPath)
	}
	if !res.Changed || !strings.HasSuffix(res.Summary, `; shelved as "WIP on dev"`) {
		t.Fatalf("result = %+v", res)
	}
}

func TestSmartPullWorktreeDirtyAbort(t *testing.T) {
	t.Parallel()
	repo, wtPath := worktreeBehindWithIncomingFile(t)
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "abort", ch)
	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("abort must not be an error: %v", err)
	}
	if res.Changed || res.Summary != "pull cancelled" {
		t.Fatalf("result = %+v", res)
	}
	if got := readFile(t, filepath.Join(wtPath, "n.txt")); got != "mine\n" {
		t.Fatalf("n.txt = %q, must survive an abort", got)
	}
}

// A diverged branch answered "rebase" meets the dirt next (a rebase pull
// refuses any dirty tree): the same question clears it, then the rebase runs.
func TestSmartPullDivergedRebaseAsksAboutDirt(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("edited\n"), 0o644)
	ch := make(chan Event, 64)
	deps, call := dirtyPullDeps(t, repo, "shelve", ch)
	deps.Decider = MapDecider{"non-fast-forward": "rebase", PullDirtyDecisionID: "shelve"}
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), deps)
	if err != nil {
		t.Fatalf("pull: %v", err)
	}
	if call.staged != "local.txt" {
		t.Fatalf("shelved %q, want local.txt", call.staged)
	}
	if !res.Changed || res.Summary != `pulled (rebased) main; shelved as "WIP on main"` {
		t.Fatalf("result = %+v", res)
	}
	if got := readFile(t, filepath.Join(clone, "f.txt")); got != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (rebased onto the remote)", got)
	}
}

// An unanswered pull.dirty (a CLI with no policy) is the pull's failure: it
// must not go on to ask the diverged question about a branch that is merely
// behind.
func TestSmartPullDirtyUnansweredStops(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("local\n"), 0o644)
	ch := make(chan Event, 64)
	deps, _ := dirtyPullDeps(t, repo, "", ch)
	deps.Decider = MapDecider{}
	_, err := SmartPull{Intent: PullAndStay}.Run(context.Background(), deps)
	if !errors.Is(err, ErrDecisionRequired) {
		t.Fatalf("err = %v, want ErrDecisionRequired", err)
	}
	close(ch)
	for _, e := range drain(ch) {
		if d, ok := e.(DecisionNeeded); ok && d.Request.ID == "non-fast-forward" {
			t.Fatal("an unanswered pull.dirty must not fall through to the diverged question")
		}
	}
}
