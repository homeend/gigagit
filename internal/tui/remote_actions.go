package tui

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
)

// copyShaRow builds a "Copy commit sha" action that resolves ref to its full
// 40-char object id on invoke (git rev-parse via domain), NOT at menu-build
// time — so opening the menu costs no git call. A nil service or a resolve
// error falls back to fallbackShort (the short hash the row already carries),
// so the copy always yields a usable value.
//
// The resolve is DEADLINED (updateThreadGitTimeout): it runs on the Update
// thread, where an undeadlined domain read can wedge the whole UI behind a
// queued tree-write. A busy repo therefore copies the short hash instead of
// freezing — the same fallback the resolve-error case already took, so this
// needs no new outcome for the user to understand.
func (m Model) copyShaRow(ref, fallbackShort string) actionRow {
	return actionRow{
		id:    "copy-commit-sha",
		label: i18n.T("Copy commit sha"),
		run: func(m Model) (tea.Model, tea.Cmd) {
			full := m.resolveShaWithin(ref, fallbackShort, updateThreadGitTimeout)
			return m, m.copyToClipboardCmd(i18n.T("Copied commit sha %s", shortHash(full)), full)
		},
	}
}

// resolveShaWithin is copyShaRow's resolve with the deadline exposed, so a
// test can drive the expiry path without racing a real reservation.
func (m Model) resolveShaWithin(ref, fallbackShort string, d time.Duration) string {
	if m.svc == nil {
		return fallbackShort
	}
	ctx, cancel := updateThreadCtx(d)
	defer cancel()
	if s, err := m.svc.RevParse(ctx, ref); err == nil && s != "" {
		return s
	}
	return fallbackShort
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
		label: i18n.T("Create worktree from %s", rb.Name),
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
		label: i18n.T("Merge %s into current (%s)", rb.Name, cur),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartMerge{Source: rb.Name}, i18n.T("Merge %s into current branch?", rb.Name))
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
		label: i18n.T("Rebase current (%s) onto %s", cur, rb.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartRebase{Onto: rb.Name}, i18n.T("Rebase current branch onto %s?", rb.Name))
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
		label: i18n.T("Reset current (%s) to %s tip", cur, rb.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.mustConfirmOp(engine.Reset{Commit: rb.Name, Mode: "hard"},
				i18n.T("Reset %s to %s? This discards local commits and uncommitted changes.", cur, rb.Name))
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
		label: i18n.T("Prune remotes (drop deleted branches)"),
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
		label: i18n.T("Delete %s", rb.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.DeleteRemoteBranch{Remote: rb.Remote, Branch: rb.Branch})
		},
	}, true
}

// remoteCheckoutAsRow offers "Check out <remote> as…" on the Remotes tab: the
// name popup materializes the remote ref under a user-chosen local name
// (stay on the current branch). Pre-fills the remote's own branch name.
func (m Model) remoteCheckoutAsRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-checkout-as",
		label: i18n.T("Check out %s as…", rb.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCheckoutAsPopup(rb.Name, rb.Branch, engine.CheckoutStay), nil
		},
	}, true
}

// remoteSwitchAsRow is remoteCheckoutAsRow with switch intent: create the
// local branch under the chosen name AND switch to it (SmartSwitch autostash
// semantics, same as s).
func (m Model) remoteSwitchAsRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "remote-switch-as",
		label: i18n.T("Switch to %s as…", rb.Name),
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCheckoutAsPopup(rb.Name, rb.Branch, engine.CheckoutSwitch), nil
		},
	}, true
}
