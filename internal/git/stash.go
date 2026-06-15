package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// StashPush saves the working-tree changes for the given paths (all changes
// when paths is empty) to a new stash with the message, leaving them reverted.
// includeUntracked adds -u so untracked paths are stashable.
func (r *Repo) StashPush(ctx context.Context, message string, paths []string, includeUntracked bool) error {
	b := gitcmd.New("stash").Arg("push", "-m", message).ArgIf(includeUntracked, "-u")
	if len(paths) > 0 {
		b = b.Arg("--").Arg(paths...)
	}
	_, err := r.Runner.Run(ctx, "git stash push", b.ToArgv())
	return err
}

// StashPop restores ref (newest when ref is "") and drops it. A conflict leaves
// the stash in place and returns an error (git's behavior).
func (r *Repo) StashPop(ctx context.Context, ref string) error {
	b := gitcmd.New("stash").Arg("pop").ArgIf(ref != "", ref)
	_, err := r.Runner.Run(ctx, "git stash pop", b.ToArgv())
	return err
}

// StashApply restores ref into the working tree, keeping the stash.
func (r *Repo) StashApply(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git stash apply", gitcmd.New("stash").Arg("apply", ref).ToArgv())
	return err
}

// StashDrop deletes ref without applying it.
func (r *Repo) StashDrop(ctx context.Context, ref string) error {
	_, err := r.Runner.Run(ctx, "git stash drop", gitcmd.New("stash").Arg("drop", ref).ToArgv())
	return err
}

// StashCommit resolves a stash ref (e.g. stash@{0}) to its commit SHA so the
// file tree / diff can read it as an ordinary commit.
func (r *Repo) StashCommit(ctx context.Context, ref string) (string, error) {
	res, err := r.Runner.Run(ctx, "git rev-parse (stash)", gitcmd.New("rev-parse").Arg(ref).ToArgv())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// StashList returns the stash entries, newest first (one description per line).
func (r *Repo) StashList(ctx context.Context) ([]string, error) {
	argv := gitcmd.New("stash").Arg("list").ToArgv()
	res, err := r.Runner.Run(ctx, "git stash list", argv)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, line := range strings.Split(strings.TrimRight(res.Stdout, "\n"), "\n") {
		if strings.TrimSpace(line) != "" {
			out = append(out, line)
		}
	}
	return out, nil
}
