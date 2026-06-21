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
	// %(refname:lstrip=2) yields the bare branch name (refs/heads/<x> → <x>)
	// without git's ambiguity disambiguation. %(refname:short) renders a branch
	// that collides with a same-named tag as "heads/<x>" — a form that breaks
	// `git switch` and worktree-by-name matching (e.g. a worktree created at a
	// tag with a branch of the same name).
	const format = "%(HEAD)%00%(refname:lstrip=2)%00%(upstream:short)%00%(objectname:short)%00%(upstream:track)%00%(committerdate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/heads").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref", argv)
	if err != nil {
		return nil, err
	}
	return ParseBranches([]byte(res.Stdout))
}

// RemoteBranches returns remote-tracking branches (refs/remotes), excluding the
// per-remote HEAD symref.
func (r *Repo) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	// lstrip=2 mirrors %(refname:short) for remotes (refs/remotes/<x> → <x>) but
	// without ambiguity disambiguation — see Branches.
	const format = "%(refname:lstrip=2)%00%(objectname:short)%00%(committerdate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--format="+format, "refs/remotes").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (remotes)", argv)
	if err != nil {
		return nil, err
	}
	return ParseRemoteBranches([]byte(res.Stdout))
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

// Tags returns the repository's tags (refs/tags), newest first. The peeled
// object resolves an annotated tag to its commit.
func (r *Repo) Tags(ctx context.Context) ([]model.Tag, error) {
	// lstrip=2 yields the bare tag name without ambiguity disambiguation — a tag
	// colliding with a same-named branch would otherwise render "tags/<x>". See
	// Branches.
	const format = "%(refname:lstrip=2)%00%(objecttype)%00%(objectname:short)%00%(*objectname:short)%00%(contents:subject)%00%(creatordate:unix)"
	argv := gitcmd.New("for-each-ref").Arg("--sort=-creatordate", "--format="+format, "refs/tags").ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (tags)", argv)
	if err != nil {
		return nil, err
	}
	return ParseTags([]byte(res.Stdout))
}
