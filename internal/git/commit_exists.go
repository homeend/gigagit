package git

import (
	"context"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// CommitExists reports whether ref resolves to a commit object. ref may be any
// commit-ish: a branch, tag, SHA, or revspec like "abc123~2". rev-parse exit
// code 1 is the clean "no such commit" signal (vs a real error).
func (r *Repo) CommitExists(ctx context.Context, ref string) (bool, error) {
	b := gitcmd.New("rev-parse").Arg("-q", "--verify", ref+"^{commit}")
	res, err := r.Runner.Run(ctx, "git rev-parse verify commit", b.ToArgv())
	if err == nil {
		return true, nil
	}
	if res.ExitCode == 1 {
		return false, nil
	}
	return false, err
}
