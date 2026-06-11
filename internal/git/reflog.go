package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// LastReflogSubject returns the subject of the most recent HEAD reflog entry,
// e.g. "commit: add foo" or "checkout: moving from main to dev". Returns "" if
// there is no reflog.
func (r *Repo) LastReflogSubject(ctx context.Context) (string, error) {
	argv := gitcmd.New("reflog").Arg("-1", "--format=%gs").ToArgv()
	res, err := r.Runner.Run(ctx, "git reflog", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// ResetSoft moves the current branch to ref, leaving the index and working tree
// unchanged (git reset --soft). The undone commit's changes remain staged.
func (r *Repo) ResetSoft(ctx context.Context, ref string) error {
	argv := gitcmd.New("reset").Arg("--soft", ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git reset --soft", argv)
	return err
}
