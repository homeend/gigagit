package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// IsDirty reports whether the working tree has staged, unstaged, or conflicted
// tracked changes (untracked files alone do not count).
func (r *Repo) IsDirty(ctx context.Context) (bool, error) {
	st, err := r.Status(ctx)
	if err != nil {
		return false, err
	}
	c := st.Counts()
	return c.Staged+c.Unstaged+c.Conflicted > 0, nil
}

// RemoteForBranch returns the remote that branch tracks, defaulting to "origin"
// when the branch has no configured upstream.
func (r *Repo) RemoteForBranch(ctx context.Context, branch string) (string, error) {
	argv := gitcmd.New("for-each-ref").
		Arg("--format=%(upstream:remotename)", "refs/heads/"+branch).ToArgv()
	res, err := r.Runner.Run(ctx, "git for-each-ref (remote)", argv)
	if err != nil {
		return "", err
	}
	remote := strings.TrimSpace(res.Stdout)
	if remote == "" {
		return "origin", nil
	}
	return remote, nil
}
