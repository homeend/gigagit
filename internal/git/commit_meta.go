package git

import (
	"context"
	"fmt"

	"github.com/homeend/gigagit/internal/gitcmd"
	"github.com/homeend/gigagit/internal/model"
)

// CommitMeta returns one commit's metadata — hash, parents, author, AUTHOR time
// (%at, the field every gg surface renders as "the commit's date"), subject and
// ref decorations — in ONE invocation, with no walk.
//
// It exists for the surfaces that are handed a bare sha and never see the feed
// row behind it: the files view opened from Tags / goto-SHA / the reflog, whose
// synthesized model.Commit carries only a hash. Deliberately NOT CommitTimes,
// which reports %ct (committer time) and would disagree with the date the
// commit-message popup shows for the same commit.
//
// The format and parser are the feed's (logFormat/ParseLog), so a commit read
// here is byte-identical to the same commit read by the walk.
func (r *Repo) CommitMeta(ctx context.Context, rev string) (model.Commit, error) {
	argv := gitcmd.New("log").
		Arg("-1", "--decorate=full", "--decorate-refs-exclude=refs/gg/*", "--source", "--format="+logFormat, rev).
		ToArgv()
	res, err := r.Runner.Run(ctx, "git log", argv)
	if err != nil {
		return model.Commit{}, err
	}
	commits, err := ParseLog([]byte(res.Stdout))
	if err != nil {
		return model.Commit{}, err
	}
	if len(commits) == 0 {
		return model.Commit{}, fmt.Errorf("no commit at %q", rev)
	}
	return commits[0], nil
}
