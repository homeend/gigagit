package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Commit records staged changes. When all is true, modified/deleted tracked
// files are staged first (commit -a).
func (r *Repo) Commit(ctx context.Context, message string, all bool) error {
	argv := gitcmd.New("commit").ArgIf(all, "-a").Arg("-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git commit", argv)
	return err
}

// Switch checks out an existing branch.
func (r *Repo) Switch(ctx context.Context, branch string) error {
	argv := gitcmd.New("switch").Arg(branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch", argv)
	return err
}

// CreateBranch creates a new branch at HEAD without switching to it.
func (r *Repo) CreateBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("branch").Arg(name).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch", argv)
	return err
}

// CurrentBranch returns the checked-out branch name, or "" if detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	argv := gitcmd.New("symbolic-ref").Arg("--quiet", "--short", "HEAD").ToArgv()
	res, err := r.Runner.Run(ctx, "git symbolic-ref", argv)
	if err != nil {
		// Detached HEAD: symbolic-ref exits 1. Treat as no branch; surface
		// any other exit code (e.g. 128 = not a repo) as a real error.
		if res.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
