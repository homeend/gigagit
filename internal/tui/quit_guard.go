package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
)

// quitHeldMsg replaces a tea.QuitMsg while agent sessions are alive: the
// sessions popup opens in quit mode instead of gg exiting.
type quitHeldMsg struct{}

// killAllGrace bounds how long quitting waits for sessions to end.
const killAllGrace = 5 * time.Second

// quitFilter is the program's message filter (tea.WithFilter). gg quits
// from many places — q, ctrl+c inside every popup, the palette — and all of
// them end in a tea.QuitMsg, so this is the ONE place the live-session
// guard can sit. A quit confirmed from the quit-mode popup passes.
func quitFilter(m tea.Model, msg tea.Msg) tea.Msg {
	if _, ok := msg.(tea.QuitMsg); !ok {
		return msg
	}
	if mm, ok := m.(Model); ok && !mm.quitConfirmed && liveWork() > 0 {
		return quitHeldMsg{}
	}
	return msg
}

// killAllAndQuitCmd ends every session, then quits.
func killAllAndQuitCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), killAllGrace)
		defer cancel()
		domain.Tasks().KillAll(ctx) // first: the records say cancelled
		domain.Sessions().KillAll(ctx)
		return tea.QuitMsg{}
	}
}

// liveWork is what quitting would end: live agent sessions plus live AI
// tasks without a running session of their own (queued ones, headless ones —
// an interactive task's session is already counted as a session).
func liveWork() int {
	n := domain.Sessions().LiveCount()
	for _, info := range domain.Tasks().List() {
		if !info.State.Live() {
			continue
		}
		if info.Session != "" {
			if s, ok := domain.Sessions().Get(info.Session); ok && s.Info().State == domain.SessionRunning {
				continue
			}
		}
		n++
	}
	return n
}
