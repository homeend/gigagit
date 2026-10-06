package agentsession

import (
	"fmt"
	"strings"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

// Key is one keystroke for SendKey, encoded by the emulator so the child's
// terminal modes (application cursor keys, kitty flags) are honoured.
type Key = uv.KeyEvent

// Mouse is one mouse event for SendMouse (click, release, wheel, motion),
// encoded by the emulator in the mouse mode the child enabled.
type Mouse = uv.MouseEvent

// InputModes is what the child asked of its terminal's input: mouse
// tracking (any of modes 9/1000/1001/1002/1003) and the alternate screen,
// plus the emulator's size for clamping coordinates.
type InputModes struct {
	Mouse, AltScreen bool
	Cols, Rows       int
}

// Screen is a copied snapshot of the visible grid.
type Screen struct {
	Lines            []string // one ANSI-styled line per row, exactly Rows long
	Cols, Rows       int
	CursorX, CursorY int
	CursorVisible    bool
	AltScreen        bool
}

func (s *Session) running() bool { return s.Info().State == Running }

// Touch marks the session as used now (Info.LastUsed) — a frontend showing
// it to the user; input sent to it touches it on its own.
func (s *Session) Touch() { s.mu.Lock(); s.info.LastUsed = time.Now(); s.mu.Unlock() }

// SendKey encodes k for the child. A no-op once the session has exited.
// Modified special keys (alt+arrow, shift+home, …) are encoded here —
// the emulator drops them (see encodeModifiedKey); everything else takes
// the emulator's mode-aware path.
func (s *Session) SendKey(k Key) {
	if !s.running() {
		return
	}
	s.Touch()
	s.lastIn.mark()
	if seq, ok := encodeModifiedKey(k); ok {
		s.withEmu(func() { s.emu.SendText(seq) })
		return
	}
	s.withEmu(func() { s.emu.SendKey(k) })
}

// ScrollKey is a wheel notch sent as a cursor key to an alt-screen program
// without mouse tracking (xterm's alternate scroll): encoded like SendKey,
// but like SendMouse it is neither typed input (LastInput — agent_wait and
// the report store read that as "the user typed") nor a use (LastUsed).
func (s *Session) ScrollKey(k Key) {
	if s.running() {
		s.withEmu(func() { s.emu.SendKey(k) })
	}
}

// SendText sends literal text (no paste bracketing).
func (s *Session) SendText(text string) {
	if s.running() {
		s.Touch()
		s.lastIn.mark()
		s.withEmu(func() { s.emu.SendText(text) })
	}
}

// Paste sends text bracketed when the child enabled bracketed paste.
func (s *Session) Paste(text string) {
	if s.running() {
		s.Touch()
		s.lastIn.mark()
		s.withEmu(func() { s.emu.Paste(text) })
	}
}

// Input reports the child's input modes and the emulator size.
func (s *Session) Input() InputModes {
	return InputModes{
		Mouse:     s.mouseModes.Load() != 0,
		AltScreen: s.emu.IsAltScreen(),
		Cols:      s.emu.Width(),
		Rows:      s.emu.Height(),
	}
}

// SendMouse encodes ev for the child in the mouse mode it enabled (a no-op
// when it enabled none, or once the session has exited). Coordinates are
// 0-based cells of the emulator. A mouse event is neither typed input
// (LastInput) nor a use of the session (LastUsed): a hover wheel must not
// reorder the alt+a cycle; the click that focuses a console touches it.
func (s *Session) SendMouse(ev Mouse) {
	if !s.running() {
		return
	}
	s.withEmu(func() { s.emu.SendMouse(ev) })
}

// Resize resizes the emulator and the PTY (SIGWINCH / ConPTY resize).
// A no-op when the size is unchanged or the session has exited.
func (s *Session) Resize(cols, rows int) error {
	if !s.running() {
		return nil
	}
	cols, rows = max(cols, 20), max(rows, 5)
	var err error
	s.withEmu(func() {
		if cols == s.emu.Width() && rows == s.emu.Height() {
			return
		}
		s.emu.Resize(cols, rows)
		err = s.pty.Resize(cols, rows)
		if s.traceEv != nil {
			fmt.Fprintf(s.traceEv, "%d %d %d\n", s.traced.Load(), cols, rows)
		}
	})
	s.signal()
	return err
}

// Screen snapshots the visible grid.
func (s *Session) Screen() Screen { return s.screen(false) }

// ScreenWithCursor is Screen with the cursor cell painted reversed when the
// child shows its cursor — for a console that has keyboard focus.
func (s *Session) ScreenWithCursor() Screen { return s.screen(true) }

func (s *Session) screen(cursor bool) Screen {
	s.ioMu.Lock() // one consistent snapshot: no write or resize in between
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	lines := strings.Split(s.emu.Render(), "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	pos := s.emu.CursorPosition()
	if cursor && !s.cursorHidden.Load() && pos.Y >= 0 && pos.Y < h && pos.X >= 0 && pos.X < w && pos.Y < len(lines) {
		lines[pos.Y] = s.cursorLine(w, pos.X, pos.Y)
	}
	return Screen{
		Lines: lines[:h], Cols: w, Rows: h,
		CursorX: pos.X, CursorY: pos.Y,
		// SafeEmulator has no visibility getter; the CursorVisibility
		// callback installed in start feeds cursorHidden.
		CursorVisible: !s.cursorHidden.Load(),
		AltScreen:     s.emu.IsAltScreen(),
	}
}

// ScrollbackLen is the number of lines scrolled off the top (capped at
// ScrollbackLines).
func (s *Session) ScrollbackLen() int { return s.emu.ScrollbackLen() }

// cursorLine re-renders row y from its cells with the cell at x reversed.
// An empty cell becomes a reversed space, which Render then emits.
func (s *Session) cursorLine(w, x, y int) string {
	row := make(uv.Line, w)
	for i := range w {
		if c := s.emu.CellAt(i, y); c != nil {
			row[i] = *c
		} else {
			row[i] = uv.EmptyCell
		}
	}
	// A wide glyph's right half is a zero cell: paint the glyph it belongs
	// to, or the reversed space would push the row a column wider.
	if x > 0 && row[x].IsZero() && row[x-1].Width > 1 {
		x--
	}
	cc := row[x]
	if cc.IsZero() || cc.Content == "" {
		cc = uv.EmptyCell
	}
	cc.Style.Attrs ^= uv.AttrReverse
	row[x] = cc
	return row.Render()
}
