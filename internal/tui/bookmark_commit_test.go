package tui

import (
	"strings"
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestCommitBookmarkStoresSubject(t *testing.T) {
	c := model.Commit{Hash: "a1b2c3d4e5", Subject: "Fix the parser"}
	b := commitBookmark(c)
	if !b.IsCommit() || b.Label != "Fix the parser" {
		t.Fatalf("commit bookmark should store the subject as Label: %+v", b)
	}
}

func TestBookmarkDisplayCommitIncludesSubject(t *testing.T) {
	b := model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Branch: "feat", Path: "", Label: "Fix the parser"}
	got := bookmarkDisplay(b)
	if !strings.Contains(got, "feat / a1b2c3d") || !strings.Contains(got, "Fix the parser") {
		t.Fatalf("commit bookmark display should include sha + subject: %q", got)
	}
	// A file bookmark must ignore Label (display unchanged).
	f := model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "x.go", Label: "ignored"}
	if strings.Contains(bookmarkDisplay(f), "ignored") {
		t.Fatalf("file bookmark display must not append Label: %q", bookmarkDisplay(f))
	}
}

func TestCommitBookmarkRendersWithoutPath(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.width, m.height = 100, 30
	cb := model.Bookmark{State: model.StateCommitted, Commit: "a1b2c3d4e5", Path: "", ID: "cb1"}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))
	out := m.renderBookmarkPopupBox(m.bookmarkSwitcher())
	if !strings.Contains(out, "commit / a1b2c3d") {
		t.Fatalf("commit bookmark should render its short sha with no path:\n%s", out)
	}
	if strings.Contains(out, "a1b2c3d /") {
		t.Fatalf("commit bookmark must not render a trailing path separator:\n%s", out)
	}
}

func TestCommitBookmarkRowPresentOnCommits(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	r, ok := m.commitBookmarkRow()
	if !ok {
		t.Fatal("Bookmark this commit should be offered on the Commits panel")
	}
	if r.id != "commit-bookmark" || r.run == nil {
		t.Fatalf("bad row: %+v", r)
	}
	m.focus = panelBranches
	if _, ok := m.commitBookmarkRow(); ok {
		t.Fatal("must not be offered off the Commits panel")
	}
}

func TestCommitBookmarkRowInMenu(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	found := false
	for _, r := range availableActions(m) {
		if r.id == "commit-bookmark" {
			found = true
		}
	}
	if !found {
		t.Fatal("commit-bookmark must appear in the Commits . menu")
	}
}

func commitBmPopupModel(t *testing.T) Model {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	cb := model.Bookmark{State: model.StateCommitted, Commit: m.commits[0].Hash, Path: "", ID: "cb1"}
	return m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))
}

func TestCommitBookmarkPasteIsNoop(t *testing.T) {
	m := commitBmPopupModel(t)
	mm, _ := m.Update(keyMsg("p"))
	m = mm.(Model)
	if m.bookmarkSwitcher() == nil {
		t.Fatal("paste on a commit bookmark must not leave/replace the switcher")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a notice that paste is unavailable for a commit bookmark")
	}
}

func TestCommitBookmarkMarkIsNoop(t *testing.T) {
	m := commitBmPopupModel(t)
	mm, _ := m.Update(keyMsg("m"))
	m = mm.(Model)
	if p := m.bookmarkSwitcher(); p == nil || p.markID != "" {
		t.Fatal("m on a commit bookmark must not record a mark")
	}
	if m.statusMsg == "" {
		t.Fatal("expected a notice for m on a commit bookmark")
	}
}

func TestCommitBookmarkEnterComparesVsSelected(t *testing.T) {
	m := loadedModelLinearCommits(t, 3) // newest-first feed: index 0 newest
	m.focus = panelCommits
	base := m.commits[2].Hash // an older commit — the bookmark
	m.sel[panelCommits] = 0   // select the newest as the subject
	cb := model.Bookmark{State: model.StateCommitted, Commit: base, Path: "", ID: "cb1"}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))

	mm, cmd := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.bookmarkSwitcher() != nil {
		t.Fatal("enter should close the switcher (cleared layers)")
	}
	if !m.filesCompare || m.filesView == nil {
		t.Fatal("enter on a commit bookmark should open the compare files view")
	}
	if m.filesLeft.Hash != base {
		t.Fatalf("left/base must be the bookmark commit, got %q want %q", m.filesLeft.Hash, base)
	}
	if m.filesRight.Hash != m.commits[0].Hash {
		t.Fatalf("right/subject must be the selected commit, got %q", m.filesRight.Hash)
	}
	if cmd == nil {
		t.Fatal("expected a load command for the compare")
	}
}

func TestCommitBookmarkEnterSelfCompareNoop(t *testing.T) {
	m := loadedModelLinearCommits(t, 2)
	m.focus = panelCommits
	m.sel[panelCommits] = 0
	same := m.commits[0].Hash
	cb := model.Bookmark{State: model.StateCommitted, Commit: same, Path: "", ID: "cb1"}
	m = m.pushLayer(newBookmarkPopup([]model.Bookmark{cb}))

	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	if m.filesView != nil {
		t.Fatal("comparing a commit against itself must not open a compare")
	}
	if m.bookmarkSwitcher() == nil || m.statusMsg == "" {
		t.Fatal("expected the switcher to stay open with a notice")
	}
}
