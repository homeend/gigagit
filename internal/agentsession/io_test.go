package agentsession

import (
	"strings"
	"testing"
	"time"

	uv "github.com/charmbracelet/ultraviolet"
)

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if f() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSendTextAndEnterEcho(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `read line; printf 'ECHO[%s]' "$line"; sleep 1`)
	s.SendText("hi there")
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	eventually(t, "echo", func() bool { return strings.Contains(s.screenText(), "ECHO[hi there]") })
}

func TestResizeReachesChild(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `read _; stty size; sleep 1`)
	if err := s.Resize(61, 17); err != nil {
		t.Fatal(err)
	}
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	eventually(t, "stty size", func() bool { return strings.Contains(s.screenText(), "17 61") })
	if sc := s.Screen(); sc.Cols != 61 || sc.Rows != 17 || len(sc.Lines) != 17 {
		t.Fatalf("screen dims = %dx%d lines=%d", sc.Cols, sc.Rows, len(sc.Lines))
	}
}

func TestScreenCursorAndAltScreen(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf '\033[?1049h\033[3;5HX'; sleep 2`)
	eventually(t, "alt screen", func() bool { return s.Screen().AltScreen })
	sc := s.Screen()
	if !strings.Contains(sc.Lines[2], "X") || sc.CursorY != 2 || sc.CursorX != 5 {
		t.Fatalf("cursor=(%d,%d) line2=%q", sc.CursorX, sc.CursorY, sc.Lines[2])
	}
	if !sc.CursorVisible {
		t.Fatal("cursor should start visible")
	}
}

func TestScreenCursorHidden(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf '\033[?25l'; sleep 2`)
	eventually(t, "hidden cursor", func() bool { return !s.Screen().CursorVisible })
}

func TestScrollbackCapped(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `i=0; while [ $i -lt 12000 ]; do echo $i; i=$((i+1)); done`)
	waitDone(t, s)
	if n := s.ScrollbackLen(); n > ScrollbackLines || n < ScrollbackLines-20 {
		t.Fatalf("scrollback = %d, want ≈%d (capped)", n, ScrollbackLines)
	}
}

func TestPasteToNonReaderDoesNotBlock(t *testing.T) {
	t.Parallel()
	needSh(t)
	// Raw mode: canonical mode would silently drop overflow input instead of
	// back-pressuring the writer, hiding a stalled pump.
	s := startSh(t, `stty raw -echo; sleep 5`) // never reads stdin
	done := make(chan struct{})
	go func() {
		s.Paste(strings.Repeat("x", 256*1024))
		s.SendText("more")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Paste blocked on a child that does not read stdin")
	}
	_ = s.Screen() // the emulator lock must still be free
}

func TestInputAfterExitIsNoop(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `exit 0`)
	waitDone(t, s)
	s.SendText("x") // must not panic / block
	s.Paste("y")
	s.SendKey(uv.KeyPressEvent{Code: uv.KeyEnter})
	if err := s.Resize(50, 10); err != nil {
		t.Fatalf("resize after exit = %v, want nil no-op", err)
	}
}

func TestScreenWithCursorReversesCursorCell(t *testing.T) {
	t.Parallel()
	needSh(t)
	s := startSh(t, `printf 'ab'; sleep 2`)
	eventually(t, "text", func() bool { return strings.Contains(s.screenText(), "ab") })
	sc := s.ScreenWithCursor()
	if sc.CursorX != 2 || sc.CursorY != 0 {
		t.Fatalf("cursor = (%d,%d)", sc.CursorX, sc.CursorY)
	}
	if !strings.Contains(sc.Lines[0], "\x1b[7m") {
		t.Fatalf("cursor row has no reverse-video cell: %q", sc.Lines[0])
	}
	if plain := s.Screen(); strings.Contains(plain.Lines[0], "\x1b[7m") {
		t.Fatalf("Screen() must not paint the cursor: %q", plain.Lines[0])
	}
}
