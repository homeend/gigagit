package tui

import (
	"io"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

const fortyLines = `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; printf 'TAIL'; while :; do sleep 5; done`

// keys sends each key through Update.
func keys(m Model, ks ...tea.KeyMsg) Model {
	for _, k := range ks {
		nm, _ := m.Update(k)
		m = nm.(Model)
	}
	return m
}

var altPgUp = tea.KeyMsg{Type: tea.KeyPgUp, Alt: true}

func TestScrollWheelEntersAndShowsHistory(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	for range 20 {
		m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	}
	if m.console.scroll == nil || m.console.scroll.top != 0 {
		t.Fatalf("scroll = %+v", m.console.scroll)
	}
	if out := m.View(); !strings.Contains(out, "row-0") || strings.Contains(out, "TAIL") {
		t.Fatal("top of history not shown")
	}
}

func TestScrollWheelEntersOnUnfocusedConsole(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused, m.focus = false, panelBranches
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	if m.console.scroll == nil || m.focus != panelBranches {
		t.Fatalf("scroll=%v focus=%v", m.console.scroll != nil, m.focus)
	}
}

func TestScrollPastBottomLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelUp, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	m = mouseAt(m, x0, y0, tea.MouseButtonWheelDown, tea.MouseActionPress)
	if m.console.scroll != nil {
		t.Fatal("still scrolling at the bottom")
	}
}

const hexEcho = `stty raw -echo; printf 'READY\n'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`

func TestScrollEscNeverReachesAgent(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, hexEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m = keys(m, altPgUp)
	if m.console.scroll == nil {
		t.Fatal("alt+pgup did not enter scroll mode")
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.console.scroll != nil || !m.console.focused {
		t.Fatalf("esc: scroll=%v focused=%v", m.console.scroll != nil, m.console.focused)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("z")})
	waitScreen(t, s, "7a")
	if strings.Contains(strings.Join(s.Screen().Lines, ""), "1b") {
		t.Fatal("esc (or alt+pgup) reached the agent")
	}
}

func TestScrollTypedKeyLeavesAndForwards(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, hexEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m = keys(m, altPgUp, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.console.scroll != nil {
		t.Fatal("typing did not leave scroll mode")
	}
	waitScreen(t, s, "79")
}

func TestScrollKeysMoveCursorAndView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m = keys(m, altPgUp)
	last := m.console.scroll.hist.Len() - 1
	if m.console.scroll.cursor != last {
		t.Fatalf("cursor %d, want %d", m.console.scroll.cursor, last)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyHome})
	if m.console.scroll.cursor != 0 || m.console.scroll.top != 0 {
		t.Fatalf("home: cursor=%d top=%d", m.console.scroll.cursor, m.console.scroll.top)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.console.scroll.cursor != 1 {
		t.Fatalf("down: cursor=%d", m.console.scroll.cursor)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("G")})
	if m.console.scroll == nil || m.console.scroll.cursor != last {
		t.Fatal("G did not go to the bottom (and must not leave)")
	}
}

func TestScrollUnfocusedPgUpEnters(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused = false // its column keeps the focus
	m = keys(m, tea.KeyMsg{Type: tea.KeyPgUp})
	if m.console.scroll == nil {
		t.Fatal("pgup on the unfocused console did not enter scroll mode")
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.console == nil || m.console.scroll != nil {
		t.Fatal("esc must leave scroll mode, not close the console")
	}
}

func TestScrollResizeLeaves(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m = keys(m, altPgUp)
	nm, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 30})
	m = nm.(Model)
	if m.console.scroll != nil {
		t.Fatal("a resize kept the old-width snapshot")
	}
}

