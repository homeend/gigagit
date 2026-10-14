package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// The split's file pane is the file viewer in half the screen: its . menu
// copies the cursor line and the file's path, name and commit, and offers the
// file at its commit to bookmarks, the shelf and compares — as the
// full-screen viewer of the same file does.
func TestSplitFilePaneMenuCopiesLines(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	sv := m.splitLayer()
	sv.focus(false)
	rd := sv.right.doc()
	if rd == nil || rd.src.kind != srcCommit {
		t.Fatalf("setup: the right pane shows a file at a commit: %+v", rd)
	}
	got := map[string]actionRow{}
	for _, r := range availableActions(m) {
		got[r.id] = r
	}
	for _, want := range []string{"copy-line", "copy-file-path", "copy-file-name", "copy-commit-id", "bookmark-add", "shelf-add"} {
		if _, ok := got[want]; !ok {
			t.Errorf("the split's file pane menu lacks %q", want)
		}
	}
	if r := got["copy-line"]; r.copyText != rd.p.lines[rd.p.cur].raw {
		t.Errorf("copy-line copies %q, want the cursor line %q", r.copyText, rd.p.lines[rd.p.cur].raw)
	}
	if b, ok := m.focusedBookmark(); !ok || b.State != model.StateCommitted || b.Commit != rd.src.rev || b.Path != rd.path {
		t.Errorf("the pane's file bookmarks as %+v, want %s at %s", b, rd.path, shortHash(rd.src.rev))
	}
}

// An overview is no file: neither its split pane nor its full-screen viewer
// offers path, name, file link, bookmark, shelf or compare rows for it.
func TestOverviewMenuOffersNoFileRows(t *testing.T) {
	t.Parallel()
	noFile := func(t *testing.T, m Model, where string) {
		t.Helper()
		got := menuIDs(m)
		for _, id := range []string{"copy-file-path", "copy-file-name", "copy-file-abspath", "copy-file-link", "bookmark-add", "shelf-add", "bookmark-compare", "shelf-compare-against"} {
			if got[id] {
				t.Errorf("%s: the overview's menu offers %q", where, id)
			}
		}
	}
	m, id := commitOverviewModel(t)
	split := openCommitOverviewT(t, m, id) // focus on the overview
	noFile(t, split, "split")
	m.width = 50
	full := openCommitOverviewT(t, m, id)
	noFile(t, full, "full-screen")
}

// The full-screen viewer of a file at a commit (an anchor opened in a narrow
// terminal) bookmarks that commit's version, not the working tree's.
func TestCommitFileViewerBookmarksTheCommit(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m.width = 50
	m = openCommitOverviewT(t, m, id)
	m = pressDrain(t, m, "enter") // open the first anchor's file
	d := topDoc(m)
	if d == nil || d.src.kind != srcCommit {
		t.Fatalf("setup: the viewer shows a file at a commit: %+v", d)
	}
	if b, ok := m.focusedBookmark(); !ok || b.State != model.StateCommitted || b.Commit != d.src.rev || b.Path != d.path {
		t.Fatalf("bookmark = %+v, want %s at %s", b, d.path, shortHash(d.src.rev))
	}
}

// A split on top owns the menu: the Files panel's marked set under it is not
// what "Add to shelf" freezes.
func TestSplitIsNoBareFilesPanel(t *testing.T) {
	t.Parallel()
	m, id := commitOverviewModel(t)
	m = openCommitOverviewT(t, m, id)
	m.filesView, m.focus = nil, panelFiles
	if m.onBareFilesPanel() {
		t.Fatal("a split on top still counts as the bare Files panel")
	}
}
