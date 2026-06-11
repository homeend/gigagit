package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Fetch updates remote-tracking refs from remote. --no-write-fetch-head allows
// a concurrent pull without FETCH_HEAD contention.
func (r *Repo) Fetch(ctx context.Context, remote string) error {
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch", argv)
	return err
}

// PullStrategy selects how Pull integrates upstream changes.
type PullStrategy int

const (
	PullFF     PullStrategy = iota // --ff-only: never create a merge commit
	PullRebase                     // --rebase: replay local commits on top
	PullMerge                      // --no-rebase: create a merge commit if needed
)

// Pull integrates remote/branch into the current branch using the given strategy.
func (r *Repo) Pull(ctx context.Context, remote, branch string, strategy PullStrategy) error {
	b := gitcmd.New("pull").Arg("--no-edit")
	switch strategy {
	case PullRebase:
		b = b.Arg("--rebase")
	case PullMerge:
		b = b.Arg("--no-rebase")
	default:
		b = b.Arg("--ff-only")
	}
	b = b.Arg(remote, branch)
	_, err := r.Runner.Run(ctx, "git pull", b.ToArgv())
	return err
}

// PullFFOnly fast-forwards the current branch only (no merge commit).
func (r *Repo) PullFFOnly(ctx context.Context, remote, branch string) error {
	return r.Pull(ctx, remote, branch, PullFF)
}

// PullInWorktree fast-forwards branch inside another linked worktree at
// worktreePath, without touching the current worktree.
func (r *Repo) PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error {
	argv := gitcmd.New("pull").Dir(worktreePath).Arg("--no-edit", "--ff-only", remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git pull (worktree)", argv)
	return err
}

// FastForwardRef updates a NON-checked-out local branch to its remote tip
// without a checkout, via a fetch refspec (origin branch:branch). Fails if the
// update would not be a fast-forward.
func (r *Repo) FastForwardRef(ctx context.Context, remote, branch string) error {
	refspec := branch + ":" + branch
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote, refspec).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch (ff-ref)", argv)
	return err
}

// Push pushes branch to remote. When setUpstream is true it records the
// upstream tracking ref (-u).
func (r *Repo) Push(ctx context.Context, remote, branch string, setUpstream bool) error {
	argv := gitcmd.New("push").ArgIf(setUpstream, "-u").Arg(remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push", argv)
	return err
}
