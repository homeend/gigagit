package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func TestQuitFilterHoldsQuitWithLiveSessions(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	if _, held := quitFilter(m, tea.QuitMsg{}).(quitHeldMsg); !held {
		t.Fatal("quit with a live session must be held")
	}
	m.quitConfirmed = true
	if _, quit := quitFilter(m, tea.QuitMsg{}).(tea.QuitMsg); !quit {
		t.Fatal("a confirmed quit must pass")
	}
	if _, ok := quitFilter(m, keyMsg("x")).(tea.KeyMsg); !ok {
		t.Fatal("other messages pass untouched")
	}
}

func TestQuitFilterPassesWithoutLiveSessions(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `exit 0`)
	<-s.Done()
	if _, quit := quitFilter(m, tea.QuitMsg{}).(tea.QuitMsg); !quit {
		t.Fatal("no live session: quit must pass")
	}
}

func TestQuitHeldOpensQuitModePopup(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`)
	mm, _ := m.Update(quitHeldMsg{})
	m = mm.(Model)
	p, ok := m.topLayer().(*sessionsPopup)
	if !ok || !p.quitMode {
		t.Fatalf("top = %T", m.topLayer())
	}
	if !strings.Contains(m.View(), "quit gg?") {
		t.Fatal("quit-mode title missing")
	}
}

func TestKillAllAndQuit(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 30`)
	m, _ = m.openSessionsPopup(true)
	mm, cmd := m.Update(keyMsg("Q"))
	m = mm.(Model)
	if !m.quitConfirmed || cmd == nil {
		t.Fatal("Q must confirm the quit and return the kill-all cmd")
	}
	if _, quit := cmd().(tea.QuitMsg); !quit {
		t.Fatal("the kill-all cmd must end in QuitMsg")
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("kill-all must have ended the session before quitting")
	}
}

func TestExitNoticeWhenUnfocused(t *testing.T) {
	m := loadedModel(t)
	s := startTestSession(t, m, `sleep 0.3; exit 3`)
	m, _ = m.onSessionsChanged() // records the running state
	<-s.Done()
	m, _ = m.onSessionsChanged()
	if !strings.Contains(m.statusMsg, "exited (3)") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestNoExitNoticeForFocusedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 0.3; exit 3`)
	m, _ = m.openConsole(s.Info().ID)
	m, _ = m.onSessionsChanged()
	<-s.Done()
	m.statusMsg = ""
	m, _ = m.onSessionsChanged()
	if m.statusMsg != "" {
		t.Fatalf("a focused console shows the exit itself; status = %q", m.statusMsg)
	}
}

func TestDeleteWorktreeWithRunningSessionRefused(t *testing.T) {
	m := loadedModel(t)
	startTestSession(t, m, `sleep 5`) // fresh manager
	other := t.TempDir()
	m.worktrees = append(m.worktrees, model.Worktree{Path: other, Branch: "feat"})
	if _, err := domain.Sessions().Start(sessionSpecForTest("Claude", other)); err != nil {
		t.Fatal(err)
	}
	m.focus = panelWorktrees
	for i, e := range m.worktreeEntries() {
		if e.sess == "" && m.worktrees[e.wt].Path == other {
			m.sel[panelWorktrees] = i
		}
	}
	mm, _ := m.Update(keyMsg("d"))
	m = mm.(Model)
	if m.running {
		t.Fatal("delete must not start while an agent runs in the worktree")
	}
	if !strings.Contains(m.statusMsg, "Claude") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	_ = time.Second
}

func TestQuitGuardHoldsForHeadlessTask(t *testing.T) {
	m := launchTestModel(t)
	id := submitReview(t, m, "sleep 5")
	if _, held := quitFilter(m, tea.QuitMsg{}).(quitHeldMsg); !held {
		t.Fatal("quit must be held while a task runs")
	}
	if got := liveWork(); got != 1 {
		t.Fatalf("liveWork = %d, want 1", got)
	}
	msg := killAllAndQuitCmd()()
	if _, ok := msg.(tea.QuitMsg); !ok {
		t.Fatalf("got %T", msg)
	}
	if info, _ := domain.Tasks().Get(id); info.State != domain.TaskCancelled {
		t.Fatalf("state %s", info.State)
	}
}

func TestQuitPopupListsTasksWhenOnlyTasksAreAlive(t *testing.T) {
	m := launchTestModel(t)
	submitReview(t, m, "sleep 5")
	m, _ = m.openSessionsPopup(true)
	p := layerOf[*sessionsPopup](m)
	if p == nil || p.tab != tabTasks {
		t.Fatalf("quit popup: %+v", p)
	}
	out := p.render(m, "")
	if !strings.Contains(out, "review — ") || !strings.Contains(out, "[Q]") {
		t.Fatalf("quit popup must list the task and offer Q:\n%s", out)
	}
}
