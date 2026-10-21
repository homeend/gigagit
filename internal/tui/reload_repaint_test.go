package tui

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// repaintMsgs reports whether msgs hold both halves of r's repaint: a
// terminal size query (a resize gg missed lands as a WindowSizeMsg) and a
// full-screen clear (rows the renderer thinks unchanged are drawn again).
func repaintMsgs(msgs []tea.Msg) (size, clear bool) {
	sizeT := fmt.Sprintf("%T", tea.WindowSize()())
	clearT := fmt.Sprintf("%T", tea.ClearScreen())
	for _, msg := range msgs {
		switch fmt.Sprintf("%T", msg) {
		case sizeT:
			size = true
		case clearT:
			clear = true
		}
	}
	return size, clear
}

// onlyRepaint reports whether cmd is r's repaint and nothing else (a refused
// reload dispatches no read).
func onlyRepaint(t *testing.T, cmd tea.Cmd) bool {
	t.Helper()
	msgs := flattenCmd(t, cmd)
	size, clear := repaintMsgs(msgs)
	return size && clear && len(msgs) == 2
}

func TestReloadKeyRepaints(t *testing.T) {
	t.Parallel()
	m := newTestModelForReload(t)
	nm, cmd := m.Update(key("r"))
	if !nm.(Model).anySourceInflight() {
		t.Fatal("r should still reload the panels")
	}
	if size, clear := repaintMsgs(flattenCmd(t, cmd)); !size || !clear {
		t.Fatalf("r should re-read the terminal size (%v) and clear the screen (%v)", size, clear)
	}
}

// A reload r refuses (a load or a repo switch in flight) still repaints: a
// stale screen is no reason to wait.
func TestReloadKeyRepaintsWhileBlocked(t *testing.T) {
	t.Parallel()
	m := newTestModelForReload(t)
	m.loading = true
	_, cmd := m.Update(key("r"))
	if !onlyRepaint(t, cmd) {
		t.Fatal("a blocked r should re-read the terminal size and clear the screen, and dispatch nothing else")
	}
}
