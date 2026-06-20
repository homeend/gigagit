package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CherryPick applies commit onto the branch checked out at dir ("" = this
// repo's own worktree) as a new commit. A conflict leaves CHERRY_PICK_HEAD set
// and unmerged paths in the index (detected via CherryPickInProgress).
func (r *Repo) CherryPick(ctx context.Context, dir, commit string) error {
	b := gitcmd.New("cherry-pick").Arg(commit)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git cherry-pick", b.ToArgv())
	return err
}

// CherryPickContinue finalizes a cherry-pick after conflicts are resolved.
// GIT_EDITOR=true keeps it non-interactive (the original message is reused).
func (r *Repo) CherryPickContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("cherry-pick").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git cherry-pick --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}

// CherryPickAbort aborts an in-progress cherry-pick at dir ("" = this repo's
// worktree), restoring the pre-cherry-pick tip.
func (r *Repo) CherryPickAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("cherry-pick").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git cherry-pick --abort", b.ToArgv())
	return err
}

// CherryPickInProgress reports whether a cherry-pick is paused at dir ("" =
// this repo's worktree), i.e. CHERRY_PICK_HEAD resolves. rev-parse exit code 1
// is the normal "no" answer, not a failure (the MergeInProgress pattern).
//
// NOTE: a paused interactive-rebase *pick* also sets CHERRY_PICK_HEAD, so
// callers that distinguish ops must probe RebaseInProgress BEFORE this.
func (r *Repo) CherryPickInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", "CHERRY_PICK_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse CHERRY_PICK_HEAD", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}

// CherryPickHeadSummary returns "<short-hash> <subject>" for the commit being
// cherry-picked (CHERRY_PICK_HEAD), to attribute the conflict. "" when it
// cannot be read.
func (r *Repo) CherryPickHeadSummary(ctx context.Context, dir string) (string, error) {
	b := gitcmd.New("log").Arg("-1", "--format=%h %s", "CHERRY_PICK_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git log CHERRY_PICK_HEAD", b.ToArgv())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
