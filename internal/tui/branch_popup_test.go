package tui

import (
	"os/exec"
	"testing"

	"github.com/gigagit/gg/internal/domain"
	"github.com/gigagit/gg/internal/git"
)

type gitRepoT = git.Repo

func tuiBranchExists(t *testing.T, dir, name string) bool {
	t.Helper()
	return exec.Command("git", "-C", dir, "rev-parse", "--verify", "refs/heads/"+name).Run() == nil
}

// loadModel builds a Model over dir with panel data loaded.
func loadModel(t *testing.T, repo *gitRepoT) Model {
	t.Helper()
	m := New(domain.New(repo))
	loaded, _ := m.Update(m.loadCmd()())
	return loaded.(Model)
}

func TestBKeyOpensBranchPopupWithSelectedStartPoint(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	m.sel[panelBranches] = 0

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	if m.branchPopup == nil {
		t.Fatal("b should open the branch popup")
	}
	if m.branchPopup.startPoint != m.branches[0].Name {
		t.Fatalf("startPoint = %q, want selected branch %q", m.branchPopup.startPoint, m.branches[0].Name)
	}
	if m.branchPopup.switchAfter {
		t.Fatal("b must not set switchAfter")
	}
}

func TestBKeyInertOnOtherPanels(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelCommits

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	if m.branchPopup != nil {
		t.Fatal("b must be inert outside the Branches panel")
	}
}

func TestBranchPopupTypeEnterCreatesBranch(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches

	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)
	for _, r := range "feat/new" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	if m.branchPopup.name != "feat/new" {
		t.Fatalf("typed name = %q", m.branchPopup.name)
	}

	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)
	if m.branchPopup != nil {
		t.Fatal("enter should close the popup")
	}
	for i := 0; i < 100 && m.running; i++ {
		if cmd == nil {
			t.Fatal("no command but op still running")
		}
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	if !tuiBranchExists(t, dir, "feat/new") {
		t.Fatal("branch not created")
	}
}

func TestBranchPopupEnterOnEmptyNameDoesNothing(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("enter"))
	m = u.(Model)
	if m.branchPopup == nil {
		t.Fatal("enter with an empty name must keep the popup open")
	}
	if m.running {
		t.Fatal("no op may start for an empty name")
	}
}

func TestBranchPopupEscClosesWithoutOp(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("esc"))
	m = u.(Model)
	if m.branchPopup != nil || m.running {
		t.Fatal("esc must close the popup without starting an op")
	}
}

func TestShiftBChainsSmartSwitchAfterCreate(t *testing.T) {
	dir, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches

	updated, _ := m.Update(keyMsg("B"))
	m = updated.(Model)
	if m.branchPopup == nil || !m.branchPopup.switchAfter {
		t.Fatal("B should open the popup with switchAfter set")
	}
	for _, r := range "feat/go" {
		u, _ := m.Update(keyMsg(string(r)))
		m = u.(Model)
	}
	updated, cmd := m.Update(keyMsg("enter"))
	m = updated.(Model)

	// Drive create + the chained SmartSwitch to completion. SmartSwitch on a
	// clean tree needs no decisions.
	for i := 0; i < 200 && cmd != nil; i++ {
		u, next := m.Update(cmd())
		m = u.(Model)
		cmd = next
	}
	out, err := exec.Command("git", "-C", dir, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(out); got != "feat/go\n" {
		t.Fatalf("HEAD = %q, want feat/go (SmartSwitch should have chained)", got)
	}
	if m.pendingSwitchBranch != "" {
		t.Fatal("pendingSwitchBranch must be cleared")
	}
}

func TestBranchPopupSwallowsActionKeys(t *testing.T) {
	_, repo := newRepoDir(t)
	m := loadModel(t, repo)
	m.focus = panelBranches
	updated, _ := m.Update(keyMsg("b"))
	m = updated.(Model)

	u, _ := m.Update(keyMsg("q")) // would quit outside the popup
	m = u.(Model)
	if m.branchPopup == nil {
		t.Fatal("popup must swallow ordinary keys")
	}
	if m.branchPopup.name != "q" {
		t.Fatalf("typed rune should land in the name, got %q", m.branchPopup.name)
	}
}
