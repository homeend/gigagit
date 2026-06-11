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
