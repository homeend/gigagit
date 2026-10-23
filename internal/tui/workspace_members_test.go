package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/gittest"
	"github.com/homeend/gigagit/internal/workspace"
)

// settleTail runs the Update tail once (any message) and what it chains.
func settleTail(t *testing.T, m Model) Model {
	t.Helper()
	u, c := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	return settleWorkspace(t, u.(Model), c)
}

func bareRepoWithWorktree(t *testing.T, withWT bool) (bare, wt string) {
	t.Helper()
	src := gittest.BasicRepo(t, "hi\n")
	bare = filepath.Join(t.TempDir(), "bare.git")
	gittest.Run(t, filepath.Dir(bare), "clone", "--bare", src, bare)
	if withWT {
		wt = filepath.Join(t.TempDir(), "wt1")
		gittest.Run(t, bare, "worktree", "add", "-b", "feat", wt)
	}
	return bare, wt
}

// A bare member is never shown as a worktree: with no linked worktree the
// switch is refused; before its load lands, a switch waits on its list.
func TestBareMemberIsNeverAWorktree(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	bare, _ := bareRepoWithWorktree(t, false)
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, bare}})
	m = settleWorkspace(t, nm, cmd)
	nm, ok := m.switchRepo(m.ws.members[1], "", true)
	if ok || filepath.Clean(nm.currentWorktree) == filepath.Clean(bare) || !strings.Contains(nm.statusMsg, "bare") {
		t.Fatalf("ok=%v current=%q status=%q", ok, nm.currentWorktree, nm.statusMsg)
	}
	m2 := loadedModel(t)
	bare2, wt := bareRepoWithWorktree(t, true)
	nm, cmd = m2.openWorkspace(workspace.Entry{Name: "w", Repos: []string{m2.homeRepo().mainPath, bare2}})
	m2 = nm // the load has not landed
	nm, ok = m2.switchRepo(m2.ws.members[1], "", true)
	if filepath.Clean(nm.currentWorktree) == filepath.Clean(bare2) {
		t.Fatalf("before its load a switch landed on the bare dir (ok=%v status=%q)", ok, nm.statusMsg)
	}
	m2 = settleWorkspace(t, nm, cmd)
	m2 = settleTail(t, m2)
	nm, ok = m2.switchRepo(m2.ws.members[1], "", true)
	if !ok || filepath.Clean(nm.currentWorktree) != filepath.Clean(wt) {
		t.Fatalf("after the load: ok=%v current=%q want %q status=%q", ok, nm.currentWorktree, wt, nm.statusMsg)
	}
}

