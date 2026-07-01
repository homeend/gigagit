package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
)

// copyShaRow builds a "Copy commit sha" action that resolves ref to its full
// 40-char object id on invoke (git rev-parse via domain), NOT at menu-build
// time — so opening the menu costs no git call. A nil service or a resolve
// error falls back to fallbackShort (the short hash the row already carries),
// so the copy always yields a usable value.
func (m Model) copyShaRow(ref, fallbackShort string) actionRow {
	return actionRow{
		id:    "copy-commit-sha",
		label: "Copy commit sha",
		run: func(m Model) (tea.Model, tea.Cmd) {
			full := fallbackShort
			if m.svc != nil {
				if s, err := m.svc.RevParse(context.Background(), ref); err == nil && s != "" {
					full = s
				}
			}
			return m, m.copyToClipboardCmd("Copied commit sha "+shortHash(full), full)
		},
	}
}

// canFetchRemotes gates f (fetch) and the Prune . menu action on the Remotes tab.
func (m Model) canFetchRemotes() bool {
	return m.focus == panelRemotes && m.opsIdle()
}

// remoteCurrentBranch returns the checked-out branch name and whether HEAD is
// attached. Porcelain reports detached HEAD as "" or "(detached)"; guard both
// (same dual-guard as the fast-forward feature).
func (m Model) remoteCurrentBranch() (string, bool) {
	cur := m.status.Branch
	if cur == "" || cur == "(detached)" {
		return "", false
	}
	return cur, true
}

// remoteCreateWorktreeRow offers "Create worktree from <remote branch>" on the
// Remotes tab, reusing the worktree-from-ref popup seeded with the remote ref
// as start-point and the de-prefixed branch name as the prefill.
func (m Model) remoteCreateWorktreeRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-worktree",
		label: "Create worktree from " + rb.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openWorktreeAt(rb.Name, rb.Branch), nil
		},
	}, true
}

// remoteMergeRow offers "Merge <remote branch> into current". SmartMerge with an
// empty Target defaults to the current branch; conflicts/dirty trees are handled
// by SmartMerge's own Decider ladder (mapped to the TUI modal). Hidden on
// detached HEAD. The engine rejects Source==Target, and a remote ref can never
// equal a local branch name, so no extra equality guard is needed here.
func (m Model) remoteMergeRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-merge",
		label: "Merge " + rb.Name + " into current (" + cur + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartMerge{Source: rb.Name}, "Merge "+rb.Name+" into current branch?")
		},
	}, true
}

// remoteRebaseRow offers "Rebase current onto <remote branch>". SmartRebase with
// an empty Branch defaults to the current branch. Hidden on detached HEAD.
func (m Model) remoteRebaseRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-rebase",
		label: "Rebase current (" + cur + ") onto " + rb.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartRebase{Onto: rb.Name}, "Rebase current branch onto "+rb.Name+"?")
		},
	}, true
}

// remoteResetRow offers "Reset current (<cur>) to <remote> tip" on the Remotes
// tab, but ONLY when the selected remote branch is the remote counterpart of the
// checked-out branch (rb.Branch == cur). git reset moves HEAD's branch, so a hard
// reset to origin/<cur> only lands on the right branch when <cur> is checked out;
// offering it for a remote whose local branch is elsewhere would reset the wrong
// branch. engine.Reset with Mode:"hard" skips the soft/mixed/hard picker and the
// non-ancestor confirm — so the mustConfirmOp modal below is the ONLY guard before
// local commits and uncommitted changes are discarded. It uses mustConfirmOp (not
// confirmOp) so this one-click destructive reset still prompts even when the user
// has turned off slow-op confirms ([ui] disable_slow_op_confirm).
func (m Model) remoteResetRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached || rb.Branch != cur {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-reset",
		label: "Reset current (" + cur + ") to " + rb.Name + " tip",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.mustConfirmOp(engine.Reset{Commit: rb.Name, Mode: "hard"},
				"Reset "+cur+" to "+rb.Name+"? This discards local commits and uncommitted changes.")
		},
	}, true
}

// remotePruneRow offers Prune on the Remotes tab (no dedicated key).
func (m Model) remotePruneRow() (actionRow, bool) {
	if !m.canFetchRemotes() {
		return actionRow{}, false
	}
	return actionRow{
		id:    "prune-remotes",
		label: "Prune remotes (drop deleted branches)",
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.Prune{}) },
	}, true
}

// remoteDeleteRow offers "Delete <remote branch>" on the Remotes tab. The
// engine's Decider confirm (surfaced as the TUI modal) gates the actual delete;
// a single keypress never deletes a remote ref unconfirmed.
func (m Model) remoteDeleteRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-delete",
		label: "Delete " + rb.Name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.DeleteRemoteBranch{Remote: rb.Remote, Branch: rb.Branch})
		},
	}, true
}
