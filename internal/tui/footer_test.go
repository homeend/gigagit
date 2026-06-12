package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

// footerModel is an idle fixture: Branches focused (zero value), two branches
// (main is HEAD, selected by default), two worktrees ("/repo" is current,
// selected by default). Every panel except Status/Commits has rows.
func footerModel() Model {
	return Model{
		width:     120,
		height:    40,
		sel:       map[panel]int{},
		sortModes: map[panel]sortMode{},
		status:    model.WorkingTreeStatus{Branch: "main"},
		branches: []model.Branch{
			{Name: "main", IsHead: true},
			{Name: "feat/x"},
		},
		worktrees: []model.Worktree{
			{Path: "/repo", Branch: "main"},
			{Path: "/repo/wt-x", Branch: "feat/x"},
		},
		currentWorktree: "/repo",
	}
}

// The honesty tests pin the predicate-sharing contract: when a shared
// predicate is false, the key must be a complete no-op (no op spawned, no
// state change) — these three used to start operations git then rejects.

func TestSwitchKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel() // sel 0 = main, the checked-out branch
	u, cmd := m.Update(keyMsg("s"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("s on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnHeadBranch(t *testing.T) {
	m := footerModel()
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the checked-out branch must be a no-op")
	}
}

func TestDeleteKeyNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees // sel 0 = /repo = currentWorktree
	u, cmd := m.Update(keyMsg("d"))
	mm := u.(Model)
	if cmd != nil || mm.running {
		t.Fatal("d on the current worktree must be a no-op")
	}
}

func TestEnterNoOpOnCurrentWorktree(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	u, cmd := m.Update(keyMsg("enter"))
	mm := u.(Model)
	// reRoot sets loading (not running like startOp)
	if cmd != nil || mm.loading {
		t.Fatal("enter on the current worktree must not re-root")
	}
}

func TestSwitchKeyNoOpWhileLoading(t *testing.T) {
	m := footerModel()
	m.loading = true
	m.sel[panelBranches] = 1 // feat/x would otherwise be switchable
	u, cmd := m.Update(keyMsg("s"))
	if cmd != nil || u.(Model).running {
		t.Fatal("s while loading must be a no-op")
	}
}

func TestMarkKeyNoOpWhileRunning(t *testing.T) {
	m := footerModel()
	m.running = true
	u, _ := m.Update(keyMsg("m"))
	if u.(Model).mark != nil {
		t.Fatal("m while an op runs must not mark")
	}
}

func TestPredicatesOnSelectableRows(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x (not HEAD)
	if !m.canSwitchBranch() || !m.canDeleteBranch() || !m.canOpenBranchPopup() || !m.canOpenWorktreePopup() {
		t.Error("branch predicates must hold on an idle model with a non-HEAD row selected")
	}
	m.sel[panelWorktrees] = 1 // /repo/wt-x (not current)
	if !m.canDeleteWorktree() || !m.canEnterWorktree() {
		t.Error("worktree predicates must hold on a non-current worktree row")
	}
	if !m.canMark() {
		t.Error("canMark must hold when the focused panel has a selected row")
	}
	m.running = true
	if m.opsIdle() || m.canSwitchBranch() || m.canMark() {
		t.Error("all op predicates must be false while running")
	}
}

func TestFooterBranchesContextNonHead(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	f := m.footerLine()
	for _, want := range []string{
		"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark",
		"•", "[p]ull", "[P]ush", "[q] quit",
	} {
		if !strings.Contains(f, want) {
			t.Errorf("footer %q must contain %q", f, want)
		}
	}
}

func TestFooterHeadBranchHidesSwitchAndDelete(t *testing.T) {
	m := footerModel() // sel 0 = main (HEAD)
	f := m.footerLine()
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[d]elete") {
		t.Errorf("HEAD branch row must not offer switch/delete: %q", f)
	}
	if !strings.Contains(f, "[b]ranch") || !strings.Contains(f, "[w]orktree") {
		t.Errorf("branch/worktree creation stays available on the HEAD row: %q", f)
	}
}

func TestFooterWorktreesContext(t *testing.T) {
	m := footerModel()
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 1 // not the current worktree
	f := m.footerLine()
	if !strings.Contains(f, "[enter] switch") || !strings.Contains(f, "[d]elete") {
		t.Errorf("other-worktree row must offer enter/delete: %q", f)
	}
	if strings.Contains(f, "[s]witch") || strings.Contains(f, "[b]ranch") {
		t.Errorf("branch actions must not show on Worktrees focus: %q", f)
	}
	m.sel[panelWorktrees] = 0 // the current worktree
	f = m.footerLine()
	if strings.Contains(f, "[enter] switch") || strings.Contains(f, "[d]elete") {
		t.Errorf("current-worktree row must not offer enter/delete: %q", f)
	}
}

func TestFooterStatusFocusHasNoContextSegment(t *testing.T) {
	m := footerModel()
	m.focus = panelStatus
	f := m.footerLine()
	if strings.Contains(f, "•") {
		t.Errorf("Status focus has no context actions, no separator: %q", f)
	}
	if !strings.HasPrefix(f, "[p]ull") {
		t.Errorf("global tail must lead when there is no context segment: %q", f)
	}
}

func TestFooterMarkStates(t *testing.T) {
	m := footerModel()
	m.sel[panelBranches] = 1 // feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m]ark") || strings.Contains(f, "[m] pair") {
		t.Errorf("no mark yet: want [m]ark only, got %q", f)
	}
	u, _ := m.Update(keyMsg("m")) // mark feat/x
	m = u.(Model)
	if f := m.footerLine(); !strings.Contains(f, "[m] unmark") {
		t.Errorf("cursor on the marked row: want [m] unmark, got %q", f)
	}
	m.sel[panelBranches] = 0 // cursor to main; mark still on feat/x
	if f := m.footerLine(); !strings.Contains(f, "[m] pair") {
		t.Errorf("cursor on another row with a live mark: want [m] pair, got %q", f)
	}
}

func TestFooterRunningCollapses(t *testing.T) {
	m := footerModel()
	m.running = true
	want := "[tab] focus [?] help [q] quit"
	if f := m.footerLine(); f != want {
		t.Errorf("running footer = %q, want %q", f, want)
	}
}

func TestFooterFilterTypingOverride(t *testing.T) {
	m := footerModel()
	m.filterTyping = true
	want := "filter: type to search  [enter] keep  [esc] cancel"
	if f := m.footerLine(); f != want {
		t.Errorf("filter-typing footer = %q, want %q", f, want)
	}
}

func TestFooterEmptyPanelsHideRowActions(t *testing.T) {
	m := Model{width: 80, height: 24, sel: map[panel]int{}, sortModes: map[panel]sortMode{}}
	f := m.footerLine()
	for _, banned := range []string{"[s]witch", "[b]ranch", "[w]orktree", "[d]elete", "[m]ark", "[P]ush"} {
		if strings.Contains(f, banned) {
			t.Errorf("empty repo: %q must not appear in %q", banned, f)
		}
	}
	if !strings.Contains(f, "[p]ull") {
		t.Errorf("global tail must survive an empty repo: %q", f)
	}
}
