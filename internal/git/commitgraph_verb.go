package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// WriteCommitGraph writes/refreshes the commit-graph cache
// (`git commit-graph write --reachable`), letting later `git log --date-order`
// use generation numbers instead of parsing every commit. Writes atomically;
// safe to run alongside reads.
func (r *Repo) WriteCommitGraph(ctx context.Context) error {
	argv := gitcmd.New("commit-graph").Arg("write", "--reachable").ToArgv()
	_, err := r.Runner.Run(ctx, "git commit-graph write", argv)
	return err
}
