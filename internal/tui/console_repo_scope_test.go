package tui

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
)

// switchAndLoad is a repo switch as the user sees it: reRoot, then the new
// repo's snapshot landing.
func switchAndLoad(t *testing.T, m Model, path string) Model {
	t.Helper()
	nm, _ := m.reRoot(path)
	m = nm.(Model)
	return landLoad(t, m)
}

// landLoad applies the pending switch's snapshot.
func landLoad(t *testing.T, m Model) Model {
	t.Helper()
	nm, _ := m.Update(m.loadCmd()())
	m = nm.(Model)
	if m.err != nil {
		t.Fatalf("load: %v", m.err)
	}
	return m
}

// A console showing one of repo A's sessions closes once a switch lands in
// repo B, which does not own it; the session itself keeps running.
func TestRepoSwitchClosesAnotherReposConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m = switchAndLoad(t, m, gittest.BasicRepo(t, "b\n"))
	if m.console != nil {
		t.Fatalf("console = %+v: repo B does not own repo A's session", m.console)
	}
	if s.Info().State != domain.SessionRunning {
		t.Fatal("closing the screen must not end the session")
	}
	if !strings.Contains(m.statusMsg, s.Info().Label) {
		t.Fatalf("status = %q, want it to say the session keeps running", m.statusMsg)
	}
}

// A switch to another worktree of the SAME repo keeps the console: the
// session still belongs to the repository on screen.
func TestWorktreeSwitchKeepsTheReposConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	other := filepath.Join(t.TempDir(), "wt2")
	if out, err := exec.Command("git", "-C", m.currentWorktree, "worktree", "add", "-b", "wt2", other).CombinedOutput(); err != nil {
		t.Fatalf("worktree add: %v\n%s", err, out)
	}
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m = switchAndLoad(t, m, other)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatalf("console = %+v: a worktree switch inside the repo keeps it", m.console)
	}
}

// alt+a cycles only the sessions of the repository on screen.
func TestAltACyclesOnlyThisReposSessions(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	mine := startTestSession(t, m, `sleep 5`)
	if _, err := domain.Sessions().Start(sessionSpecForTest("foreign", t.TempDir())); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ { // mine, every time: the foreign one is not in the ring
		mm, _ := m.Update(altKey('a'))
		m = mm.(Model)
		if m.console == nil || m.console.id != mine.Info().ID || !m.console.focused {
			t.Fatalf("alt+a #%d: console = %+v, want only this repo's session, bound", i+1, m.console)
		}
	}
	if !m.hasRepoSessions(false) {
		t.Fatal("the footer's alt+a gate sees this repo's agent")
	}
}

func TestAltAWithOnlyAnotherReposSessionSaysSo(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 5`)
	if _, err := domain.Sessions().Start(sessionSpecForTest("foreign", t.TempDir())); err != nil {
		t.Fatal(err)
	}
	if err := domain.Sessions().Kill(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	<-s.Done()
	mm, _ := m.Update(altKey('a'))
	m = mm.(Model)
	if m.console != nil || m.statusMsg == "" {
		t.Fatalf("console=%+v status=%q: another repo's agent is not this repo's", m.console, m.statusMsg)
	}
	if m.hasRepoSessions(false) {
		t.Fatal("the footer must not advertise alt+a for another repo's agent")
	}
}

// enter on another repo's session in the ctrl+\ popup switches to that
// repository first; the console opens once it has loaded.
func TestSessionsPopupEnterOnAnotherReposSessionSwitches(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	_ = startTestSession(t, m, `sleep 5`) // swaps in the test manager
	repoB := gittest.BasicRepo(t, "b\n")
	foreign, err := domain.Sessions().Start(sessionSpecForTest("foreign", repoB))
	if err != nil {
		t.Fatal(err)
	}
	mm, _ := m.Update(ctrlBackslash())
	m = mm.(Model)
	p, ok := m.topLayer().(*sessionsPopup)
	if !ok {
		t.Fatalf("top = %T", m.topLayer())
	}
	for i, id := range p.ids {
		if id == foreign.Info().ID {
			p.sel = i
		}
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(Model)
	if m.switchTarget != repoB {
		t.Fatalf("switchTarget = %q, want the session's repo %q", m.switchTarget, repoB)
	}
	if m.console != nil {
		t.Fatal("the console waits for the new repo to load")
	}
	m = landLoad(t, m)
	if m.console == nil || m.console.id != foreign.Info().ID || !m.console.focused {
		t.Fatalf("console = %+v, want the chosen session focused", m.console)
	}
}

// The switch to another repo's session is refused while something that owns
// the screen is still open under the popup.
func TestOpenAnotherReposSessionRefusedOverAnOpenWindow(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	_ = startTestSession(t, m, `sleep 5`)
	foreign, err := domain.Sessions().Start(sessionSpecForTest("foreign", gittest.BasicRepo(t, "b\n")))
	if err != nil {
		t.Fatal(err)
	}
	m = m.pushLayer(&agentStartPopup{})
	before := m.switchTarget
	m, _ = m.openSessionAnywhere(foreign.Info().ID)
	if m.switchTarget != before || m.consoleSwitch.armed || m.console != nil {
		t.Fatalf("switched over an open window: target=%q armed=%v console=%+v", m.switchTarget, m.consoleSwitch.armed, m.console)
	}
	if m.statusMsg == "" {
		t.Fatal("the refusal must say why")
	}
}

// enter on another worktree of the same repo keeps the console AND keeps
// the screen: no reload, the view is the target.
func TestWorktreeEnterKeepsConsoleAndScreen(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m, other := addWorktree(t, m, "wt2")
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	nm, _ := m.guardedReRoot(other, true)
	m = nm.(Model)
	if m.console == nil || !m.ready || m.viewed != model.KeyOf(other) {
		t.Fatalf("console=%+v ready=%v viewed=%q", m.console, m.ready, m.viewed)
	}
}
