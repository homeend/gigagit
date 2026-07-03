package tui

import (
	"strings"
	"testing"
)

// TestFooterAdvertisesSpaceStates pins the two always-true footer hints: mark
// (unmarked cursor, ≤1 raw mark) and unmark (marked cursor). With 2 marks and
// an unmarked cursor the outcome depends on mark validity, so no space hint
// shows — the footer never advertises an outcome that might not happen.
func TestFooterAdvertisesSpaceStates(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	if f := m.footerLine(); !strings.Contains(f, "[space] mark") {
		t.Fatalf("empty set must advertise [space] mark: %q", f)
	}
	m.commitCompareSet = map[string]bool{m.commits[1].Hash: true}
	if f := m.footerLine(); !strings.Contains(f, "[space] mark") {
		t.Fatalf("one mark + unmarked cursor must still advertise [space] mark: %q", f)
	}
	m.sel[panelCommits] = 1 // cursor onto the mark
	if f := m.footerLine(); !strings.Contains(f, "[space] unmark") {
		t.Fatalf("marked cursor must advertise [space] unmark: %q", f)
	}
	m.commitCompareSet[m.commits[2].Hash] = true
	m.sel[panelCommits] = 0 // 2 marks, cursor unmarked → outcome ambiguous → no hint
	if f := m.footerLine(); strings.Contains(f, "[space]") {
		t.Fatalf("2 marks + unmarked cursor must advertise no space key: %q", f)
	}
}
