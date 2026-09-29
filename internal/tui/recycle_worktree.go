package tui

import (
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

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
		label: i18n.T("Recycle a worktree"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openRecyclePicker(name, ""), nil },
	}, true
}

// remoteRecycleRow offers the same "Recycle a worktree" on the Remotes tab:
// the op checks the remote branch out first (creating or fast-forwarding its
// local branch) and recycles onto that. Hidden when the local branch of that
// name is already checked out somewhere — the recycle would refuse it.
func (m Model) remoteRecycleRow() (actionRow, bool) {
	rb, ok := m.selectedRemoteForAction()
	if !ok || rb.Branch == "" {
		return actionRow{}, false
	}
	if _, has := m.worktreeAbsPathForBranch(rb.Branch); has {
		return actionRow{}, false
	}
	if len(m.recycleCandidates()) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "recycle-worktree",
		label: i18n.T("Recycle a worktree"),
		run:   func(m Model) (tea.Model, tea.Cmd) { return m.openRecyclePicker(rb.Branch, rb.Name), nil },
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
func (m Model) openRecyclePicker(branch, remoteRef string) Model {
	m.recycleBranch, m.recycleRemote = branch, remoteRef
	live := map[string]bool{}
	for _, info := range domain.Sessions().List() {
		live[filepath.Clean(info.Dir)] = true
	}
	// Budget = the FULL popup text width minus the "> " prefix (the action menu
	// sizes itself to its content, so a path shows whole whenever the terminal
	// has room). The branch (and a live marker) keeps its columns, the PATH is
	// what elides — to ONE shared budget (the widest suffix decides), and every
	// elided path is then padded to the widest one so the branch column lines
	// up like a table.
	w, _ := m.overlayDims()
	textW := popupTextWidth(popupFullInnerWidth(w)) - 2
	cands := m.recycleCandidates()
	suffixes := make([]string, len(cands))
	lives := make([]bool, len(cands))
	maxSuffix := 0
	for i, w := range cands {
		cur := w.Branch
		if cur == "" {
			cur = i18n.T("detached")
		}
		suffixes[i] = "  " + cur
		if lives[i] = live[filepath.Clean(w.Path)]; lives[i] {
			suffixes[i] += "  " + i18n.T("(agent session running)")
		}
		maxSuffix = max(maxSuffix, lipgloss.Width(suffixes[i]))
	}
	pathW := max(12, textW-maxSuffix)
	paths := make([]string, len(cands))
	col := 0
	for i, w := range cands {
		paths[i] = elidePath(w.Path, pathW)
		col = max(col, lipgloss.Width(paths[i]))
	}
	rows := make([]actionRow, 0, len(cands))
	for i, w := range cands {
		dir, isLive := w.Path, lives[i]
		rows = append(rows, actionRow{
			id:    "recycle-into:" + dir,
			label: padRight(paths[i], col) + suffixes[i],
			run:   func(m Model) (tea.Model, tea.Cmd) { return m.recycleInto(dir, isLive) },
		})
	}
	m.actionMenu = &actionMenu{rows: rows}
	return m
}

// recycleOpFor is the one place the op is built from a picked dir and the
// captured branch and remote ref (pure, so tests pin the pairing).
func recycleOpFor(dir, branch, remoteRef string) engine.RecycleWorktree {
	return engine.RecycleWorktree{Dir: dir, Branch: branch, RemoteRef: remoteRef}
}

// recycleInto dispatches the op for the picked worktree; a live agent
// session there gets the yes/no gate first.
func (m Model) recycleInto(dir string, live bool) (tea.Model, tea.Cmd) {
	op := recycleOpFor(dir, m.recycleBranch, m.recycleRemote)
	if op.RemoteRef != "" {
		// A diverged local branch lands in the checkout recovery modal, whose
		// rename re-dispatches this recycle under the new name.
		m.pendingCheckout = pendingCheckout{remoteRef: op.RemoteRef, base: op.Branch, intent: engine.CheckoutStay, recycleDir: dir}
	}
	if live {
		return m.mustConfirmOp(op, i18n.T("An agent session is running in %s. Recycle it anyway?", dir))
	}
	mm, cmd := m.startOp(op)
	return mm, cmd
}
