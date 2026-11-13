package engine

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/homeend/gigagit/internal/git"
)

// CheckoutRemoteBranch materializes a remote branch that has no local
// remote-tracking ref yet — one the clone's fetch refspec does not cover (the
// narrowed/single-branch monorepo clone), or one pushed since the last fetch:
// it writes a per-branch fetch mapping unless the wildcard already covers the
// remote (never adding the wildcard — widening could turn the next `git
// fetch` into a mass download), fetches exactly that branch,
// then hands off to SmartCheckout to create the local tracking branch and
// optionally switch. Frontends resolve Remote/Branch before calling (the
// browse-remote-branches picker); decision-free itself, though SmartSwitch's
// stash-pop-conflict fork can surface through the hand-off.
type CheckoutRemoteBranch struct {
	Remote string
	Branch string
	Intent CheckoutIntent
}

func (op CheckoutRemoteBranch) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Remote == "" || op.Branch == "" {
		return Result{}, fmt.Errorf("checkout remote branch: Remote and Branch are required")
	}
	key := "remote." + op.Remote + ".fetch"
	spec := fetchSpec(op.Remote, op.Branch)
	have, err := deps.Repo.ConfigGetAll(ctx, key)
	if err != nil {
		return Result{}, err
	}
	// An ordinary clone's wildcard already maps the branch: `git fetch
	// <remote> <branch>` then updates its tracking ref on its own, and a
	// per-branch line would only linger after the branch is deleted upstream.
	if !slices.Contains(have, spec) && !coversAllBranches(have, op.Remote) { // idempotent after a previous run whose fetch failed
		if err := deps.Repo.ConfigAdd(ctx, git.ConfigLocal, key, spec); err != nil {
			return Result{}, err
		}
	}
	deps.emit(ctx, Progress{Step: "fetching", Detail: op.Remote + " " + op.Branch})
	if err := deps.Repo.FetchBranches(ctx, op.Remote, []string{op.Branch}); err != nil {
		return Result{}, err // mapping stays; a re-run is idempotent
	}
	// Run inline (shared deps) — never via domain.Execute, which would take a
	// nested repogate reservation under the one we already hold.
	return SmartCheckout{RemoteRef: op.Remote + "/" + op.Branch, Local: op.Branch, Intent: op.Intent}.Run(ctx, deps)
}

var _ Operation = CheckoutRemoteBranch{}

// coversAllBranches reports whether specs hold the standard wildcard mapping
// of remote's branches (forced or not).
func coversAllBranches(specs []string, remote string) bool {
	want := "refs/heads/*:refs/remotes/" + remote + "/*"
	for _, s := range specs {
		if strings.TrimPrefix(s, "+") == want {
			return true
		}
	}
	return false
}
