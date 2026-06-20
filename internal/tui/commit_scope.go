package tui

import (
	"context"
	"slices"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/model"
)

// commitsReloadedMsg carries a scope-reload's page-0 state. gen is THIS load's
// generation (gen0); the handler drops it when a newer reload bumped feed.Gen().
type commitsReloadedMsg struct {
	gen   int
	state domain.FeedState
}

// reloadFeedCmd applies the model's scope to the feed and reloads page 0 off the
// UI thread. SetScope+LoadInitial bumps the feed gen (dropping stale pages) and
// cancels any superseded in-flight walk.
func (m Model) reloadFeedCmd() tea.Cmd {
	feed := m.feed
	scope := domain.LogScope{Branches: append([]string(nil), m.commitScopeBranches...)}
	return func() tea.Msg {
		feed.SetScope(scope)
		st, _ := feed.LoadInitial(context.Background())
		return commitsReloadedMsg{gen: st.Gen, state: st}
	}
}

// commitSoloRow offers "Solo this branch" on the Branches panel: scope the
// Commits feed to the selected branch, or un-solo if it is already the sole one.
func (m Model) commitSoloRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-solo",
		label: "Solo this branch",
		run: func(m Model) (tea.Model, tea.Cmd) {
			if len(m.commitScopeBranches) == 1 && m.commitScopeBranches[0] == b.Name {
				m.commitScopeBranches = nil // re-solo → un-solo
			} else {
				m.commitScopeBranches = []string{b.Name}
			}
			return m, m.reloadFeedCmd()
		},
	}, true
}

// commitToggleRow offers "Add to commit view" / "Remove from commit view" on the
// Branches panel: add or remove the selected branch from the multi-branch
// Commits-feed scope. Removing the last branch returns the feed to all branches.
func (m Model) commitToggleRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	in := slices.Contains(m.commitScopeBranches, b.Name)
	label := "Add to commit view"
	if in {
		label = "Remove from commit view"
	}
	return actionRow{
		id:    "commits-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if in {
				m.commitScopeBranches = without(m.commitScopeBranches, b.Name)
			} else {
				m.commitScopeBranches = append(append([]string(nil), m.commitScopeBranches...), b.Name)
			}
			return m, m.reloadFeedCmd()
		},
	}, true
}

// without returns a new slice with the first occurrence of s removed, preserving
// the order of the remaining elements. A fresh allocation is deliberate: the
// value-receiver Model shares its slice backing with the prior copy, so an
// in-place delete would corrupt it.
func without(ss []string, s string) []string {
	out := make([]string, 0, len(ss))
	for _, x := range ss {
		if x == s {
			continue
		}
		out = append(out, x)
	}
	return out
}

// commitViewModeRow toggles the Commits feed between the lane graph and a flat
// ●-gutter list. Offered from the Branches or Commits panel.
func (m Model) commitViewModeRow() (actionRow, bool) {
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	label := "Show as list"
	if m.commitListMode {
		label = "Show as graph"
	}
	return actionRow{
		id:    "commits-viewmode",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitListMode = !m.commitListMode
			return m, nil
		},
	}, true
}

// commitGotoTipRow offers "Go to tip in commits" on the Branches panel: move the
// Commits cursor to the selected branch's tip commit (the loaded commit decorated
// with that branch) and focus the Commits panel. Mirrors commitSoloRow's gating.
func (m Model) commitGotoTipRow() (actionRow, bool) {
	if m.focus != panelBranches {
		return actionRow{}, false
	}
	b, ok := m.selectedBranch()
	if !ok {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-goto-tip",
		label: "Go to tip in commits",
		run: func(m Model) (tea.Model, tea.Cmd) {
			_, idx := m.panelView(panelCommits)
			for di, bi := range idx {
				if bi >= 0 && bi < len(m.commits) && commitHasLocalRef(m.commits[bi], b.Name) {
					m.sel[panelCommits] = di
					m.focus = panelCommits
					return m, nil
				}
			}
			m.statusMsg = "branch " + b.Name + " tip not in the loaded commits"
			return m, nil
		},
	}, true
}

// commitCreateBranchRow offers "Create branch here" on the Commits panel: open
// the create-branch dialog with the selected commit as the start point. The whole
// create stack (branchPopup → startOp → engine.CreateBranch) already exists.
func (m Model) commitCreateBranchRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous start-point
	return actionRow{
		id:    "commit-create-branch",
		label: "Create branch here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m = m.pushOverlay(&branchPopup{startPoint: hash})
			return m, nil
		},
	}, true
}

// commitCreateWorktreeRow offers "Create worktree here" on the Commits panel:
// open the create-worktree dialog based at the selected commit, with a user-typed
// (non-templated) branch name.
func (m Model) commitCreateWorktreeRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-create-worktree",
		label: "Create worktree here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openWorktreeFromCommit(hash), nil
		},
	}, true
}

// commitHasLocalRef reports whether commit c is decorated with a local branch ref
// named name (ignoring remote/tag kinds and the Head flag). A branch ref
// decorates only its tip, so this identifies the branch's tip commit.
func commitHasLocalRef(c model.Commit, name string) bool {
	for _, r := range c.Refs {
		if r.Kind == model.RefLocal && r.Name == name {
			return true
		}
	}
	return false
}

// commitShowAllRow offers "Show all branches" — present only when the feed is
// scoped — from either the Branches or the Commits panel menu.
func (m Model) commitShowAllRow() (actionRow, bool) {
	if !m.opsIdle() || len(m.commitScopeBranches) == 0 {
		return actionRow{}, false
	}
	if m.focus != panelBranches && m.focus != panelCommits {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commits-showall",
		label: "Show all branches",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitScopeBranches = nil
			return m, m.reloadFeedCmd()
		},
	}, true
}
