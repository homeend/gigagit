package git

import (
	"context"
	"strings"

	"github.com/homeend/gigagit/internal/gitcmd"
)

// MergeBase returns the best common ancestor of a and b
// (`git merge-base <a> <b>`). One invocation. An error (no common ancestor, a
// bad ref) propagates to the caller — BranchReviewTarget falls back to the
// upstream ref, then the tip alone.
func (r *Repo) MergeBase(ctx context.Context, a, b string) (string, error) {
	argv := gitcmd.New("merge-base").Arg(a, b).ToArgv()
	res, err := r.Runner.Run(ctx, "git merge-base", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}

// UpstreamRef returns ref's configured upstream branch
// (`git rev-parse --abbrev-ref <ref>@{upstream}`), e.g. "origin/feature". One
// invocation. A ref with no upstream configured errors — callers (e.g.
// BranchReviewTarget) treat any error as "no upstream".
func (r *Repo) UpstreamRef(ctx context.Context, ref string) (string, error) {
	argv := gitcmd.New("rev-parse").Arg("--abbrev-ref", ref+"@{upstream}").ToArgv()
	res, err := r.Runner.Run(ctx, "git rev-parse (upstream)", argv)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
