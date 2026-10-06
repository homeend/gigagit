package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	uv "github.com/charmbracelet/ultraviolet"

	"github.com/homeend/gigagit/internal/domain"
)

// consoleRect is the console box on screen: the whole body when maximised
// (below the header row), else the Commits column's box.
func (m Model) consoleRect() (x, y, w, h int) {
	w, h = m.consoleBox()
	if m.console != nil && m.console.maximized {
		return 0, 1, w, h
	}
	p := m.layout().pos[panelCommits]
	return p.x, p.y, w, h
}

// consoleCell maps a screen cell to the console's content area: border and
// padding across, border and the title row down (consoleInner's geometry).
func (m Model) consoleCell(x, y int) (cx, cy int, inBox, inContent bool) {
	bx, by, w, h := m.consoleRect()
	inBox = x >= bx && x < bx+w && y >= by && y < by+h
	cols, rows := consoleInner(w, h)
	cx, cy = x-bx-2, y-by-2
	inContent = inBox && cx >= 0 && cx < cols && cy >= 0 && cy < rows
	return cx, cy, inBox, inContent
}

// consoleMouseOwner reports whether the console may take the mouse at all:
// nothing is layered above it (the keyboard's precedence in updateConsoleKey).
func (m Model) consoleMouseOwner() bool {
	return m.console != nil && m.modal == nil && m.proc == nil && m.actionMenu == nil && m.topLayer() == nil
}

// focusConsoleByClick is a left click's focus move onto the console: what
// enter does on the unfocused console.
func (m Model) focusConsoleByClick() Model {
	if m.console.focused && m.focus == panelCommits {
		return m
	}
	m.filterTyping = false
	m = m.rememberLeftFocus()
	m.focus = panelCommits
	if m.filesView != nil {
		m = m.focusRight()
	}
	m.console.focused = true
	m.touchConsole()
	if m.console.scroll != nil {
		return m // the size comes back when scroll mode ends: a resize would drop the frozen view
	}
	return m.syncConsoleSize() // gaining focus takes the size back
}

// consoleMouse routes a mouse event over the console by the child's live
// modes (spec §1): a program that tracks the mouse gets it — a drag it
// started keeps going wherever the pointer goes, so its release always
// arrives — and an alt-screen program without tracking gets ↑/↓ for the
// wheel (xterm's alternate scroll). The wheel never moves focus; a left
// press focuses the console as enter does. false = not the console's:
// handleMouse goes on.
func (m Model) consoleMouse(msg tea.MouseMsg) (Model, tea.Cmd, bool) {
	if !m.consoleMouseOwner() {
		return m, nil, false
	}
	cx, cy, inBox, inContent := m.consoleCell(msg.X, msg.Y)
	held := m.console.held != tea.MouseButtonNone || m.console.press != nil ||
		(m.console.scroll != nil && m.console.scroll.drag.active)
	if !inBox && !held {
		return m, nil, false
	}
	sess, ok := m.consoleSession()
	if !ok {
		return m, nil, false // the gone-session console: handleMouse swallows
	}
	running := sess.Info().State == domain.SessionRunning
	modes := sess.Input()
	if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && inBox {
		m = m.focusConsoleByClick()
	}
	switch {
	case m.console.scroll != nil && running && (modes.Mouse || modes.AltScreen) && msg.Action == tea.MouseActionPress:
		// The program took the mouse (or the alt screen) since scroll mode
		// began: leave it and route by the new mode.
		return m.leaveConsoleScroll().consoleMouse(msg)
	case running && modes.Mouse:
		if !inContent && !held && msg.Action == tea.MouseActionPress {
			return m, nil, true // a press on the border or title only focuses
		}
		if ev, ok := consoleMouseEvent(msg, clampInt(cx, 0, modes.Cols-1), clampInt(cy, 0, modes.Rows-1)); ok {
			sess.SendMouse(ev)
		}
		switch {
		case msg.Action == tea.MouseActionRelease:
			m.console.held = tea.MouseButtonNone
		case msg.Action == tea.MouseActionPress && !isWheel(msg.Button):
			m.console.held = msg.Button
		}
		return m, nil, true
	case running && modes.AltScreen:
		m.console.held = tea.MouseButtonNone
		if k, ok := wheelArrow(msg.Button); ok && msg.Action == tea.MouseActionPress {
			for range 3 {
				sess.SendKey(k)
			}
		}
		return m, nil, true
	}
	m.console.held = tea.MouseButtonNone
	return m.consoleScrollMouse(msg, cx, cy, inContent)
}

func isWheel(b tea.MouseButton) bool {
	return b == tea.MouseButtonWheelUp || b == tea.MouseButtonWheelDown ||
		b == tea.MouseButtonWheelLeft || b == tea.MouseButtonWheelRight
}

// wheelArrow is xterm's alternate scroll: a wheel notch as a cursor key.
func wheelArrow(b tea.MouseButton) (domain.SessionKey, bool) {
	switch b {
	case tea.MouseButtonWheelUp:
		return uv.KeyPressEvent{Code: uv.KeyUp}, true
	case tea.MouseButtonWheelDown:
		return uv.KeyPressEvent{Code: uv.KeyDown}, true
	}
	return nil, false
}

var teaToUVButton = map[tea.MouseButton]uv.MouseButton{
	tea.MouseButtonNone: uv.MouseNone, tea.MouseButtonLeft: uv.MouseLeft,
	tea.MouseButtonMiddle: uv.MouseMiddle, tea.MouseButtonRight: uv.MouseRight,
	tea.MouseButtonWheelUp: uv.MouseWheelUp, tea.MouseButtonWheelDown: uv.MouseWheelDown,
	tea.MouseButtonWheelLeft: uv.MouseWheelLeft, tea.MouseButtonWheelRight: uv.MouseWheelRight,
	tea.MouseButtonBackward: uv.MouseBackward, tea.MouseButtonForward: uv.MouseForward,
}

// consoleMouseEvent turns a Bubble Tea mouse message into the emulator's
// event at content cell (x, y).
func consoleMouseEvent(msg tea.MouseMsg, x, y int) (domain.SessionMouse, bool) {
	b, ok := teaToUVButton[msg.Button]
	if !ok {
		return nil, false
	}
	var mod uv.KeyMod
	if msg.Shift {
		mod |= uv.ModShift
	}
	if msg.Alt {
		mod |= uv.ModAlt
	}
	if msg.Ctrl {
		mod |= uv.ModCtrl
	}
	mm := uv.Mouse{X: x, Y: y, Button: b, Mod: mod}
	switch {
	case msg.Action == tea.MouseActionPress && isWheel(msg.Button):
		return uv.MouseWheelEvent(mm), true
	case msg.Action == tea.MouseActionPress:
		return uv.MouseClickEvent(mm), true
	case msg.Action == tea.MouseActionRelease:
		return uv.MouseReleaseEvent(mm), true
	case msg.Action == tea.MouseActionMotion:
		return uv.MouseMotionEvent(mm), true
	}
	return nil, false
}
