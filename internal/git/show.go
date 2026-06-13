package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// ShowFile returns the raw blob content of path at rev (`git show
// <rev>:<path>`). The path is repo-root-relative regardless of the process
// cwd; no textconv or smudge filters apply (callers see the stored bytes).
// One invocation. A path absent from rev is an error — callers only ask for
// sides they expect to exist.
func (r *Repo) ShowFile(ctx context.Context, rev, path string) ([]byte, error) {
	argv := gitcmd.New("show").Arg(rev + ":" + path).ToArgv()
	res, err := r.Runner.Run(ctx, "git show", argv)
	if err != nil {
		return nil, err
	}
	return []byte(res.Stdout), nil
}
