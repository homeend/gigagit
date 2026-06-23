package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
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
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartMerge{Source: name}) },
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
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.SmartRebase{Onto: name}) },
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
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.PushTag{Name: name}) },
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
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.startOp(engine.DeleteRemoteTag{Tag: name}) },
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
	target := m.tags[bi].Target
	idx := m.displayIndices(panelCommits)
	for di, ci := range idx {
		if c, ok := m.commitAtUnified(ci); ok && strings.HasPrefix(c.Hash, target) {
			m.sel[panelCommits] = di
			m.focus = panelCommits
			return m, nil
		}
	}
	m.statusMsg = "tag " + m.tags[bi].Name + " target not in the loaded commits"
	return m, nil
}
