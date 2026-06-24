package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// WorktreeForBranch returns the worktree that currently has branch checked out,
// or nil if none does.
func (r *Repo) WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error) {
	wts, err := r.Worktrees(ctx)
	if err != nil {
		return nil, err
	}
	for i := range wts {
		if wts[i].Branch == branch {
			return &wts[i], nil
		}
	}
	return nil, nil
}

// CanFastForward reports whether ancestor is an ancestor of descendant, i.e.
// descendant can be fast-forwarded onto / from ancestor. Uses
// `git merge-base --is-ancestor`, which exits 0 (true) or 1 (false).
func (r *Repo) CanFastForward(ctx context.Context, ancestor, descendant string) (bool, error) {
	argv := gitcmd.New("merge-base").Arg("--is-ancestor", ancestor, descendant).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base --is-ancestor", argv)
	if err == nil {
		return true, nil
	}
	// Exit code 1 means "not an ancestor" (a normal answer, not a failure).
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
