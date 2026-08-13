package engine

import (
	"context"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// GitOps is the set of git verbs operations use. *git.Repo satisfies it and is
// the only production implementor. OpDeps.Repo is this interface so the repo
// is decoratable (LimitRunner-style wrapping composes underneath) and a new
// verb an op needs becomes a visible addition here. Ops are tested against a
// real git repo in a t.TempDir() (convention: newTestRepo), not mocks; the
// rare pure-unit case nil-embeds GitOps and implements only the verbs under
// test (see writefile_test.go).
type GitOps interface {
	Status(ctx context.Context) (model.WorkingTreeStatus, error)
	Branches(ctx context.Context) ([]model.Branch, error)
	CurrentBranch(ctx context.Context) (string, error)
	RemoteForBranch(ctx context.Context, branch string) (string, error)
	IsDirty(ctx context.Context) (bool, error)
	LastReflogSubject(ctx context.Context) (string, error)
	TopLevel(ctx context.Context) (string, error)
	// GitDir is THIS worktree's git dir; GitCommonDir is the shared one. They
	// differ for a linked worktree, and both are needed to bound where
	// RemoveGitLocks is allowed to delete: index.lock/HEAD.lock are
	// per-worktree, packed-refs.lock/config.lock live in the common dir.
	GitDir(ctx context.Context) (string, error)
	GitCommonDir(ctx context.Context) (string, error)
	Worktrees(ctx context.Context) ([]model.Worktree, error)
	WorktreeForBranch(ctx context.Context, branch string) (*model.Worktree, error)
	LogRangeMessages(ctx context.Context, onto, branch string) ([]model.RangeCommit, error)

	Fetch(ctx context.Context, remote string) error
	FetchAll(ctx context.Context) error
	RemoteNames(ctx context.Context) ([]string, error)
	PruneRemotes(ctx context.Context, names ...string) error
	Pull(ctx context.Context, remote, branch string, strategy git.PullStrategy) error
	PullInWorktree(ctx context.Context, worktreePath, remote, branch string) error
	FastForwardRef(ctx context.Context, remote, branch string) error
	Push(ctx context.Context, remote, branch string, setUpstream bool, force git.PushForce) error
	PushTag(ctx context.Context, remote, name string) error
	PushTags(ctx context.Context, remote string, names []string) error
	PushDelete(ctx context.Context, remote, branch string) error
	PushDeleteTag(ctx context.Context, remote, tag string) error
	Switch(ctx context.Context, branch string) error
	SwitchDetach(ctx context.Context, ref string) error
	SwitchCreate(ctx context.Context, branch, start string) error
	Commit(ctx context.Context, message string, all, amend bool) error
	LastCommitMessage(ctx context.Context) (string, error)
	CommitMessage(ctx context.Context, rev string) (string, error)
	CommitLine(ctx context.Context, rev string) (model.LogLine, error)
	LogLines(ctx context.Context, rev string, n int) ([]model.LogLine, error)
	RevParse(ctx context.Context, rev string) (string, error)
	ResetSoft(ctx context.Context, ref string) error

	DiffPatch(ctx context.Context, spec model.DiffSpec) (string, error)
	DiffNumstat(ctx context.Context, spec model.DiffSpec) (string, error)

	StashList(ctx context.Context) ([]string, error)
	StashPush(ctx context.Context, message string, paths []string, includeUntracked bool) error
	StashPop(ctx context.Context, ref string) error
	StashApply(ctx context.Context, ref string) error
	StashDrop(ctx context.Context, ref string) error
	StashCommit(ctx context.Context, ref string) (string, error)

	CheckRefFormatBranch(ctx context.Context, name string) error
	CreateBranch(ctx context.Context, name, startPoint string) error
	UpdateRef(ctx context.Context, ref, sha string) error
	DeleteRef(ctx context.Context, ref string) error
	ForEachRef(ctx context.Context, prefix string) ([]model.RefInfo, error)
	CreateTag(ctx context.Context, name, commit, message string, force bool) error
	DeleteTag(ctx context.Context, name string) error
	RenameBranch(ctx context.Context, oldName, newName string) error
	CreateTrackingBranch(ctx context.Context, name, upstream string) error
	DeleteBranch(ctx context.Context, name string, force bool) error
	LocalBranchExists(ctx context.Context, name string) (bool, error)
	IsAncestor(ctx context.Context, a, b string) (bool, error)
	CommitExists(ctx context.Context, ref string) (bool, error)
	FastForwardToRef(ctx context.Context, branch, source string) error

	AddWorktree(ctx context.Context, path, branch, startPoint string, onLine func(string)) error
	AddWorktreeForBranch(ctx context.Context, path, branch string, onLine func(string)) error
	RemoveWorktree(ctx context.Context, path string, force bool, onLine func(string)) error
	UnlockWorktree(ctx context.Context, path string) error
	PruneWorktrees(ctx context.Context) error
	WorktreeRepair(ctx context.Context, path string) error
	MoveWorktree(ctx context.Context, fromDir, path, dest string, onLine func(string)) error
	// ParentCount reports how many parents rev has (0 root, 1 normal, ≥2
	// merge) — the keep-modes pre-check before any worktree is created.
	ParentCount(ctx context.Context, rev string) (int, error)
	// ResetInDir resets another worktree's checkout (git -C dir reset).
	ResetInDir(ctx context.Context, dir, ref string, soft bool) error

	Merge(ctx context.Context, dir, branch string) error
	MergeFFOnly(ctx context.Context, dir, commit string) error
	MergeAbort(ctx context.Context, dir string) error
	MergeInProgress(ctx context.Context, dir string) (bool, error)

	Rebase(ctx context.Context, dir, onto string) error
	RebaseInteractive(ctx context.Context, dir, onto string, env []string) error
	HasMergeCommits(ctx context.Context, dir, onto, branch string) (bool, error)
	RebaseAbort(ctx context.Context, dir string) error
	RebaseInProgress(ctx context.Context, dir string) (bool, error)

	CherryPick(ctx context.Context, dir string, commits ...string) error
	CherryPickSkip(ctx context.Context, dir string) error
	CherryPickAbort(ctx context.Context, dir string) error
	CherryPickContinue(ctx context.Context, dir string) error
	CherryPickInProgress(ctx context.Context, dir string) (bool, error)
	CherryPickHeadSummary(ctx context.Context, dir string) (string, error)

	Revert(ctx context.Context, dir, commit string) error
	RevertAbort(ctx context.Context, dir string) error
	RevertContinue(ctx context.Context, dir string) error
	RevertInProgress(ctx context.Context, dir string) (bool, error)
	RevertHeadSummary(ctx context.Context, dir string) (string, error)

	Reset(ctx context.Context, mode, ref string) error

	ConfigSet(ctx context.Context, scope git.ConfigScope, key, value string) error
	ConfigUnset(ctx context.Context, scope git.ConfigScope, key string) error
	ConfigAdd(ctx context.Context, scope git.ConfigScope, key, value string) error
	ConfigGetAll(ctx context.Context, key string) ([]string, error)
	FetchBranches(ctx context.Context, remote string, branches []string) error
	CommitGraphWrite(ctx context.Context, onLine func(string)) error

	StagePaths(ctx context.Context, paths []string) error
	StageAll(ctx context.Context) error
	UnstagePaths(ctx context.Context, paths []string) error
	RestoreWorktree(ctx context.Context, paths []string) error
	CleanUntracked(ctx context.Context, paths []string) error

	CheckoutSide(ctx context.Context, path, side string) error
	CheckoutBaseStage(ctx context.Context, path string) error
	RemoveFile(ctx context.Context, path string) error
	ReadWorktreeFile(ctx context.Context, path string) ([]byte, error)
	WriteWorktreeFile(ctx context.Context, path string, content []byte) error
	StageBlob(ctx context.Context, path string, content []byte) error
	ApplyPatch(ctx context.Context, path string, threeWay bool) error
	PatchPaths(ctx context.Context, path string) ([]string, error)
	AmMailbox(ctx context.Context, path string, threeWay bool) error
	AmAbort(ctx context.Context) error
	AmInProgress(ctx context.Context) (bool, error)
	MergeContinue(ctx context.Context, dir string) error
	RebaseContinue(ctx context.Context, dir string) error
}

// Compile-time proof the concrete repo implements the interface; a drift
// fails the build.
var _ GitOps = (*git.Repo)(nil)
