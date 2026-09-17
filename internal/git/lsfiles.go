package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ListFiles returns tracked paths relative to the working-tree root.
//
// deleted=false is the INDEX's entries (plain `git ls-files`, which is
// `--cached` — they are the same list). That IS the index endpoint's member
// set, and it is the tracked half of the working tree's.
//
// deleted=true is `git ls-files --deleted`: entries the index still holds but
// which are gone from disk. The working-tree member set must SUBTRACT these —
// git has no "files on disk" listing, and calling a `rm`'d file present sends
// a later byte read after a file that is not there.
//
// -z so paths with spaces or non-ASCII bytes (which git otherwise quotes)
// come through raw, matching UntrackedFiles and DiffTreeFiles.
func (r *Repo) ListFiles(ctx context.Context, deleted bool) ([]string, error) {
	b := gitcmd.New("ls-files").Arg("-z").ArgIf(deleted, "--deleted")
	res, err := r.Runner.Run(ctx, "git ls-files (member set)", b.ToArgv())
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
