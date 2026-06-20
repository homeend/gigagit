package tui

import (
	"testing"

	"github.com/gigagit/gg/internal/model"
)

func TestEnterOnTagJumpsToCommit(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{
		{Hash: "1111111aaaa", Subject: "one"},
		{Hash: "2222222bbbb", Subject: "two"},
	}
	m.tags = []model.Tag{{Name: "v1", Target: "2222222", Annotated: false}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel[panelTags] = 0

	u, _ := m.Update(keyMsg("enter"))
	mm := u.(Model)
	if mm.focus != panelCommits {
		t.Fatalf("focus = %v, want panelCommits", mm.focus)
	}
	_, idx := mm.panelView(panelCommits)
	if got := idx[mm.sel[panelCommits]]; got != 1 {
		t.Fatalf("selected commit backing idx = %d, want 1 (the v1 target)", got)
	}
}

func TestEnterOnTagNotLoadedNotices(t *testing.T) {
	m := footerModel()
	m.commits = []model.Commit{{Hash: "1111111aaaa", Subject: "one"}}
	m.tags = []model.Tag{{Name: "v1", Target: "9999999"}}
	m.activeFilesTab = panelTags
	m.focus = panelTags
	m.sel[panelTags] = 0
	u, _ := m.Update(keyMsg("enter"))
	if mm := u.(Model); mm.statusMsg == "" {
		t.Fatal("expected a 'tag target not loaded' notice")
	}
}
