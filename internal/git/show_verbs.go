package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// ShowNumstat returns `git show --numstat -z --format=` output for rev,
// optionally scoped to paths — the commit's change stats with no message
// block. On a merge commit git prints an empty combined stat, which renders
// as "no changes" downstream (matching git's own behavior).
func (r *Repo) ShowNumstat(ctx context.Context, rev string, paths []string) (string, error) {
	b := gitcmd.New("show").Arg("--numstat", "-z", "--format=", rev)
	if len(paths) > 0 {
		b.Arg("--").Arg(paths...)
	}
	res, err := r.Runner.Run(ctx, "git show", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// ShowPatch returns rev's full patch (no message block), optionally scoped
// to paths.
func (r *Repo) ShowPatch(ctx context.Context, rev string, paths []string) (string, error) {
	b := gitcmd.New("show").Arg("--patch", "--format=", rev)
	if len(paths) > 0 {
		b.Arg("--").Arg(paths...)
	}
	res, err := r.Runner.Run(ctx, "git show", b.ToArgv())
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}
