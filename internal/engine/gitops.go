package engine

import (
	"context"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
)

// GitOps is the set of git verbs operations use. *git.Repo satisfies it.
// OpDeps.Repo is this interface so operations are decoratable and mockable,
// and a new verb an op needs becomes a visible addition here.
type GitOps interface {
	Status(ctx context.Context) (model.WorkingTreeStatus, error)
	Branches(ctx context.Context) ([]model.Branch, error)
	CurrentBranch(ctx context.Context) (string, error)
	RemoteForBranch(ctx context.Context, branch string) (string, error)
	IsDirty(ctx context.Context) (bool, error)
	LastReflogSubject(ctx context.Context) (string, error)
	TopLevel(ctx context.Context) (string, error)
	Worktrees(ctx context.Context) ([]model.Worktree, error)
	WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error)

	Fetch(ctx context.Context, remote string) error
	Pull(ctx context.Context, remote, branch string, strategy git.PullStrategy) error
	PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error
	FastForwardRef(ctx context.Context, remote, branch string) error
	Push(ctx context.Context, remote, branch string, setUpstream bool) error
	Switch(ctx context.Context, branch string) error
	Commit(ctx context.Context, message string, all, amend bool) error
	LastCommitMessage(ctx context.Context) (string, error)
	ResetSoft(ctx context.Context, ref string) error

	StashList(ctx context.Context) ([]string, error)
	StashPush(ctx context.Context, message string, paths []string, includeUntracked bool) error
	StashPop(ctx context.Context, ref string) error
	StashApply(ctx context.Context, ref string) error
	StashDrop(ctx context.Context, ref string) error
	StashCommit(ctx context.Context, ref string) (string, error)

	CheckRefFormatBranch(ctx context.Context, name string) error
	CreateBranch(ctx context.Context, name, startPoint string) error
	DeleteBranch(ctx context.Context, name string, force bool) error

	AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error
	AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error
	RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error

	Merge(ctx context.Context, dir, branch string) error
	MergeAbort(ctx context.Context, dir string) error
	MergeInProgress(ctx context.Context, dir string) (bool, error)

	Rebase(ctx context.Context, dir, onto string) error
	RebaseInteractive(ctx context.Context, dir, onto string, env []string) error
	HasMergeCommits(ctx context.Context, dir, onto, branch string) (bool, error)
	RebaseAbort(ctx context.Context, dir string) error
	RebaseInProgress(ctx context.Context, dir string) (bool, error)

	StagePaths(ctx context.Context, paths []string) error
	UnstagePaths(ctx context.Context, paths []string) error

	CheckoutSide(ctx context.Context, path, side string) error
	CheckoutBaseStage(ctx context.Context, path string) error
	RemoveFile(ctx context.Context, path string) error
	MergeContinue(ctx context.Context, dir string) error
	RebaseContinue(ctx context.Context, dir string) error
}

// Compile-time proof the concrete repo implements the interface; a drift
// fails the build.
var _ GitOps = (*git.Repo)(nil)
