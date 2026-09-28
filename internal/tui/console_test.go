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
	s, err := m.svc.StartSession(t.Context(), config.ToolCommand{Category: "session", Name: "Shell", Mode: "session", Command: script}, dir, "", 80, 20, nil)
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

func TestFocusedConsoleStepsOutWhenItsAgentExits(t *testing.T) {
	m := newTestModel(t)
	s := startTestSession(t, m, "sleep 0.3")
	m, _ = m.openConsole(s.Info().ID)
	m.console.maximized = true
	m, _ = m.onSessionsChanged() // seen running
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	m, _ = m.onSessionsChanged()
	if m.console == nil || m.console.focused || m.console.maximized {
		t.Fatalf("an exited agent leaves nothing to type into: console %+v", m.console)
	}
	if !strings.Contains(m.statusMsg, "exited") {
		t.Fatalf("status %q", m.statusMsg)
	}
}

// A removed session takes its console with it: the Commits column comes back
// instead of a box saying "(agent session gone)" — in every worktree, since
// the console is not per-worktree. Focus stays where the user left it.
func TestConsoleClosesWhenItsSessionIsRemoved(t *testing.T) {
	m := newTestModel(t)
	s := startTestSession(t, m, "sleep 0.3")
	m, _ = m.openConsole(s.Info().ID)
	m, _ = m.onSessionsChanged() // seen running
	select {
	case <-s.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("session did not exit")
	}
	m, _ = m.onSessionsChanged() // exited: steps out, stays docked
	m.focus = panelWorktrees
	if m.console == nil || m.console.focused {
		t.Fatalf("precondition: an exited session stays docked, unfocused: %+v", m.console)
	}
	if err := domain.Sessions().Remove(s.Info().ID); err != nil {
		t.Fatal(err)
	}
	m, _ = m.onSessionsChanged()
	if m.console != nil {
		t.Fatalf("a removed session's console must close, got %+v", m.console)
	}
	if m.focus != panelWorktrees {
		t.Fatalf("focus moved to %v; closing an unfocused console must leave it alone", m.focus)
	}
}

func TestUnfocusedConsoleDoesNotPushItsSizeOnWindowResize(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m.console.focused = false
	_ = s.Resize(100, 30) // another viewer (the web) owns the size now
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 160, Height: 50})
	m = mm.(Model)
	if sc := s.Screen(); sc.Cols != 100 || sc.Rows != 30 {
		t.Fatalf("an unfocused console pushed %dx%d on window resize", sc.Cols, sc.Rows)
	}
}

func TestConsolePushesItsSizeWhenItGainsFocus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	m.console.focused = false
	_ = s.Resize(100, 30)
	m.focus = panelCommits
	mm, _ := m.Update(keyMsg("enter"))
	m = mm.(Model)
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); !m.console.focused || sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("after focus: focused=%v emulator %dx%d, want %dx%d", m.console.focused, sc.Cols, sc.Rows, cols, rows)
	}
}

// ctrl+t on a left panel fullscreens it even while an agent console is docked
// in the Commits column: the console is a right-column occupant like the
// commit list, not a surface the pin must yield to. The console hides with the
// column and comes back, at its column size, when the pin drops.
func TestFullscreenLeftPanelHidesDockedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `printf 'HELLO-CONSOLE'; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "HELLO-CONSOLE")
	m = press(t, m, m.stepOutKey()) // keyboard back to gg
	m = press(t, m, "tab")          // focus a left panel
	if m.focus == panelCommits {
		t.Fatalf("tab left focus on Commits")
	}
	left := m.focus

	m = press(t, m, "ctrl+t")
	if !m.fullMaxed || m.fullMax != left || !m.fullMaxActive() {
		t.Fatalf("ctrl+t with a docked console: fullMaxed=%v fullMax=%v active=%v", m.fullMaxed, m.fullMax, m.fullMaxActive())
	}
	if m.console == nil {
		t.Fatal("the pin must hide the console, not close it")
	}
	if g := m.layout(); g.boxH[panelCommits] != 0 || g.leftW != g.w {
		t.Fatalf("pinned layout: commits box %d, leftW %d of %d", g.boxH[panelCommits], g.leftW, g.w)
	}
	if out := m.View(); strings.Contains(out, "HELLO-CONSOLE") {
		t.Fatal("a fullscreen left panel still painted the console")
	}
	if !m.console.focused && m.focus != left {
		t.Fatalf("focus drifted to %v", m.focus)
	}

	m = press(t, m, "ctrl+t")
	if m.fullMaxed {
		t.Fatal("second ctrl+t must drop the pin")
	}
	if out := m.View(); !strings.Contains(out, "HELLO-CONSOLE") {
		t.Fatal("console missing after the pin dropped")
	}
	w, h := m.consoleBox()
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("emulator %dx%d after the round trip, want the column size %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
}

// Opening a console while a left panel is fullscreen clears the pin: the user
// asked to see the agent, and a pinned column would put the keyboard on a
// hidden console and shrink its PTY to nothing.
func TestOpenConsoleClearsFullscreenPin(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `sleep 5`)
	m.focus = panelBranches
	m = press(t, m, "ctrl+t")
	if !m.fullMaxActive() {
		t.Fatal("baseline: Branches should be fullscreen")
	}
	m, _ = m.openConsole(s.Info().ID)
	if m.fullMaxed || !m.console.focused || m.focus != panelCommits {
		t.Fatalf("after open: fullMaxed=%v focused=%v focus=%v", m.fullMaxed, m.console.focused, m.focus)
	}
	w, h := m.consoleBox()
	if w != m.layout().rightW || h != m.layout().bodyH {
		t.Fatalf("console box %dx%d, want the Commits column %dx%d", w, h, m.layout().rightW, m.layout().bodyH)
	}
	cols, rows := consoleInner(w, h)
	if sc := s.Screen(); sc.Cols != cols || sc.Rows != rows {
		t.Fatalf("emulator %dx%d, want %dx%d", sc.Cols, sc.Rows, cols, rows)
	}
}
