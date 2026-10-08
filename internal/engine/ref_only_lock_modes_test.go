package engine

import (
	"testing"

	"github.com/homeend/gigagit/internal/repogate"
)

// Ref-only ops must not take the exclusive default: a TreeWrite queued behind
// a minutes-long Read holder (a headless conflict agent) would stall itself
// AND every later read (strict FIFO). These ops write refs only, never
// index/worktree/HEAD, so RefWrite — compatible with reads — is correct.
func TestRefOnlyOpsAreRefWrite(t *testing.T) {
	t.Parallel()
	for name, op := range map[string]interface{ LockMode() repogate.Mode }{
		"CreateBranch": CreateBranch{},
		"DeleteBranch": DeleteBranch{},
		"CreateTag":    CreateTag{},
		"DeleteTag":    DeleteTag{},
	} {
		if op.LockMode() != repogate.RefWrite {
			t.Errorf("%s must be RefWrite, got %v", name, op.LockMode())
		}
	}
}

// RenameBranch stays exclusive: renaming the checked-out branch rewrites
// HEAD's symbolic ref, and HEAD is part of TreeWrite's definition.
func TestRenameBranchHasNoLockMode(t *testing.T) {
	t.Parallel()
	if _, ok := any(RenameBranch{}).(interface{ LockMode() repogate.Mode }); ok {
		t.Fatal("RenameBranch must keep the exclusive default")
	}
}
