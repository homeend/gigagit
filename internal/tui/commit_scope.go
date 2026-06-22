package tui

import (
	"context"
	"slices"
	"strconv"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/model"
)

// commitsReloadedMsg carries a scope-reload's page-0 state. gen is THIS load's
// generation (gen0); the handler drops it when a newer reload bumped feed.Gen().
type commitsReloadedMsg struct {
	gen   int
	state domain.FeedState
}

// startFeedReload sets the Commits loading indicator and returns the scope
// reload cmd, so the title shows it is working while the (possibly slow) re-walk
// runs. The indicator clears when commitsReloadedMsg arrives.
func (m Model) startFeedReload() (Model, tea.Cmd) {
	m.commitsLoading = true
	return m, m.reloadFeedCmd()
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
			return m.startFeedReload()
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
			return m.startFeedReload()
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

// graphWindowRows offers the commit-graph window controls in the . menu when the
// windowed lane graph is active in the Commits panel.
func (m Model) graphWindowRows() []actionRow {
	if m.focus != panelCommits || !m.graphActive() {
		return nil
	}
	return []actionRow{
		{id: "graph-widen", label: "Widen graph", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() + m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-narrow", label: "Narrow graph", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphCols = m.clampCols(m.graphCols() - m.graphStep())
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll)
			return m, nil
		}},
		{id: "graph-pan-left", label: "Pan graph left", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll - m.graphPanStep())
			return m, nil
		}},
		{id: "graph-pan-right", label: "Pan graph right", run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitGraphScroll = m.clampScroll(m.commitGraphScroll + m.graphPanStep())
			return m, nil
		}},
		{id: "graph-center", label: "Center on selected commit", run: func(m Model) (tea.Model, tea.Cmd) {
			return m.snapGraphToSelected(), nil
		}},
	}
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
			idx := m.displayIndices(panelCommits)
			for di, bi := range idx {
				if c, ok := m.commitAtUnified(bi); ok && commitHasLocalRef(c, b.Name) {
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
			m = m.pushLayer(&branchPopup{startPoint: hash})
			return m, nil
		},
	}, true
}

// commitCreateTagRow offers "Create tag here" on the Commits panel: open the
// create-tag dialog targeting the selected commit (name + optional message).
func (m Model) commitCreateTagRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-create-tag",
		label: "Create tag here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.pushLayer(&tagPopup{commit: hash}), nil
		},
	}, true
}

// commitCompareWorktreeRow / commitCompareStagedRow open the files view as a
// whole-tree comparison of the selected commit against the working tree / the
// index. No marking needed — the common "what does my working copy look like
// vs this commit" case.
func (m Model) commitCompareWorktreeRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-worktree",
		label: "Compare against working tree",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointWorkTree})
		},
	}, true
}

func (m Model) commitCompareStagedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	return actionRow{
		id:    "commit-compare-staged",
		label: "Compare against staged",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: hash},
				model.Endpoint{Kind: model.EndpointIndex})
		},
	}, true
}

// commitCompareMarkedRow offers "Compare with marked commit" when a commit is
// marked (m key) on a different row: opens the files view as a commit↔commit
// whole-tree diff, ordered older→newer by feed position (the feed is
// newest-first, so a larger m.commits index is the older commit).
func (m Model) commitCompareMarkedRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	if m.mark == nil || m.mark.panel != panelCommits || !m.markAlive() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	sel := m.commits[bi].Hash
	marked := m.mark.key
	if sel == marked {
		return actionRow{}, false
	}
	older, newer := orderByFeed(m.commits, marked, sel)
	return actionRow{
		id:    "commit-compare-marked",
		label: "Compare with marked commit (" + shortHash(marked) + ")",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openCompareFiles(
				model.Endpoint{Kind: model.EndpointCommit, Hash: older},
				model.Endpoint{Kind: model.EndpointCommit, Hash: newer})
		},
	}, true
}

