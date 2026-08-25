package tui

import (
	"strings"
	"testing"
)

func TestSpaceMarksAndUnmarksCommit(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0

	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[m.commits[0].Hash] {
		t.Fatalf("space must mark the cursor commit, set=%v", m.commitCompareSet)
	}
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("space on a marked commit must unmark it, set=%v", m.commitCompareSet)
	}
}

func TestSecondSpaceOpensCompareAndKeepsMarks(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	u, _ := m.Update(keyMsg("space")) // first mark: the tip
	m = u.(Model)
	m.sel[panelCommits] = 1
	u, cmd := m.Update(keyMsg("space")) // second mark → compare opens
	m = u.(Model)
	if m.filesView == nil || !m.inCompareMode() {
		t.Fatal("second space-mark must open the compare files view")
	}
	if m.filesLeft.Hash != m.commits[1].Hash || m.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints %s ↔ %s, want older %s ↔ newer %s",
			m.filesLeft.Hash, m.filesRight.Hash, m.commits[1].Hash, m.commits[0].Hash)
	}
	if !m.commitCompareSet[m.commits[0].Hash] || !m.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("marks must persist after the compare opens, set=%v", m.commitCompareSet)
	}
	if cmd == nil {
		t.Fatal("compare open must start the file-list load")
	}
}

func TestThirdSpaceRefusedAtTwoMarks(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true, m.commits[1].Hash: true}
	m.sel[panelCommits] = 2
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if m.commitCompareSet[m.commits[2].Hash] || len(m.commitCompareSet) != 2 {
		t.Fatalf("space must not grow a 2-mark set, set=%v", m.commitCompareSet)
	}
	if !strings.Contains(m.statusMsg, "2 commits already marked") {
		t.Fatalf("refusal must set the status hint, got %q", m.statusMsg)
	}
}

func TestStaleMarkDoesNotEatASpaceSlot(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash:                          true,
		"deadbeefdeadbeefdeadbeefdeadbeefdeadbeef": true, // not in the feed
	}
	m.sel[panelCommits] = 1
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[m.commits[1].Hash] {
		t.Fatalf("1 valid + 1 stale mark must still leave a slot, set=%v", m.commitCompareSet)
	}
	if !m.inCompareMode() {
		t.Fatal("the second VALID mark must open the compare")
	}
}

func TestSpaceTogglesWipRow(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 0 // ◇ Working tree pseudo-row
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.commitCompareSet[wipKey(wipRow{kind: wipWorktree})] {
		t.Fatalf("space must mark the WIP row's sentinel key, set=%v", m.commitCompareSet)
	}
	m.sel[panelCommits] = m.wipCount() // the tip commit's unified row
	u, _ = m.Update(keyMsg("space"))
	m = u.(Model)
	if !m.inCompareMode() {
		t.Fatal("commit + WIP pair must open the compare")
	}
}

func TestSpaceInertWhileOpRunning(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	m.running = true // an async op is in flight
	u, _ := m.Update(keyMsg("space"))
	m = u.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("space must not edit the ◉ set mid-op (m's canMark gate), set=%v", m.commitCompareSet)
	}
}

// reRoot must clear commitCompareSet so ◉ marks from the old repo don't eat
// the two space slots or skew Unmark-all's count in the new repo.
func TestReRootClearsCommitCompareSet(t *testing.T) {
	t.Parallel()
	m := Model{commitCompareSet: map[string]bool{"deadbeef": true, "beadfeed": true}}
	updated, _ := m.reRoot(t.TempDir())
	if got := updated.(Model).commitCompareSet; got != nil {
		t.Fatalf("commitCompareSet = %v after reRoot, want nil", got)
	}
}

// Esc on the focused Commits panel clears ALL ◉ marks in one press (space/m
// re-mark cheaply; partial unmark is what space is for).
func TestEscClearsCommitMarksWhenCommitsFocused(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true, m.commits[1].Hash: true}
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("esc on Commits must clear all ◉ marks, set=%v", m.commitCompareSet)
	}
}

// The clear is focus-scoped: esc pressed in another panel leaves the ◉ set.
func TestEscElsewhereLeavesCommitMarks(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelBranches
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if !m.commitCompareSet[m.commits[0].Hash] {
		t.Fatalf("esc outside Commits must not touch the ◉ set, set=%v", m.commitCompareSet)
	}
}

// Peel order: ◉ marks clear before a committed @-highlight — one state per press.
func TestEscPeelsMarksBeforeHighlight(t *testing.T) {
	t.Parallel()
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{m.commits[0].Hash: true}
	m.highlightQuery = "c1"
	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if len(m.commitCompareSet) != 0 {
		t.Fatalf("first esc must clear the ◉ marks, set=%v", m.commitCompareSet)
	}
	if m.highlightQuery == "" {
		t.Fatal("first esc must leave the highlight for the second press")
	}
	u, _ = m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.highlightQuery != "" {
		t.Fatalf("second esc must clear the highlight, got %q", m.highlightQuery)
	}
}

// TestFooterAdvertisesSpaceStates pins the two always-true footer hints: mark
// (unmarked cursor, ≤1 raw mark) and unmark (marked cursor). With 2 marks and
// an unmarked cursor the outcome depends on mark validity, so no space hint
// shows — the footer never advertises an outcome that might not happen.
func TestFooterAdvertisesSpaceStates(t *testing.T) {
	t.Parallel()
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
