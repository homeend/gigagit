package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// divergedClone builds the two shapes a non-fast-forward rejection can have,
// both against a real origin, and returns the clone's repo.
//
// rewrite=true  — the branch was REBASED locally: origin/feature holds nothing
//
//	but stale copies of the caller's own commits.
//
// rewrite=false — a teammate pushed to the same branch: origin/feature holds
//
//	genuinely new work.
func divergedClone(t *testing.T, rewrite bool) (string, *git.Repo) {
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
	gitAt(t, clone, "config", "user.name", "t")
	gitAt(t, clone, "config", "user.email", "t@t")
	gitAt(t, clone, "checkout", "-b", "feature")
	os.WriteFile(filepath.Join(clone, "mine.txt"), []byte("mine\n"), 0o644)
	gitAt(t, clone, "add", ".")
	gitAt(t, clone, "commit", "-m", "F1 my work")
	gitAt(t, clone, "push", "-u", "origin", "feature")

	if rewrite {
		// main advances on the remote; the clone rebases feature onto it, so
		// its published copies are now stale duplicates.
		os.WriteFile(filepath.Join(seed, "f.txt"), []byte("v2\n"), 0o644)
		gitAt(t, seed, "add", ".")
		gitAt(t, seed, "commit", "-m", "M1 trunk moves")
		gitAt(t, seed, "push", "origin", "main")
		gitAt(t, clone, "fetch", "origin")
		gitAt(t, clone, "rebase", "origin/main")
	} else {
		// A teammate pushes real new work to feature.
		gitAt(t, seed, "fetch", "origin")
		gitAt(t, seed, "checkout", "-b", "feature", "origin/feature")
		os.WriteFile(filepath.Join(seed, "theirs.txt"), []byte("theirs\n"), 0o644)
		gitAt(t, seed, "add", ".")
		gitAt(t, seed, "commit", "-m", "T1 their work")
		gitAt(t, seed, "push", "origin", "feature")
		os.WriteFile(filepath.Join(clone, "mine2.txt"), []byte("more\n"), 0o644)
		gitAt(t, clone, "add", ".")
		gitAt(t, clone, "commit", "-m", "F2 my work")
	}
	return clone, &git.Repo{Runner: gitexec.NewExecRunner("git", clone, observ.NewRing(50))}
}

// captureReject runs a plain push and returns the push-rejected request,
// answering it with "abort".
func captureReject(t *testing.T, repo *git.Repo) DecisionRequest {
	t.Helper()
	var got DecisionRequest
	dec := DeciderFunc(func(_ context.Context, req DecisionRequest) (DecisionResponse, error) {
		if req.ID != "push-rejected" {
			return DecisionResponse{}, fmt.Errorf("unexpected decision %q", req.ID)
		}
		got = req
		return DecisionResponse{Option: "abort"}, nil
	})
	if _, err := (Push{Remote: "origin", Branch: "feature", SetUpstream: true}).Run(
		context.Background(), OpDeps{Repo: repo, Decider: dec}); err != nil {
		t.Fatalf("push: %v", err)
	}
	if got.ID == "" {
		t.Fatal("push was not rejected — the fixture is wrong")
	}
	return got
}

// TestPushRejectAfterLocalRebaseLeadsWithForce: the remote holds only stale
// copies of commits the user rewrote, so rebasing onto them would resurrect
// the old copies and undo the rebase. Force must lead the option list and the
// prompt must not claim the remote has new commits.
func TestPushRejectAfterLocalRebaseLeadsWithForce(t *testing.T) {
	_, repo := divergedClone(t, true)
	req := captureReject(t, repo)

	if len(req.Options) == 0 || req.Options[0] != "force" {
		t.Fatalf("options = %v, want force first after a local rewrite", req.Options)
	}
	if !hasOption(req.Options, "rebase") || !hasOption(req.Options, "abort") {
		t.Fatalf("options = %v, want rebase and abort still available", req.Options)
	}
	if strings.Contains(req.Prompt, "Remote has new commits") {
		t.Fatalf("prompt = %q — the remote has no new commits, only stale copies", req.Prompt)
	}
}

// TestPushRejectWithRealRemoteWorkLeadsWithRebase is the regression guard for
// the case the recovery was designed for: someone else pushed real work, so
// integrating it (rebase) stays the leading answer.
func TestPushRejectWithRealRemoteWorkLeadsWithRebase(t *testing.T) {
	_, repo := divergedClone(t, false)
	req := captureReject(t, repo)

	if len(req.Options) == 0 || req.Options[0] != "rebase" {
		t.Fatalf("options = %v, want rebase first when the remote gained real work", req.Options)
	}
	if !strings.Contains(req.Prompt, "new commits") {
		t.Fatalf("prompt = %q, want it to report the remote's new commits", req.Prompt)
	}
}

// TestPushRejectAfterLocalRebaseForceKeepsTheRebase: taking the leading option
// publishes the rewritten branch — the remote ends up with the rebased shape,
// not the resurrected old copies.
func TestPushRejectAfterLocalRebaseForceKeepsTheRebase(t *testing.T) {
	clone, repo := divergedClone(t, true)
	local := strings.TrimSpace(gitOut(t, clone, "rev-parse", "feature"))

	res, err := (Push{Remote: "origin", Branch: "feature", SetUpstream: true}).Run(
		context.Background(), OpDeps{Repo: repo, Decider: MapDecider{
			"push-rejected": "force",
			"push-force":    "force-with-lease",
		}})
	if err != nil {
		t.Fatalf("push (force recovery): %v", err)
	}
	if !res.Changed {
		t.Fatalf("res = %+v, want a changed push", res)
	}
	remote := strings.TrimSpace(gitOut(t, clone, "ls-remote", "origin", "refs/heads/feature"))
	if !strings.HasPrefix(remote, local) {
		t.Fatalf("origin/feature = %q, want the local rebased tip %s", remote, local)
	}
}

// TestPushRejectRewriteOnNonCurrentBranchStillSaysWhy: pushing a rewritten
// branch that is NOT checked out cannot offer rebase (it would rewrite HEAD's
// branch), but the prompt must still say the remote holds only old copies
// instead of claiming new commits.
func TestPushRejectRewriteOnNonCurrentBranchStillSaysWhy(t *testing.T) {
	clone, repo := divergedClone(t, true)
	gitAt(t, clone, "checkout", "main")

	req := captureReject(t, repo)
	if len(req.Options) != 2 || req.Options[0] != "force" || req.Options[1] != "abort" {
		t.Fatalf("options = %v, want [force abort] for a non-current branch", req.Options)
	}
	if strings.Contains(req.Prompt, "Remote has new commits") {
		t.Fatalf("prompt = %q — the remote has no new commits, only stale copies", req.Prompt)
	}
}

func hasOption(opts []string, want string) bool {
	for _, o := range opts {
		if o == want {
			return true
		}
	}
	return false
}
