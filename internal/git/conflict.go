package git

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
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

// MergeHeadName resolves MERGE_HEAD to a local branch name (e.g. "feature") for
// attributing a merge conflict. "" when it cannot be named as a branch.
func (r *Repo) MergeHeadName(ctx context.Context, dir string) (string, error) {
	b := gitcmd.New("name-rev").Arg("--name-only", "--refs=refs/heads/*", "MERGE_HEAD")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git name-rev MERGE_HEAD", b.ToArgv())
	if err != nil {
		return "", err
	}
	return cleanRefName(strings.TrimSpace(res.Stdout)), nil
}

// RebaseParties returns the branch being rebased and the branch it is being
// rebased onto, read from the rebase-merge state. Empty strings (no error) when
// the state is absent or uses a backend we don't model (am/rebase-apply).
func (r *Repo) RebaseParties(ctx context.Context, dir string) (branch, onto string, err error) {
	b := gitcmd.New("rev-parse").Arg("--absolute-git-dir")
	if dir != "" {
		b = b.Dir(dir)
	}
	res, err := r.Runner.Run(ctx, "git rev-parse --absolute-git-dir", b.ToArgv())
	if err != nil {
		return "", "", err
	}
	gitDir := strings.TrimSpace(res.Stdout)
	headName, herr := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "head-name"))
	if herr != nil {
		return "", "", nil // no merge-backend rebase state
	}
	branch = cleanRefName(strings.TrimSpace(string(headName)))
	if ontoSHA, oerr := os.ReadFile(filepath.Join(gitDir, "rebase-merge", "onto")); oerr == nil {
		nb := gitcmd.New("name-rev").Arg("--name-only", "--refs=refs/heads/*", strings.TrimSpace(string(ontoSHA)))
		if dir != "" {
			nb = nb.Dir(dir)
		}
		if nr, nerr := r.Runner.Run(ctx, "git name-rev (onto)", nb.ToArgv()); nerr == nil {
			onto = cleanRefName(strings.TrimSpace(nr.Stdout))
		}
	}
	return branch, onto, nil
}

// cleanRefName strips a refs/heads/ prefix and a name-rev suffix
// (e.g. "feature~2" -> "feature"). name-rev prints the literal "undefined" when
// nothing matches (e.g. merging a tag or a raw commit); that is treated as ""
// so callers show no (mis)attribution rather than the word "undefined".
func cleanRefName(s string) string {
	s = strings.TrimPrefix(s, "refs/heads/")
	if i := strings.IndexAny(s, "~^"); i >= 0 {
		s = s[:i]
	}
	if s == "undefined" {
		return ""
	}
	return s
}
