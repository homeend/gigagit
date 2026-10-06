package agentsession

import (
	"runtime"
	"testing"

	uv "github.com/charmbracelet/ultraviolet"
)

func TestInputModesFollowMouseTracking(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m1", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf 'A\n'; sleep 0.3; printf '\033[?1000h\033[?1006hB\n'; sleep 0.3; printf '\033[?1049hC'; sleep 0.3; printf '\033[?1000l\033[?1049lD\n'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "A")
	if m := s.Input(); m.Mouse || m.AltScreen {
		t.Fatalf("at start: %+v", m)
	}
	waitText(t, s, "B")
	if m := s.Input(); !m.Mouse || m.AltScreen || m.Cols != 40 || m.Rows != 5 {
		t.Fatalf("after ?1000h: %+v", m)
	}
	waitText(t, s, "C")
	if m := s.Input(); !m.Mouse || !m.AltScreen {
		t.Fatalf("after ?1049h: %+v", m)
	}
	waitText(t, s, "D")
	if m := s.Input(); m.Mouse || m.AltScreen {
		t.Fatalf("after reset: %+v", m)
	}
}

// The child turns SGR tracking on, then prints in hex the bytes it reads.
func TestSendMouseReachesChildAsSGR(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m2", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`stty raw -echo; printf '\033[?1000h\033[?1006hREADY'; head -c 9 | od -An -tx1 | tr -d ' \n'; printf 'END'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "READY")
	s.SendMouse(uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft})
	// ESC [ < 0 ; 3 ; 4 M — x/ansi adds 1 to both coordinates.
	waitText(t, s, "1b5b3c303b333b344dEND")
}

func TestSendMouseIsInertWithoutTracking(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m3", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`stty raw -echo; printf 'READY'; head -c 1 | od -An -tx1 | tr -d ' \n'; printf 'END'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "READY")
	s.SendMouse(uv.MouseClickEvent{X: 2, Y: 3, Button: uv.MouseLeft})
	s.SendText("z")
	waitText(t, s, "7aEND") // only the z arrived: the click produced no bytes
}

func TestSessionClipboardFromChild(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("c1", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf '\033]52;c;Y29waWVk\007DONE'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "DONE")
	if c := s.Clipboard(); c.Text != "copied" || c.Seq != 1 || c.Over {
		t.Fatalf("clip = %+v", c)
	}
}

// The wheel over a console is not a use of the session: an alt+a cycle
// orders sessions by LastUsed and must not reshuffle under a hover.
func TestSendMouseDoesNotTouch(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m4", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf '\033[?1000h\033[?1006hREADY'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "READY")
	before := s.Info().LastUsed
	s.SendMouse(uv.MouseWheelEvent{X: 1, Y: 1, Button: uv.MouseWheelUp})
	if after := s.Info().LastUsed; !after.Equal(before) {
		t.Fatalf("LastUsed moved %v → %v", before, after)
	}
}

// A reset (RIS, ESC c — what `reset` prints) turns tracking off, leaves the
// alt screen and shows the cursor. x/vt's fullReset re-applies every mode
// through setMode, so the mode callbacks fire; this pins that (its reset
// carries an "investigate" note). One printf each: the order inside one
// read matters.
func TestResetClearsInputModes(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m4", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`printf '\033[?1049h\033[?1000h\033[?25lA\033cB\n'; sleep 0.3; printf '\033c\033[?1002hC\n'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "B")
	if m := s.Input(); m.Mouse || m.AltScreen {
		t.Fatalf("after a reset: %+v", m)
	}
	if !s.Screen().CursorVisible {
		t.Fatal("the cursor stays hidden after a reset")
	}
	waitText(t, s, "C")
	if m := s.Input(); !m.Mouse {
		t.Fatalf("tracking turned on after a reset is lost: %+v", m)
	}
}

// ScrollKey is the wheel as a cursor key (xterm's alternate scroll): it
// reaches the child, but it is neither typed input nor a use.
func TestScrollKeyIsNotInput(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	s, err := start("m5", StartSpec{Dir: t.TempDir(), Cols: 40, Rows: 5, Argv: []string{"sh", "-c",
		`stty raw -echo; printf '\033[?1049hREADY'; head -c 3 | od -An -tx1 | tr -d ' \n'; printf 'END'; sleep 5`}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.kill(); <-s.Done() })
	waitText(t, s, "READY")
	used, in := s.Info().LastUsed, s.LastInput()
	s.ScrollKey(uv.KeyPressEvent{Code: uv.KeyUp})
	waitText(t, s, "1b5b41END")
	if !s.Info().LastUsed.Equal(used) || !s.LastInput().Equal(in) {
		t.Fatalf("LastUsed %v → %v, LastInput %v → %v", used, s.Info().LastUsed, in, s.LastInput())
	}
}
