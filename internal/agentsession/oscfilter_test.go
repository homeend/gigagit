package agentsession

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"
)

// screenOf feeds chunks through the filter into a fresh emulator and returns
// its plain-text rows.
func screenOf(cols, rows int, chunks ...[]byte) []string {
	e := vt.NewSafeEmulator(cols, rows)
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, err := e.Read(buf); err != nil {
				return
			}
		}
	}()
	var f oscFilter
	for _, c := range chunks {
		_, _ = e.Write(f.filter(c))
	}
	out := make([]string, rows)
	for y := range rows {
		var b strings.Builder
		for x := range cols {
			if c := e.CellAt(x, y); c != nil && c.Content != "" {
				b.WriteString(c.Content)
			} else {
				b.WriteByte(' ')
			}
		}
		out[y] = strings.TrimRight(b.String(), " ")
	}
	if pw, ok := e.InputPipe().(*io.PipeWriter); ok { // not e.Close: it races the drain's Read
		_ = pw.Close()
	}
	return out
}

// x/ansi reads byte 0x9C inside an OSC as ST even mid-UTF-8, so the title
// "✳ Claude Code" (E2 9C B3 …) ended early and " Claude Code" was printed at
// the cursor. The filter must keep such titles out of the screen.
func TestOSCFilterKeepsUTF8TitlesOffScreen(t *testing.T) {
	t.Parallel()
	for _, title := range []string{"✳ Claude Code", "◐ Claude Code", "◑ x", "é ok"} {
		got := screenOf(40, 2, []byte("\x1b]0;"+title+"\x07AB"))
		if got[0] != "AB" {
			t.Errorf("title %q leaked: row0 = %q", title, got[0])
		}
		got = screenOf(40, 2, []byte("\x1b]2;"+title+"\x1b\\AB"))
		if got[0] != "AB" {
			t.Errorf("ST-terminated title %q leaked: row0 = %q", title, got[0])
		}
	}
}

func TestOSCFilterAcrossChunkBoundaries(t *testing.T) {
	t.Parallel()
	in := []byte("X\x1b]0;✳ Claude Code\x07AB\x1b[1mé\x1b[0m")
	for cut := 1; cut < len(in); cut++ {
		got := screenOf(40, 2, in[:cut], in[cut:])
		if got[0] != "XABé" {
			t.Fatalf("cut at %d: row0 = %q", cut, got[0])
		}
	}
}

// The real ConPTY stream recorded with GG_SESSION_TRACE on Windows
// (2026-09-24): Claude Code's header must render as the logo, not as a
// stray "Claude Code" over it.
func TestOSCFilterClaudeConPTYTrace(t *testing.T) {
	t.Parallel()
	raw, err := os.ReadFile("testdata/claude-conpty-title.raw")
	if err != nil {
		t.Fatal(err)
	}
	got := screenOf(122, 49, raw)
	if !strings.HasPrefix(got[0], " ▐▛███") || strings.Count(got[0], "Claude Code") != 1 {
		t.Fatalf("header row = %q", got[0])
	}
	if strings.TrimSpace(got[13]) != "❯" {
		t.Fatalf("prompt row = %q (a title leaked into the input box)", got[13])
	}
}
