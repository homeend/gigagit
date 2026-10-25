package domain

import (
	"context"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
)

// RepoConfigAction is one whole-file relocation of the per-repo config
// between the committed .gg.toml and the private user-dir file (Settings →
// Repo settings location). The values are the web's wire ids.
type RepoConfigAction string

const (
	CopyToPrivate   RepoConfigAction = "copy-to-private"
	MoveToPrivate   RepoConfigAction = "move-to-private"
	CopyToCommitted RepoConfigAction = "copy-to-committed"
	MoveToCommitted RepoConfigAction = "move-to-committed"
)

// RepoConfigActions lists the applicable relocations given which files exist
// and which paths are available: to-private needs the committed file and a
// private path to write to; to-committed is the mirror.
func RepoConfigActions(committedExists, privateExists, haveCommitted, havePrivate bool) []RepoConfigAction {
	var a []RepoConfigAction
	if committedExists && havePrivate {
		a = append(a, CopyToPrivate, MoveToPrivate)
	}
	if privateExists && haveCommitted {
		a = append(a, CopyToCommitted, MoveToCommitted)
	}
	return a
}

// RepoConfigEndpoints maps an action to its (source, destination, isMove).
func RepoConfigEndpoints(act RepoConfigAction, committed, private string) (src, dst string, isMove bool) {
	switch act {
	case CopyToPrivate:
		return committed, private, false
	case MoveToPrivate:
		return committed, private, true
	case CopyToCommitted:
		return private, committed, false
	case MoveToCommitted:
		return private, committed, true
	}
	return "", "", false
}

// RepoConfigActionLabel is an action's English label (the TUI translates
// its own).
func RepoConfigActionLabel(act RepoConfigAction) string {
	switch act {
	case CopyToPrivate:
		return "Copy to private (user dir)"
	case MoveToPrivate:
		return "Move to private (user dir)"
	case CopyToCommitted:
		return "Copy to committed (.gg.toml)"
	case MoveToCommitted:
		return "Move to committed (.gg.toml)"
	}
	return "?"
}

// RepoConfigPaths is this repo's two per-repo config places: the current
// worktree's committed .gg.toml and the private file keyed on the MAIN
// worktree (shared by every worktree; "" when it cannot be read).
func (s *Service) RepoConfigPaths(ctx context.Context) (committed, private string, err error) {
	top, err := s.TopLevel(ctx)
	if err != nil {
		return "", "", err
	}
	if wts, werr := s.Worktrees(ctx); werr == nil && len(wts) > 0 && wts[0].Path != "" {
		private = config.PrivateRepoPath(wts[0].Path)
	}
	return filepath.Join(top, ".gg.toml"), private, nil
}

// DefaultToolChecked is the External-tools wizard's initial checkbox: a new
// row starts checked, EXCEPT an OptIn template (an aggressive yolo /
// auto-approve variant) and EVERY conflict_complete row — that category
// completes the user's paused operation autonomously, so adding any of it is
// an explicit opt-in even where no bypass flag marks OptIn (Kimi). A
// configured row shows checked; it is skipped on apply regardless.
func DefaultToolChecked(t exttool.CommandTemplate, existing bool) bool {
	return existing || !(t.OptIn || t.Category == exttool.CatConflictComplete)
}
