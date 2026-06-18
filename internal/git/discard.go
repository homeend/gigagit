package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// RestoreWorktree resets the given paths in the working tree to the index
// (git restore --worktree), discarding unstaged changes while keeping any
// staged hunks. Pass ":/" to restore the entire tree from the repo root.
func (r *Repo) RestoreWorktree(ctx context.Context, paths []string) error {
	b := gitcmd.New("restore").Arg("--worktree", "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git restore --worktree", b.ToArgv())
	return err
}

// CleanUntracked removes untracked files and directories (git clean -f -d).
// Empty paths cleans the whole working tree. The "--" guard is added only when
// paths are present so the all-paths call stays a bare `git clean -f -d`.
func (r *Repo) CleanUntracked(ctx context.Context, paths []string) error {
	b := gitcmd.New("clean").Arg("-f", "-d").ArgIf(len(paths) > 0, "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git clean -f -d", b.ToArgv())
	return err
}
