package tui

import (
	"slices"

	"github.com/homeend/gigagit/internal/model"
)

// worktreeFileList is the working tree's files as F lists them: the tracked
// files (ls-files) minus those deleted in the working tree or the index,
// plus the untracked files the status already knows — no extra git walk
// (an untracked scan is the slow half of git status on a large tree).
// Sorted and deduplicated; untracked names the untracked ones.
func worktreeFileList(tracked []string, st model.WorkingTreeStatus) (paths []string, untracked map[string]bool) {
	deleted := map[string]bool{}
	untracked = map[string]bool{}
	for _, f := range st.Files {
		switch {
		case f.Kind == model.KindUntracked:
			untracked[f.Path] = true
		case f.Staged == 'D' || f.Unstaged == 'D':
			deleted[f.Path] = true
		}
	}
	for _, p := range tracked {
		if !deleted[p] {
			paths = append(paths, p)
		}
	}
	for p := range untracked {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	return slices.Compact(paths), untracked
}
