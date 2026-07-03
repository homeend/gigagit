package tui

import (
	"strings"
	"testing"
)

func TestSpaceMarksAndUnmarksCommit(t *testing.T) {
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
