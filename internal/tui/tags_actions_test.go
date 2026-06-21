package tui

import (
	"os/exec"
	"strings"
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

func gitCurrentBranch(t *testing.T, dir string) string {
	t.Helper()
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	return strings.TrimSpace(string(out))
}

func TestTagCheckoutRowDetached(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	gitIn(t, dir, "commit", "--allow-empty", "-m", "c2")
	m := loadModel(t, repo)
	m.focus = panelTags
	m.activeFilesTab = panelTags
	m.sel[panelTags] = 0
	row, ok := m.tagCheckoutRow()
	if !ok {
		t.Fatal("checkout row must appear on the Tags panel")
	}
	u, _ := row.run(m)
	m = u.(Model)
	if m.modal == nil {
		t.Fatal("checkout must open the detached/branch modal")
	}
	um, cmd := m.modal.onResolve(m, "Detached")
	m = um.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "" {
		t.Fatalf("expected detached HEAD, on %q", b)
	}
}

func TestTagCheckoutPopupCreatesBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	gitIn(t, dir, "tag", "v1.0.0")
	m := loadModel(t, repo)
	m = m.pushLayer(&tagCheckoutPopup{tag: "v1.0.0"})
	for _, r := range "rel" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	for i := 0; i < 100 && m.running; i++ {
		uu, next := m.Update(cmd())
		m = uu.(Model)
		cmd = next
	}
	if b := gitCurrentBranch(t, dir); b != "rel" {
		t.Fatalf("on branch %q, want rel", b)
	}
}

// Detached HEAD (CurrentBranch == "") must still render without panicking.
func TestRenderWithDetachedHead(t *testing.T) {
	m := footerModel()
	m.status.Branch = "" // detached
	m.focus = panelCommits
	m.commits = []model.Commit{{Hash: "1111111", Subject: "x"}}
	m.sel[panelCommits] = 0
	out := m.View()
	if out == "" {
		t.Fatal("View() must render with a detached HEAD")
	}
}
