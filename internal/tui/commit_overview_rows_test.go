package tui

import (
	"path/filepath"
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
	if strings.Join(labels, "|") != "Open overview: Locks|Export as HTML: Locks|Open overview: Parsing|Export as HTML: Parsing" {
		t.Fatalf("menu rows = %v", labels)
	}
}

func TestOverviewRowsFollowReload(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits
	m.sel[panelCommits] = 0
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", m.commits[0].Hash, "", "Locks")}
	if len(m.commitOverviewRows()) != 2 {
		t.Fatal("one overview: Open + Export")
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

// The └ Overview sub-row: enter opens the overview; its . menu is Open,
// Export as HTML and Delete, and Delete asks (overview-remove) before
// RemoveOverview.
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
	if strings.Join(labels, "|") != "open-overview|overview-export|delete-overview" {
		t.Fatalf("sub-row menu = %v", labels)
	}
	if msg := runExportRow(t, m, m.branchOverviewRows()); msg.err != nil || filepath.Base(msg.defaultPath) != "overview-tour-of-a.html" {
		t.Fatalf("export = err %v path %q", msg.err, msg.defaultPath)
	}
	tm, _ := m.branchOverviewRows()[2].run(m)
	if mm := tm.(Model); mm.modal == nil || mm.modal.req.ID != "overview-remove" || mm.modal.sel != 1 {
		t.Fatalf("delete asks first: %+v", mm.modal)
	}
	m = pressDrain(t, m, "enter")
	if m.splitLayer() == nil {
		t.Fatalf("enter on the sub-row opens the overview; top %T status %q", m.topLayer(), m.statusMsg)
	}
}

// An overview sub-row keys apart from its branch and from a sibling, so a
// reload's selection restore stays on it.
func TestBranchOverviewSubRowKeys(t *testing.T) {
	t.Parallel()
	l := branchList{items: []model.Branch{{Name: "feat"}},
		ents: []brEntry{{br: 0}, {br: 0, overview: "o1"}, {br: 0, overview: "o2"}}}
	if l.Key(0) == l.Key(1) || l.Key(1) == l.Key(2) {
		t.Fatalf("keys = %q %q %q", l.Key(0), l.Key(1), l.Key(2))
	}
}

// An overview open in a SLEEPING worktree slot closes too: the slot shows
// again without it.
func TestGoneOverviewClosesInASleepingSlot(t *testing.T) {
	m, id := commitOverviewModel(t)
	m.width, m.height = 120, 40
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}}
	home := m.currentWorktree
	m = openCommitOverviewT(t, m, id)
	m = m.closeSplit(m.splitLayer()) // esc: the overview stays open, in the background
	if len(m.openFiles.list(home)) == 0 {
		t.Fatal("setup: the overview stays open after esc")
	}
	m, other := addWorktree(t, m, "wt2")
	m, ok := m.userSwitchView(other)
	if !ok {
		t.Fatalf("setup: switch refused: %q", m.statusMsg)
	}
	m = m.dropGoneOverviews(domain.NoteCounts{}) // deleted while home sleeps
	if m, ok = m.userSwitchView(home); !ok {
		t.Fatalf("switch back refused: %q", m.statusMsg)
	}
	for _, d := range m.openFiles.list(m.currentWorktree) {
		if d.src.kind == srcCommitOverview {
			t.Fatalf("the deleted overview is still open: %+v", d.src)
		}
	}
}

// A delete from the TUI closes the open overview at once and says so ONCE:
// the counts reload after it finds nothing left to close.
func TestDeletedOverviewSaysSoOnce(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}}
	m = openCommitOverviewT(t, m, id)
	m, _ = m.onStoredDeleted(storedDeletedMsg{id: id, overview: true})
	if m.splitLayer() != nil || m.statusMsg != "deleted the overview" {
		t.Fatalf("after the delete: split %v status %q", m.splitLayer() != nil, m.statusMsg)
	}
	m.statusMsg = "" // anything the reload says now is a second line
	m = m.dropGoneOverviews(domain.NoteCounts{})
	if m.statusMsg != "" {
		t.Fatalf("the reload said %q too", m.statusMsg)
	}
}

// The └ Overview sub-row's footer says what enter does there.
func TestBranchOverviewSubRowFooter(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.width, m.height = 160, 40
	m.filesView, m.focus, m.activeLeftTab = nil, panelBranches, panelBranches
	tip := m.commits[0].Hash
	for i := range m.branches {
		m.branches[i].Hash = tip
	}
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead(id, tip, m.branches[0].Name, "Tour of a")}
	for i, e := range m.branchEntries() {
		if e.overview == id {
			m.sel[panelBranches] = i
		}
	}
	if _, ok := m.selectedBranchOverview(); !ok {
		t.Fatal("setup: the cursor is on the sub-row")
	}
	if f := m.footerLine(); !strings.Contains(f, "[enter] open") || strings.Contains(f, "[enter] tip") {
		t.Fatalf("footer = %q, want [enter] open", f)
	}
}

// Serial: installs a process-global session manager.
//
// A console over the overview's split parks the split under it; the slot
// sleeps, the overview is deleted, the slot shows again with the console
// still up, and the console closes: the split of the deleted overview never
// returns.
func TestGoneOverviewUnderAConsoleStaysClosed(t *testing.T) {
	m, id := commitOverviewModel(t)
	m.width, m.height = 120, 40
	m.noteCounts.Overviews = []domain.OverviewHead{{ID: id}}
	home := m.currentWorktree
	m = openCommitOverviewT(t, m, id)
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	sid := startSessionIn(t, m, home, "Shell")
	m, _ = m.showConsole(sid, false)
	// The UI refuses this swap today (the split owns the keyboard); the
	// slot calls themselves pin loadView's order for any path that sleeps one.
	swap := func(m Model, path string) Model { return m.parkView().sleepView().loadView(m.ensureView(path)) }
	m = swap(m, other)
	m = m.dropGoneOverviews(domain.NoteCounts{})
	m = swap(m, home)
	if m.console != nil {
		m = m.closeConsole()
	}
	if m.splitLayer() != nil {
		t.Fatal("the deleted overview's split came back from under the console")
	}
}

// Another repository's open overviews are not judged by this one's counts.
func TestGoneSweepKeepsOtherRepositories(t *testing.T) {
	t.Parallel()
	m, _ := commitOverviewModel(t)
	d := &openFile{src: fileSource{kind: srcCommitOverview, rev: "elsewhere"}}
	m.openFiles.touch("/some/other/repo", d, func(*openFile) bool { return true })
	m = m.dropGoneOverviews(domain.NoteCounts{})
	if d.gone {
		t.Fatal("an open overview of another repository was marked gone")
	}
}
