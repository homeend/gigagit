package tui

import "github.com/homeend/gigagit/internal/worktree"

// The cross-environment path logic (WSL vs Windows notations of one disk)
// moved to internal/worktree so the web frontend can share it — archtest
// forbids web→tui. These aliases keep the TUI call sites and tests as they
// were; the semantics live (and are documented) in worktree/crossenv.go.

type switchVerdict = worktree.SwitchVerdict

const (
	switchOK          = worktree.SwitchOK
	switchRepairable  = worktree.SwitchRepairable
	switchUnreachable = worktree.SwitchUnreachable
)

var (
	translatePath     = worktree.TranslatePath
	checkSwitchTarget = worktree.CheckSwitchTarget
)
