package git

import (
	"context"
	"fmt"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CommitParents returns rev's parents as FULL shas, in order
// (`git rev-list --parents -n 1 <rev>`). A root commit answers an empty
// slice. The order is load-bearing for a stash: parent 1 is the HEAD it was
// made on, parent 2 its index, and — only for a `-u` stash — parent 3 the
// parentless commit holding the untracked files.
func (r *Repo) CommitParents(ctx context.Context, rev string) ([]string, error) {
	argv := gitcmd.New("rev-list").Arg("--parents", "-n", "1", rev).ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-list (parents)", argv)
	if err != nil {
		return nil, err
	}
	fields := strings.Fields(res.Stdout)
	if len(fields) == 0 {
		return nil, fmt.Errorf("no commit at %q", rev)
	}
	return fields[1:], nil
}
