package git

import (
	"context"

	"github.com/gigagit/gg/internal/gitcmd"
)

// CheckoutSide restores the "ours" (stage 2) or "theirs" (stage 3) version of a
// conflicted path into the working tree. Caller stages it to mark resolved.
func (r *Repo) CheckoutSide(ctx context.Context, path, side string) error {
	_, err := r.Runner.Run(ctx, "git checkout --"+side,
		gitcmd.New("checkout").Arg("--"+side, "--", path).ToArgv())
	return err
}

// CheckoutBaseStage restores the common-ancestor (stage 1) version of a
// conflicted path. Errors if there is no stage 1 (e.g. added-by-both).
func (r *Repo) CheckoutBaseStage(ctx context.Context, path string) error {
	_, err := r.Runner.Run(ctx, "git checkout-index (base)",
		gitcmd.New("checkout-index").Arg("--stage=1", "-f", "--", path).ToArgv())
	return err
}

// RemoveFile removes a path from the working tree and index (resolves a
// modify/delete conflict toward deletion). -f forces past git rm's "local
// modifications" guard: the conflicted worktree copy differs from the index.
func (r *Repo) RemoveFile(ctx context.Context, path string) error {
	_, err := r.Runner.Run(ctx, "git rm", gitcmd.New("rm").Arg("-f", "--", path).ToArgv())
	return err
}

// MergeContinue finalizes a merge after conflicts are resolved. GIT_EDITOR=true
// keeps it non-interactive (the prepared MERGE_MSG is used unchanged).
func (r *Repo) MergeContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("merge").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git merge --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}

// RebaseContinue resumes a rebase after conflicts are resolved, non-interactive.
func (r *Repo) RebaseContinue(ctx context.Context, dir string) error {
	b := gitcmd.New("rebase").Arg("--continue")
	if dir != "" {
		b = b.Dir(dir)
	}
	_, err := r.Runner.RunEnv(ctx, "git rebase --continue", b.ToArgv(), []string{"GIT_EDITOR=true"})
	return err
}
