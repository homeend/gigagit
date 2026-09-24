package agentsession

import (
	"strings"

	uv "github.com/charmbracelet/ultraviolet"
)

// Key is one keystroke for SendKey, encoded by the emulator so the child's
// terminal modes (application cursor keys, kitty flags) are honoured.
type Key = uv.KeyEvent

// Screen is a copied snapshot of the visible grid.
type Screen struct {
	Lines            []string // one ANSI-styled line per row, exactly Rows long
	Cols, Rows       int
	CursorX, CursorY int
	CursorVisible    bool
	AltScreen        bool
}

func (s *Session) running() bool { return s.Info().State == Running }

// SendKey encodes k for the child. A no-op once the session has exited.
func (s *Session) SendKey(k Key) {
	if s.running() {
		s.withEmu(func() { s.emu.SendKey(k) })
	}
}

// SendText sends literal text (no paste bracketing).
func (s *Session) SendText(text string) {
	if s.running() {
		s.withEmu(func() { s.emu.SendText(text) })
	}
}

// Paste sends text bracketed when the child enabled bracketed paste.
func (s *Session) Paste(text string) {
	if s.running() {
		s.withEmu(func() { s.emu.Paste(text) })
	}
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
	})
	s.signal()
	return err
}

// Screen snapshots the visible grid.
func (s *Session) Screen() Screen {
	s.ioMu.Lock() // one consistent snapshot: no write or resize in between
	defer s.ioMu.Unlock()
	w, h := s.emu.Width(), s.emu.Height()
	lines := strings.Split(s.emu.Render(), "\n")
	for len(lines) < h {
		lines = append(lines, "")
	}
	pos := s.emu.CursorPosition()
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
