package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/model"
)

// Repo provides read-only git operations through a Runner.
type Repo struct {
	Runner gitexec.Runner
}

// Status returns the working-tree status.
func (r *Repo) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	// --untracked-files=all lists each untracked file individually rather than
	// collapsing a fully-untracked directory into a single "dir/" entry.
	argv := gitcmd.New("status").Arg("--porcelain=v2", "-z", "--branch", "--untracked-files=all").ToArgv()
	res, err := r.Runner.Run(ctx, "git status", argv)
	if err != nil {
		return model.WorkingTreeStatus{}, err
	}
	return ParseStatusV2([]byte(res.Stdout))
}

// Branches returns local branches.
func (r *Repo) Branches(ctx context.Context) ([]model.Branch, error) {
	const format = "%(HEAD)%00%(refname:short)%00%(upstream:short)%00%(objectname:short)%00%(upstream:track)%00%(committerdate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/heads").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref", argv)
	if err != nil {
		return nil, err
	}
	return ParseBranches([]byte(res.Stdout))
}

// Worktrees returns the repository's worktrees.
func (r *Repo) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	argv := gitcmd.New("worktree").Arg("list", "--porcelain").ToArgv()
	res, err := r.Runner.Run(ctx, "git worktree list", argv)
	if err != nil {
		return nil, err
	}
	return ParseWorktrees([]byte(res.Stdout))
}
