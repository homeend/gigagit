package engine

import (
	"context"
	"slices"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// FetchMappingDecisionID is the post-push "this branch isn't covered by the
// fetch refspec" fork: options "add" (write a per-branch mapping and fetch
// just that branch) and "skip". Raised only AFTER a successful push whose
// remote-tracking ref did not end up at the pushed tip — the single-branch/
// shallow monorepo clone case, where the ↓↑ tip markers and ahead/behind can
// never follow the branch.
const FetchMappingDecisionID = "fetch_mapping.add"

// fetchSpec is the per-branch mapping written on "add". Never the wildcard:
// widening the refspec could make the next `git fetch` a mass download on a
// monorepo remote.
func fetchSpec(remote, branch string) string {
	return "+refs/heads/" + branch + ":refs/remotes/" + remote + "/" + branch
}

// exactRefHash returns the hash of the ref exactly named ref, or "" — the
// BranchVersions over-match guard (for-each-ref patterns match on component
// boundaries, so an exact name can still return children of ref/).
func exactRefHash(refs []model.RefInfo, ref string) string {
	for _, r := range refs {
		if r.Ref == ref {
			return r.Hash
		}
	}
	return ""
}

// ensureRemoteTracking runs after a SUCCESSFUL push of branch to remote,
// before the Done event: when the remote-tracking ref did not move to the
// pushed tip (the fetch refspec does not map the branch), it forks the
// FetchMappingDecisionID decision and, on "add", writes the per-branch
// mapping + fetches only that branch (near-free — the remote just received
// our objects). Every failure path returns res with at most a summary note:
// the push already succeeded and must never be failed retroactively. A
// decider error skips (the post-create-hook convention: safe default skip).
func ensureRemoteTracking(ctx context.Context, deps OpDeps, remote, branch string, res Result) Result {
	localRef := "refs/heads/" + branch
	remoteRef := "refs/remotes/" + remote + "/" + branch
	locals, err := deps.Repo.ForEachRef(ctx, localRef)
	if err != nil {
		return res // cannot resolve the pushed tip; not worth a note
	}
	tip := exactRefHash(locals, localRef)
	if tip == "" {
		return res
	}
	remotes, err := deps.Repo.ForEachRef(ctx, remoteRef)
	if err == nil && exactRefHash(remotes, remoteRef) == tip {
		return res // mapped and current — the healthy fast path
	}
	choice, derr := deps.decide(ctx, PromptReq(FetchMappingDecisionID,
		"%s/%s is not tracked by the fetch refspec — tip markers and ahead/behind cannot follow it. Add a tracking mapping for this branch?",
		[]string{"add", "skip"}, remote, branch))
	if derr != nil || choice.Option != "add" {
		return res
	}
	key := "remote." + remote + ".fetch"
	spec := fetchSpec(remote, branch)
	have, _ := deps.Repo.ConfigGetAll(ctx, key)
	if !slices.Contains(have, spec) { // idempotent after a previous add whose fetch failed
		if err := deps.Repo.ConfigAdd(ctx, git.ConfigLocal, key, spec); err != nil {
			return res.AppendSummary("; could not add fetch mapping: %s", err.Error())
		}
	}
	deps.emit(ctx, Progress{Step: "fetching", Detail: remote + " " + branch})
	if err := deps.Repo.FetchBranches(ctx, remote, []string{branch}); err != nil {
		return res.AppendSummary("; fetch mapping added but fetch failed: %s", err.Error())
	}
	return res.AppendSummary("; mapped %s/%s for tracking", remote, branch)
}
