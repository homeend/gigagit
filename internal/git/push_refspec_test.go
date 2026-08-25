package git

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// mappedPushRepo seeds a clone whose branch `feature` tracks origin/main, plus
// whatever push config the case needs — the shapes in which git rewrites the
// DESTINATION of a `git push <remote> <branch>` out from under the caller.
func mappedPushRepo(t *testing.T, config ...string) (dir, origin string, repo *Repo) {
	t.Helper()
	dir, _ = newTestRepo(t)
	origin = t.TempDir()
	gitRun(t, origin, "init", "--bare", ".")
	gitRun(t, dir, "remote", "add", "origin", origin)
	gitRun(t, dir, "push", "-u", "origin", "main")
	gitRun(t, dir, "checkout", "-b", "feature", "origin/main") // upstream = origin/main
	for i := 0; i+1 < len(config); i += 2 {
		gitRun(t, dir, "config", config[i], config[i+1])
	}
	writeFile(t, dir, "mine.txt", "mine\n")
	commitAll(t, dir, "F1")
	return dir, origin, &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))}
}

// remoteRef reads one ref out of a bare repo, or "" when it is absent.
func remoteRef(t *testing.T, origin, ref string) string {
	t.Helper()
	out := gitOutIn(t, origin, "for-each-ref", "--format=%(refname) %(objectname)")
	for _, ln := range strings.Split(out, "\n") {
		name, sha, ok := strings.Cut(strings.TrimSpace(ln), " ")
		if ok && name == ref {
			return sha
		}
	}
	return ""
}

// TestPushIgnoresPushDefaultUpstream: with push.default=upstream git rewrites a
// bare `feature` refspec to the branch's UPSTREAM (refs/heads/main here), so a
// "push this branch" would silently land on main. Push must name the
// destination itself.
func TestPushIgnoresPushDefaultUpstream(t *testing.T) {
	t.Parallel()
	dir, origin, repo := mappedPushRepo(t, "push.default", "upstream")
	mainBefore := remoteRef(t, origin, "refs/heads/main")

	if err := repo.Push(context.Background(), "origin", "feature", true, PushNoForce); err != nil {
		t.Fatalf("push: %v", err)
	}

	local := gitOutIn(t, dir, "rev-parse", "feature")
	if got := remoteRef(t, origin, "refs/heads/feature"); got != local {
		t.Fatalf("origin refs/heads/feature = %q, want the pushed tip %s", got, local)
	}
	if got := remoteRef(t, origin, "refs/heads/main"); got != mainBefore {
		t.Fatalf("origin main moved to %s — the push landed on the wrong branch", got)
	}
}

// TestPushIgnoresRemotePushRefspec: a configured remote.<name>.push refspec
// remaps the destination the same way (the monorepo/gerrit shape).
func TestPushIgnoresRemotePushRefspec(t *testing.T) {
	t.Parallel()
	dir, origin, repo := mappedPushRepo(t, "remote.origin.push", "refs/heads/*:refs/heads/sandbox/*")

	if err := repo.Push(context.Background(), "origin", "feature", true, PushNoForce); err != nil {
		t.Fatalf("push: %v", err)
	}

	local := gitOutIn(t, dir, "rev-parse", "feature")
	if got := remoteRef(t, origin, "refs/heads/feature"); got != local {
		t.Fatalf("origin refs/heads/feature = %q, want the pushed tip %s", got, local)
	}
	if got := remoteRef(t, origin, "refs/heads/sandbox/feature"); got != "" {
		t.Fatalf("origin sandbox/feature = %s — the push refspec still redirected the push", got)
	}
}

// TestPushSetsUpstreamToTheBranchItPushed: -u must still record the branch's
// own remote-tracking ref, not the upstream it was created from.
func TestPushSetsUpstreamToTheBranchItPushed(t *testing.T) {
	t.Parallel()
	dir, _, repo := mappedPushRepo(t, "push.default", "upstream")

	if err := repo.Push(context.Background(), "origin", "feature", true, PushNoForce); err != nil {
		t.Fatalf("push: %v", err)
	}

	if got := gitOutIn(t, dir, "config", "--get", "branch.feature.merge"); got != "refs/heads/feature" {
		t.Fatalf("branch.feature.merge = %q, want refs/heads/feature", got)
	}
}

// TestPushForceModesKeepTheExplicitDestination: the force lanes push the same
// refspec (a lease-protected overwrite must not land on another ref either).
func TestPushForceModesKeepTheExplicitDestination(t *testing.T) {
	t.Parallel()
	dir, origin, repo := mappedPushRepo(t, "push.default", "upstream")
	mainBefore := remoteRef(t, origin, "refs/heads/main")
	gitRun(t, dir, "push", "origin", "feature:refs/heads/feature")
	writeFile(t, dir, "mine.txt", "rewritten\n")
	gitRun(t, dir, "commit", "-a", "--amend", "-m", "F1 rewritten")

	if err := repo.Push(context.Background(), "origin", "feature", false, PushForcePlain); err != nil {
		t.Fatalf("force push: %v", err)
	}

	local := gitOutIn(t, dir, "rev-parse", "feature")
	if got := remoteRef(t, origin, "refs/heads/feature"); got != local {
		t.Fatalf("origin refs/heads/feature = %q, want the force-pushed tip %s", got, local)
	}
	if got := remoteRef(t, origin, "refs/heads/main"); got != mainBefore {
		t.Fatalf("origin main = %s, want it untouched at %s", got, mainBefore)
	}
}
