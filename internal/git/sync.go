package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// Fetch updates remote-tracking refs from remote. --no-write-fetch-head allows
// a concurrent pull without FETCH_HEAD contention.
func (r *Repo) Fetch(ctx context.Context, remote string) error {
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch", argv)
	return err
}

// FetchAll updates tracking refs for every configured remote (no prune).
func (r *Repo) FetchAll(ctx context.Context) error {
	argv := gitcmd.New("fetch").Arg("--all", "--no-write-fetch-head").ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch --all", argv)
	return err
}

// FetchBranches updates the remote-tracking refs for exactly the named
// branches (`git fetch <remote> <branch>…`). Callers guarantee non-empty
// branches (engine ops no-op before calling). --no-write-fetch-head matches
// Fetch's concurrency contract.
func (r *Repo) FetchBranches(ctx context.Context, remote string, branches []string) error {
	b := gitcmd.New("fetch").Arg("--no-write-fetch-head", remote)
	for _, br := range branches {
		b = b.Arg(br)
	}
	_, err := r.Runner.Run(ctx, "git fetch (branches)", b.ToArgv())
	return err
}

// RemoteNames lists configured remote names, one per line.
func (r *Repo) RemoteNames(ctx context.Context) ([]string, error) {
	argv := gitcmd.New("remote").ToArgv()
	res, err := r.Runner.Run(ctx, "git remote", argv)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, ln := range strings.Split(res.Stdout, "\n") {
		if s := strings.TrimSpace(ln); s != "" {
			names = append(names, s)
		}
	}
	return names, nil
}

// PruneRemotes removes tracking refs for branches deleted on the named remotes,
// in one invocation. Empty names is a no-op (no error).
func (r *Repo) PruneRemotes(ctx context.Context, names ...string) error {
	if len(names) == 0 {
		return nil
	}
	argv := gitcmd.New("remote").Arg("prune").Arg(names...).ToArgv()
	_, err := r.Runner.Run(ctx, "git remote prune", argv)
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

// FastForwardToRef fast-forwards a NON-checked-out local branch to source (a
// fully-qualified local ref, e.g. refs/remotes/origin/foo) without a checkout or
// network access. Fails if the update is not a fast-forward, or if <branch> is
// checked out in any worktree.
func (r *Repo) FastForwardToRef(ctx context.Context, branch, source string) error {
	refspec := source + ":refs/heads/" + branch
	argv := gitcmd.New("fetch").Arg("--no-write-fetch-head", ".", refspec).ToArgv()
	_, err := r.Runner.Run(ctx, "git fetch (ff-to-ref)", argv)
	return err
}

// PushForce selects how Push overwrites a diverged remote branch.
type PushForce int

const (
	PushNoForce        PushForce = iota // plain push (rejected on non-fast-forward)
	PushForceWithLease                  // --force-with-lease (refuses if the remote moved)
	PushForcePlain                      // --force (overwrites unconditionally)
)

// Push pushes branch to remote's branch of the SAME NAME. When setUpstream is
// true it records the upstream tracking ref (-u). The force mode chooses how a
// diverged remote is overwritten: none (default), --force-with-lease (safe),
// or --force (plain).
//
// The destination is spelled out (`<branch>:refs/heads/<branch>`) rather than
// left to git: for a bare one-sided refspec git resolves the destination
// through `push.default` and any `remote.<name>.push` refspec, so under
// `push.default = upstream` a branch whose upstream is origin/main would be
// pushed ONTO main — silently, since gg's UI only ever says "push <branch>".
// An explicit destination makes the argv mean what the caller asked for.
func (r *Repo) Push(ctx context.Context, remote, branch string, setUpstream bool, force PushForce) error {
	b := gitcmd.New("push").ArgIf(setUpstream, "-u")
	switch force {
	case PushForceWithLease:
		b = b.Arg("--force-with-lease")
	case PushForcePlain:
		b = b.Arg("--force")
	}
	argv := b.Arg(remote, branch+":refs/heads/"+branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push", argv)
	return err
}

// PushTag pushes a single tag to remote (git push <remote> refs/tags/<name>).
// The explicit refs/tags/ refspec avoids a branch/tag name ambiguity.
func (r *Repo) PushTag(ctx context.Context, remote, name string) error {
	argv := gitcmd.New("push").Arg(remote, "refs/tags/"+name).ToArgv()
	_, err := r.Runner.Run(ctx, "git push (tag)", argv)
	return err
}

// PushTags pushes the named tags to a remote in one invocation:
// `git push <remote> refs/tags/<n>…`. Empty names is a no-op.
func (r *Repo) PushTags(ctx context.Context, remote string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	b := gitcmd.New("push").Arg(remote)
	for _, n := range names {
		b = b.Arg("refs/tags/" + n)
	}
	_, err := r.Runner.Run(ctx, "git push (tags)", b.ToArgv())
	return err
}

// PushDelete deletes branch on remote (git push <remote> --delete <branch>).
func (r *Repo) PushDelete(ctx context.Context, remote, branch string) error {
	argv := gitcmd.New("push").Arg(remote, "--delete", branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git push delete", argv)
	return err
}

// PushDeleteTag deletes tag on remote (git push <remote> --delete
// refs/tags/<tag>). The full refs/tags/ ref disambiguates from a same-named
// branch.
func (r *Repo) PushDeleteTag(ctx context.Context, remote, tag string) error {
	argv := gitcmd.New("push").Arg(remote, "--delete", "refs/tags/"+tag).ToArgv()
	_, err := r.Runner.Run(ctx, "git push delete tag", argv)
	return err
}
