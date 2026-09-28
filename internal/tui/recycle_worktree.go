package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// recycleWorktreeRow offers "Recycle a worktree…" on a LOCAL branch that no
// worktree has checked out — the exact inverse of showInWorktreesRow — when
// there is at least one worktree to recycle (not the one gg runs in, not
// bare). Picking it turns the action menu into the worktree picker.
func (m Model) recycleWorktreeRow() (actionRow, bool) {
	if m.focus != panelBranches || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelBranches)
	if !ok || bi < 0 || bi >= len(m.branches) {
		return actionRow{}, false
	}
	b := m.branches[bi]
	if _, has := m.worktreeAbsPathForBranch(b.Name); has {
		return actionRow{}, false
	}
	if len(m.recycleCandidates()) == 0 {
		return actionRow{}, false
	}
	name := b.Name
	return actionRow{
		id:    "recycle-worktree",
		label: i18n.T("Recycle a worktree…"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openRecyclePicker(name), nil },
	}, true
}

// recycleCandidates is every worktree the picker lists: linked or main, not
// bare, and not the one gg is running in.
func (m Model) recycleCandidates() []model.Worktree {
	var out []model.Worktree
	for _, w := range m.worktrees {
		if w.Bare || w.Path == "" || domain.SameCheckout(w.Path, m.currentWorktree) {
			continue
		}
		out = append(out, w)
	}
	return out
}

// openRecyclePicker re-populates the action menu with one row per candidate
// worktree. The branch is captured at open into m.recycleBranch: a
// background refresh may move the Branches cursor before the user picks,
// and a Model field (not a closure) keeps the capture test-observable. A
// worktree with a running agent session asks once before the op starts.
func (m Model) openRecyclePicker(branch string) Model {
	m.recycleBranch = branch
	live := map[string]bool{}
	for _, info := range domain.Sessions().List() {
		live[filepath.Clean(info.Dir)] = true
	}
	width := max(24, m.width/2)
	var rows []actionRow
	for _, w := range m.recycleCandidates() {
		dir := w.Path
		cur := w.Branch
		if cur == "" {
			cur = i18n.T("detached")
		}
		label := elidePath(dir, width) + "  " + cur
		isLive := live[filepath.Clean(dir)]
		if isLive {
			label += "  " + i18n.T("(agent session running)")
		}
		rows = append(rows, actionRow{
			id:    "recycle-into:" + dir,
			label: label,
			run:   func(m Model) (tea.Model, tea.Cmd) { return m.recycleInto(dir, isLive) },
		})
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// recycleOpFor is the one place the op is built from a picked dir and the
// captured branch (pure, so tests pin the pairing).
func recycleOpFor(dir, branch string) engine.RecycleWorktree {
	return engine.RecycleWorktree{Dir: dir, Branch: branch}
}

// recycleInto dispatches the op for the picked worktree; a live agent
// session there gets the yes/no gate first.
func (m Model) recycleInto(dir string, live bool) (tea.Model, tea.Cmd) {
	op := recycleOpFor(dir, m.recycleBranch)
	if live {
		return m.mustConfirmOp(op, i18n.T("An agent session is running in %s. Recycle it anyway?", dir))
	}
	mm, cmd := m.startOp(op)
	return mm, cmd
}
