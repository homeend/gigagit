package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// rowOfMember is the display row of member mi's first non-heading entry in
// panel p (-1 when none); headingOf is its heading's row.
func rowOfMember(m Model, p panel, mi int) (row, heading int) {
	row, heading = -1, -1
	idx := m.displayIndices(p)
	for i, u := range idx {
		var member int
		var head bool
		switch p {
		case panelBranches:
			e := m.branchEntries()[u]
			member, head = e.member, e.heading
		case panelWorktrees:
			e := m.worktreeEntries()[u]
			member, head = e.member, e.heading
		}
		if member != mi {
			continue
		}
		if head {
			heading = i
		} else if row < 0 {
			row = i
		}
	}
	return row, heading
}

// Enter on another member's worktree row adopts into that worktree.
func TestEnterOnOtherMemberWorktreeRow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelWorktrees)
	row, _ := rowOfMember(m, panelWorktrees, 1)
	if row < 0 {
		t.Fatal("B has no worktree row")
	}
	m.sel[panelWorktrees] = row
	nm, _ := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.liveRepo() != b || m.home != b.key || !m.inWorkspace() {
		t.Fatalf("live ok=%v home=%q ws=%v", m.liveRepo() == b, m.home, m.inWorkspace())
	}
}

// A click on a member's heading adopts into its last-viewed worktree, or
// its main checkout when it was never viewed.
func TestHeadingClickAdoptsLastViewed(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelWorktrees)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = nm.(Model)
	_, heading := rowOfMember(m, panelWorktrees, 1)
	if heading < 0 {
		t.Fatal("B has no heading")
	}
	nm2, _ := m.clickHeading(panelWorktrees, heading)
	m = nm2
	if m.liveRepo() != b || m.home != b.key || model.KeyOf(m.currentWorktree) != b.key {
		t.Fatalf("heading click: live ok=%v home=%q current=%q", m.liveRepo() == b, m.home, m.currentWorktree)
	}
}

// An action key on another member's branch row switches the view there
// first, then runs: enter (go to tip) lands with B live.
func TestBranchActionOnOtherMemberSwitchesFirst(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelBranches)
	row, _ := rowOfMember(m, panelBranches, 1)
	if row < 0 {
		t.Fatal("B has no branch row")
	}
	m.sel[panelBranches] = row
	home := m.home
	nm, _ := m.Update(keyMsg("enter"))
	m = nm.(Model)
	if m.liveRepo() != b {
		t.Fatal("enter on B's branch did not switch to B first")
	}
	if m.home != home {
		t.Fatal("a branch action adopted; it should only look")
	}
	if m.sel[panelBranches] != row {
		t.Fatalf("the cursor moved from %d to %d across the switch", row, m.sel[panelBranches])
	}
	// Navigation keys never switch: up from B's row stays in A's view.
	m2 := m
	nm, _ = m2.Update(keyMsg("up"))
	if nm.(Model).liveRepo() != b {
		t.Fatal("navigation switched members")
	}
}

// f between a mark in A and a row in B is refused in place.
func TestCompareAcrossMembersRefused(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelBranches)
	m.mark = &markState{} // something marked in A
	row, _ := rowOfMember(m, panelBranches, 1)
	m.sel[panelBranches] = row
	nm, _ := m.Update(keyMsg("f"))
	m = nm.(Model)
	if m.liveRepo() == b || m.statusMsg != "compare works within one repository" {
		t.Fatalf("live is B=%v status=%q", m.liveRepo() == b, m.statusMsg)
	}
}

// alt+1 with the cursor in B's group acts on B (switched to first), not A.
func TestAltDigitActsOnCursorGroup(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	m = m.activateTab(panelBranches)
	aSlot := m.branchFilterSlot[panelBranches]
	row, _ := rowOfMember(m, panelBranches, 1)
	m.sel[panelBranches] = row
	nm, _ := m.Update(keyMsg("alt+1"))
	m = nm.(Model)
	if m.liveRepo() != b {
		t.Fatal("alt+1 did not act on the cursor's group")
	}
	if m.homeRepo().branchFilterSlot[panelBranches] != aSlot {
		t.Fatal("A's slot changed")
	}
}

// The sort mode is per panel: a name sort orders every group.
func TestSortAppliesToEveryGroup(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	b.branches = []model.Branch{{Name: "zeta"}, {Name: "alpha"}}
	m.branches = []model.Branch{{Name: "mid"}, {Name: "aaa"}}
	m.sortModes[panelBranches] = sortNameAsc
	l := m.listFor(panelBranches)
	var names []string
	for i := 0; i < l.Len(); i++ {
		names = append(names, l.Name(i))
	}
	want := []string{headingText(headingLabel(m.ws.members, 0)), "aaa", "mid", headingText(headingLabel(m.ws.members, 1)), "alpha", "zeta"}
	if len(names) != len(want) {
		t.Fatalf("names = %v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("names = %v, want %v", names, want)
		}
	}
}
