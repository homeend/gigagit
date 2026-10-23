package tui

import (
	"path/filepath"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
)

// The grouped lists of a workspace (spec 2026-10-22-workspaces-design §5):
// the Worktrees and Branches tabs show every member's rows under a heading
// per member, in workspace order. Each group is built by the ordinary
// single-repository code run over a VIEW of that member (asMember: the
// Model with that member's repository-wide fields swapped in and no
// workspace), so a member's rows, sort, text filter and branch-filter slot
// are exactly what the tab would show with that repository alone; the
// grouped list is the concatenation, already in display order, and
// displayIndices over it is the identity.

// ringStop is one stop of the worktree ring: a worktree of member `member`.
type ringStop struct {
	member int
	path   string
}

// headingText is the heading row's text.
func headingText(label string) string { return "[" + label + "]" }

// headingRow is member i's heading row: its label, "(missing)" when its
// path was not there when the workspace opened.
func (m Model) headingRow(i int) string {
	label := headingLabel(m.ws.members, i)
	if m.ws.members[i].missing {
		label = i18n.T("%s (missing)", label)
	}
	return headingText(label)
}

// liveMember is the index of the member on screen (0 with no workspace).
func (m Model) liveMember() int {
	if m.ws == nil {
		return 0
	}
	return m.ws.live
}

// memberCount is the number of groups the tabs show: 1 with no workspace.
func (m Model) memberCount() int {
	if m.ws == nil {
		return 1
	}
	return len(m.ws.members)
}

// asMember is the Model as the single-repository gg would be for member i:
// the live Model (its fields ARE the live member's) with the workspace
// cleared, or a copy holding member i's remembered repository-wide data.
// A pure value: nothing is written back.
func (m Model) asMember(i int) Model {
	mm := m
	mm.markViewedBranch = m.ws != nil // the Branches gutter's ':' column exists only in a workspace
	mm.ws = nil
	if m.ws == nil || i == m.ws.live || i < 0 || i >= len(m.ws.members) {
		return mm
	}
	return mm.withRepoData(m.ws.members[i])
}

// withRepoData swaps member r's remembered lists and list state into a copy
// of the Model for RENDERING its group: the lists the two tabs read, the
// branch filters, the commit scope marker. No service, no slots, no
// windows. The viewed-worktree star is off: none of r's worktrees is the
// one the panels show.
func (m Model) withRepoData(r *repoView) Model {
	m.branches, m.remoteBranches, m.tags, m.worktrees = r.branches, r.remoteBranches, r.tags, r.worktrees
	m.commitScopeBranches = r.commitScopeBranches
	m.branchFilters, m.branchFilterSlot, m.bfSlotsLoaded, m.bfCfgApplied = r.branchFilters, r.branchFilterSlot, r.bfSlotsLoaded, r.bfCfgApplied
	m.bfMemo = r.bfMemo
	if m.bfMemo == nil {
		m.bfMemo = &branchFilterMemos{}
	}
	if m.branchFilterSlot == nil {
		m.branchFilterSlot = map[panel]int{}
	}
	m.currentWorktree = ""
	return m
}

// mergedHeadTimes is a live refresh's head times over the shared map:
// fresh wins, and the sleeping members' worktrees keep theirs (a refresh
// of the member on screen reads only its own worktrees; keyed by sha, a
// wholesale replace would drop the others' date sort until their next
// read). A single repository replaces.
func (m Model) mergedHeadTimes(fresh map[string]int64) map[string]int64 {
	if m.ws == nil {
		return fresh
	}
	out := make(map[string]int64, len(fresh)+len(m.headTimes))
	for i, r := range m.ws.members {
		if i == m.ws.live {
			continue
		}
		for _, w := range r.worktrees {
			if t, ok := m.headTimes[w.Head]; ok {
				out[w.Head] = t
			}
		}
	}
	for k, v := range fresh {
		out[k] = v
	}
	return out
}

// mergedWorktreeMarks is mergedHeadTimes for the claim/reserve marks
// (keyed by worktree path).
func (m Model) mergedWorktreeMarks(fresh map[string]domain.WorktreeMark) map[string]domain.WorktreeMark {
	if m.ws == nil {
		return fresh
	}
	out := make(map[string]domain.WorktreeMark, len(fresh)+len(m.worktreeMarks))
	for i, r := range m.ws.members {
		if i == m.ws.live {
			continue
		}
		for _, w := range r.worktrees {
			if mk, ok := m.worktreeMarks[w.Path]; ok {
				out[w.Path] = mk
			}
		}
	}
	for k, v := range fresh {
		out[k] = v
	}
	return out
}

// headingLabel is member i's heading: its directory name, with the parent
// directory added when another member has the same name.
func headingLabel(members []*repoView, i int) string {
	name := members[i].name
	for j, o := range members {
		if j != i && o.name == name {
			return name + " (" + filepath.Base(filepath.Dir(members[i].mainPath)) + ")"
		}
	}
	return name
}

