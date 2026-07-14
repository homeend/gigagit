package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// tagAnnotateRow offers "Annotate <tag>" on the Tags panel: open a message
// popup (prefilled with the tag's subject) that force-recreates the tag as
// annotated at its current target. Local-only, no confirm.
func (m Model) tagAnnotateRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-annotate",
		label: "Annotate " + name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m, _ = m.openAnnotateTagPopup()
			return m, nil
		},
	}, true
}

// tagCheckoutRow offers "Check out tag" on the Tags panel: ask detached vs a new
// branch (never-trap Cancel), then check out.
func (m Model) tagCheckoutRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-checkout",
		label: "Check out tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "checkout-tag",
					Prompt:  "Check out " + name + ":",
					Options: []string{"Detached", "Create branch…", "Create worktree…", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					switch opt {
					case "Detached":
						return m.startOp(engine.Checkout{Ref: name})
					case "Create branch…":
						// Prefill the branch name with the tag name; the user can edit.
						return m.pushLayer(&tagCheckoutPopup{tag: name, name: newTextField(name)}), nil
					case "Create worktree…":
						// Branch + worktree at the tag; the dialog is seeded with the
						// tag name (the path derives from it, sanitized per-OS).
						return m.openWorktreeAt(name, name), nil
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}

// tagMergeRow offers "Merge <tag> into current". SmartMerge with an empty Target
// defaults to the current branch; conflicts/dirty trees are handled by
// SmartMerge's Decider ladder (mapped to the TUI modal). Hidden on detached HEAD.
func (m Model) tagMergeRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-merge",
		label: "Merge " + name + " into current (" + cur + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartMerge{Source: name}, i18n.T("Merge %s into current branch?", name))
		},
	}, true
}

// tagRebaseRow offers "Rebase current onto <tag>". Hidden on detached HEAD.
func (m Model) tagRebaseRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	cur, attached := m.remoteCurrentBranch()
	if !attached {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-rebase",
		label: "Rebase current (" + cur + ") onto " + name,
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmOp(engine.SmartRebase{Onto: name}, i18n.T("Rebase current branch onto %s?", name))
		},
	}, true
}

// tagPushRow offers "Push tag" on the Tags panel. The engine resolves the remote
// (auto when one is configured, else a modal pick), so the row just starts the op.
func (m Model) tagPushRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-push",
		label: "Push tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingRemoteTagSet = name
			return m.startOp(engine.PushTag{Name: name})
		},
	}, true
}

// tagSoloRow offers "Solo this tag" on the Tags panel: scope the Commits feed to
// the tag's history (git log <tag>) and focus the Commits panel, or un-solo if it
// is already the sole scope. Mirrors commitSoloRow — a tag is just a ref to git
// log, so the existing scope machinery handles it.
func (m Model) tagSoloRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-solo",
		label: "Solo this tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			if len(m.commitScopeBranches) == 1 && m.commitScopeBranches[0] == name {
				m.commitScopeBranches = nil // re-solo → un-solo
			} else {
				m.commitScopeBranches = []string{name}
			}
			m = m.focusCommitsPanel() // land on the freshly-scoped list (Tags is mid-column)
			return m.startFeedReload()
		},
	}, true
}

// tagDeleteRow offers "Delete tag" on the Tags panel: a confirm modal (never-trap
// Cancel) then the delete op.
func (m Model) tagDeleteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-delete",
		label: "Delete tag",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.modal = &decisionState{
				req: engine.DecisionRequest{
					ID:      "delete-tag",
					Prompt:  "Delete tag " + name + "?",
					Options: []string{"Delete", "Cancel"},
				},
				onResolve: func(m Model, opt string) (tea.Model, tea.Cmd) {
					if opt == "Delete" {
						return m.startOp(engine.DeleteTag{Name: name})
					}
					return m, nil
				},
			}
			return m, nil
		},
	}, true
}

// tagDeleteRemoteRow offers "Delete <tag> from remote" on the Tags panel. The
// engine resolves the remote (auto/pick) and confirms via the Decider (surfaced
// as the TUI modal); a single keypress never deletes a remote ref unconfirmed.
func (m Model) tagDeleteRemoteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return actionRow{}, false
	}
	name := m.tags[bi].Name
	return actionRow{
		id:    "tag-delete-remote",
		label: "Delete " + name + " from remote",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.pendingRemoteTagUnset = name
			return m.startOp(engine.DeleteRemoteTag{Tag: name})
		},
	}, true
}

// tagRefreshRemoteRow offers "Refresh remote status" on the Tags panel: run a
// one-shot ls-remote and annotate every tag with ▲. Available whenever the panel
// is focused and non-empty; operates on the whole list, not the selected row.
func (m Model) tagRefreshRemoteRow() (actionRow, bool) {
	if m.focus != panelTags || !m.opsIdle() || len(m.tags) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "tag-refresh-remote",
		label: "Refresh remote status",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m, m.remoteTagsCmd(context.Background(), true)
		},
	}, true
}

// tagJumpToCommit moves the Commits cursor to the selected tag's target commit
// (matched by short-hash prefix) and focuses the Commits panel. A target that
// isn't in the loaded commit page leaves a notice (never-trap: no-op + explain).
func (m Model) tagJumpToCommit() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, nil
	}
	t := m.tags[bi]
	// If the target commit is already in the loaded feed, jump the Commits cursor
	// to it (in-graph context).
	idx := m.displayIndices(panelCommits)
	for di, ci := range idx {
		if c, ok := m.commitAtUnified(ci); ok && strings.HasPrefix(c.Hash, t.Target) {
			m.sel[panelCommits] = di
			m = m.focusCommitsPanel()
			return m, nil
		}
	}
	// Otherwise open the target commit's changed-files view directly by hash. In a
	// big repo (e.g. babel: 922 tags, old releases) the target is almost never in
	// the loaded page, so paging the whole history to find it would be unbounded —
	// inspect it by hash instead, exactly like enter on a reflog entry.
	m, cmd := m.openChangedFiles(model.Commit{Hash: t.Target, Subject: t.Subject})
	m.focus = panelCommits
	m = m.focusTree()
	return m, cmd
}
