package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// DiffNoIndex diffs two absolute filesystem paths outside any git index
// (`git diff --no-index -- <a> <b>`). One invocation. git exits 1 when the
// files differ — the normal "has a diff" outcome, not an error — so exit 1
// is mapped to success with the diff text (the ConfigUnset exit-5 pattern);
// exit 0 is an empty diff (identical). Any other failure propagates.
func (r *Repo) DiffNoIndex(ctx context.Context, a, b string) (string, error) {
	argv := gitcmd.New("diff").Arg("--no-index", "--", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git diff --no-index", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return res.Stdout, nil
		}
		return "", err
	}
	return res.Stdout, nil
}
