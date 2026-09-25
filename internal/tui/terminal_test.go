package tui

import (
	"runtime"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// Serial: installs a process-global session manager.
func TestOpenTerminalRowStartsAFocusedShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.cfg.Console.Shell = "/bin/sh"
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
	m.focus = panelWorktrees
	m.sel[panelWorktrees] = 0
	var run func(Model) (Model, error)
	for _, r := range m.sessionMenuRows() {
		if r.id == "open-terminal" {
			r := r
			run = func(m Model) (Model, error) {
				mm, cmd := r.run(m)
				m = mm.(Model)
				if cmd == nil {
					return m, nil
				}
				mm, _ = m.Update(cmd())
				return mm.(Model), nil
			}
		}
	}
	if run == nil {
		t.Fatal("no Open terminal row on a worktree")
	}
	m, _ = run(m)
	if m.console == nil || !m.console.focused {
		t.Fatalf("the terminal console must open focused: %+v", m.console)
	}
	s, ok := m.consoleSession()
	if !ok || s.Info().Label != "Terminal" {
		t.Fatalf("session = %+v", s)
	}
}

func TestChildEnvNamesTheInbox(t *testing.T) {
	t.Parallel()
	m, dir := steerModel(t)
	if got := m.childEnv(); len(got) != 1 || got[0] != "GG_INBOX="+dir {
		t.Fatalf("childEnv = %v", got)
	}
	m.cfg.UI.AgentSteering = "off"
	if got := m.childEnv(); got != nil {
		t.Fatalf("steering off must name no inbox: %v", got)
	}
}

func TestSteerReplyGoesToTheCommandsInbox(t *testing.T) {
	t.Parallel()
	m, _ := steerModel(t)
	other := t.TempDir()
	c := steer.Command{ID: "x1", Cmd: "focus", Panel: "branches", Wait: true, From: other}
	_, cmd := m.applySteer(c)
	if cmd == nil {
		t.Fatal("a waited command must be answered")
	}
	cmd()
	rep, ok := steer.AwaitReply(other, "x1", time.Second)
	if !ok || !rep.OK {
		t.Fatalf("the reply must land in the inbox the command came from: %+v %v", rep, ok)
	}
}