func TestScrollTitleShowsPositionAndNewOutput(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; read _; echo LATER; sleep 5`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "row-39")
	m = keys(m, altPgUp)
	if out := m.View(); !strings.Contains(out, "scroll ↑") || strings.Contains(out, "new output") {
		t.Fatal("title before new output")
	}
	s.SendText("\r")
	waitScreen(t, s, "LATER")
	if out := m.View(); !strings.Contains(out, "new output") {
		t.Fatal("new output not flagged")
	}
}

// runCmdMsgs runs cmd and, for a tea.BatchMsg, each of its commands.
func runCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, runCmdMsgs(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}

func TestScrollSpaceSpaceEnterCopiesLines(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m = keys(m, altPgUp, tea.KeyMsg{Type: tea.KeyHome},
		tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeySpace},
		tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeyDown}, tea.KeyMsg{Type: tea.KeySpace})
	if out := m.View(); !strings.Contains(out, "\x1b[7mrow-2") {
		t.Fatal("the line selection is not drawn")
	}
	nm, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = nm.(Model)
	if cmd == nil {
		t.Fatal("enter produced no copy")
	}
	runCmdMsgs(cmd)
	if copied != "row-1\nrow-2\nrow-3" {
		t.Fatalf("copied %q", copied)
	}
	if m.console.scroll == nil || m.console.scroll.sel.on {
		t.Fatal("a copy must stay in scroll mode and clear the selection")
	}
}

// pressRelease clicks the left button at (x, y) and runs every command the
// press and the release returned.
func pressRelease(m Model, x, y int) Model {
	nm, c1 := m.Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress})
	nm, c2 := nm.(Model).Update(tea.MouseMsg{X: x, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	runCmdMsgs(c1)
	runCmdMsgs(c2)
	return nm.(Model)
}

func TestScrollDragAtLiveViewCopiesText(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf 'alpha beta\r\ngamma delta'; while :; do sleep 5; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "delta")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+6, y0, tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouseAt(m, x0+4, y0+1, tea.MouseButtonLeft, tea.MouseActionMotion)
	if out := m.View(); !strings.Contains(out, "\x1b[7mbeta") {
		t.Fatal("the dragged span is not drawn")
	}
	nm, cmd := m.Update(tea.MouseMsg{X: x0 + 4, Y: y0 + 1, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = nm.(Model)
	runCmdMsgs(cmd)
	if copied != "beta\ngamma" {
		t.Fatalf("copied %q", copied)
	}
	if m.console.scroll == nil {
		t.Fatal("a drag must enter scroll mode")
	}
}

func TestScrollDoubleClickWordTripleClickLine(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf 'one two.three four'; while :; do sleep 5; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "four")
	x0, y0 := contentOrigin(m)
	m = pressRelease(m, x0+6, y0)
	if copied != "" {
		t.Fatalf("a single click copied %q", copied)
	}
	m = pressRelease(m, x0+6, y0)
	if copied != "two.three" {
		t.Fatalf("double click copied %q", copied)
	}
	_ = pressRelease(m, x0+6, y0)
	if copied != "one two.three four" {
		t.Fatalf("triple click copied %q", copied)
	}
}

func TestScrollDragPastEdgeScrolls(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0, y0+2, tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouseAt(m, x0+1, y0+2, tea.MouseButtonLeft, tea.MouseActionMotion) // the drag begins: scroll mode
	top := m.console.scroll.top
	m = mouseAt(m, x0, y0-1, tea.MouseButtonLeft, tea.MouseActionMotion) // above the content: the title row
	if m.console.scroll.top != top-1 {
		t.Fatalf("top %d, want %d", m.console.scroll.top, top-1)
	}
	m = mouseAt(m, x0, y0+2, tea.MouseButtonWheelUp, tea.MouseActionPress) // wheel while held extends
	if !m.console.scroll.drag.active || m.console.scroll.top >= top-1 {
		t.Fatal("wheel during a drag did not scroll the held selection")
	}
}

func TestScrollDragHeldOutsideBoxStillExtends(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `printf 'alpha beta'; while :; do sleep 5; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "beta")
	x0, y0 := contentOrigin(m)
	m = mouseAt(m, x0+6, y0, tea.MouseButtonLeft, tea.MouseActionPress)
	// Released over the left panels: the selection ends at column 0.
	nm, cmd := m.Update(tea.MouseMsg{X: 1, Y: y0, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = nm.(Model)
	runCmdMsgs(cmd)
	if copied != "alpha b" || m.console.scroll.drag.active {
		t.Fatalf("copied %q active=%v", copied, m.console.scroll.drag.active)
	}
}

// A plain click on a normal-screen console focuses it and stays live: what
// is typed next reaches the agent (review Critical: it used to freeze the
// view and eat the keys).
func TestConsolePlainClickStaysLive(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, hexEcho)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "READY")
	m.console.focused, m.focus = false, panelBranches
	x0, y0 := contentOrigin(m)
	m = pressRelease(m, x0+3, y0+1)
	if !m.console.focused || m.console.scroll != nil {
		t.Fatalf("focused=%v scroll=%v", m.console.focused, m.console.scroll != nil)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("g")})
	waitScreen(t, s, "67")
}