// orderByFeed returns (older, newer) for two hashes by their position in the
// newest-first commit feed: the hash at the larger index is older. Callers pass
// only loaded hashes.
func orderByFeed(commits []model.Commit, a, b string) (older, newer string) {
	ia, ib := -1, -1
	for i := range commits {
		switch commits[i].Hash {
		case a:
			ia = i
		case b:
			ib = i
		}
	}
	if ia >= ib { // a sits lower in the feed (older) — or b is absent
		return a, b
	}
	return b, a
}

// compareSetDisplayIndices returns the display-row indices in panel p that are
// in the commit compare selection (◉). Empty unless p is the Commits panel and
// the set is non-empty.
func (m Model) compareSetDisplayIndices(p panel) map[int]bool {
	out := map[int]bool{}
	if p != panelCommits || len(m.commitCompareSet) == 0 {
		return out
	}
	l := m.listFor(p)
	_, idx := m.panelView(p)
	for n, i := range idx {
		if m.commitCompareSet[l.Key(i)] {
			out[n] = true
		}
	}
	return out
}

// commitCompareToggleRow adds/removes the selected commit to the ◉ compare
// selection (the multi-commit analog of marking).
func (m Model) commitCompareToggleRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash
	in := m.commitCompareSet[hash]
	label := "Add to compare selection"
	if in {
		label = "Remove from compare selection"
	}
	return actionRow{
		id:    "commit-compare-toggle",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			if m.commitCompareSet == nil {
				m.commitCompareSet = map[string]bool{}
			}
			if in {
				delete(m.commitCompareSet, hash)
			} else {
				m.commitCompareSet[hash] = true
			}
			return m, nil
		},
	}, true
}

// commitCompareClearRow clears the ◉ compare selection.
func (m Model) commitCompareClearRow() (actionRow, bool) {
	if m.focus != panelCommits || len(m.commitCompareSet) == 0 {
		return actionRow{}, false
	}
	return actionRow{
		id:    "commit-compare-clear",
		label: "Clear compare selection",
		run: func(m Model) (tea.Model, tea.Cmd) {
			m.commitCompareSet = nil
			return m, nil
		},
	}, true
}

// compareSelectionEndpoints resolves the ◉ selection into a (left, right)
// endpoint pair, ordered older→newer by feed position:
//   - exactly 2  → a tree-to-tree diff of the two commits (GitKraken's exact
//     2-commit semantic; no ancestry needed).
//   - 3 or more  → the combined diff of the RANGE: git diff oldest^ newest.
//     This is a range approximation — exact only on a topological chain — and
//     is refused when the oldest selected commit is a root (no parent).
//
// ok is false (with a note) when fewer than 2 are selected or the root guard
// trips.
func (m Model) compareSelectionEndpoints() (left, right model.Endpoint, note string, ok bool) {
	type sc struct {
		hash string
		idx  int
	}
	var sel []sc
	for i := range m.commits {
		if m.commitCompareSet[m.commits[i].Hash] {
			sel = append(sel, sc{m.commits[i].Hash, i})
		}
	}
	if len(sel) < 2 {
		return left, right, "select at least 2 commits to compare", false
	}
	// Oldest = largest feed index (newest-first feed); newest = smallest.
	oldest, newest := sel[0], sel[0]
	for _, s := range sel {
		if s.idx > oldest.idx {
			oldest = s
		}
		if s.idx < newest.idx {
			newest = s
		}
	}
	if len(sel) == 2 {
		return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.hash},
			model.Endpoint{Kind: model.EndpointCommit, Hash: newest.hash}, "", true
	}
	// 3+: squash from oldest^. Refuse if oldest is a root commit (no parent).
	if len(m.commits[oldest.idx].Parents) == 0 {
		return left, right, "can't squash a range from the root commit", false
	}
	return model.Endpoint{Kind: model.EndpointCommit, Hash: oldest.hash + "^"},
		model.Endpoint{Kind: model.EndpointCommit, Hash: newest.hash}, "", true
}

