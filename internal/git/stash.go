package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StashPush saves the working tree and index to a new stash with the given
// message, leaving a clean tree.
func (r *Repo) StashPush(ctx context.Context, message string) error {
	argv := gitcmd.New("stash").Arg("push", "-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git stash push", argv)
	return err
}

// StashPop restores the most recent stash and drops it. A conflict leaves the
// stash in place and returns an error (git's behavior).
func (r *Repo) StashPop(ctx context.Context) error {
	argv := gitcmd.New("stash").Arg("pop").ToArgv()
	_, err := r.Runner.Run(ctx, "git stash pop", argv)
	return err
}

// StashList returns the stash entries, newest first (one description per line).
func (r *Repo) StashList(ctx context.Context) ([]string, error) {
	argv := gitcmd.New("stash").Arg("list").ToArgv()
	res, err := r.Runner.Run(ctx, "git stash list", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
