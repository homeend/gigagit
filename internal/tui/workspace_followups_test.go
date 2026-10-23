package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
	"github.com/homeend/gigagit/internal/workspace"
)

// A refused workspace name is explained in the user's language: the popup
// shows the translation key's text, never the package's English error.
func TestWorkspaceNameErrorsTranslated(t *testing.T) {
	m := loadedModel(t)
	m, p := openWSPopup(t, m, nil)
	m, _ = wsPress(t, m, "n")
	m, _ = wsPress(t, m, "enter") // blank
	if p.inputErr != i18n.T("a workspace needs a name") {
		t.Fatalf("blank: %q", p.inputErr)
	}
	m = wsType(t, m, "a/b")
	m, _ = wsPress(t, m, "enter")
	if p.inputErr != i18n.T("a workspace name may not contain / ? or control characters") {
		t.Fatalf("bad rune: %q", p.inputErr)
	}
	for range 3 {
		m, _ = wsPress(t, m, "backspace")
	}
	m = wsType(t, m, strings.Repeat("x", workspace.MaxNameRunes+1))
	m, _ = wsPress(t, m, "enter")
	if p.inputErr != i18n.T("a workspace name has at most %d characters", workspace.MaxNameRunes) {
		t.Fatalf("long: %q", p.inputErr)
	}
}

// R → a member's recent entry (its main checkout) lands on the member's
// last-viewed worktree, as a member switch does — not on the main checkout
// the registry names.
func TestRepoPopupMemberEntryLandsOnLastViewed(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m.statePath = filepath.Join(t.TempDir(), "repos.toml")
	m, b := openTwo(t, m)
	linked := filepath.Join(t.TempDir(), "b-linked")
	if out, err := exec.Command("git", "-C", b.mainPath, "worktree", "add", "-b", "linked", linked).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	m = settleWorkspace(t, m, memberLoadCmd(b)) // B's list now holds the linked worktree
	b.lastViewed = filepath.Clean(linked)
	if err := repos.Touch(m.statePath, b.mainPath, "", m.loadStart); err != nil {
		t.Fatal(err)
	}
	u, _ := m.Update(keyMsg("R"))
	m = u.(Model)
	p := layerOf[*repoPopup](m)
	if p == nil {
		t.Fatal("no repo popup")
	}
	vis := p.visible()
	row := -1
	for i, e := range vis {
		if model.KeyOf(e.Path) == b.key {
			row = i
		}
	}
	if row < 0 {
		t.Fatalf("B not listed: %+v", vis)
	}
	p.sel = len(p.wsVisible()) + row
	u, _ = m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.liveRepo() != b || model.KeyOf(m.currentWorktree) != model.KeyOf(linked) {
		t.Fatalf("live ok=%v current=%q, want %q", m.liveRepo() == b, m.currentWorktree, linked)
	}
}

// ctrl+\ on a session of a repository outside the workspace re-roots there
// and says the workspace was left, as every other switch out does.
func TestOpenSessionOutsideWorkspaceSaysLeft(t *testing.T) {
	m := loadedModel(t)
	m, _ = openTwo(t, m)
	_ = startTestSession(t, m, "sleep 5")
	outside := secondRepo(t)
	s, err := domain.Sessions().Start(sessionSpecForTest("outside", outside))
	if err != nil {
		t.Fatal(err)
	}
	m, _ = m.openSessionAnywhere(s.Info().ID)
	if m.inWorkspace() || m.statusMsg != i18n.T("left workspace %s", "shop") {
		t.Fatalf("ws=%v status=%q", m.inWorkspace(), m.statusMsg)
	}
}
