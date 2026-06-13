package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Rebase replays the branch checked out at dir ("" = this repo's own
// worktree) onto onto. Plain `git rebase <onto>` is non-interactive by default
// (no editor unless -i). Non-interactive replay only (no -i, no --onto form).
func (r *Repo) Rebase(ctx context.Context, dir, onto string) error {
	b := gitcmd.New("rebase").Arg(onto)
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase", b.ToArgv())
	return err
}

// RebaseAbort aborts an in-progress rebase at dir ("" = this repo's worktree),
// restoring the pre-rebase tip.
func (r *Repo) RebaseAbort(ctx context.Context, dir string) error {
	b := gitcmd.New("rebase").Arg("--abort")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase --abort", b.ToArgv())
	return err
}

// RebaseInProgress reports whether a rebase is paused at dir ("" = this repo's
// worktree). `git rebase --show-current-patch` exits 0 when a patch is current
// (a rebase is paused, e.g. on a conflict) and non-zero ("No rebase in
// progress?") when none — the exit-code analogue of MergeInProgress. Unlike a
// merge there is no reliable single ref (REBASE_HEAD is version-dependent), so
// this exit-code probe is used; it also honors -C dir for the worktree rung.
func (r *Repo) RebaseInProgress(ctx context.Context, dir string) (bool, error) {
	b := gitcmd.New("rebase").Arg("--show-current-patch")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.Run(ctx, "git rebase --show-current-patch", b.ToArgv())
	if err == nil {
		return true, nil // exit 0: a patch is current → a rebase is paused
	}
	return false, nil // non-zero: no rebase in progress
}
