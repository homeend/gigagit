package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ResolveCommit resolves ref to its full commit SHA
// (`git rev-parse --verify <ref>^{commit}`). Unlike CommitExists (which
// passes -q and turns exit 1 into a clean false), this returns the resolved
// SHA and propagates any error — callers that need a stable, non-injectable
// value (e.g. BranchReviewTarget substituting a branch name into a shell
// command's <range> token) use this to turn an arbitrary ref name into pure
// hex before it ever reaches a command line.
func (r *Repo) ResolveCommit(ctx context.Context, ref string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--verify", ref+"^{commit}").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse verify commit (resolve)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
