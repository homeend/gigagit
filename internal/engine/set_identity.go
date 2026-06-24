package engine

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// SetIdentity writes user.name and user.email to one git config scope. It is
// the single write path behind both "edit identity" (typed values) and "apply
// a profile" (saved values). Decision-free: the scope is fixed before any work
// (Global true = ~/.gitconfig, false = this repo's .git/config).
type SetIdentity struct {
	Name   string
	Email  string
	Global bool
}

// LockMode: a config write touches neither refs nor the work tree (and a
// global write is not even repo-scoped), so the lightest reservation suffices.
func (op SetIdentity) LockMode() repogate.Mode { return repogate.Read }

func (op SetIdentity) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if op.Name == "" || op.Email == "" {
		return Result{}, fmt.Errorf("set identity: name and email are required")
	}
	scope := git.ConfigLocal
	where := "this repo"
	if op.Global {
		scope = git.ConfigGlobal
		where = "globally"
	}
	deps.emit(ctx, Progress{Step: "setting identity", Detail: where})
	if err := deps.Repo.ConfigSet(ctx, scope, "user.name", op.Name); err != nil {
		return Result{}, fmt.Errorf("set identity: user.name: %w", err)
	}
	if err := deps.Repo.ConfigSet(ctx, scope, "user.email", op.Email); err != nil {
		return Result{}, fmt.Errorf("set identity: user.email: %w", err)
	}
	res := Result{Summary: fmt.Sprintf("identity %s <%s> set %s", op.Name, op.Email, where), Changed: true}
	deps.emit(ctx, Done{Result: res})
	return res, nil
}

var _ Operation = SetIdentity{}
