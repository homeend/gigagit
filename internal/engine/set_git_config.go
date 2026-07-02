package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// SetGitConfig writes one git config key at one scope — the generic sibling
// of SetIdentity (which stays as the dedicated identity-pair op). Backs the
// notification center's "enable fetch.writeCommitGraph" action; stage 3's
// config explorer reuses it. Decision-free: the scope is fixed before any
// work (Global true = ~/.gitconfig, false = this repo's .git/config — a
// bool, not git.ConfigScope, so frontends can construct it without
// importing internal/git).
type SetGitConfig struct {
	Key    string
	Value  string
	Global bool
	Unset  bool // remove the key at the chosen scope; Value is ignored
}

// LockMode: a config write touches neither refs nor the work tree (and a
// global write is not even repo-scoped), so the lightest reservation suffices.
func (op SetGitConfig) LockMode() repogate.Mode { return repogate.Read }

func (op SetGitConfig) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Key == "" {
		return Result{}, fmt.Errorf("set git config: key is required")
	}
	scope := git.ConfigLocal
	where := "in this repo"
	if op.Global {
		scope = git.ConfigGlobal
		where = "globally"
	}
	if op.Unset {
		deps.emit(ctx, Progress{Step: "unsetting git config", Detail: op.Key + " " + where})
		if err := deps.Repo.ConfigUnset(ctx, scope, op.Key); err != nil {
			return Result{}, fmt.Errorf("unset git config: %s: %w", op.Key, err)
		}
		res := Result{Summary: fmt.Sprintf("%s unset %s", op.Key, where), Changed: true}
		deps.emit(ctx, Done{Result: res})
		return res, nil
	}
	deps.emit(ctx, Progress{Step: "setting git config", Detail: op.Key + " " + where})
	if err := deps.Repo.ConfigSet(ctx, scope, op.Key, op.Value); err != nil {
		return Result{}, fmt.Errorf("set git config: %s: %w", op.Key, err)
	}
	res := Result{Summary: fmt.Sprintf("%s = %s set %s", op.Key, op.Value, where), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = SetGitConfig{}
