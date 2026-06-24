package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/rebaseplan"
)

func selectionSet(keys ...string) map[string]bool {
	s := map[string]bool{}
	for _, k := range keys {
		s[k] = true
	}
	return s
}

// m on the Commits panel toggles the compare selection set; a second m on a
// different commit adds it (no diff), and m again on a selected row toggles off.
func TestMarkOnCommitsTogglesSelectionSet(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits

	m.sel[panelCommits] = 0
	mm, _ := m.handleMarkKey()
	m = mm.(Model)
	k0, _ := m.selectedKey(panelCommits)
	if !m.commitCompareSet[k0] {
		t.Fatalf("first m did not add %s to the selection set", k0)
	}
	if m.filesView != nil {
		t.Fatal("m must not open a compare/files view")
	}

	m.sel[panelCommits] = 1
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	k1, _ := m.selectedKey(panelCommits)
	if !m.commitCompareSet[k0] || !m.commitCompareSet[k1] {
		t.Fatalf("second m did not keep both commits selected: %v", m.commitCompareSet)
	}

	// m again on the same row toggles it off.
	mm, _ = m.handleMarkKey()
	m = mm.(Model)
	if m.commitCompareSet[k1] {
		t.Fatalf("re-marking %s did not toggle it off", k1)
	}
}

func TestSquashNonAdjacentOpensReorderModal(t *testing.T) {
	m := loadedModelLinearCommits(t, 4) // commits[0..3], newest-first
	m.focus = panelCommits
	// Select the newest and the third-newest (a gap at commits[1]).
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[2].Hash)

	onto := m.commits[2].Hash + "^"
	cs, err := m.svc.CommitRange(context.Background(), onto, m.status.Branch)
	if err != nil {
		t.Fatalf("CommitRange: %v", err)
	}
	u, _ := m.Update(squashRangeLoadedMsg{
		branch:  m.status.Branch,
		onto:    onto,
		targets: []string{m.commits[0].Hash, m.commits[2].Hash},
		commits: cs,
	})
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("a non-adjacent squash must open the reorder confirm modal")
	}
	opts := m.modal.req.Options
	if len(opts) == 0 || !strings.Contains(strings.ToLower(opts[0]), "reorder") {
		t.Fatalf("modal options = %v, want a Reorder option first", opts)
	}

	// Choosing Cancel is a no-op: no op starts, the selection is left intact.
	// (This exercises the onResolve closure wiring without firing a live rebase.)
	cm, cmd := m.modal.onResolve(m, "Cancel")
	cmModel := cm.(Model)
	if cmd != nil {
		t.Fatal("Cancel must not start an operation")
	}
	if !cmModel.commitCompareSet[m.commits[0].Hash] || !cmModel.commitCompareSet[m.commits[2].Hash] {
		t.Fatalf("Cancel must leave the selection intact, got %v", cmModel.commitCompareSet)
	}
}

func TestSquashRowVisibleWith2Commits(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	if _, ok := m.commitSquashRow(); ok {
		t.Fatal("squash row should be hidden with <2 selected")
	}
	m.commitCompareSet = selectionSet(m.commits[0].Hash, m.commits[1].Hash)
	row, ok := m.commitSquashRow()
	if !ok {
		t.Fatal("squash row should be visible with 2 commits selected")
	}
	if !strings.Contains(row.label, "Squash") {
		t.Fatalf("label = %q, want it to mention Squash", row.label)
	}
}

func TestSquashRefusesWipInSelection(t *testing.T) {
	m := loadedModelLinearCommits(t, 3)
	m.focus = panelCommits
	m.commitCompareSet = selectionSet(m.commits[0].Hash, wipKey(wipRow{kind: wipWorktree}))
	row, ok := m.commitSquashRow()
	if !ok {
		t.Fatal("row should still appear; refusal happens on run")
	}
	mm, _ := row.run(m)
	if got := mm.(Model).statusMsg; !strings.Contains(got, "commits-only") {
		t.Fatalf("statusMsg = %q, want a commits-only refusal", got)
	}
}

// The squash plan's order (Pick/Squash) is derived from the real onto..HEAD
// range, not the feed: the older commit is Pick, the newer is Squash.
func TestSquashBuildsAdjacentPlanFromRange(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // newest-first feed: commits[0] newest
	m.focus = panelCommits

	newer, older := m.commits[0].Hash, m.commits[1].Hash
	m.commitCompareSet = selectionSet(newer, older)

	onto := older + "^"
	cs, err := m.svc.CommitRange(context.Background(), onto, m.status.Branch)
	if err != nil {
		t.Fatalf("CommitRange: %v", err)
	}
	plan, err := rebaseplan.BuildSquash(cs, []string{newer, older})
	if err != nil {
		t.Fatalf("BuildSquash: %v", err)
	}
	got := map[string]rebaseplan.Action{}
	for _, e := range plan.Entries {
		got[e.Sha] = e.Action
	}
	if got[older] != rebaseplan.Pick || got[newer] != rebaseplan.Squash {
		t.Fatalf("plan = %v, want older:pick newer:squash", got)
	}
}