// commitCompareSelectionRow offers "Compare selection" when 2+ commits are in
// the ◉ set. The label is honest about the 3+ range semantic.
func (m Model) commitCompareSelectionRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	n := len(m.commitCompareSet)
	if n < 2 {
		return actionRow{}, false
	}
	label := "Compare the 2 selected commits"
	if n >= 3 {
		label = "Compare range of " + strconv.Itoa(n) + " commits (combined diff)"
	}
	return actionRow{
		id:    "commit-compare-selection",
		label: label,
		run: func(m Model) (tea.Model, tea.Cmd) {
			left, right, note, ok := m.compareSelectionEndpoints()
			if !ok {
				m.statusMsg = note
				return m, nil
			}
			return m.openCompareFiles(left, right)
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
			return m.openWorktreeAt(hash, ""), nil
		},
	}, true
}

// commitCherryPickRow offers "Cherry-pick here" on the Commits panel: apply the
// selected commit onto the current branch as a new commit. A conflict drops into
// the existing conflict resolver (continue/abort).
func (m Model) commitCherryPickRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "commit-cherry-pick",
		label: "Cherry-pick here",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.CherryPick{Commit: hash})
		},
	}, true
}

// commitRevertRow offers "Revert this commit" on the Commits panel: create a new
// commit on the current branch that undoes the selected commit. A conflict drops
// into the existing conflict resolver. A merge commit is refused with a clean
// message (reverting a merge needs -m <parent>, out of scope for v1).
func (m Model) commitRevertRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	c := m.commits[bi]
	hash := c.Hash
	isMerge := len(c.Parents) > 1
	return actionRow{
		id:    "commit-revert",
		label: "Revert this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			if isMerge {
				m.statusMsg = "cannot revert a merge commit (v1)"
				return m, nil
			}
			return m.startOp(engine.Revert{Commit: hash})
		},
	}, true
}

// commitResetRow offers "Reset to this commit" on the Commits panel: move the
// current branch to the selected commit. The op asks for the mode (soft/mixed/
// hard) via the modal, and confirms when the target is not on the current branch.
func (m Model) commitResetRow() (actionRow, bool) {
	if m.focus != panelCommits || !m.opsIdle() {
		return actionRow{}, false
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return actionRow{}, false
	}
	hash := m.commits[bi].Hash // full SHA → unambiguous
	return actionRow{
		id:    "commit-reset",
		label: "Reset to this commit",
		run: func(m Model) (tea.Model, tea.Cmd) {
			return m.startOp(engine.Reset{Commit: hash})
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
			return m.startFeedReload()
		},
	}, true
}

// commitBranchRows offers Rename/Delete branch on the Commits panel for every
// local branch whose tip is the selected commit. Rename applies to every local
// tip (including the current branch); Delete is suppressed for any branch
// checked out in a worktree (this worktree's HEAD or another), since
// engine.DeleteBranch refuses those — no point offering a row that can't
// succeed. Reuses the renameBranchPopup and engine.DeleteBranch backends.
func (m Model) commitBranchRows() []actionRow {
	if m.focus != panelCommits || !m.opsIdle() {
		return nil
	}
	bi, ok := m.backingIndex(panelCommits)
	if !ok {
		return nil
	}
	checkedOut := map[string]bool{}
	for _, w := range m.worktrees {
		if w.Branch != "" {
			checkedOut[w.Branch] = true
		}
	}
	var rows []actionRow
	for _, r := range m.commits[bi].Refs {
		if r.Kind != model.RefLocal {
			continue
		}
		name := r.Name
		rows = append(rows, actionRow{
			id:    "rename-branch",
			label: "Rename branch " + name,
			run: func(m Model) (tea.Model, tea.Cmd) {
				return m.pushLayer(&renameBranchPopup{old: name, name: name}), nil
			},
		})
		if !r.Head && !checkedOut[name] {
			rows = append(rows, actionRow{
				id:    "delete-branch",
				label: "Delete branch " + name,
				run: func(m Model) (tea.Model, tea.Cmd) {
					return m.startOp(engine.DeleteBranch{Name: name})
				},
			})
		}
	}
	return rows
}
