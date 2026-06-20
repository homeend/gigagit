package tui

import (
	"os/exec"
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

func TestTagDeleteRowOpensConfirmThenDeletes(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0") // before loadModel so the snapshot loads it
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0

	row, ok := m.tagDeleteRow()
	if !ok {
		t.Fatal("delete row must appear on the Tags panel with a selection")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("delete must open a confirm modal")
	}
	um, cmd := m.modal.onResolve(m, "Delete")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() == nil {
		t.Fatal("tag should be gone after confirm")
	}
}

func TestTagDeleteRowCancelKeepsTag(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, _ := m.tagDeleteRow()
	u, _ := row.run(m)
	m = u.(Model)
	um, _ := m.modal.onResolve(m, "Cancel")
	m = um.(Model)
	if m.running {
		t.Fatal("Cancel must not start a delete")
	}
	if exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/tags/v1.0.0").Run() != nil {
		t.Fatal("tag must survive Cancel")
	}
}

func TestTagDeleteRowInertOffTagsPanel(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	if _, ok := m.tagDeleteRow(); ok {
		t.Fatal("delete row must be inert off the Tags panel")
	}
}
