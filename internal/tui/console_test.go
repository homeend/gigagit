package tui

import (
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// startTestSession starts `sh -c script` through the process-global manager
// the TUI reads. Serial tests only (UseSessionManager is global).
func startTestSession(t *testing.T, m Model, script string) *domain.AgentSession {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() {
		domain.Sessions().KillAll(t.Context())
		restore()
	})
	dir := m.currentWorktree
	if dir == "" {
		dir = t.TempDir()
	}
	s, err := m.svc.StartSession(t.Context(), config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: script}, dir, 80, 20)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func waitScreen(t *testing.T, s *domain.AgentSession, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, l := range s.Screen().Lines {
			if strings.Contains(l, want) {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q", want)
}

func TestOpenConsoleDocksFocused(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `printf 'HELLO-CONSOLE'; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	if m.console == nil || !m.console.focused || m.console.maximized || m.focus != panelCommits {
		t.Fatalf("console = %+v focus=%v", m.console, m.focus)
	}
	waitScreen(t, s, "HELLO-CONSOLE")
	if out := m.View(); !strings.Contains(out, "HELLO-CONSOLE") {
		t.Fatal("docked console output missing from the frame")
	}
}

func TestConsoleFollowsBoxSize(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("docked emulator %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = mm.(Model)
	w, h = m.consoleBox()
	cols, rows = consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("after resize emulator %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
	m.console.maximized = true
	m = m.syncConsoleSize()
	w, h = m.consoleBox()
	if w != m.layout().w {
		t.Fatalf("maximised box width %d, want full %d", w, m.layout().w)
	}
}

func TestCloseConsoleRestoresCommits(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelWorktrees
	m = m.rememberLeftFocus()
	m, _ = m.openConsole(s.Info().ID)
	m = m.closeConsole()
	if m.console != nil || m.focus != panelWorktrees {
		t.Fatalf("console=%v focus=%v", m.console, m.focus)
	}
	if s.Info().State != domain.SessionRunning {
		t.Fatal("closing the console must not end the session")
	}
}

func TestReRootKeepsConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	mm, _ := m.reRoot(m.currentWorktree)
	m = mm.(Model)
	if m.console == nil || m.console.id != s.Info().ID {
		t.Fatal("a worktree/repo switch must not close the console")
	}
}

func TestStaleConsoleRepaintDropped(t *testing.T) {
	m := loadedModel(t)
	m.console = &consoleState{id: "s9", gen: 2}
	mm, cmd := m.Update(consoleChangedMsg{id: "s9", gen: 1})
	if cmd != nil {
		t.Fatal("a stale generation must not re-arm the waiter")
	}
	_ = mm
}

func sessionSpecForTest(label, dir string) agentsession.StartSpec {
	return agentsession.StartSpec{Label: label, Repo: "elsewhere", Dir: dir, Argv: []string{"sh", "-c", "sleep 5"}, Cols: 40, Rows: 10}
}

func TestConsoleFooterAndHelp(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 160, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	if f, ok := m.footerOverride(); !ok || !strings.Contains(f, "ctrl+]") || !strings.Contains(f, "ctrl+\\") {
		t.Fatalf("focused footer = %q", f)
	}
	mm, _ := m.Update(tea.KeyMsg{Type: tea.KeyCtrlCloseBracket})
	m = mm.(Model)
	if f, ok := m.footerOverride(); !ok || !strings.Contains(f, "[enter]") {
		t.Fatalf("unfocused footer = %q", f)
	}
	found := false
	for _, l := range helpContent() {
		if strings.HasPrefix(l.text, "ctrl+]") {
			found = true
		}
	}
	if !found {
		t.Fatal("help must document ctrl+]")
	}
}
