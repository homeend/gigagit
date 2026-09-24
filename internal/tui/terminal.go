package tui

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// childEnv is the environment gg adds to every session it starts: GG_INBOX
// names this TUI's steering inbox, so `gg session …` run inside the child
// reaches THIS gg whichever worktree it later shows (steer_kept.go keeps the
// inbox answered). nil while steering is off.
func (m Model) childEnv() []string {
	if dir := m.childInboxDir(); dir != "" {
		return []string{"GG_INBOX=" + dir}
	}
	return nil
}

// childInboxDir is the inbox a new child is handed ("" = none).
func (m Model) childInboxDir() string {
	if !m.steerActive() {
		return ""
	}
	return m.steerDir
}

// openTerminal starts an interactive shell in worktree ([console] shell, else
// the platform default) and opens its console focused — the same path an
// agent start takes once its session exists.
func (m Model) openTerminal(worktree string) (tea.Model, tea.Cmd) {
	g := m.layout()
	cols, rows := consoleInner(g.rightW, g.boxH[panelCommits])
	svc, shell, env, inbox := m.svc, m.cfg.Console.Shell, m.childEnv(), m.childInboxDir()
	name := i18n.T("Terminal")
	m.statusMsg = i18n.T("starting a terminal…")
	return m, func() tea.Msg {
		s, err := svc.StartTerminal(context.Background(), shell, worktree, cols, rows, env)
		if err != nil {
			return agentStartedMsg{name: name, err: err}
		}
		return agentStartedMsg{id: s.Info().ID, name: name, inbox: inbox}
	}
}
