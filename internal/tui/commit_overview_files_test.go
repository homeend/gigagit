package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/domain"
)

// A commit's overviews are an "Overviews" block under its Reviews block (or
// first when it has none); re-laying replaces the block, never stacks one.
func TestLayCommitOverviews(t *testing.T) {
	t.Parallel()
	files := []contentLine{{text: "a.go", path: "a.go"}}
	ovs := []domain.OverviewHead{ovHead("o1", "abc1234", "", "Locks"), ovHead("o2", "abc1234", "", "Parsing")}
	got := layCommitOverviews(ovs, files)
	if len(got) != 4 || !got[0].heading || got[0].text != "Overviews" || got[1].overviewID != "o1" || got[2].overviewID != "o2" || got[3].path != "a.go" {
		t.Fatalf("lines = %+v", got)
	}
	// Under the Overviews heading an overview is "└ <title> · <date> <agent>".
	if !strings.HasPrefix(got[1].text, "  └ Locks · ") || !strings.HasSuffix(got[1].text, " Claude Code") {
		t.Fatalf("row text = %q", got[1].text)
	}
	if again := layCommitOverviews(ovs, got); len(again) != 4 {
		t.Fatalf("re-laid = %+v", again)
	}
	withRevs := withReviewLines([]domain.Review{{ID: "r1", Agent: "fake"}}, got)
	relaid := layCommitOverviews(ovs[:1], withRevs)
	if len(relaid) != 5 || relaid[0].text != "Reviews" || relaid[1].noteID != "r1" || relaid[2].text != "Overviews" ||
		relaid[3].overviewID != "o1" || relaid[4].path != "a.go" {
		t.Fatalf("under the reviews = %+v", relaid)
	}
	if gone := layCommitOverviews(nil, relaid); len(gone) != 3 || gone[2].path != "a.go" {
		t.Fatalf("overviews gone = %+v", gone)
	}
	// An empty commit: the block takes the "(no files)" placeholder's place,
	// and the placeholder comes back when the block goes.
	empty := commitFileLines(nil)
	if only := layCommitOverviews(ovs[:1], empty); len(only) != 2 || only[1].overviewID != "o1" {
		t.Fatalf("no files = %+v", only)
	}
	if back := layCommitOverviews(nil, layCommitOverviews(ovs[:1], empty)); len(back) != 1 || back[0].text != empty[0].text {
		t.Fatalf("placeholder back = %+v", back)
	}
}

func TestCommitFilesListTheCommitsOverviews(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", m.commits[0].Hash, "", "Locks"), ovHead("o9", "0001111deadbeef", "", "Elsewhere")}
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	ls := m.filesView.lines
	if len(ls) < 3 || ls[0].text != "Overviews" || ls[1].overviewID != "o1" || ls[2].path == "" {
		t.Fatalf("lines = %+v", ls)
	}
	for _, l := range ls {
		if l.overviewID == "o9" {
			t.Fatal("another commit's overview is listed")
		}
	}
	if s := ansi.Strip(m.View()); !strings.Contains(s, "└ Locks · ") {
		t.Fatalf("screen lacks the row:\n%s", s)
	}
	// A commit opened by a short sha (from Tags) lists them too.
	if got := m.commitOverviewHeads(m.commits[0].Hash[:7]); len(got) != 1 || got[0].ID != "o1" {
		t.Fatalf("short sha = %+v", got)
	}
}

// The block follows the counts: an overview saved (or removed) while the
// commit's list is open comes (goes) with the next notes reload, and the
// cursor stays on the file it was on.
func TestCommitOverviewsFollowTheCounts(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	h := m.commits[0].Hash
	m.filesView.sel = 1
	file := m.filesView.visible()[1].path
	u, _ := m.Update(dataAvailableMsg{source: srcNotes, gen: m.srcGen[srcNotes], value: domain.NoteCounts{
		Overviews: []domain.OverviewHead{ovHead("o1", h, "", "Locks")}}})
	m = u.(Model)
	if ls := m.filesView.lines; ls[0].text != "Overviews" || ls[1].overviewID != "o1" {
		t.Fatalf("lines = %+v", ls)
	}
	if got := m.filesView.visible()[m.filesView.sel].path; got != file {
		t.Fatalf("cursor on %q, want %q", got, file)
	}
	// j/k moved filesHash on and that commit's list is still loading: the
	// list on screen is the previous commit's, so a reload leaves it be.
	m.filesReadInflight, m.filesHash = true, "0001111deadbeef"
	m.noteCounts.Overviews = append(m.noteCounts.Overviews, ovHead("o9", "0001111deadbeef", "", "Other"))
	if got := m.setCommitOverviews().filesView.lines; got[1].overviewID != "o1" || len(got) != len(m.filesView.lines) {
		t.Fatalf("an in-flight move re-laid the list: %+v", got)
	}
	m.filesReadInflight, m.filesHash = false, h
	u, _ = m.Update(dataAvailableMsg{source: srcNotes, gen: m.srcGen[srcNotes], value: domain.NoteCounts{}})
	m = u.(Model)
	for _, l := range m.filesView.lines {
		if l.overviewID != "" || l.text == "Overviews" {
			t.Fatalf("the block stayed: %+v", m.filesView.lines)
		}
	}
}

// The overview row's . menu is the overview's own: Open, Export as HTML,
// Delete; enter opens the split.
func TestCommitOverviewRowMenuAndEnter(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	loaded, _ := m.Update(m.loadCmd()()) // the c2 commit the helper made
	m = loaded.(Model)
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead(id, m.commits[0].Hash, "", "Tour of a")}
	m, cmd := m.openChangedFiles(m.commits[0])
	m = drainCmds(t, m, cmd)
	m.filesTreeFocused = true
	m.filesView.sel = 1
	rows, ok := m.noteRowMenu()
	var ids []string
	for _, r := range rows {
		ids = append(ids, r.id)
	}
	if !ok || strings.Join(ids, "|") != "open-overview|overview-export|overview-copy-link|delete-overview" {
		t.Fatalf("row menu = %v", ids)
	}
	if msg := runExportRow(t, m, rows); msg.err != nil || filepath.Base(msg.defaultPath) != "overview-tour-of-a.html" {
		t.Fatalf("export = err %v path %q", msg.err, msg.defaultPath)
	}
	tm, cmd := m.Update(keyMsg("enter"))
	m = drainCmds(t, tm.(Model), cmd)
	if sv := m.splitLayer(); sv == nil || sv.left.doc() == nil || sv.left.doc().src.rev != id {
		t.Fatalf("enter did not open the overview; top %T", m.topLayer())
	}
}

// The Commits . menu no longer lists a commit's overviews: they are rows of
// its file list.
func TestCommitMenuHasNoOverviewRows(t *testing.T) {
	t.Parallel()
	m := stackRepoModel(t)
	m.filesView, m.focus = nil, panelCommits
	m.sel[panelCommits] = 0
	m.noteCounts.Overviews = []domain.OverviewHead{ovHead("o1", m.commits[0].Hash, "", "Locks")}
	for _, r := range availableActions(m) {
		if strings.Contains(r.label, "Locks") || strings.Contains(r.id, "overview") {
			t.Fatalf("commit menu row %q (%s)", r.label, r.id)
		}
	}
}
