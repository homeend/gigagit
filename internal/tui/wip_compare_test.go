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
