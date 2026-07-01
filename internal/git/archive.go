package git

import (
	"context"
	"errors"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ArchiveFiles returns a tar archive of the given repo-relative paths as they
// exist at rev (`git archive --format=tar <rev> -- <paths>`). One invocation.
// It refuses an empty path list so a caller can never accidentally archive the
// whole tree (a monorepo hazard). The tar bytes are captured raw; converting
// gitexec's Result.Stdout (a bytes.Buffer.String()) back to []byte is
// byte-preserving.
func (r *Repo) ArchiveFiles(ctx context.Context, rev string, paths []string) ([]byte, error) {
	if len(paths) == 0 {
		return nil, errors.New("git archive: no paths")
	}
	b := gitcmd.New("archive").Arg("--format=tar", rev, "--")
	for _, p := range paths {
		b = b.Arg(p)
	}
	res, err := r.Runner.Run(ctx, "git archive (changed files)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
