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

// consoleMouse routes a mouse event over the console: scroll mode's frozen
// view first, else by the child's live modes (spec §1): a program that
// tracks the mouse gets it — a drag it
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
	if held && msg.Action == tea.MouseActionPress && !isWheel(msg.Button) {
		// A press while a button is still held: its release was lost (let
		// go outside the terminal). End what it held, then route the press
		// as a new one — outside the box it is the panels'.
		m, lost := m.endHeldMouse(cx, cy)
		nm, cmd, ok := m.consoleMouse(msg)
		return nm, tea.Batch(lost, cmd), ok
	}
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
	case m.console.scroll != nil:
		// Scroll mode keeps the mouse until it is left, even when the program
		// took the mouse or the alt screen since: the frozen view is what the
		// user is looking at (user ruling 2026-10-07).
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
				sess.ScrollKey(k)
			}
		}
		return m, nil, true
	}
	m.console.held = tea.MouseButtonNone
	return m.consoleScrollMouse(msg, cx, cy, inContent)
}

// endHeldMouse ends a press whose release never came: the child gets the
// release of the button it was given (at content cell (cx, cy), clamped),
// a pending live-view press is dropped, and a scroll-mode drag ends where it
// got to — copied, as its release would have.
func (m Model) endHeldMouse(cx, cy int) (Model, tea.Cmd) {
	if b := m.console.held; b != tea.MouseButtonNone {
		if sess, ok := m.consoleSession(); ok {
			in := sess.Input()
			sess.SendMouse(uv.MouseReleaseEvent{X: clampInt(cx, 0, in.Cols-1), Y: clampInt(cy, 0, in.Rows-1), Button: teaToUVButton[b]})
		}
		m.console.held = tea.MouseButtonNone
	}
	m.console.press = nil
	return m, m.finishDrag()
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
