package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StagePaths stages the given paths into the index (git add). The "--" guard
// keeps a path that looks like a flag from being parsed as one.
func (r *Repo) StagePaths(ctx context.Context, paths []string) error {
	b := gitcmd.New("add").Arg("--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git add", b.ToArgv())
	return err
}

// UnstagePaths removes the given paths from the index, keeping working-tree
// content (git restore --staged).
func (r *Repo) UnstagePaths(ctx context.Context, paths []string) error {
	b := gitcmd.New("restore").Arg("--staged", "--").Arg(paths...)
	_, err := r.Runner.Run(ctx, "git restore --staged", b.ToArgv())
	return err
}
