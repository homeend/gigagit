package engine

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/repogate"
)

// DeleteBranchVersion removes one recorded branch-version ref. Refuses any
// ref outside the versions namespace so a frontend bug can never delete a
// real branch or tag through this op.
type DeleteBranchVersion struct {
	Ref string // required, must start with refs/gg/versions/
}

var _ Operation = DeleteBranchVersion{}

// LockMode: moves (removes) a ref only; never index/worktree/HEAD.
func (op DeleteBranchVersion) LockMode() repogate.Mode { return repogate.RefWrite }

func (op DeleteBranchVersion) Run(ctx context.Context, deps OpDeps) (Result, error) {
	if !strings.HasPrefix(op.Ref, git.VersionRefPrefix) {
		return Result{}, fmt.Errorf("delete version: %s is not a branch-version ref", op.Ref)
	}
	deps.emit(ctx, Progress{Step: "deleting branch version", Detail: op.Ref})
	if err := deps.Repo.DeleteRef(ctx, op.Ref); err != nil {
		return Result{}, fmt.Errorf("delete version: %w", err)
	}
	res := Result{Changed: true}.WithSummary("deleted branch version %s", strings.TrimPrefix(op.Ref, git.VersionRefPrefix))
	deps.emit(ctx, Done{Result: res})
	return res, nil
}
