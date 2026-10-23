package tui

import (
	"strings"
	"testing"
)

// Inside a workspace every member's group carries its own head star, so the
// Branches tab marks the branch checked out in the worktree ON SCREEN with
// a ':' after the star (the live member only); the other members' heads
// keep a bare star in an aligned column. A single repository shows no such
// column — its star already means the viewed worktree.
func TestViewedBranchMarkerInWorkspace(t *testing.T) {
	m := loadedModel(t)
	for _, row := range m.branchRows() {
		if strings.HasPrefix(row, "*:") {
			t.Fatalf("single repository marks the viewed branch: %q", row)
		}
	}
	m, _ = openTwo(t, m)
	ents := m.branchEntries()
	l := m.listFor(panelBranches)
	if l.Len() != len(ents) {
		t.Fatalf("rows %d != entries %d", l.Len(), len(ents))
	}
	var live, other string
	for i, e := range ents {
		if e.heading || e.sub() {
			continue
		}
		mm := m.asMember(e.member)
		if e.br < 0 || e.br >= len(mm.branches) || !mm.branches[e.br].IsHead {
			continue
		}
		if e.member == m.liveMember() {
			live = l.Row(i)
		} else {
			other = l.Row(i)
		}
	}
	if !strings.HasPrefix(live, "*: ") {
		t.Fatalf("the viewed worktree's branch row = %q, want a '*: ' gutter", live)
	}
	if !strings.HasPrefix(other, "*  ") {
		t.Fatalf("another member's head row = %q, want an aligned '*  ' gutter", other)
	}
}
