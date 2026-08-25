package tui

import (
	"context"
	"strings"
	"testing"
)

// TestDropSelectionRowAppears: with 2+ commits in the ◉ set, "Drop N selected
// commits" appears and counts the selection.
func TestDropSelectionRowAppears(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits

	if _, ok := m.commitDropSelectionRow(); ok {
		t.Fatal("drop-selection row must be absent with an empty set")
	}
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[1].Hash)
	row, ok := m.commitDropSelectionRow()
	if !ok {
		t.Fatal("drop-selection row must appear with 2 commits selected")
	}
	if want := "Drop 2 selected commits"; row.label != want {
		t.Fatalf("label = %q, want %q", row.label, want)
	}
}

// TestStaleKeyDoesNotInflateCount is the reported bug: a key for a row that no
// longer exists (e.g. a commit a prior rebase dropped) must NOT be counted by
// the drop / squash / compare labels. Selecting 2 live commits with 1 stale key
// in the set must read as "2", not "3".
func TestStaleKeyDoesNotInflateCount(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	// 2 live commits + a hash that isn't in the feed (a rewritten/dropped commit).
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[1].Hash, "deadbeefdeadbeefdeadbeefdeadbeefdeadbeef")

	if got := len(m.validCompareKeys()); got != 2 {
		t.Fatalf("validCompareKeys = %d, want 2 (stale key excluded)", got)
	}
	drop, ok := m.commitDropSelectionRow()
	if !ok || drop.label != "Drop 2 selected commits" {
		t.Fatalf("drop label = %q (ok=%v), want \"Drop 2 selected commits\"", drop.label, ok)
	}
	sq, ok := m.commitSquashRow()
	if !ok || sq.label != "Squash 2 commits" {
		t.Fatalf("squash label = %q (ok=%v), want \"Squash 2 commits\"", sq.label, ok)
	}
	cmp, ok := m.commitCompareSelectionRow()
	if !ok || cmp.label != "Compare the 2 selected commits" {
		t.Fatalf("compare label = %q (ok=%v), want the 2-commit label", cmp.label, ok)
	}
}

// TestDropSelectionRefusesWip: a working-tree / staged row in the selection is
// refused at run time (drop is commits-only), mirroring squash.
func TestDropSelectionRefusesWip(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.wipRows = []wipRow{{wipWorktree, 1}} // a live WIP row so its sentinel key is valid
	m.commitCompareSet = selectionSet(m.commits[0].Hash, wipKey(wipRow{kind: wipWorktree}))

	row, ok := m.commitDropSelectionRow()
	if !ok {
		t.Fatal("row should still appear; refusal happens on run")
	}
	mm, _ := row.run(m)
	if got := mm.(Model).statusMsg; !strings.Contains(got, "commits-only") {
		t.Fatalf("statusMsg = %q, want a commits-only refusal", got)
	}
}

// TestDropSelectionConsumesSelection: dispatching the drop clears the ◉ set, so
// the count can't accumulate across operations (the second symptom of the bug).
func TestDropSelectionConsumesSelection(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[1].Hash)
	onto := m.commits[1].Hash + "^"
	cs, err := m.svc.CommitRange(context.Background(), onto, m.status.Branch)
	if err != nil {
		t.Fatalf("CommitRange: %v", err)
	}
	mm, _ := m.Update(dropRangeLoadedMsg{
		branch:  m.status.Branch,
		onto:    onto,
		targets: []string{m.commits[0].Hash, m.commits[1].Hash},
		commits: cs,
	})
	if n := len(mm.(Model).commitCompareSet); n != 0 {
		t.Fatalf("selection must be cleared after drop dispatch, got %d", n)
	}
}
