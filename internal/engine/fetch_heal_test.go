package engine

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// addStaleMapping writes an exact per-branch fetch refspec for a branch that
// does not exist on origin, plus the dangling remote-tracking ref such a
// mapping leaves behind — the state a deleted remote branch strands a repo in.
func addStaleMapping(t *testing.T, clone, branch string) {
	t.Helper()
	spec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
	gitAt(t, clone, "config", "--add", "remote.origin.fetch", spec)
	sha := strings.TrimSpace(revAt(t, clone, "HEAD"))
	gitAt(t, clone, "update-ref", "refs/remotes/origin/"+branch, sha)
}

func fetchSpecs(t *testing.T, clone string) string {
	t.Helper()
	cmd := exec.Command("git", "config", "--get-all", "remote.origin.fetch")
	cmd.Dir = clone
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("config --get-all: %v", err)
	}
	return string(out)
}

func hasRef(t *testing.T, clone, ref string) bool {
	t.Helper()
	cmd := exec.Command("git", "rev-parse", "--verify", "--quiet", ref)
	cmd.Dir = clone
	return cmd.Run() == nil
}

// A stale exact fetch mapping makes every `git fetch` exit 128, blocking the
// pull. Answering remove-and-retry removes EVERY stale mapping (git reports
// only the first missing ref per fetch), deletes the dangling tracking refs,
// and retries the fetch — the pull then completes.
func TestSmartPullHealsStaleFetchMappings(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	addStaleMapping(t, clone, "gone/one")
	addStaleMapping(t, clone, "gone/two")

	res, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{StaleFetchMappingDecisionID: "remove-and-retry"}})
	if err != nil {
		t.Fatalf("smart pull: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if revAt(t, clone, "main") != revAt(t, clone, "origin/main") {
		t.Fatal("main was not fast-forwarded to origin/main")
	}
	specs := fetchSpecs(t, clone)
	if strings.Contains(specs, "gone/one") || strings.Contains(specs, "gone/two") {
		t.Fatalf("stale mappings survived:\n%s", specs)
	}
	if hasRef(t, clone, "refs/remotes/origin/gone/one") || hasRef(t, clone, "refs/remotes/origin/gone/two") {
		t.Fatal("dangling tracking refs survived")
	}
}

// "abort" keeps the mapping and surfaces the original fetch failure.
func TestSmartPullStaleFetchMappingAbort(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	addStaleMapping(t, clone, "gone")

	_, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{StaleFetchMappingDecisionID: "abort"}})
	if err == nil || !strings.Contains(err.Error(), "couldn't find remote ref") {
		t.Fatalf("err = %v, want the original fetch failure", err)
	}
	if !strings.Contains(fetchSpecs(t, clone), "gone") {
		t.Fatal("abort must not remove the mapping")
	}
}

// A decider that cannot answer (the background auto-refresh lane runs with an
// empty MapDecider) skips the heal: the original error stands, nothing hangs,
// and no config is mutated unseen.
func TestSmartPullStaleFetchMappingNoDecider(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	addStaleMapping(t, clone, "gone")

	_, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{}})
	if err == nil || !strings.Contains(err.Error(), "couldn't find remote ref") {
		t.Fatalf("err = %v, want the original fetch failure", err)
	}
	if !strings.Contains(fetchSpecs(t, clone), "gone") {
		t.Fatal("unanswered decision must not remove the mapping")
	}
}

// A fetch failure that is NOT a missing-configured-ref (here: a bogus remote
// URL) passes through untouched — no decision is raised even with an answer
// available.
func TestSmartPullNonStaleFetchFailurePassesThrough(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	gitAt(t, clone, "remote", "set-url", "origin", clone+"/does-not-exist")

	_, err := SmartPull{Intent: PullAndStay}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{StaleFetchMappingDecisionID: "remove-and-retry"}})
	if err == nil || strings.Contains(err.Error(), "stale") {
		t.Fatalf("err = %v, want the raw fetch failure", err)
	}
}

// The fetch-all op (`f`) heals too: with no remote hint it locates the remote
// whose config maps the missing branch.
func TestFetchOpHealsStaleFetchMapping(t *testing.T) {
	t.Parallel()
	clone, repo := cloneOnMainBehindOrigin(t)
	addStaleMapping(t, clone, "gone")

	res, err := Fetch{}.Run(context.Background(),
		OpDeps{Repo: repo, Decider: MapDecider{StaleFetchMappingDecisionID: "remove-and-retry"}})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !res.Changed {
		t.Fatalf("result = %+v, want Changed", res)
	}
	if strings.Contains(fetchSpecs(t, clone), "gone") {
		t.Fatal("stale mapping survived")
	}
}
