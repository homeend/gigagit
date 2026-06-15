package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Commit records staged changes. When all is true, modified/deleted tracked
// files are staged first (commit -a). When amend is true, it rewrites the last
// commit (commit --amend) instead of creating a new one.
func (r *Repo) Commit(ctx context.Context, message string, all, amend bool) error {
	argv := gitcmd.New("commit").ArgIf(all, "-a").ArgIf(amend, "--amend").Arg("-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git commit", argv)
	return err
}

// LastCommitMessage returns HEAD's full commit message (subject + body).
func (r *Repo) LastCommitMessage(ctx context.Context) (string, error) {
	argv := gitcmd.New("log").Arg("-1", "--pretty=%B").ToArgv()
	res, err := r.Runner.Run(ctx, "git log -1", argv)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// Switch checks out an existing branch.
func (r *Repo) Switch(ctx context.Context, branch string) error {
	argv := gitcmd.New("switch").Arg(branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch", argv)
	return err
}

// CreateBranch creates a new branch without switching to it. An empty
// startPoint means HEAD.
func (r *Repo) CreateBranch(ctx context.Context, name, startPoint string) error {
	argv := gitcmd.New("branch").Arg(name).ArgIf(startPoint != "", startPoint).ToArgv()
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
