package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/model"
)

// The user's alt+w over a popup that can wait parks it with its worktree,
// half-typed text included; it is back when the worktree returns.
func TestAltWParksAParkableHalfTypedPopup(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	m.focus = panelBranches // alt+w's first hit with the keyboard elsewhere only focuses Branches
	m, ok := m.openBranchPopup(false)
	if !ok {
		t.Fatal("precondition: the branch popup did not open")
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("feat")})
	m = nm.(Model)
	m = pressAlt(t, m, 'w')
	if m.viewed != model.KeyOf(other) || m.topLayer() != nil {
		t.Fatalf("viewed=%q top=%T: the swap did not park the popup (%s)", m.viewed, m.topLayer(), m.statusMsg)
	}
	m = pressAlt(t, m, 'w')
	p, _ := m.topLayer().(*branchPopup)
	if m.viewed != m.home || p == nil || p.name.Value() != "feat" {
		t.Fatalf("viewed=%q top=%T: the popup did not come back with its text", m.viewed, m.topLayer())
	}
}

// The command palette cannot wait: alt+w refuses over it.
func TestAltWRefusesOverThePalette(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, _ = addWorktree(t, m, "wt2")
	m.focus = panelBranches
	m, _ = m.openCommandPalette()
	m = pressAlt(t, m, 'w')
	if m.viewed != m.home || layerOf[*commandPalette](m) == nil {
		t.Fatalf("viewed=%q: alt+w swapped over the palette", m.viewed)
	}
}

// A console show (an agent's doing, or asked for a task) still waits under
// a parkable popup: the swap queues until the popup clears.
func TestConsoleShowStillQueuesOverAParkablePopup(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	installSessionManager(t)
	id := startSessionIn(t, m, other, "Shell")
	m, _ = m.openBranchVersions("main", false, false)
	m, _ = m.showConsole(id, false)
	if m.viewed != m.home || m.pendingReturnView != model.KeyOf(other) {
		t.Fatalf("viewed=%q pending=%q: an agent-triggered show swapped under a popup", m.viewed, m.pendingReturnView)
	}
}

// A commit popup half typed in home, parked by alt+w and brought back,
// commits in HOME when submitted — never in the worktree shown meanwhile.
func TestParkedCommitPopupSubmitsInItsWorktree(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	home := m.currentWorktree
	m, other := addWorktree(t, m, "wt2")
	if err := os.WriteFile(filepath.Join(home, "parked.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("git", "-C", home, "add", "parked.txt").CombinedOutput(); err != nil {
		t.Fatalf("git add: %v\n%s", err, out)
	}
	m.focus = panelBranches
	m = m.pushLayer(&commitPopup{title: newTextField("parked commit")})
	m = pressAlt(t, m, 'w')
	if m.viewed != model.KeyOf(other) {
		t.Fatalf("viewed=%q: %s", m.viewed, m.statusMsg)
	}
	m = pressAlt(t, m, 'w')
	if m.viewed != m.home || layerOf[*commitPopup](m) == nil {
		t.Fatalf("viewed=%q top=%T", m.viewed, m.topLayer())
	}
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = nm.(Model)
	if !m.running {
		t.Fatalf("the submit did not start the commit: %s", m.statusMsg)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		out, _ := exec.Command("git", "-C", home, "log", "-1", "--format=%s").Output()
		if strings.TrimSpace(string(out)) == "parked commit" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("home's HEAD is %q, want the parked commit", strings.TrimSpace(string(out)))
		}
		time.Sleep(50 * time.Millisecond)
	}
	if out, _ := exec.Command("git", "-C", other, "log", "-1", "--format=%s").Output(); strings.TrimSpace(string(out)) == "parked commit" {
		t.Fatal("the commit landed in the other worktree")
	}
}

// A goto popup with a resolve pending switches by itself: not parkable.
func TestGotoPopupWithAResolvePendingIsNotParkable(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	m.focus = panelBranches
	m = m.pushLayer(&gotoCommitPopup{pending: &gotoLinkSwitch{checkout: other}})
	m = pressAlt(t, m, 'w')
	if m.viewed != m.home {
		t.Fatalf("viewed=%q: alt+w swapped over a goto popup with a resolve pending", m.viewed)
	}
}
