package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CommitGraphWrite writes the repo's commit-graph file
// (`git commit-graph write --reachable`) — the derived cache that makes
// ordered commit walks (--date-order paging) near-flat on big repos. Output
// lines are forwarded to onLine (nil allowed; git's progress goes to stderr,
// which Stream buffers rather than forwards, so onLine is usually silent);
// the write is cancellable via ctx — it can take ~a minute on a
// million-commit repo.
func (r *Repo) CommitGraphWrite(ctx context.Context, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("commit-graph").Arg("write", "--reachable").ToArgv()
	_, err := r.Runner.Stream(ctx, "git commit-graph write", argv, onLine)
	return err
}
