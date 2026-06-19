package git

import (
	"context"
	"fmt"
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

// RevParse resolves rev to a full object id (git rev-parse --verify). It errors
// on an unknown/out-of-range revision — callers use that to detect a root
// commit (RevParse(sha+"^") fails when sha has no parent).
func (r *Repo) RevParse(ctx context.Context, rev string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--verify", "--quiet", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse", argv)
	if err != nil {
		return "", err
	}
	out := strings.TrimSpace(res.Stdout)
	if out == "" {
		return "", fmt.Errorf("rev-parse: unknown revision %q", rev)
	}
	return out, nil
}

// CommitMessage returns rev's full commit message (subject + body).
func (r *Repo) CommitMessage(ctx context.Context, rev string) (string, error) {
	argv := gitcmd.New("log").Arg("-1", "--pretty=%B", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git log -1 --pretty=%B", argv)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
