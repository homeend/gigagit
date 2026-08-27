package engine

import (
	"context"
	"regexp"
	"strings"

	"github.com/homeend/gigagit/internal/git"
)

// StaleFetchMappingDecisionID is the "a configured per-branch fetch refspec
// names a branch the remote no longer has" fork: options "remove-and-retry"
// (drop every stale mapping + its dangling tracking ref, then fetch again)
// and "abort" (keep everything; the original fetch failure stands). Raised
// only when a plain fetch fails with git's missing-configured-ref error AND
// the missing branch is one of the exact (non-wildcard) refspecs in
// remote.<name>.fetch — one such stale mapping makes EVERY fetch of that
// remote exit 128, so pulls of unrelated branches are blocked until it goes.
const StaleFetchMappingDecisionID = "fetch_mapping.stale"

// missingRemoteRefRe extracts the branch from git's fatal line for an exact
// configured refspec whose remote branch is gone:
// "fatal: couldn't find remote ref refs/heads/<branch>".
var missingRemoteRefRe = regexp.MustCompile(`couldn't find remote ref refs/heads/(\S+)`)

// refspecSrcBranch returns the branch an exact fetch refspec maps from
// ("+refs/heads/foo:refs/remotes/origin/foo" → "foo"), or "" for anything
// else: wildcard specs match-nothing silently (never fatal), and negative
// ("^…") or non-branch specs are not per-branch mappings.
func refspecSrcBranch(spec string) string {
	if strings.HasPrefix(spec, "^") {
		return ""
	}
	src, _, _ := strings.Cut(strings.TrimPrefix(spec, "+"), ":")
	branch, ok := strings.CutPrefix(src, "refs/heads/")
	if !ok || branch == "" || strings.Contains(branch, "*") {
		return ""
	}
	return branch
}

// healStaleFetchMappings runs after a plain fetch (configured refspecs, no
// explicit command-line refspec) failed. When the failure is git's
// missing-configured-ref fatal and the missing branch is an exact per-branch
// mapping (the kind ensureRemoteTracking writes), it verifies against
// ls-remote which mapped branches the remote still has, forks
// StaleFetchMappingDecisionID once for ALL stale mappings (git reports only
// the FIRST missing ref per fetch, so healing one at a time would loop the
// prompt), and on "remove-and-retry" unsets each stale refspec value, deletes
// its dangling remote-tracking ref, and runs retry. Every other outcome —
// unparseable error, no matching mapping, ls-remote failure, "abort", or a
// decider that cannot answer (the background auto-refresh lane fetches with
// an empty MapDecider) — returns fetchErr unchanged: config is never mutated
// unseen, and the caller's original failure stands.
//
// remote narrows the sweep to one remote (the SmartPull case); "" scans every
// remote with per-branch mappings (the fetch-all case, where the fatal line
// does not name the remote).
func healStaleFetchMappings(ctx context.Context, deps OpDeps, remote string, fetchErr error, retry func(context.Context) error) error {
	m := missingRemoteRefRe.FindStringSubmatch(fetchErr.Error())
	if m == nil {
		return fetchErr
	}
	missing := m[1]

	pairs, err := deps.Repo.ConfigGetRegexp(ctx, `^remote\..+\.fetch$`)
	if err != nil {
		return fetchErr
	}
	type mapping struct{ remote, branch, spec string }
	var mapped []mapping
	found := false
	for _, kv := range pairs {
		r := strings.TrimSuffix(strings.TrimPrefix(kv[0], "remote."), ".fetch")
		if remote != "" && r != remote {
			continue
		}
		b := refspecSrcBranch(kv[1])
		if b == "" {
			continue
		}
		mapped = append(mapped, mapping{r, b, kv[1]})
		found = found || b == missing
	}
	if !found {
		return fetchErr // the missing ref is not one of our per-branch mappings
	}

	live := map[string]map[string]bool{}
	var stale []mapping
	for _, mp := range mapped {
		if live[mp.remote] == nil {
			heads, herr := deps.Repo.ListRemoteHeads(ctx, mp.remote)
			if herr != nil {
				return fetchErr
			}
			set := make(map[string]bool, len(heads))
			for _, h := range heads {
				set[h.Name] = true
			}
			live[mp.remote] = set
		}
		if !live[mp.remote][mp.branch] {
			stale = append(stale, mp)
		}
	}
	if len(stale) == 0 {
		return fetchErr
	}

	names := make([]string, len(stale))
	for i, mp := range stale {
		names[i] = mp.remote + "/" + mp.branch
	}
	choice, derr := deps.decide(ctx, PromptReq(StaleFetchMappingDecisionID,
		"fetch is blocked: %s deleted on the remote, but a fetch mapping still references it. Remove the stale mapping and retry?",
		[]string{"remove-and-retry", "abort"}, strings.Join(names, ", ")))
	if derr != nil || choice.Option != "remove-and-retry" {
		return fetchErr
	}

	for _, mp := range stale {
		if err := deps.Repo.ConfigUnsetValue(ctx, git.ConfigLocal, "remote."+mp.remote+".fetch", mp.spec); err != nil {
			return fetchErr
		}
		// Best-effort: the tracking ref may already be gone (or pruned).
		_ = deps.Repo.DeleteRef(ctx, "refs/remotes/"+mp.remote+"/"+mp.branch)
	}
	deps.emit(ctx, Progress{Step: "retrying fetch", Detail: strings.Join(names, ", ")})
	return retry(ctx)
}
