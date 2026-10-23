package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// With no workspace open the ring is exactly today's Branches-tab order of
// the worktrees.
func TestRingSingleRepoMatchesWorktreeOrder(t *testing.T) {
	m := loadedModel(t)
	m, _ = addWorktree(t, m, "wt2")
	ring := m.worktreeRing()
	order := m.worktreeOrder()
	if len(ring) != len(order) || len(ring) != 2 {
		t.Fatalf("ring=%d order=%d", len(ring), len(order))
	}
	for i, idx := range order {
		if ring[i].member != 0 || model.KeyOf(ring[i].path) != model.KeyOf(m.worktrees[idx].Path) {
			t.Fatalf("stop %d = %+v, want %s", i, ring[i], m.worktrees[idx].Path)
		}
	}
	if m.ringIndex(m.currentWorktree) < 0 || m.ringIndex(filepath.Join(t.TempDir(), "x")) != -1 {
		t.Fatal("ringIndex")
	}
}

// In a workspace the ring is every member's worktrees, group after group in
// workspace order.
func TestRingSpansMembersInOrder(t *testing.T) {
	m := loadedModel(t)
	m, _ = addWorktree(t, m, "wt2")
	m, b := workspaceOf(t, m)
	ring := m.worktreeRing()
	if len(ring) != 3 {
		t.Fatalf("ring = %+v, want 3 stops", ring)
	}
	if ring[0].member != 0 || ring[1].member != 0 || ring[2].member != 1 || model.KeyOf(ring[2].path) != b.key {
		t.Fatalf("ring = %+v", ring)
	}
	if m.ringIndex(b.mainPath) != 2 {
		t.Fatalf("ringIndex(B) = %d", m.ringIndex(b.mainPath))
	}
}

// Both tabs list every member under a heading row; the cursor never rests
// on a heading.
func TestEntriesHaveHeadings(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := workspaceOf(t, m)
	wts := m.worktreeEntries()
	brs := m.branchEntries()
	if len(wts) < 4 || !wts[0].heading || wts[0].member != 0 || wts[1].heading {
		t.Fatalf("worktree entries = %+v", wts)
	}
	if !brs[0].heading || brs[0].member != 0 {
		t.Fatalf("branch entries = %+v", brs)
	}
	// Exactly one heading per member, each followed by that member's rows.
	heads := 0
	for _, e := range brs {
		if e.heading {
			heads++
		}
	}
	if heads != 2 {
		t.Fatalf("%d headings, want 2", heads)
	}
	rows := m.listFor(panelBranches)
	if !strings.Contains(rows.Row(0), m.homeRepo().name) {
		t.Fatalf("heading row 0 = %q", rows.Row(0))
	}
	// B's heading sits right after A's last row and names B.
	bHead := -1
	for i, e := range brs {
		if e.heading && e.member == 1 {
			bHead = i
		}
	}
	if bHead < 0 || !strings.Contains(rows.Row(bHead), b.name) {
		t.Fatalf("B heading at %d: %q", bHead, rows.Row(bHead))
	}
	// Cursor: down from the row above B's heading lands on B's first row.
	m.focus, m.activeLeftTab = panelBranches, panelBranches
	m.sel[panelBranches] = bHead - 1
	nm, _ := m.Update(keyMsg("down"))
	m = nm.(Model)
	if m.sel[panelBranches] != bHead+1 {
		t.Fatalf("down from %d landed on %d (heading at %d)", bHead-1, m.sel[panelBranches], bHead)
	}
	nm, _ = m.Update(keyMsg("up"))
	m = nm.(Model)
	if m.sel[panelBranches] != bHead-1 {
		t.Fatalf("up from %d landed on %d", bHead+1, m.sel[panelBranches])
	}
	// The first row is never the heading: up from row 1 stays on row 1.
	m.sel[panelBranches] = 1
	nm, _ = m.Update(keyMsg("up"))
	if nm.(Model).sel[panelBranches] != 1 {
		t.Fatalf("up from the first row landed on %d", nm.(Model).sel[panelBranches])
	}
}

// Two members with the same directory name are told apart by their parent.
func TestHeadingsDisambiguated(t *testing.T) {
	a := newRepoView(filepath.Join("/work", "shop", "api"), nil)
	b := newRepoView(filepath.Join("/work", "billing", "api"), nil)
	c := newRepoView(filepath.Join("/work", "web"), nil)
	members := []*repoView{a, b, c}
	if got := headingLabel(members, 0); got != "api (shop)" {
		t.Fatalf("a = %q", got)
	}
	if got := headingLabel(members, 1); got != "api (billing)" {
		t.Fatalf("b = %q", got)
	}
	if got := headingLabel(members, 2); got != "web" {
		t.Fatalf("c = %q", got)
	}
}

// The session ring (alt+a) walks every member's running agents in ring
// order: A's first, then B's.
func TestSessionRingCrossesMembers(t *testing.T) {
	m := loadedModel(t)
	m, b := workspaceOf(t, m)
	sa := startTestSession(t, m, "sleep 5") // in A (home); swaps in the test manager
	sb, err := domain.Sessions().Start(sessionSpecForTest("in-b", b.mainPath))
	if err != nil {
		t.Fatal(err)
	}
	aID, bID := sa.Info().ID, sb.Info().ID
	ring := m.sessionRing(false)
	ids := []domain.SessionID{}
	for _, s := range ring {
		ids = append(ids, s.ID)
	}
	if len(ring) != 2 || ring[0].ID != aID || ring[1].ID != bID {
		t.Fatalf("session ring = %v, want [%s %s]", ids, aID, bID)
	}
}
