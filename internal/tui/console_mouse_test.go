package tui

import (
	"io"
	"strconv"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

// mouseAt sends one mouse message through Update.
func mouseAt(m Model, x, y int, b tea.MouseButton, a tea.MouseAction) Model {
	nm, _ := m.Update(tea.MouseMsg{X: x, Y: y, Button: b, Action: a})
	return nm.(Model)
}

// contentOrigin is the screen cell of the console's first content cell.
func contentOrigin(m Model) (int, int) {
	x, y, _, _ := m.consoleRect()
	return x + 2, y + 2
}

// hexOf is s as od -tx1 prints it with the spaces removed.
func hexOf(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		b.WriteString(strconv.FormatInt(int64(s[i])|0x100, 16)[1:])
	}
	return b.String()
}

// A child with SGR tracking on prints the hex of what it reads.
const sgrEcho = `stty raw -echo; printf '\033[?1000h\033[?1002h\033[?1006hREADY'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`

func TestConsoleForwardsClickToTrackingChild(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+2, y0+3, tea.MouseButtonLeft, tea.MouseActionPress)
	waitScreen(t, s, hexOf("\x1b[<0;3;4M"))
	_ = mouseAt(m, x0+2, y0+3, tea.MouseButtonLeft, tea.MouseActionRelease)
	waitScreen(t, s, hexOf("\x1b[<0;3;4m"))
}

func TestConsoleForwardsReleaseOutsideBox(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonLeft, tea.MouseActionPress)
	// Released over the left panels: still reaches the child, clamped to col 0.
	m = mouseAt(m, 1, y0+1, tea.MouseButtonLeft, tea.MouseActionRelease)
	waitScreen(t, s, hexOf("\x1b[<0;1;2m"))
	if m.console.held != tea.MouseButtonNone {
		t.Fatal("held button not cleared on release")
	}
}

func TestConsoleMouseClampsToEmulator(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	in := s.Input()
	cols := in.Cols - 10
	_ = s.Resize(cols, in.Rows) // another viewer shrank the PTY
	x0, y0 := contentOrigin(m)
	_ = mouseAt(m, x0+in.Cols-2, y0, tea.MouseButtonLeft, tea.MouseActionPress)
	// ESC [ < 0 ; <cols> ; 1 M — the last column the PTY has.
	waitScreen(t, s, hexOf("\x1b[<0;"+itoa(cols)+";1M"))
}

func TestConsoleWheelHoverKeepsFocus(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, sgrEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m.console.focused = false
	m.focus = panelBranches
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+4, y0+2, tea.MouseButtonWheelUp, tea.MouseActionPress)
	waitScreen(t, s, hexOf("\x1b[<64;5;3M"))
	if m.focus != panelBranches || m.console.focused {
		t.Fatalf("wheel moved focus: focus=%v consoleFocused=%v", m.focus, m.console.focused)
	}
}

func TestConsoleAltScreenWheelSendsArrows(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	s := startTestSession(t, m, `stty raw -echo; printf '\033[?1049hREADY'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	x0, y0 := contentOrigin(m)
	_ = mouseAt(m, x0, y0, tea.MouseButtonWheelUp, tea.MouseActionPress)
	waitScreen(t, s, strings.Repeat(hexOf("\x1b[A"), 3))
}

func TestConsoleOSC52GoesToClipboard(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf '\033]52;c;b25lCnR3bw==\007OLD'; read _; printf '\033]52;c;bmV3\007NEW'; sleep 5`)
	waitScreen(t, s, "OLD")
	m, _ = m.openConsole(s.Info().ID) // a copy made before the console showed is not replayed
	m, cmd := m.consumeConsoleClip()
	if cmd != nil {
		t.Fatal("a stale clipboard write was replayed")
	}
	s.SendText("\r")
	waitScreen(t, s, "NEW")
	m, cmd = m.consumeConsoleClip()
	if cmd == nil {
		t.Fatal("no copy command for the new OSC 52 write")
	}
	msg := cmd().(clipboardCopiedMsg)
	if copied != "new" || msg.err != nil || msg.ok == "" {
		t.Fatalf("copied=%q msg=%+v", copied, msg)
	}
	if _, cmd = m.consumeConsoleClip(); cmd != nil {
		t.Fatal("the same write was copied twice")
	}
}

// A copy ending in a newline (a whole line selected) is still two lines.
func TestConsoleOSC52CountsLinesWithoutTheTrailingNewline(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 40
	m.clipWrite = func(io.Writer, string) (string, error) { return "fake", nil }
	s := startTestSession(t, m, `read _; printf '\033]52;c;b25lCnR3bwo=\007NEW'; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	s.SendText("\r")
	waitScreen(t, s, "NEW")
	_, cmd := m.consumeConsoleClip()
	if cmd == nil {
		t.Fatal("no copy command")
	}
	if msg := cmd().(clipboardCopiedMsg); !strings.Contains(msg.ok, "2 lines") {
		t.Fatalf("status %q, want 2 lines", msg.ok)
	}
}
