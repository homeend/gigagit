package domain

// Branch filters: the five [[branches.filter]] slots, compiled once from the
// effective config, plus the exemption rules both frontends share. The
// ACTIVE slot is the frontends' business (promptstate, per repo per list):
// domain has no state-dir handle and evaluation is pure over what the
// caller already holds.

import (
	"context"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/model"
)

// BranchFilters returns the five compiled slots (index = slot-1) for this
// repo's effective config. An invalid block is inert inside its Compiled
// (Err set), never an error here.
func (s *Service) BranchFilters(ctx context.Context) ([branchfilter.MaxSlots]branchfilter.Compiled, []string, error) {
	cfg, err := s.EffectiveConfig(ctx)
	if err != nil {
		return [branchfilter.MaxSlots]branchfilter.Compiled{}, nil, err
	}
	all, warnings := branchfilter.CompileAll(cfg.Branches.Filter)
	return all, warnings, nil
}

// ExemptBranches marks the rows a filter may never hide: HEAD, and any
// branch checked out in a worktree (switching to it would strand the user
// on an invisible row).
func ExemptBranches(bs []model.Branch, wts []model.Worktree) []bool {
	checked := make(map[string]bool, len(wts))
	for _, w := range wts {
		if w.Branch != "" {
			checked[w.Branch] = true
		}
	}
	out := make([]bool, len(bs))
	for i, b := range bs {
		out[i] = b.IsHead || checked[b.Name]
	}
	return out
}

// ExemptRemoteBranches marks the current branch's upstream row (the one the
// Remotes tab's f/find and pull land on). Detached HEAD or no upstream:
// nothing is exempt.
func ExemptRemoteBranches(rbs []model.RemoteBranch, bs []model.Branch) []bool {
	upstream := ""
	for _, b := range bs {
		if b.IsHead {
			upstream = b.Upstream
			break
		}
	}
	out := make([]bool, len(rbs))
	if upstream == "" {
		return out
	}
	for i, rb := range rbs {
		out[i] = rb.Name == upstream
	}
	return out
}

// BranchRows adapts local branches for branchfilter.Apply.
func BranchRows(bs []model.Branch) []branchfilter.Row {
	rows := make([]branchfilter.Row, len(bs))
	for i, b := range bs {
		rows[i] = branchfilter.Row{Name: b.Name, UnixTime: b.UnixTime}
	}
	return rows
}

// RemoteBranchRows adapts remote branches: name rules see the BRANCH part
// ("feat/x"), never the remote prefix.
func RemoteBranchRows(rbs []model.RemoteBranch) []branchfilter.Row {
	rows := make([]branchfilter.Row, len(rbs))
	for i, rb := range rbs {
		rows[i] = branchfilter.Row{Name: rb.Branch, UnixTime: rb.UnixTime}
	}
	return rows
}
