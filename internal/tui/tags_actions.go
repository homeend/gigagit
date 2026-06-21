package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/engine"
)

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
						return m.startOp(engine.CheckoutTag{Name: name})
					case "Create branch…":
						// Prefill the branch name with the tag name; the user can edit.
						return m.pushLayer(&tagCheckoutPopup{tag: name, name: name}), nil
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

// tagJumpToCommit moves the Commits cursor to the selected tag's target commit
// (matched by short-hash prefix) and focuses the Commits panel. A target that
// isn't in the loaded commit page leaves a notice (never-trap: no-op + explain).
func (m Model) tagJumpToCommit() (tea.Model, tea.Cmd) {
	bi, ok := m.backingIndex(panelTags)
	if !ok || bi < 0 || bi >= len(m.tags) {
		return m, nil
	}
	target := m.tags[bi].Target
	_, idx := m.panelView(panelCommits)
	for di, ci := range idx {
		if ci >= 0 && ci < len(m.commits) && strings.HasPrefix(m.commits[ci].Hash, target) {
			m.sel[panelCommits] = di
			m.focus = panelCommits
			return m, nil
		}
	}
	m.statusMsg = "tag " + m.tags[bi].Name + " target not in the loaded commits"
	return m, nil
}