// A click inside scroll mode keeps the frozen view even when focusing the
// console would resize its PTY (review Important 1).
func TestScrollClickKeepsFrozenView(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	s := startTestSession(t, m, fortyLines)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	m.console.focused, m.focus = false, panelBranches
	in := s.Input()
	_ = s.Resize(in.Cols-10, in.Rows) // another viewer holds a different size
	x0, y0 := contentOrigin(m)
	for range 20 {
		m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	}
	if m.console.scroll == nil || m.console.scroll.top != 0 {
		t.Fatal("setup: not scrolled to the top")
	}
	m = pressRelease(m, x0+1, y0+3)
	if sc := m.console.scroll; sc == nil || sc.top != 0 || sc.cursor != 3 {
		if sc == nil {
			t.Fatal("scroll mode left")
		}
		t.Fatalf("view jumped: top=%d cursor=%d", sc.top, sc.cursor)
	}
	m = keys(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if w, h := m.consoleBox(); func() bool { c, r := consoleInner(w, h); i := s.Input(); return i.Cols != c || i.Rows != r }() {
		t.Fatal("leaving scroll mode did not take the size back")
	}
}

// Scroll mode keeps the mouse after the program took it (user ruling
// 2026-10-07): the frozen view is what the user is looking at, so the wheel
// scrolls it and a drag copies from it until scroll mode is left; the
// program gets nothing meanwhile.
func TestScrollKeepsMouseAfterProgramTakesIt(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 120, 30
	var copied string
	m.clipWrite = func(_ io.Writer, s string) (string, error) { copied = s; return "fake", nil }
	s := startTestSession(t, m, `i=0; while [ $i -lt 40 ]; do echo "row-$i"; i=$((i+1)); done; printf 'TAIL'; read _; stty raw -echo; printf '\033[?1049h\033[?1000h\033[?1006hALT'; while :; do head -c 1 | od -An -tx1 | tr -d ' \n'; done`)
	m, _ = m.openConsole(s.Info().ID)
	waitScreen(t, s, "TAIL")
	x0, y0 := contentOrigin(m)
	for range 3 {
		m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	}
	if m.console.scroll == nil {
		t.Fatal("the wheel did not enter scroll mode")
	}
	s.SendText("\r")
	waitScreen(t, s, "ALT")
	top := m.console.scroll.top
	m = mouseAt(m, x0+1, y0+1, tea.MouseButtonWheelUp, tea.MouseActionPress)
	if m.console.scroll == nil || m.console.scroll.top >= top {
		t.Fatalf("the wheel left or did not scroll the frozen view: %+v", m.console.scroll)
	}
	m = mouseAt(m, x0, y0+1, tea.MouseButtonLeft, tea.MouseActionPress)
	m = mouseAt(m, x0+3, y0+2, tea.MouseButtonLeft, tea.MouseActionMotion)
	nm, cmd := m.Update(tea.MouseMsg{X: x0 + 3, Y: y0 + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease})
	m = nm.(Model)
	runCmdMsgs(cmd)
	if m.console.scroll == nil || !strings.HasPrefix(copied, "row-") {
		t.Fatalf("scroll=%v copied=%q", m.console.scroll != nil, copied)
	}
	s.SendText("z")
	waitScreen(t, s, "7a")
	for _, l := range s.Screen().Lines {
		if strings.Contains(l, hexOf("\x1b[<")) {
			t.Fatalf("the program got the mouse: %q", l)
		}
	}
}
