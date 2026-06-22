package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Merge merges branch into the branch checked out at dir ("" = this repo's
// own worktree). --no-edit keeps the merge-commit message non-interactive.
func (r *Repo) Merge(ctx context.Context, dir, branch string) error {
	b := gitcmd.New("merge").Arg("--no-edit", branch)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge", b.ToArgv())
	return err
}

// MergeFFOnly fast-forwards the branch checked out at dir ("" = this repo's own
// worktree) to commit. --ff-only refuses (non-zero exit) when commit is not a
// descendant of HEAD; --no-edit keeps it non-interactive. One invocation.
func (r *Repo) MergeFFOnly(ctx context.Context, dir, commit string) error {
	b := gitcmd.New("merge").Arg("--ff-only", "--no-edit", commit)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge --ff-only", b.ToArgv())
	return err
}

// MergeAbort aborts an in-progress merge at dir ("" = this repo's worktree).
func (r *Repo) MergeAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("merge").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git merge --abort", b.ToArgv())
	return err
}

// MergeInProgress reports whether a merge is in progress at dir ("" = this
// repo's worktree), i.e. MERGE_HEAD resolves. rev-parse exit code 1 is the
// normal "no" answer, not a failure (the CanFastForward pattern).
func (r *Repo) MergeInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", "MERGE_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse MERGE_HEAD", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
