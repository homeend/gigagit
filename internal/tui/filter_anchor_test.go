package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/homeend/gigagit/internal/model"
)

// The /-filter must search from the cursor, not from the top: engaging it keeps
// the cursor in place, each query edit re-seats the cursor on the nearest match
// at or after it (wrapping like the @-snap), and leaving the filter re-seats the
// cursor on the same row in the full list.

func TestSlashEntryKeepsCursor(t *testing.T) {
	m := eagerModel(t, []model.Commit{
		{Hash: "a", Subject: "one"}, {Hash: "b", Subject: "two"}, {Hash: "c", Subject: "three"},
	})
	m.sel[panelCommits] = 2
	nm, _ := m.Update(key("/"))
	got := nm.(Model)
	if !got.filterTyping || got.filterPanel != panelCommits {
		t.Fatalf("/ should open filter typing on the focused panel (typing=%v panel=%v)", got.filterTyping, got.filterPanel)
	}
	if got.sel[panelCommits] != 2 {
		t.Fatalf("sel = %d, want 2 (engaging / must not jump the cursor to the top)", got.sel[panelCommits])
	}
}

func TestFilterTypingSnapsToMatchAtOrAfterCursor(t *testing.T) {
	m := eagerModel(t, []model.Commit{
		{Hash: "a", Subject: "docs one"}, {Hash: "b", Subject: "noise"}, {Hash: "c", Subject: "docs two"},
	})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.sel[panelCommits] = 1 // on "noise", mid-list
	nm, _ := m.Update(key("docs"))
	got := nm.(Model)
	idx := got.displayIndices(panelCommits)
	// Filtered display = [a, c]; the nearest match at/after "noise" is c.
	if got.sel[panelCommits] != 1 || idx[got.sel[panelCommits]] != 2 {
		t.Fatalf("sel = %d (backing %d), want 1 (backing 2 — the match below the cursor, not the top)",
			got.sel[panelCommits], idx[got.sel[panelCommits]])
	}
}

func TestFilterTypingWrapsToTopWhenNoMatchBelow(t *testing.T) {
	m := eagerModel(t, []model.Commit{
		{Hash: "a", Subject: "docs alpha"}, {Hash: "b", Subject: "docs beta"}, {Hash: "c", Subject: "zzz"},
	})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.sel[panelCommits] = 2 // on "zzz", below every match
	nm, _ := m.Update(key("docs"))
	got := nm.(Model)
	if got.sel[panelCommits] != 0 {
		t.Fatalf("sel = %d, want 0 (no match below the cursor wraps to the top, the @-snap rule)", got.sel[panelCommits])
	}
}

func TestFilterBackspaceKeepsRow(t *testing.T) {
	m := eagerModel(t, []model.Commit{
		{Hash: "a", Subject: "car"}, {Hash: "b", Subject: "cart"}, {Hash: "c", Subject: "dog"},
	})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.filterQuery = "cart"
	m.sel[panelCommits] = 0 // filtered display = [b]; cursor on backing 1
	nm, _ := m.Update(keyType(tea.KeyBackspace))
	got := nm.(Model)
	// Widened to "car": display = [a, b]; the cursor must stay on b (display 1).
	if got.filterQuery != "car" {
		t.Fatalf("filterQuery = %q, want %q", got.filterQuery, "car")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (backspace must keep the cursor on the same row, not reset to the top)", got.sel[panelCommits])
	}
}

func TestFilterEscReseatsCursorInFullList(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "noise"}, {Hash: "b", Subject: "docs"}})
	m.filterTyping = true
	m.filterPanel = panelCommits
	m.filterQuery = "docs"
	m.sel[panelCommits] = 0 // filtered display = [b]
	nm, _ := m.Update(keyType(tea.KeyEsc))
	got := nm.(Model)
	if got.filterQuery != "" || got.filterTyping {
		t.Fatal("esc must clear the filter")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (esc must keep the cursor on the same commit in the full list)", got.sel[panelCommits])
	}
}

func TestCommittedFilterEscReseatsCursor(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "noise"}, {Hash: "b", Subject: "docs"}})
	m.filterPanel = panelCommits
	m.filterQuery = "docs" // committed (not typing)
	m.sel[panelCommits] = 0
	nm, _ := m.Update(keyType(tea.KeyEsc))
	got := nm.(Model)
	if got.filterQuery != "" {
		t.Fatal("esc must clear the committed filter")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (esc must keep the cursor on the same commit)", got.sel[panelCommits])
	}
}

func TestHighlightEngageOverFilterReseatsCursor(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "noise"}, {Hash: "b", Subject: "docs"}})
	m.filterPanel = panelCommits
	m.filterQuery = "docs" // committed / filter
	m.sel[panelCommits] = 0
	nm, _ := m.Update(key("@"))
	got := nm.(Model)
	if !got.highlightTyping || got.filterQuery != "" {
		t.Fatalf("@ must clear the / filter and start highlight typing (typing=%v query=%q)", got.highlightTyping, got.filterQuery)
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (@ over a / filter must keep the cursor on the same commit)", got.sel[panelCommits])
	}
}

func TestCtrlRClearFilterReseatsCursor(t *testing.T) {
	m := eagerModel(t, []model.Commit{{Hash: "a", Subject: "noise"}, {Hash: "b", Subject: "docs"}})
	m.filterPanel = panelCommits
	m.filterQuery = "docs"
	m.sel[panelCommits] = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	got := nm.(Model)
	if got.filterQuery != "" {
		t.Fatal("ctrl+r must clear the filter")
	}
	if got.sel[panelCommits] != 1 {
		t.Fatalf("sel = %d, want 1 (ctrl+r must keep the cursor on the same commit)", got.sel[panelCommits])
	}
}

func TestFilterTypingSnapsOnBranchesPanel(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches = []model.Branch{{Name: "one-x"}, {Name: "two"}, {Name: "three-x"}}
	m.focus = panelBranches
	m.filterTyping = true
	m.filterPanel = panelBranches
	m.sel[panelBranches] = 1 // on "two"
	nm, _ := m.Update(key("x"))
	got := nm.(Model)
	idx := got.displayIndices(panelBranches)
	// Filtered display = [one-x, three-x]; nearest at/after "two" is three-x.
	if got.sel[panelBranches] != 1 || idx[got.sel[panelBranches]] != 2 {
		t.Fatalf("sel = %d (backing %d), want 1 (backing 2 — the / snap is panel-generic)",
			got.sel[panelBranches], idx[got.sel[panelBranches]])
	}
}

func TestSnapFilterSelNonDefaultSort(t *testing.T) {
	m := newTestModelForReload(t)
	m.branches = []model.Branch{{Name: "delta"}, {Name: "alpha"}, {Name: "charlie"}, {Name: "bravo"}}
	m.sortModes[panelBranches] = sortNameAsc
	// Display order: alpha(1), bravo(3), charlie(2), delta(0).
	for anchor, want := range map[int]int{1: 0, 3: 1, 2: 2, 0: 3} {
		got := m.snapFilterSel(panelBranches, anchor)
		if got.sel[panelBranches] != want {
			t.Fatalf("anchor %d: sel = %d, want %d (at-or-after must compare in DISPLAY order under a sort)",
				anchor, got.sel[panelBranches], want)
		}
	}
}
