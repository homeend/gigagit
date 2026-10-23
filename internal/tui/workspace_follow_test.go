package tui

import (
	"path/filepath"
	"testing"
)

// The hosted page follows an adopt into another member (it is handed that
// member's service) and never a look.
func TestAdoptIntoMemberMovesWebPage(t *testing.T) {
	f := installFakeHost(t)
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, cmd := m.openInBrowser()
	m = runOne(t, m, cmd)
	m, b := openTwo(t, m)
	before := len(f.reroots)
	// A look: nothing is handed to the page.
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	if len(f.reroots) != before {
		t.Fatal("a look re-rooted the page")
	}
	nm, ok = nm.switchRepo(m.homeRepo(), "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	m = nm
	// An adopt: the page gets B's service.
	tm, cmd := m.guardedReRoot(b.mainPath, true, true)
	m = tm.(Model)
	got := false
	for _, msg := range runLeaves(cmd) {
		if r, ok := msg.(webRerootMsg); ok {
			got = true
			if r.err != nil {
				t.Fatal(r.err)
			}
		}
	}
	if !got || len(f.reroots) != before+1 || f.reroots[before] != b.svc || !m.inWorkspace() {
		t.Fatalf("reroot msg=%v reroots=%d handed B=%v ws=%v", got, len(f.reroots)-before, len(f.reroots) > before && f.reroots[before] == b.svc, m.inWorkspace())
	}
}

// The published (home) worktree follows an adopt into another member; the
// published view follows every switch.
func TestPublishedWorktreeFollowsAdopt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 50
	m, b := openTwo(t, m)
	home := m.homeWorktree()
	publishedWT.Store(home)
	publishedView.Store(home)
	nm, ok := m.switchRepo(b, "", true)
	if !ok {
		t.Fatal(nm.statusMsg)
	}
	if publishedWorktree() != home || filepath.Clean(publishedViewed()) != b.mainPath {
		t.Fatalf("after a look: wt=%q viewed=%q", publishedWorktree(), publishedViewed())
	}
	tm, _ := nm.adoptView()
	if publishedWorktree() != b.mainPath {
		t.Fatalf("after adopt: wt=%q", publishedWorktree())
	}
	_ = tm
}
