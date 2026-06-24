package tui

import (
	"context"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/gigagit/gg/internal/model"
)

func keyMsg(s string) tea.KeyMsg {
	switch s {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "pgup":
		return tea.KeyMsg{Type: tea.KeyPgUp}
	case "pgdown":
		return tea.KeyMsg{Type: tea.KeyPgDown}
	case "home":
		return tea.KeyMsg{Type: tea.KeyHome}
	case "end":
		return tea.KeyMsg{Type: tea.KeyEnd}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	case "ctrl+d":
		return tea.KeyMsg{Type: tea.KeyCtrlD}
	case "ctrl+up":
		return tea.KeyMsg{Type: tea.KeyCtrlUp}
	case "ctrl+down":
		return tea.KeyMsg{Type: tea.KeyCtrlDown}
	case "ctrl+left":
		return tea.KeyMsg{Type: tea.KeyCtrlLeft}
	case "ctrl+right":
		return tea.KeyMsg{Type: tea.KeyCtrlRight}
	case "space":
		return tea.KeyMsg{Type: tea.KeySpace}
	case "delete":
		return tea.KeyMsg{Type: tea.KeyDelete}
	case "ctrl+r":
		return tea.KeyMsg{Type: tea.KeyCtrlR}
	case "ctrl+s":
		return tea.KeyMsg{Type: tea.KeyCtrlS}
	case "ctrl+w":
		return tea.KeyMsg{Type: tea.KeyCtrlW}
	case "alt+down":
		return tea.KeyMsg{Type: tea.KeyDown, Alt: true}
	case "alt+up":
		return tea.KeyMsg{Type: tea.KeyUp, Alt: true}
	case "alt+left":
		return tea.KeyMsg{Type: tea.KeyLeft, Alt: true}
	case "alt+right":
		return tea.KeyMsg{Type: tea.KeyRight, Alt: true}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}
}

func TestQuitOnQ(t *testing.T) {
	m := New(nil)
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("expected a command from pressing q")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("pressing q should issue tea.Quit")
	}
}

func TestWindowSizeIsRecorded(t *testing.T) {
	m := New(nil)
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mm := updated.(Model)
	if mm.width != 120 || mm.height != 40 {
		t.Fatalf("size = %dx%d, want 120x40", mm.width, mm.height)
	}
}

func TestCtrlLForcesLoadOnCommits(t *testing.T) {
	m := newTestModelForReload(t) // real svc+feed on a FakeRunner (see commit_scope_test.go)
	m.focus = panelCommits
	// Fresh feed: exhausted=false, inFlight=false → CanLoadMore true.
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd == nil {
		t.Fatal("ctrl+l on Commits should dispatch a load")
	}
	if !nm.(Model).commitsLoading {
		t.Fatal("ctrl+l should set commitsLoading")
	}
}

func TestCtrlLNoopWhenExhausted(t *testing.T) {
	m := newTestModelForReload(t)
	m.focus = panelCommits
	// Exhaust the feed: a short initial page.
	m.feed.SetPageSizes(50, 50)
	if _, err := m.feed.LoadInitial(context.Background()); err != nil {
		t.Fatal(err)
	}
	// newTestModelForReload's fake serves a single row → 1 < 50 → exhausted.
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlL})
	if cmd != nil || nm.(Model).commitsLoading {
		t.Fatal("ctrl+l must be a no-op on an exhausted feed")
	}
}

func TestHomeEndCommitsNav(t *testing.T) {
	m := newTestModelForReload(t)
	m.focus = panelCommits
	// Give the panel several rows so home/end have somewhere to go.
	m.commits = []model.Commit{{Hash: "a"}, {Hash: "b"}, {Hash: "c"}}
	m = m.rebuildCommitGraph()
	m.sel[panelCommits] = 1

	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyHome})
	if nm.(Model).sel[panelCommits] != 0 {
		t.Fatalf("home → sel 0, got %d", nm.(Model).sel[panelCommits])
	}

	nm2, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnd})
	want := m.panelLen(panelCommits) - 1
	if nm2.(Model).sel[panelCommits] != want {
		t.Fatalf("end → sel %d, got %d", want, nm2.(Model).sel[panelCommits])
	}
}
