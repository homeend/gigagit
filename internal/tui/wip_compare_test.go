package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCompareKeyResolvers(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // commits[0]=tip
	wt := wipKey(wipRow{kind: wipWorktree})
	stg := wipKey(wipRow{kind: wipStaged})

	if e := m.compareKeyEndpoint(wt); e.Kind != model.EndpointWorkTree {
		t.Fatalf("worktree key → %v", e.Kind)
	}
	if e := m.compareKeyEndpoint(stg); e.Kind != model.EndpointIndex {
		t.Fatalf("staged key → %v", e.Kind)
	}
	h := m.commits[1].Hash
	if e := m.compareKeyEndpoint(h); e.Kind != model.EndpointCommit || e.Hash != h {
		t.Fatalf("commit key → %+v", e)
	}
	// rank: working tree newest (-2) < staged (-1) < tip commit (0) < older (1)
	if !(m.compareKeyRank(wt) < m.compareKeyRank(stg) &&
		m.compareKeyRank(stg) < m.compareKeyRank(m.commits[0].Hash) &&
		m.compareKeyRank(m.commits[0].Hash) < m.compareKeyRank(m.commits[1].Hash)) {
		t.Fatal("rank ordering wrong (want wt < staged < tip < older)")
	}
}

func TestMarkCommitUnderDirtyTreeHitsRightRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Tip commit: unified index = wipCount (display row just below the WIP rows).
	m.sel[panelCommits] = m.wipCount()
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	if m.mark == nil {
		t.Fatal("m must mark the commit row")
	}
	if m.mark.key != m.commits[0].Hash {
		t.Fatalf("mark key = %q, want tip hash %q (off-by-wipCount bug)", m.mark.key, m.commits[0].Hash)
	}
	if md := m.markDisplayIndex(panelCommits); md != m.wipCount() {
		t.Fatalf("mark display row = %d, want %d", md, m.wipCount())
	}
}

func TestMarkAndToggleWipRow(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	// Mark the Working tree row (unified index 0).
	m.sel[panelCommits] = 0
	if !m.canMark() {
		t.Fatal("canMark must allow a wip row")
	}
	u, _ := m.Update(keyMsg("m"))
	m = u.(Model)
	if m.mark == nil || m.mark.key != wipKey(wipRow{kind: wipWorktree}) {
		t.Fatalf("mark key = %v, want worktree sentinel", m.mark)
	}

	// Toggle the Staged row (unified index 1) into the ◉ set.
	m.sel[panelCommits] = 1
	r, ok := m.commitCompareToggleRow()
	if !ok {
		t.Fatal("compare-toggle must be available on a wip row")
	}
	uu, _ := r.run(m)
	m = uu.(Model)
	if !m.commitCompareSet[wipKey(wipRow{kind: wipStaged})] {
		t.Fatalf("staged sentinel not in compare set: %v", m.commitCompareSet)
	}
}

func TestCompareMarkedWipVsCommit(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()

	m.mark = &markState{panel: panelCommits, key: wipKey(wipRow{kind: wipWorktree}), display: "working tree"}
	m.sel[panelCommits] = m.wipCount() + 1 // second commit from the tip

	r, ok := m.commitCompareMarkedRow()
	if !ok {
		t.Fatal("compare-with-marked must be available with a wip mark + commit selection")
	}
	u, _ := r.run(m)
	mm := u.(Model)
	if mm.filesView == nil {
		t.Fatal("compare must open the files view")
	}
	// Older (commit) → newer (working tree): left=commit, right=working tree.
	if mm.filesLeft.Kind != model.EndpointCommit || mm.filesRight.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Commit↔WorkTree", mm.filesLeft.Kind, mm.filesRight.Kind)
	}
}

func TestCompareSelectionWithWipTwo(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.commitCompareSet = map[string]bool{
		wipKey(wipRow{kind: wipWorktree}): true,
		wipKey(wipRow{kind: wipStaged}):   true,
	}
	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("two wip rows must compare: %s", note)
	}
	if left.Kind != model.EndpointIndex || right.Kind != model.EndpointWorkTree {
		t.Fatalf("endpoints = %v↔%v, want Index↔WorkTree", left.Kind, right.Kind)
	}
}

func TestCompareSelectionRangeRefusesWip(t *testing.T) {
	m := loadedModelLinearCommits(t, 4)
	m.status = dirtyStatus()
	m.wipRows = deriveWipRows(m.status)
	m = m.rebuildCommitGraph()
	m.commitCompareSet = map[string]bool{
		wipKey(wipRow{kind: wipWorktree}): true,
		m.commits[0].Hash:                 true,
		m.commits[1].Hash:                 true,
	}
	if _, _, _, ok := m.compareSelectionEndpoints(); ok {
		t.Fatal("a 3+ range containing a wip row must be refused")
	}
}

// TestCompareSelectionSurvivesFilter guards against coupling the ◉ selection to
// the active / filter: two selected commits must still compare even when the
// filter hides one of them.
func TestCompareSelectionSurvivesFilter(t *testing.T) {
	m := loadedModelLinearCommits(t, 4) // subjects c0..c3
	m.focus = panelCommits
	m.commitCompareSet = map[string]bool{
		m.commits[0].Hash: true, // c3 (tip)
		m.commits[3].Hash: true, // c0 (root)
	}
	// Filter so only the tip is visible.
	m.filterPanel = panelCommits
	m.filterQuery = m.commits[0].Subject

	left, right, note, ok := m.compareSelectionEndpoints()
	if !ok {
		t.Fatalf("selection must survive an active filter: %s", note)
	}
	if left.Hash != m.commits[3].Hash || right.Hash != m.commits[0].Hash {
		t.Fatalf("endpoints = %s↔%s, want root↔tip", left.Hash, right.Hash)
	}
}
