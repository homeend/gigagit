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

// LocalBranchExists reports whether refs/heads/<name> exists.
func (r *Repo) LocalBranchExists(ctx context.Context, name string) (bool, error) {
	argv := gitcmd.New("show-ref").Arg("--verify", "--quiet", "refs/heads/"+name).ToArgv()
	res, err := r.Runner.Run(ctx, "git show-ref (branch exists)", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsAncestor reports whether commit a is an ancestor of commit b (a fast-forward
// from a to b is possible). a == b counts as true.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	argv := gitcmd.New("merge-base").Arg("--is-ancestor", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base --is-ancestor", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateTrackingBranch creates refs/heads/<name> at <upstream> with tracking
// configured, without switching to it.
func (r *Repo) CreateTrackingBranch(ctx context.Context, name, upstream string) error {
	argv := gitcmd.New("branch").Arg("--track", name, upstream).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch --track", argv)
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
