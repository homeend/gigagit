package tui

import "github.com/gigagit/gg/internal/model"

// wipKind distinguishes the two pseudo-rows that represent uncommitted work.
type wipKind int

const (
	wipWorktree wipKind = iota // unstaged changes (working tree)
	wipStaged                  // staged changes (index)
)

// wipRow is one pseudo-commit row at the top of the Commits panel.
type wipRow struct {
	kind  wipKind
	count int
}

func (r wipRow) label() string {
	if r.kind == wipStaged {
		return "Staged"
	}
	return "Working tree"
}

// deriveWipRows builds the dirty-only pseudo-rows from a status snapshot:
// a Working tree row when there are unstaged changes, a Staged row when there
// are staged changes, in that top→down order. A clean tree yields none.
func deriveWipRows(st model.WorkingTreeStatus) []wipRow {
	unstaged, staged := 0, 0
	for _, f := range st.Files {
		if inFilesPanel(f) {
			unstaged++
		}
		if inStagedPanel(f) {
			staged++
		}
	}
	var rows []wipRow
	if unstaged > 0 {
		rows = append(rows, wipRow{wipWorktree, unstaged})
	}
	if staged > 0 {
		rows = append(rows, wipRow{wipStaged, staged})
	}
	return rows
}

// wipCount is the number of pseudo-rows currently prepended to the Commits feed.
func (m Model) wipCount() int { return len(m.wipRows) }

// commitsTotal is the unified Commits-panel length: wip rows + real commits.
func (m Model) commitsTotal() int { return m.wipCount() + len(m.commits) }

// isWipRow reports whether a unified Commits index addresses a pseudo-row
// (the first wipCount entries) rather than a real commit.
func (m Model) isWipRow(unified int) bool {
	return unified >= 0 && unified < m.wipCount()
}

// wipRowAt returns the pseudo-row at a unified index, or false if it is a commit.
func (m Model) wipRowAt(unified int) (wipRow, bool) {
	if m.isWipRow(unified) {
		return m.wipRows[unified], true
	}
	return wipRow{}, false
}

// commitSelUnified returns the unified Commits index currently selected (the
// space the graph caches and commitList span: WIP rows ++ feed), or -1. Use this
// — not backingIndex (which yields a pure-feed index and refuses wip rows) — to
// index commitGraphRows/Lanes by the selection.
func (m Model) commitSelUnified() int {
	idx := m.displayIndices(panelCommits)
	s := m.sel[panelCommits]
	if s < 0 || s >= len(idx) {
		return -1
	}
	return idx[s]
}

// wipSyntheticHash is a deliberately git-invalid id for a WIP node in the graph
// layout (it contains a NUL, so it can never be mistaken for a real 40-hex SHA;
// a leak into a git command fails loudly instead of doing something subtle).
func wipSyntheticHash(r wipRow) string { return "\x00wip-" + r.label() }
