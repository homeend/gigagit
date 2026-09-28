package agentsession

import (
	"strings"
	"testing"
	"time"
)

func waitText(t *testing.T, s *Session, want string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(s.Text(), want) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never showed %q:\n%s", want, s.Text())
}

func TestScreenRunsMergesEqualStylesAndColours(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf '\033[1;31mred\033[0m plain'; sleep 5`)
	waitText(t, s, "red plain")
	sr := s.ScreenRuns()
	if sr.Cols != 40 || sr.Rows != 10 || len(sr.Lines) != 10 {
		t.Fatalf("size %dx%d lines=%d", sr.Cols, sr.Rows, len(sr.Lines))
	}
	row := sr.Lines[0].Runs
	if len(row) < 2 || row[0].Text != "red" || !row[0].Bold || row[0].Fg == "" || row[0].Fg[0] != '#' || len(row[0].Fg) != 7 {
		t.Fatalf("first run = %+v (row %+v)", row[0], row)
	}
	if row[1].Bold || row[1].Fg != "" || !strings.HasPrefix(row[1].Text, " plain") {
		t.Fatalf("second run = %+v", row[1])
	}
	if sr.CursorY != 0 || sr.CursorX != len("red plain") || !sr.CursorVisible || sr.AltScreen {
		t.Fatalf("cursor (%d,%d) vis=%v alt=%v", sr.CursorX, sr.CursorY, sr.CursorVisible, sr.AltScreen)
	}
}

func TestScreenRunsWideGlyphIsOneRunWithoutAPhantomCell(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf '\033[38G你'; sleep 5`) // column 38 of 40: the glyph fills 38–39
	waitText(t, s, "你")
	row := s.ScreenRuns().Lines[0].Runs
	joined := ""
	for _, r := range row {
		joined += r.Text
	}
	// 37 spaces then the glyph: the right-half cell contributes nothing.
	if want := strings.Repeat(" ", 37) + "你"; joined != want {
		t.Fatalf("row = %q, want %q", joined, want)
	}
}

func TestScreenRunsBlankRowIsEmpty(t *testing.T) {
	t.Parallel()
	s := startSh(t, `printf 'x'; sleep 5`)
	waitText(t, s, "x")
	sr := s.ScreenRuns()
	if len(sr.Lines[5].Runs) != 0 {
		t.Fatalf("row 5 = %+v, want no runs", sr.Lines[5].Runs)
	}
}
