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

// PullFFOnly fetches and fast-forwards the current branch only; it never
// creates a merge commit. Fails if the remote is not a fast-forward.
func (r *Repo) PullFFOnly(ctx context.Context, remote, branch string) error {
	argv := gitcmd.New("pull").Arg("--no-edit", "--ff-only", remote, branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git pull --ff-only", argv)
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
