package tui

import (
	"fmt"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestWorktreeFileListDropsDeletedAddsUntracked(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "b.go", Staged: '.', Unstaged: 'D'},
		{Path: "n.txt", Kind: model.KindUntracked},
		{Path: "a.go", Staged: '.', Unstaged: 'M'},
	}}
	paths, untracked := worktreeFileList([]string{"d.go", "a.go", "b.go"}, st)
	if got := fmt.Sprint(paths); got != "[a.go d.go n.txt]" {
		t.Fatalf("paths = %s, want [a.go d.go n.txt]", got)
	}
	if len(untracked) != 1 || !untracked["n.txt"] {
		t.Fatalf("untracked = %v, want {n.txt}", untracked)
	}
}

func TestWorktreeFileListStagedDeletionIsGone(t *testing.T) {
	t.Parallel()
	st := model.WorkingTreeStatus{Files: []model.FileStatus{{Path: "gone.go", Staged: 'D', Unstaged: '.'}}}
	paths, _ := worktreeFileList([]string{"gone.go", "kept.go", "kept.go"}, st)
	if got := fmt.Sprint(paths); got != "[kept.go]" {
		t.Fatalf("paths = %s, want [kept.go]", got)
	}
}
