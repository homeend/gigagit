package git

import (
	"context"
	"strings"

	"github.com/gigagit/gg/internal/gitcmd"
)

// Commit records staged changes. When all is true, modified/deleted tracked
// files are staged first (commit -a). When amend is true, it rewrites the last
// commit (commit --amend) instead of creating a new one.
func (r *Repo) Commit(ctx context.Context, message string, all, amend bool) error {
	argv := gitcmd.New("commit").ArgIf(all, "-a").ArgIf(amend, "--amend").Arg("-m", message).ToArgv()
	_, err := r.Runner.Run(ctx, "git commit", argv)
	return err
}

// LastCommitMessage returns HEAD's full commit message (subject + body).
func (r *Repo) LastCommitMessage(ctx context.Context) (string, error) {
	argv := gitcmd.New("log").Arg("-1", "--pretty=%B").ToArgv()
	res, err := r.Runner.Run(ctx, "git log -1", argv)
	if err != nil {
		return "", err
	}
	return res.Stdout, nil
}

// Switch checks out an existing branch.
func (r *Repo) Switch(ctx context.Context, branch string) error {
	argv := gitcmd.New("switch").Arg(branch).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch", argv)
	return err
}

// SwitchDetach checks out ref with a detached HEAD (git switch --detach).
func (r *Repo) SwitchDetach(ctx context.Context, ref string) error {
	argv := gitcmd.New("switch").Arg("--detach", ref).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch --detach", argv)
	return err
}

// SwitchCreate creates branch at start and switches to it in one invocation
// (git switch -c). Atomic: on failure no branch is left behind.
func (r *Repo) SwitchCreate(ctx context.Context, branch, start string) error {
	argv := gitcmd.New("switch").Arg("-c", branch).ArgIf(start != "", start).ToArgv()
	_, err := r.Runner.Run(ctx, "git switch -c", argv)
	return err
}

// CreateBranch creates a new branch without switching to it. An empty
// startPoint means HEAD.
func (r *Repo) CreateBranch(ctx context.Context, name, startPoint string) error {
	argv := gitcmd.New("branch").Arg(name).ArgIf(startPoint != "", startPoint).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch", argv)
	return err
}

// CreateTag creates a tag at commit (empty commit = HEAD). A non-empty message
// makes it annotated (git tag -a -m); otherwise it is lightweight. git refuses
// an existing tag name.
func (r *Repo) CreateTag(ctx context.Context, name, commit, message string) error {
	argv := gitcmd.New("tag").
		ArgIf(message != "", "-a", "-m", message).
		Arg(name).
		ArgIf(commit != "", commit).
		ToArgv()
	_, err := r.Runner.Run(ctx, "git tag", argv)
	return err
}

// DeleteTag deletes a tag (git tag -d). git errors if it does not exist.
func (r *Repo) DeleteTag(ctx context.Context, name string) error {
	_, err := r.Runner.Run(ctx, "git tag -d", gitcmd.New("tag").Arg("-d", name).ToArgv())
	return err
}

// RenameBranch renames local branch oldName to newName (git branch -m). git
// refuses when newName already exists; renaming a branch checked out in another
// worktree succeeds and updates that worktree's HEAD.
func (r *Repo) RenameBranch(ctx context.Context, oldName, newName string) error {
	argv := gitcmd.New("branch").Arg("-m", oldName, newName).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch -m", argv)
	return err
}

// LocalBranchExists reports whether refs/heads/<name> exists.
func (r *Repo) LocalBranchExists(ctx context.Context, name string) (bool, error) {
	argv := gitcmd.New("show-ref").Arg("--verify", "--quiet", "refs/heads/"+name).ToArgv()
	res, err := r.Runner.Run(ctx, "git show-ref (branch exists)", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// IsAncestor reports whether commit a is an ancestor of commit b (a fast-forward
// from a to b is possible). a == b counts as true.
func (r *Repo) IsAncestor(ctx context.Context, a, b string) (bool, error) {
	argv := gitcmd.New("merge-base").Arg("--is-ancestor", a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base --is-ancestor", argv)
	if err != nil {
		if res.ExitCode == 1 {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// CreateTrackingBranch creates refs/heads/<name> at <upstream> with tracking
// configured, without switching to it.
func (r *Repo) CreateTrackingBranch(ctx context.Context, name, upstream string) error {
	argv := gitcmd.New("branch").Arg("--track", name, upstream).ToArgv()
	_, err := r.Runner.Run(ctx, "git branch --track", argv)
	return err
}

// CurrentBranch returns the checked-out branch name, or "" if detached.
func (r *Repo) CurrentBranch(ctx context.Context) (string, error) {
	argv := gitcmd.New("symbolic-ref").Arg("--quiet", "--short", "HEAD").ToArgv()
	res, err := r.Runner.Run(ctx, "git symbolic-ref", argv)
	if err != nil {
		// Detached HEAD: symbolic-ref exits 1. Treat as no branch; surface
		// any other exit code (e.g. 128 = not a repo) as a real error.
		if res.ExitCode == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