// A missing member that is remounted and visited is loaded and watched
// again once it goes back to sleep; a refused revive leaves it missing.
func TestRevivedMissingMemberIsWatched(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	gone := filepath.Join(t.TempDir(), "later")
	nm, cmd := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, gone}})
	m = settleWorkspace(t, nm, cmd)
	r := m.ws.members[1]
	if !r.missing {
		t.Fatal("not missing")
	}
	src := gittest.BasicRepo(t, "hi\n")
	if err := os.Rename(src, gone); err != nil {
		t.Fatal(err)
	}
	m.running = true
	nm, ok := m.switchRepo(r, "", true)
	if ok || !r.missing || r.svc != nil {
		t.Fatalf("a refused revive mutated the member: ok=%v missing=%v svc=%v", ok, r.missing, r.svc != nil)
	}
	m = nm
	m.running = false
	nm, ok = m.switchRepo(r, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	nm, ok = m.switchRepo(home, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	if !r.loaded || !(r.watcher != nil || r.polling || r.watchStarting) {
		t.Fatalf("revived member asleep: loaded=%v watcher=%v polling=%v starting=%v", r.loaded, r.watcher != nil, r.polling, r.watchStarting)
	}
}

// A member whose first read fails is read again (the poll beat), not
// left empty for the session.
func TestMemberLoadErrorIsRetried(t *testing.T) {
	m := loadedModel(t)
	home := m.homeRepo()
	nm, _ := m.openWorkspace(workspace.Entry{Name: "w", Repos: []string{home.mainPath, secondRepo(t)}})
	m = nm
	b := m.ws.members[1]
	u, c := m.Update(memberLoadedMsg{svc: b.svc, gen: b.loadGen, err: os.ErrDeadlineExceeded})
	m = u.(Model)
	if !b.polling || c == nil {
		t.Fatalf("after a failed first read: polling=%v cmd=%v status=%q", b.polling, c != nil, m.statusMsg)
	}
}

// A member read started before a visit lands after it: dropped, the
// fresher lists stay.
func TestStaleMemberLoadIsDropped(t *testing.T) {
	m := loadedModel(t)
	m, b := openTwo(t, m)
	a := m.homeRepo()
	held := memberLoadCmd(b)() // a watcher-triggered read, in flight
	gittest.Run(t, b.mainPath, "branch", "fresh-one")
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	nm, ok = m.switchRepo(a, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = settleTail(t, nm)
	u, _ := m.Update(held)
	m = u.(Model)
	found := false
	for _, br := range b.branches {
		if br.Name == "fresh-one" {
			found = true
		}
	}
	if !found {
		t.Fatalf("a stale member read landed over B's fresher list (%d branches)", len(b.branches))
	}
}

// A submodule's identity is its checkout (what `git worktree list`
// names), never its git dir under the superproject.
func TestSubmoduleMemberIdentityIsItsCheckout(t *testing.T) {
	root := t.TempDir()
	lib, sup := filepath.Join(root, "lib"), filepath.Join(root, "super")
	for _, d := range []string{lib, sup} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		gittest.Run(t, d, "init", "-q", "-b", "main")
		gittest.Run(t, d, "commit", "-q", "--allow-empty", "-m", "c")
	}
	gittest.Run(t, sup, "-c", "protocol.file.allow=always", "submodule", "add", "-q", lib, "libs/core")
	checkout := filepath.Join(sup, "libs", "core")
	msg := resolveMainCmd(checkout)().(wsPathResolvedMsg)
	if msg.err != nil || filepath.Clean(msg.main) != filepath.Clean(checkout) {
		t.Fatalf("resolved %q (err %v), want %q", msg.main, msg.err, checkout)
	}
	m := loadedModelAt(t, checkout)
	hm, _ := m.Update(m.repoHealthCmd(m.noticeGen)())
	m = hm.(Model)
	if got := m.homeRepo().mainPath; filepath.Clean(got) != filepath.Clean(checkout) {
		t.Fatalf("home identity after the probe = %q, want %q", got, checkout)
	}
}

// alt+N on a member's group before its health probe landed is kept: the
// probe's slot load does not switch the filter off again, and the choice
// is remembered for that repository.
func TestAltDigitOnFreshMemberIsRemembered(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	if m.promptStore == nil {
		t.Skip("no prompt store")
	}
	m, b := openTwo(t, m)
	hm, _ := m.Update(m.repoHealthCmd(m.noticeGen)())
	m = hm.(Model)
	m = m.activateTab(panelBranches)
	row, _ := rowOfMember(m, panelBranches, 1)
	m.sel[panelBranches] = row
	nm, _ := m.Update(keyMsg("alt+1"))
	m = nm.(Model)
	if m.liveRepo() != b {
		t.Fatal("alt+1 did not switch to B")
	}
	picked := m.branchFilterSlot[panelBranches]
	hm2, _ := m.Update(m.repoHealthCmd(m.noticeGen)())
	m = hm2.(Model)
	if m.branchFilterSlot[panelBranches] != picked || strings.Contains(m.statusMsg, "not remembered") {
		t.Fatalf("slot after the probe = %d (picked %d) status=%q", m.branchFilterSlot[panelBranches], picked, m.statusMsg)
	}
	if got := m.promptStore.BranchFilterSlot(m.bfRepoKey(), branchFilterListName(panelBranches)); got != picked {
		t.Fatalf("remembered slot = %d, want %d", got, picked)
	}
}
