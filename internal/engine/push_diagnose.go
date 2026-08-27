package engine

import "context"

// pushDivergence names WHY a plain push came back non-fast-forward. Git
// reports one rejection for two opposite situations, and they want opposite
// answers: integrating (rebase) is right when the remote gained work, and
// wrong when the remote merely holds the pre-rewrite copies of the caller's
// own commits — rebasing onto those resurrects them and undoes the rewrite.
type pushDivergence int

const (
	// divUnknown: the classification could not be made (no remote branch of
	// that name, objects absent, a git error). Callers behave exactly as the
	// op did before the diagnosis existed.
	divUnknown      pushDivergence = iota
	divRemoteNew                   // every remote-only commit is real new work
	divLocalRewrite                // every remote-only commit is a patch copy of ours
	divMixed                       // both kinds are present
)

// diagnoseRejection classifies a rejected push WITHOUT fetching objects:
// `git ls-remote` names the remote tip, and the counting only happens when
// that tip is already an object we hold — which is exactly the local-rewrite
// case (the remote tip is our own pre-rewrite commit). A tip we don't have is
// proof the remote gained work, and every failure degrades to divUnknown, so
// the diagnosis can never turn a working push into a failing one.
func diagnoseRejection(ctx context.Context, deps OpDeps, remote, branch string) pushDivergence {
	deps.emit(ctx, Progress{Step: "inspecting the remote", Detail: remote + " " + branch})

	tip, err := deps.Repo.RemoteBranchTip(ctx, remote, branch)
	if err != nil || tip == "" {
		return divUnknown
	}
	have, err := deps.Repo.CommitExists(ctx, tip)
	if err != nil {
		return divUnknown
	}
	if !have {
		return divRemoteNew // objects we've never seen: real remote work
	}
	local := "refs/heads/" + branch
	total, err := deps.Repo.CountRange(ctx, local, tip)
	if err != nil || total == 0 {
		// total == 0 means this ref is not what rejected us (e.g. a push
		// refspec sent the branch somewhere else) — don't guess.
		return divUnknown
	}
	// A remote tip this branch itself pointed at before (its reflog names it)
	// is proof of a local rewrite: everything reachable from that tip was this
	// branch's own history, so the remote cannot be holding new work. This
	// recognizes rewrites the patch-id comparison below cannot — a rebase
	// whose conflicts were resolved by hand changes the replayed diffs. A
	// missing reflog answers false and falls through to the patch-id count.
	if wasOurs, rerr := deps.Repo.BranchReflogContains(ctx, branch, tip); rerr == nil && wasOurs {
		return divLocalRewrite
	}
	fresh, err := deps.Repo.CountRangeUnique(ctx, local, tip)
	if err != nil {
		return divUnknown
	}
	switch {
	case fresh == 0:
		return divLocalRewrite
	case fresh == total:
		return divRemoteNew
	default:
		return divMixed
	}
}
