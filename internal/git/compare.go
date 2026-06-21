package git

import (
	"context"
	"fmt"

	"github.com/gigagit/gg/internal/gitcmd"
	"github.com/gigagit/gg/internal/model"
)

// DiffTreeFiles lists the files that differ between two endpoints, with left as
// the older side and right the newer. It supports only the four forward kind
// pairs the UI ever produces; any other pair is a programming error.
//
//	commit A → commit B : git diff --name-status -M A B
//	commit A → index    : git diff --cached --name-status -M A
//	commit A → worktree : git diff --name-status -M A
//	index   → worktree  : git diff --name-status -M
func (r *Repo) DiffTreeFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	b := gitcmd.New("diff").Arg("--name-status", "-M")
	switch {
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointCommit:
		b = b.Arg(left.Hash, right.Hash)
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointIndex:
		b = b.Arg("--cached", left.Hash)
	case left.Kind == model.EndpointCommit && right.Kind == model.EndpointWorkTree:
		b = b.Arg(left.Hash)
	case left.Kind == model.EndpointIndex && right.Kind == model.EndpointWorkTree:
		// bare `git diff` already compares index → working tree.
	default:
		return nil, fmt.Errorf("DiffTreeFiles: unsupported endpoint pair %d → %d", left.Kind, right.Kind)
	}
	res, err := r.Runner.Run(ctx, "git diff (compare files)", b.ToArgv())
	if err != nil {
		return nil, err
	}
	return ParseNameStatus([]byte(res.Stdout)), nil
}
