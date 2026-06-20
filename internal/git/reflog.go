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

// Reset moves the current branch to ref with the given mode:
//   - "soft":  index + working tree kept (the diff since ref stays staged)
//   - "mixed": index reset, working tree kept (the diff stays unstaged)
//   - "hard":  index + working tree reset (uncommitted TRACKED changes discarded;
//     untracked files survive)
func (r *Repo) Reset(ctx context.Context, mode, ref string) error {
	argv := gitcmd.New("reset").Arg("--"+mode, ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git reset --"+mode, argv)
	return err
}
