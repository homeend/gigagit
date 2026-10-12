package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func ovHead(id, commit, branch, title string) domain.OverviewHead {
	return domain.OverviewHead{ID: id, Commit: commit, Branch: branch, Agent: "Claude Code", Title: title,
		Created: time.Date(2026, 10, 12, 9, 40, 0, 0, time.Local)}
}

func TestBranchOverviewRowsOnlyAtTheTip(t *testing.T) {
	t.Parallel()
	m := Model{}
	m.branches = []model.Branch{{Name: "feat", Hash: "abc1234"}, {Name: "main", Hash: "fff0000"}}
	m.noteCounts.Overviews = []domain.OverviewHead{
		ovHead("o1", "abc1234deadbeef", "feat", "How the gate works"),
		ovHead("o2", "0001111aaaaaaaa", "feat", "An older one"), // feat moved on: no row
	}
	ents := m.branchEntries()
	var subs []string
	for _, e := range ents {
		if e.overview != "" {
			subs = append(subs, e.overview)
		}
	}
	if len(subs) != 1 || subs[0] != "o1" {
		t.Fatalf("overview sub-rows = %v", subs)
	}
	rows := m.branchRowsFor(ents)
	if !strings.Contains(strings.Join(rows, "\n"), "└ Overview: How the gate works") {
		t.Fatalf("rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestTwoOverviewsTwoRows(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits // back on the Commits list (the helper opened a Files view)
	m.sel[panelCommits] = 0
	h := m.commits[0].Hash
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", h, "", "Locks"), ovHead("o2", h, "", "Parsing")}
	var labels []string
	for _, r := range m.commitOverviewRows() {
		labels = append(labels, r.label)
	}
	if strings.Join(labels, "|") != "Open overview: Locks|Open overview: Parsing" {
		t.Fatalf("menu rows = %v", labels)
	}
}

func TestOverviewRowsFollowReload(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits
	m.sel[panelCommits] = 0
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", m.commits[0].Hash, "", "Locks")}
	if len(m.commitOverviewRows()) != 1 {
		t.Fatal("one row")
	}
	m.noteCounts.Overviews = nil // the CLI removed it; the notes reload landed
	if len(m.commitOverviewRows()) != 0 {
		t.Fatal("rows follow the counts")
	}
}

func TestGoneOverviewCloses(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t) // Task 3's helper
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}}
	m = openCommitOverviewT(t, m, id)
	if m.splitLayer() == nil {
		t.Fatal("setup: the split is open")
	}
	m = m.dropGoneOverviews(domain.NoteCounts{}) // the reload landed without it
	if m.splitLayer() != nil || !strings.Contains(m.statusMsg, "deleted") {
		t.Fatalf("a deleted overview closes; top %T status %q", m.topLayer(), m.statusMsg)
	}
}

// The └ Overview sub-row: enter opens the overview; its . menu is Open +
// Delete only, and Delete asks (overview-remove) before RemoveOverview.
func TestBranchOverviewSubRowOpenAndDelete(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.filesView, m.focus = nil, panelBranches
	tip := m.commits[0].Hash
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead(id, tip, m.branches[0].Name, "Tour of a")}
	for i := range m.branches {
		m.branches[i].Hash = tip // every branch's tip is the overview's commit
	}
	m.noteCounts.Overviews[0].Branch = m.branches[0].Name
	sel := -1
	for i, e := range m.branchEntries() {
		if e.overview == id {
			sel = i
		}
	}
	if sel < 0 {
		t.Fatalf("no overview sub-row: %+v", m.branchEntries())
	}
	m.sel[panelBranches] = sel
	var labels []string
	for _, r := range m.branchOverviewRows() {
		labels = append(labels, r.id)
	}
	if strings.Join(labels, "|") != "open-overview|delete-overview" {
		t.Fatalf("sub-row menu = %v", labels)
	}
	tm, _ := m.branchOverviewRows()[1].run(m)
	if mm := tm.(Model); mm.modal == nil || mm.modal.req.ID != "overview-remove" || mm.modal.sel != 1 {
		t.Fatalf("delete asks first: %+v", mm.modal)
	}
	m = pressDrain(t, m, "enter")
	if m.splitLayer() == nil {
		t.Fatalf("enter on the sub-row opens the overview; top %T status %q", m.topLayer(), m.statusMsg)
	}
}
