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

// FindCommit is ResolveCommit for a caller to whom "no such commit" is an
// ANSWER, not a failure: found=false, nil when ref names nothing that peels to
// a commit here. With -q, rev-parse keeps exit 1 for exactly that — an unknown
// name, a gc'd sha, a blob, an empty string — and anything else (exit 128: a
// repository git cannot read) is a real error, returned as one. Folding the
// two together reports a broken checkout as "unknown revision".
func (r *Repo) FindCommit(ctx context.Context, ref string) (sha string, found bool, err error) {
	argv := gitcmd.New("rev-parse").Arg("-q", "--verify", ref+"^{commit}").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse verify commit (find)", argv)
	if err == nil {
		return strings.TrimSpace(res.Stdout), true, nil
	}
	if res.ExitCode == 1 {
		return "", false, nil
	}
	return "", false, err
}
