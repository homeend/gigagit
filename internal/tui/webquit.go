package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/i18n"
)

// The hosted page's ☰ → quit gg…: the user confirmed on the page (which
// named the live agents and AI tasks), so the terminal answers and quits
// the way the quit-mode sessions popup's Q does — the work is ended and the
// live-work guard lets the quit through. Quitting closes the page.

// webQuitRequestMsg is the page asking the terminal to quit. reply is
// buffered: Update never blocks answering it.
type webQuitRequestMsg struct {
	reply chan error
}

// quitterFor is the function the host calls for a page quit: it hands the
// request to Update and waits for the answer (bounded by the page's request
// context).
func quitterFor(w *webHostState) func(ctx context.Context) error {
	quits, stop := w.quits, w.stop
	return func(ctx context.Context) error {
		reply := make(chan error, 1)
		select {
		case quits <- webQuitRequestMsg{reply: reply}:
		case <-stop:
			return errors.New("the terminal is closing")
		case <-ctx.Done():
			return ctx.Err()
		}
		select {
		case err := <-reply:
			return err
		case <-stop:
			return errors.New("the terminal is closing")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// waitWebQuitCmd waits for the page's quit request; nil once the page is
// closed.
func waitWebQuitCmd(w *webHostState) tea.Cmd {
	quits, stop := w.quits, w.stop
	return func() tea.Msg {
		select {
		case req := <-quits:
			return req
		case <-stop:
			return nil
		}
	}
}

// onWebQuitRequest answers BEFORE quitting: closing the page closes stop,
// and an answer still owed then would reach the page as a refusal. A second
// request while quitting is answered and changes nothing.
func (m Model) onWebQuitRequest(msg webQuitRequestMsg) (Model, tea.Cmd) {
	msg.reply <- nil
	if m.quitConfirmed {
		return m, nil
	}
	m.quitConfirmed = true
	m.statusMsg = i18n.T("ending agent sessions…")
	return m, killAllAndQuitCmd()
}
