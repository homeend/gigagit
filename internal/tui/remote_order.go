package tui

import "github.com/homeend/gigagit/internal/model"

// sortRemoteBranchesLocalFirst returns remotes reordered so that a remote branch
// with a local counterpart (a branch of the same short name present in the
// Branches tab) sorts ahead of one without — the rows a user is most likely
// tracking float to the top of the Remotes tab. It's a stable partition: within
// each group git's original order (newest-first, etc.) is preserved.
//
// The result is a fresh slice; the input is not mutated. RemoteBranch.Branch is
// the de-prefixed name (e.g. "feat" for "origin/feat"), matched against
// Branch.Name.
func sortRemoteBranchesLocalFirst(remotes []model.RemoteBranch, locals []model.Branch) []model.RemoteBranch {
	hasLocal := make(map[string]bool, len(locals))
	for _, b := range locals {
		hasLocal[b.Name] = true
	}
	first := make([]model.RemoteBranch, 0, len(remotes))
	rest := make([]model.RemoteBranch, 0, len(remotes))
	for _, rb := range remotes {
		if hasLocal[rb.Branch] {
			first = append(first, rb)
		} else {
			rest = append(rest, rb)
		}
	}
	return append(first, rest...)
}
