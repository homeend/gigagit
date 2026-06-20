package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Revert creates a new commit on the branch checked out at dir ("" = this repo's
// own worktree) that undoes commit. A conflict leaves REVERT_HEAD set and
// unmerged paths in the index (detected via RevertInProgress). --no-edit keeps
// the prepared "Revert …" message non-interactive. Reverting a merge commit
// fails outright (git needs -m <parent>); that surfaces as a refused error.
func (r *Repo) Revert(ctx context.Context, dir, commit string) error {
	b := gitcmd.New("revert").Arg("--no-edit", commit)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git revert", b.ToArgv())
	return err
}

// RevertContinue finalizes a revert after conflicts are resolved. GIT_EDITOR=true
// keeps it non-interactive (the prepared message is reused).
func (r *Repo) RevertContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("revert").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git revert --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}

// RevertAbort aborts an in-progress revert at dir ("" = this repo's worktree),
// restoring the pre-revert tip.
func (r *Repo) RevertAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("revert").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git revert --abort", b.ToArgv())
	return err
}

// RevertInProgress reports whether a revert is paused at dir ("" = this repo's
// worktree), i.e. REVERT_HEAD resolves. rev-parse exit code 1 is the normal "no"
// answer, not a failure (the MergeInProgress pattern).
func (r *Repo) RevertInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", "REVERT_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse REVERT_HEAD", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// RevertHeadSummary returns "<short-hash> <subject>" for the commit being
// reverted (REVERT_HEAD), to attribute the conflict. "" when it cannot be read.
func (r *Repo) RevertHeadSummary(ctx context.Context, dir string) (string, error) {
	b := gitcmd.New("log").Arg("-1", "--format=%h %s", "REVERT_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git log REVERT_HEAD", b.ToArgv())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
