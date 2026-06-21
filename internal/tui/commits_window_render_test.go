package tui

import (
	"fmt"
	"testing"
)

// The windowed commit body must render byte-for-byte identically to full
// materialization for the same visible state, across display modes and a
// scrolled selection. commitBody styles only the visible window; this proves
// that produces the same panel output as styling every row.
func TestCommitBodyWindowedMatchesFull(t *testing.T) {
	for _, mode := range []dispMode{modeCutoff, modeScroll, modeWrap} {
		for _, sel := range []int{0, 25, 100, 199} {
			m := benchModel(200, 40, 8)
			m.dispModes[panelCommits] = mode
			m.sel[panelCommits] = sel
			boxH := 24
			label := "Commits"

			// Reference: full materialization (every row styled).
			fRows, fIdx := m.panelView(panelCommits)
			fDecos := m.commitDecorators(fRows, fIdx)
			full := m.renderPanel(panelCommits, label, fRows, fDecos, 80, boxH)

			// New: only the visible window is styled.
			wRows, _, wDecos := m.commitBody(boxH)
			win := m.renderPanel(panelCommits, label, wRows, wDecos, 80, boxH)

			if full != win {
				t.Fatalf("mode=%v sel=%d: windowed render != full\n--- FULL ---\n%s\n--- WIN ---\n%s",
					mode, sel, full, win)
			}
		}
	}
}

// Filtering must still work with lazy rows: only matching commits remain, in order.
func TestCommitBodyWithFilter(t *testing.T) {
	m := benchModel(50, 40, 8)
	m.commits[7].Subject = "UNIQUE_NEEDLE_subject"
	m.filterQuery = "UNIQUE_NEEDLE"
	m.filterPanel = panelCommits
	idx := m.displayIndices(panelCommits)
	if len(idx) != 1 || idx[0] != 7 {
		t.Fatalf("filter idx = %v, want [7]", idx)
	}
	_ = fmt.Sprint(idx)
}
