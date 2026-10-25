package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// askQuit is the page's ☰ → quit gg…: the host calls the installed quitter
// from its HTTP goroutine; the answer lands on the returned channel.
func askQuit(t *testing.T, f *fakeWebHost) <-chan error {
	t.Helper()
	if f.quitter == nil {
		t.Fatal("the TUI installed no quitter on its page")
	}
	done := make(chan error, 1)
	go func() { done <- f.quitter(context.Background()) }()
	return done
}

// The page's quit is the terminal's: the user confirmed on the page, so the
// TUI answers first (the page must not read the closing page as a refusal),
// then ends the work and quits past the live-work guard.
func TestPageQuitQuitsTheTUI(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	done := askQuit(t, f)
	msg := waitWebQuitCmd(m.web)()
	req, ok := msg.(webQuitRequestMsg)
	if !ok {
		t.Fatalf("msg = %#v, want the page's quit request", msg)
	}
	nm, cmd := m.Update(req)
	m = nm.(Model)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("quit = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the page's quit was not answered")
	}
	if !m.quitConfirmed {
		t.Fatal("a page quit must pass the live-work guard (quitConfirmed)")
	}
	if cmd == nil {
		t.Fatal("no quit command")
	}
	got := cmd()
	if _, ok := got.(tea.QuitMsg); !ok {
		t.Fatalf("cmd = %#v, want tea.QuitMsg", got)
	}
	if quitFilter(m, got) != got {
		t.Fatal("the quit guard held a quit the page confirmed")
	}
	// A second click while quitting changes nothing and is still answered.
	done = askQuit(t, f)
	nm, cmd = m.Update(waitWebQuitCmd(m.web)())
	if err := <-done; err != nil || cmd != nil || !nm.(Model).quitConfirmed {
		t.Fatalf("second quit: err=%v cmd=%v", err, cmd != nil)
	}
}

// Closing the page ends the quit wait and refuses a late ask.
func TestCloseWebEndsTheQuitWait(t *testing.T) {
	f := installFakeHost(t)
	m := servingModel(t, f)
	m.closeWeb()
	got := make(chan tea.Msg, 1)
	go func() { got <- waitWebQuitCmd(m.web)() }()
	select {
	case msg := <-got:
		if msg != nil {
			t.Fatalf("msg = %#v, want nil after close", msg)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the quit wait outlived the page")
	}
	if err := <-askQuit(t, f); err == nil {
		t.Fatal("a closed terminal must refuse")
	}
}
