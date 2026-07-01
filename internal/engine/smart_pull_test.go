package engine

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/repogate"
)

func gitAt(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func revAt(t *testing.T, dir, ref string) string {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", ref)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("rev-parse %s: %v", ref, err)
	}
	return string(out)
}

func cloneOnMainBehindOrigin(t *testing.T) (string, *git.Repo) {
	t.Helper()
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	seed := filepath.Join(root, "seed")
	clone := filepath.Join(root, "clone")

	gitAt(t, root, "init", "--bare", origin)
	gitAt(t, root, "clone", origin, seed)
	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v1\n"), 0o644)
	gitAt(t, seed, "checkout", "-b", "main")
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v1")
	gitAt(t, seed, "push", "-u", "origin", "main")

	gitAt(t, root, "clone", origin, clone)
	gitAt(t, clone, "checkout", "main")

	os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
	gitAt(t, seed, "add", ".")
	gitAt(t, seed, "commit", "-m", "v2")
	gitAt(t, seed, "push", "origin", "main")

	repo := &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
	return clone, repo
}

func TestSmartPullCurrentBranchFastForward(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not fast-forwarded to origin/main")
	}
}

func TestSmartPullCurrentBranchNonFastForwardRebase(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"non-fast-forward": "rebase"}})
	if err != nil {
		t.Fatalf("smart pull (rebase): %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err != nil {
		t.Fatalf("local.txt missing after rebase: %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote change applied)", b)
	}
}

// Answering the non-fast-forward decision "reset" hard-resets the current branch
// to the fetched remote tip: the local commit is discarded, the remote content
// wins, and an uncommitted local edit is thrown away too.
func TestSmartPullCurrentBranchNonFastForwardReset(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	os.WriteFile(filepath.Join(clone, "local.txt"), []byte("local\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "local")
	// An uncommitted edit on top: reset --hard must discard it as well.
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("dirty\n"), 0o644)

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{"non-fast-forward": "reset"}})
	if err != nil {
		t.Fatalf("smart pull (reset): %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not reset to origin/main")
	}
	if _, err := os.Stat(filepath.Join(clone, "local.txt")); err == nil {
		t.Fatal("local.txt survived reset --hard (local commit not discarded)")
	}
	if b, _ := os.ReadFile(filepath.Join(clone, "f.txt")); string(b) != "v2\n" {
		t.Fatalf("f.txt = %q, want v2 (remote content, dirty edit discarded)", b)
	}
}

func TestSmartPullBackgroundFastForwardsOtherBranch(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/main")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev-advance")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull background: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "main" {
		t.Fatalf("current branch = %q, want main (background must not checkout)", cur)
	}
}

// A background pull of a branch checked out in ANOTHER worktree must pull in that
// worktree directly — no "cannot fast-forward in the background" prompt (git
// refuses to fetch into a checked-out branch's ref, so the ff-ref path can't be
// the first thing tried). MapDecider{} errors on any decision, so a prompt fails
// the test.
func TestSmartPullBackgroundPullsWorktreeBranchNoPrompt(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev1")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")
	wtPath := filepath.Join(root, "wt-dev")
	gitAt(t, clone, "worktree", "add", wtPath, "dev")

	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev2")
	gitAt(t, seed, "push", "origin", "dev")

	res, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("background pull of a worktree branch must not prompt/err: %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want Changed", res)
	}
	gitAt(t, clone, "fetch", "origin") // refresh the clone's origin/dev view
	if revAt(t, wtPath, "HEAD") != revAt(t, clone, "origin/dev") {
		t.Fatal("dev was not fast-forwarded in its worktree")
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur != "main" {
		t.Fatalf("current branch = %q, want main (no checkout)", cur)
	}
}

