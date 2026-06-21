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