// groupedBranchEntries is the Branches tab of a workspace: per member a
// heading entry, then that member's entries (branchEntries over asMember)
// in that member's display order. It returns the entries, their row texts
// (1:1) and the per-member branch slices the entries index.
func (m Model) groupedBranchEntries() ([]brEntry, []string, [][]model.Branch) {
	var ents []brEntry
	var rows []string
	groups := make([][]model.Branch, m.memberCount())
	for i := range m.memberCount() {
		mm := m.asMember(i)
		groups[i] = mm.branches
		ents = append(ents, brEntry{br: -1, member: i, heading: true})
		rows = append(rows, m.headingRow(i))
		mEnts := mm.branchEntries()
		mRows := mm.branchRowsFor(mEnts)
		live := i == m.liveMember()
		for _, u := range mm.displayIndicesBase(panelBranches) {
			if u >= len(mEnts) {
				continue
			}
			e := mEnts[u]
			if !live && (e.review != "" || e.overview != "") {
				continue // a sleeping member's notes refresh on wake: sessions only (spec §5)
			}
			e.member = i
			ents = append(ents, e)
			rows = append(rows, mRows[u])
		}
	}
	return ents, rows, groups
}

// groupedWorktreeEntries is the Worktrees tab of a workspace, as
// groupedBranchEntries is the Branches tab's.
func (m Model) groupedWorktreeEntries() ([]wtEntry, []string, [][]model.Worktree) {
	var ents []wtEntry
	var rows []string
	groups := make([][]model.Worktree, m.memberCount())
	for i := range m.memberCount() {
		mm := m.asMember(i)
		groups[i] = mm.worktrees
		ents = append(ents, wtEntry{wt: -1, member: i, heading: true})
		rows = append(rows, m.headingRow(i))
		mEnts := mm.worktreeEntries()
		mRows := mm.worktreeRows(mEnts)
		for _, u := range mm.displayIndicesBase(panelWorktrees) {
			if u >= len(mEnts) {
				continue
			}
			e := mEnts[u]
			e.member = i
			ents = append(ents, e)
			rows = append(rows, mRows[u])
		}
	}
	return ents, rows, groups
}

// groupedPanel reports a panel whose list is the grouped concatenation:
// the two tabs, inside a workspace.
func (m Model) groupedPanel(p panel) bool {
	return m.ws != nil && (p == panelBranches || p == panelWorktrees)
}

// isHeadingRow reports that display row i of panel p is a member heading.
func (m Model) isHeadingRow(p panel, i int) bool {
	if !m.groupedPanel(p) || i < 0 {
		return false
	}
	idx := m.displayIndices(p)
	if i >= len(idx) {
		return false
	}
	switch p {
	case panelBranches:
		ents := m.branchEntries()
		return idx[i] < len(ents) && ents[idx[i]].heading
	case panelWorktrees:
		ents := m.worktreeEntries()
		return idx[i] < len(ents) && ents[idx[i]].heading
	}
	return false
}

// skipHeading moves the cursor of panel p off a heading row in direction
// dir (+1 down, -1 up); at the list's edge it comes back to `from`, the
// row the move started from.
func (m Model) skipHeading(p panel, dir, from int) Model {
	n := m.panelLen(p)
	for m.isHeadingRow(p, m.sel[p]) {
		next := m.sel[p] + dir
		if next < 0 || next >= n {
			m.sel[p] = from
			return m
		}
		m.sel[p] = next
	}
	return m
}

// cursorIntoHeadingGroup is the Update tail's rule for a cursor resting on
// a member heading (a fresh list, a restored selection, a clamp): it moves
// onto the first row of THAT heading's group. A group with no rows yet (a
// member still loading) keeps the cursor on its heading for now — never
// onto another member's row, which the next action would then switch to —
// and the pass after its rows land moves it down.
func (m Model) cursorIntoHeadingGroup(p panel) Model {
	sel := m.sel[p]
	if !m.isHeadingRow(p, sel) {
		return m
	}
	if sel+1 < m.panelLen(p) && !m.isHeadingRow(p, sel+1) {
		m.sel[p] = sel + 1
	}
	return m
}

// worktreeRing is what alt+w walks and what orders alt+a: every member's
// worktrees in the Branches tab's order of that member (worktreeOrder over
// asMember), group after group in workspace order. One list for the walks
// and the tabs' targets, so they can never disagree.
func (m Model) worktreeRing() []ringStop {
	var out []ringStop
	for i := range m.memberCount() {
		mm := m.asMember(i)
		for _, wi := range mm.worktreeOrder() {
			out = append(out, ringStop{member: i, path: filepath.Clean(mm.worktrees[wi].Path)})
		}
	}
	return out
}

// ringIndex is a worktree's position in the ring, -1 when it is no stop.
func (m Model) ringIndex(path string) int {
	key := model.KeyOf(path)
	for i, s := range m.worktreeRing() {
		if model.KeyOf(s.path) == key {
			return i
		}
	}
	return -1
}
