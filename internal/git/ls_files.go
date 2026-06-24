package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// LsFiles returns every tracked file (paths relative to the working-tree root).
// NUL-delimited (-z) so paths with spaces or non-ASCII bytes come through raw.
func (r *Repo) LsFiles(ctx context.Context) ([]string, error) {
	res, err := r.Runner.Run(ctx, "git ls-files",
		gitcmd.New("ls-files").Arg("-z").ToArgv())
	if err != nil {
		return nil, err
	}
	var out []string
	for _, p := range strings.Split(res.Stdout, "\x00") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}
