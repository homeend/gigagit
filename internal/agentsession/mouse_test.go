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
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
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
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
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
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
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
	t.Cleanup(func() { _ = s.cmd.Process.Kill() })
	waitText(t, s, "DONE")
	if c := s.Clipboard(); c.Text != "copied" || c.Seq != 1 || c.Over {
		t.Fatalf("clip = %+v", c)
	}
}
