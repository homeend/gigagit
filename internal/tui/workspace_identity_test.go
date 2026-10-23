package tui

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/workspace"
)

// linkedWorktreeRepo is a repository with one linked worktree: (main, linked).
func linkedWorktreeRepo(t *testing.T) (string, string) {
	t.Helper()
	main, _ := newRepoDir(t)
	linked := filepath.Join(t.TempDir(), "linked")
	if out, err := exec.Command("git", "-C", main, "worktree", "add", "-b", "linked", linked).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	return main, linked
}

// gg launched in a linked worktree: the home slot is seeded from the
// config arrival (before the worktree list), so its identity is the launch
// path for a moment. The launch question must wait for the list — a
// workspace naming the MAIN checkout contains this repository — not mark
// itself asked on the provisional identity.
func TestLaunchFromLinkedWorktreeAsksOnceIdentityKnown(t *testing.T) {
	main, linked := linkedWorktreeRepo(t)
	m := New(domain.New(testRepo(t, linked)))
	m.width, m.height = 120, 40
	m = withWorkspacesFile(t, m, []workspace.Entry{{Name: "shop", Repos: []string{main, secondRepo(t)}}})
	nm, _ := m.Update(configReadyMsg{cfg: config.Defaults(), top: linked})
	m = nm.(Model)
	if m.homeRepo() == nil {
		t.Fatal("the config arrival did not seed the home slot")
	}
	m, _ = m.maybeAskWorkspace() // the Update tail's retry, before the worktree list
	if m.workspaceAsked || m.topLayer() != nil {
		t.Fatalf("asked on the provisional identity: asked=%v top=%T", m.workspaceAsked, m.topLayer())
	}
	nm, _ = m.Update(m.readSourceCmd(context.Background(), srcWorktrees, reloadOpts{manual: true})())
	m = nm.(Model)
	if got := m.homeRepo().mainPath; got != filepath.Clean(main) {
		t.Fatalf("identity after the worktree list = %q, want %q", got, main)
	}
	m.loading = false // the startup fan-out is done: the Update tail's retry condition
	m, _ = m.maybeAskWorkspace()
	if _, ok := m.topLayer().(*workspaceLaunchPopup); !ok {
		t.Fatalf("top = %T, want the launch dialog", m.topLayer())
	}
}

// R → a workspace while the home identity is still the launch worktree's
// path: the entry names this repository's main checkout, so the open must
// recognise the repository as the member (and fix the identity) instead of
// switching to the main checkout and losing the request.
func TestOpenWorkspaceFromProvisionalIdentity(t *testing.T) {
	main, linked := linkedWorktreeRepo(t)
	m := loadedModelAt(t, linked)
	r := m.homeRepo()
	r.mainPath, r.key, r.name = filepath.Clean(linked), model.KeyOf(linked), "linked"
	e := workspace.Entry{Name: "shop", Repos: []string{main, secondRepo(t)}}
	nm, cmd := m.openWorkspace(e)
	m = settleWorkspace(t, nm, cmd)
	if !m.inWorkspace() || m.ws.name != "shop" || m.startWorkspace != nil {
		t.Fatalf("ws=%v start=%v status=%q", m.inWorkspace(), m.startWorkspace != nil, m.statusMsg)
	}
	if model.KeyOf(m.currentWorktree) != model.KeyOf(linked) {
		t.Fatalf("the open moved the view to %q", m.currentWorktree)
	}
	if m.liveRepo() != r || r.mainPath != filepath.Clean(main) {
		t.Fatalf("live ok=%v identity=%q, want %q", m.liveRepo() == r, r.mainPath, main)
	}
}

// A workspace asked for across a re-root survives a health probe that
// lands before the load seeded the new home: the request is kept, and the
// first load that seeds the slot opens it.
func TestStartWorkspaceSurvivesEarlyProbe(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo().mainPath
	e := workspace.Entry{Name: "shop", Repos: []string{home, secondRepo(t)}}
	m.startWorkspace = &e
	m.homeRepoView, m.home, m.viewed = nil, "", "" // as reRoot leaves them until the load lands
	nm, _ := m.Update(repoHealthMsg{gen: m.noticeGen, health: model.RepoHealth{GitCommonDir: filepath.Join(home, ".git")}})
	m = nm.(Model)
	if m.startWorkspace == nil {
		t.Fatalf("the early probe dropped the request: status=%q", m.statusMsg)
	}
	nm, cmd := m.Update(m.loadCmd()())
	m = settleWorkspace(t, nm.(Model), cmd)
	if !m.inWorkspace() || m.ws.name != "shop" || m.startWorkspace != nil {
		t.Fatalf("after the load: ws=%v start=%v status=%q", m.inWorkspace(), m.startWorkspace != nil, m.statusMsg)
	}
}