// A diverged worktree branch must NOT be silently merged: PullInWorktree is
// ff-only, so a background pull errors and leaves the worktree branch untouched
// (the user resolves it deliberately, like the same-branch ff-only-then-ask path).
func TestSmartPullBackgroundWorktreeDivergedErrsNoMerge(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev1")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/dev")
	wtPath := filepath.Join(root, "wt-dev")
	gitAt(t, clone, "worktree", "add", wtPath, "dev")

	// Diverge: a local-only commit in the worktree AND a different commit upstream.
	gitAt(t, wtPath, "commit", "--allow-empty", "-m", "local-dev")
	local := revAt(t, wtPath, "HEAD")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "dev2")
	gitAt(t, seed, "push", "origin", "dev")

	_, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil {
		t.Fatal("a diverged worktree background pull must error (ff-only), not merge silently")
	}
	if revAt(t, wtPath, "HEAD") != local {
		t.Fatal("diverged worktree branch was moved — ff-only must leave it untouched")
	}
}

func TestSmartPullStayStashesAndMovesToTarget(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	gitAt(t, clone, "branch", "feature", "origin/main")
	gitAt(t, clone, "push", "-u", "origin", "feature")
	os.WriteFile(filepath.Join(clone, "f.txt"), []byte("dirty-local\n"), 0o644)

	res, err := SmartPull{Branch: "feature", Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err != nil {
		t.Fatalf("smart pull stay: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v", res)
	}
	cur, _ := repo.CurrentBranch(context.Background())
	if cur != "feature" {
		t.Fatalf("current branch = %q, want feature (PullAndStay ends on target)", cur)
	}
	dirty, _ := repo.IsDirty(context.Background())
	if !dirty {
		t.Fatal("expected dirty change restored on feature")
	}
}

func TestSmartPullLockMode(t *testing.T) {
	if got := (SmartPull{Intent: PullInBackground}).LockMode(); got != repogate.RefWrite {
		t.Fatalf("background lock mode = %v, want RefWrite", got)
	}
	if got := (SmartPull{Intent: PullAndStay}).LockMode(); got != repogate.TreeWrite {
		t.Fatalf("stay lock mode = %v, want TreeWrite", got)
	}
	if got := (SmartPull{}).LockMode(); got != repogate.TreeWrite {
		t.Fatalf("default lock mode = %v, want TreeWrite", got)
	}
}

// TestSmartPullBackgroundEscalatesBeforeCheckout proves the escalation hook
// fires BEFORE checkoutPull touches the worktree: a failing Escalate must
// abort the operation with the current branch untouched.
func TestSmartPullBackgroundEscalatesBeforeCheckout(t *testing.T) {
	clone, repo := cloneOnMainBehindOrigin(t)
	root := filepath.Dir(clone)
	seed := filepath.Join(root, "seed")

	// Make dev diverge so FastForwardRef cannot succeed: local dev has its
	// own commit, origin/dev advanced separately.
	gitAt(t, clone, "fetch", "origin")
	gitAt(t, clone, "branch", "dev", "origin/main")
	gitAt(t, clone, "switch", "dev")
	gitAt(t, clone, "commit", "--allow-empty", "-m", "local-dev")
	gitAt(t, clone, "switch", "main")
	gitAt(t, seed, "checkout", "-b", "dev")
	gitAt(t, seed, "commit", "--allow-empty", "-m", "origin-dev")
	gitAt(t, seed, "push", "-u", "origin", "dev")

	escErr := errors.New("escalation denied")
	called := false
	_, err := SmartPull{Branch: "dev", Intent: PullInBackground}.Run(context.Background(), OpDeps{
		Repo:    repo,
		Decider: MapDecider{"not-fast-forwardable": "checkout-and-resolve"},
		Escalate: func(context.Context) error {
			called = true
			return escErr
		},
	})
	if !called {
		t.Fatal("Escalate was never called on the checkout-and-resolve path")
	}
	if !errors.Is(err, escErr) {
		t.Fatalf("err = %v, want the escalation error", err)
	}
	if cur, _ := repo.CurrentBranch(context.Background()); cur != "main" {
		t.Fatalf("current branch = %q, want main (must not checkout before escalation succeeds)", cur)
	}
}
