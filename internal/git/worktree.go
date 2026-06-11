package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// TopLevel returns the absolute path of the current worktree's root
// (`git rev-parse --show-toplevel`).
func (r *Repo) TopLevel(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--show-toplevel").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (toplevel)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// CheckRefFormatBranch reports whether name is a legal git branch name
// (`git check-ref-format --branch`). A non-zero exit (illegal name) is returned
// as an error.
func (r *Repo) CheckRefFormatBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("check-ref-format").Arg("--branch", name).ToArgv()
	_, err := r.Runner.Run(ctx, "git check-ref-format", argv)
	return err
}

// AddWorktree creates a new linked worktree at path on a new branch, based on
// startPoint (`git worktree add -b <branch> <path> <startPoint>`). Output lines
// are forwarded to onLine (nil is allowed) so a frontend can show progress; the
// checkout is cancellable via ctx.
func (r *Repo) AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("add", "-b", branch, path, startPoint).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree add", argv, onLine)
	return err
}

// GitCommonDir returns the absolute path of the repository's common git
// directory (`git rev-parse --path-format=absolute --git-common-dir`). For a
// linked worktree this is the main repo's .git, so per-repo state (e.g. <seq>
// counters) is shared across all worktrees.
func (r *Repo) GitCommonDir(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--path-format=absolute", "--git-common-dir").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (common-dir)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
