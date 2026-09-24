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
	if mm, ok := m.(Model); ok && !mm.quitConfirmed && domain.Sessions().LiveCount() > 0 {
		return quitHeldMsg{}
	}
	return msg
}

// killAllAndQuitCmd ends every session, then quits.
func killAllAndQuitCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), killAllGrace)
		defer cancel()
		domain.Sessions().KillAll(ctx)
		return tea.QuitMsg{}
	}
}
