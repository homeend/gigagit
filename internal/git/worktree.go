package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// TopLevel returns the absolute path of the current worktree's root
// (`git rev-parse --show-toplevel`).
func (r *Repo) TopLevel(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--show-toplevel").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (toplevel)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// CheckRefFormatBranch reports whether name is a legal git branch name
// (`git check-ref-format --branch`). A non-zero exit (illegal name) is returned
// as an error.
func (r *Repo) CheckRefFormatBranch(ctx context.Context, name string) error {
	argv := gitcmd.New("check-ref-format").Arg("--branch", name).ToArgv()
	_, err := r.Runner.Run(ctx, "git check-ref-format", argv)
	return err
}

// AddWorktree creates a new linked worktree at path on a new branch, based on
// startPoint (`git worktree add -b <branch> <path> <startPoint>`). Output lines
// are forwarded to onLine (nil is allowed) so a frontend can show progress; the
// checkout is cancellable via ctx.
func (r *Repo) AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("add", "-b", branch, path, startPoint).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree add", argv, onLine)
	return err
}

// AddWorktreeForBranch creates a linked worktree at path that checks out the
// EXISTING branch (`git worktree add <path> <branch>`). Output lines are
// forwarded to onLine (nil is allowed); the checkout is cancellable via ctx.
// Callers must ensure branch exists locally — git would otherwise DWIM a
// remote-tracking branch into existence.
func (r *Repo) AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("add", path, branch).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree add", argv, onLine)
	return err
}

// GitCommonDir returns the absolute path of the repository's common git
// directory (`git rev-parse --path-format=absolute --git-common-dir`). For a
// linked worktree this is the main repo's .git, so per-repo state (e.g. <seq>
// counters) is shared across all worktrees.
func (r *Repo) GitCommonDir(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--path-format=absolute", "--git-common-dir").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (common-dir)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// GitDir returns the absolute path of THIS worktree's git directory
// (`git rev-parse --path-format=absolute --absolute-git-dir`). For the main
// worktree this equals GitCommonDir; for a linked worktree it is the
// per-worktree dir under <common>/worktrees/<name>, which holds this worktree's
// HEAD and logs/HEAD.
func (r *Repo) GitDir(ctx context.Context) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--path-format=absolute", "--absolute-git-dir").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (git-dir)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// RemoveWorktree removes the linked worktree at path
// (`git worktree remove [--force] <path>`). onLine receives any output lines
// (nil is allowed; git currently emits none on success). A non-zero exit
// (e.g. a dirty tree without force) is returned as an error.
func (r *Repo) RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error {
	if onLine == nil {
		onLine = func(string) {}
	}
	argv := gitcmd.New("worktree").Arg("remove").ArgIf(force, "--force").Arg(path).ToArgv()
	_, err := r.Runner.Stream(ctx, "git worktree remove", argv, onLine)
	return err
}

// UnlockWorktree releases a worktree's lock (`git worktree unlock <path>`). An
// interrupted `git worktree add` leaves the new worktree locked with reason
// "initializing", which blocks even `remove --force`; unlocking clears it.
func (r *Repo) UnlockWorktree(ctx context.Context, path string) error {
	argv := gitcmd.New("worktree").Arg("unlock", path).ToArgv()
	_, err := r.Runner.Run(ctx, "git worktree unlock", argv)
	return err
}

// PruneWorktrees drops stale $GIT_DIR/worktrees admin entries left by
// deleted worktree directories (git worktree prune).
func (r *Repo) PruneWorktrees(ctx context.Context) error {
	argv := gitcmd.New("worktree").Arg("prune").ToArgv()
	_, err := r.Runner.Run(ctx, "git worktree prune", argv)
	return err
}

// DeleteBranch deletes a local branch (`git branch -d|-D <name>`). Without force
// git refuses to delete a branch that is not fully merged; force uses -D.
func (r *Repo) DeleteBranch(ctx context.Context, name string, force bool) error {
	flag := "-d"
	if force {
		flag = "-D"
	}
	argv := gitcmd.New("branch").Arg(flag, name).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch (delete)", argv)
	return err
}
