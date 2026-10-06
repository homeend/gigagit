package agentsession

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

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

// Captured live 2026-10-04 (script, env as gg gives it): Claude Code 2.1.289,
// Codex 0.160.0, Kimi Code 2.1.1.
var (
	capClaudeIdle    = []byte("\x1b]0;\xe2\x9c\xb3 Claude Code\x07")                     // "✳ Claude Code"
	capClaudeWorking = []byte("\x1b]0;\xe2\x97\x90 Math and shell command sequence\x07") // "◐ …"
	capCodexWorking  = []byte("\x1b]0;\xe2\xa0\x8b repo\x07")                            // "⠋ repo"
	capCodexIdle     = []byte("\x1b]0;Run sleep 5 command | repo\x07")
	capKimiBusy      = []byte("\x1b]9;4;3\x07")
	capKimiClear     = []byte("\x1b]9;4;0\x07")
)

func TestOSCFilterRecordsTheTitle(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   []byte
		want string
	}{
		{capClaudeIdle, "✳ Claude Code"},
		{capClaudeWorking, "◐ Math and shell command sequence"},
		{capCodexWorking, "⠋ repo"},
		{capCodexIdle, "Run sleep 5 command | repo"},
		{[]byte("\x1b]2;◑ x\x1b\\"), "◑ x"}, // OSC 2, ST-terminated
		{[]byte("\x1b]0;\x07"), ""},         // Claude clears it on exit
	}
	for _, c := range cases {
		var f oscFilter
		f.filter(bytes.Join([][]byte{[]byte("A"), c.in, []byte("B")}, nil)) // never append to a shared fixture
		if f.title != c.want || !f.changed {
			t.Errorf("%q: title = %q changed=%v, want %q", c.in, f.title, f.changed, c.want)
		}
	}
}

func TestOSCFilterRecordsTheTitleAcrossReads(t *testing.T) {
	t.Parallel()
	in := bytes.Join([][]byte{[]byte("X"), capClaudeWorking, []byte("AB")}, nil)
	for cut := 1; cut < len(in); cut++ {
		var f oscFilter
		f.filter(in[:cut])
		f.filter(in[cut:])
		if f.title != "◐ Math and shell command sequence" {
			t.Fatalf("cut at %d: title = %q", cut, f.title)
		}
	}
}

// An ST terminator (ESC \) split between two reads still ends the string:
// the title and the progress are recorded, and nothing leaks to the screen.
func TestOSCFilterRecordsSTTerminatedStringsAcrossReads(t *testing.T) {
	t.Parallel()
	in := []byte("X\x1b]2;◑ x\x1b\\\x1b]9;4;3\x1b\\AB")
	for cut := 1; cut < len(in); cut++ {
		var f oscFilter
		f.filter(in[:cut])
		f.filter(in[cut:])
		if f.title != "◑ x" || !f.hasProgress || f.progress != 3 {
			t.Fatalf("cut at %d: title = %q progress = %d (has %v)", cut, f.title, f.progress, f.hasProgress)
		}
		if got := screenOf(40, 1, in[:cut], in[cut:])[0]; got != "XAB" {
			t.Fatalf("cut at %d: screen = %q, want %q", cut, got, "XAB")
		}
	}
}

func TestOSCFilterRecordsProgress(t *testing.T) {
	t.Parallel()
	var f oscFilter
	if f.hasProgress {
		t.Fatal("progress before any OSC 9;4")
	}
	f.filter(capKimiBusy)
	if !f.hasProgress || f.progress != 3 {
		t.Fatalf("busy: %v %d", f.hasProgress, f.progress)
	}
	f.filter([]byte("\x1b]9;4;1;40\x07")) // state;value
	if f.progress != 1 {
		t.Fatalf("normal with a value: %d", f.progress)
	}
	f.filter(capKimiClear)
	if f.progress != 0 {
		t.Fatalf("clear: %d", f.progress)
	}
	f.filter([]byte("\x1b]9;hello\x07\x1b]9;4;9\x07")) // a plain OSC 9 notification, an unknown state
	if f.progress != 0 {
		t.Fatalf("non-progress OSC 9 changed it: %d", f.progress)
	}
}

func TestOSCFilterIgnoresOtherAndCancelledStrings(t *testing.T) {
	t.Parallel()
	var f oscFilter
	f.filter(capClaudeIdle)
	f.changed = false
	big := bytes.Repeat([]byte("QUJD"), 1<<18) // 1 MiB of base64
	f.filter(bytes.Join([][]byte{[]byte("\x1b]52;c;"), big, {0x07}}, nil))
	f.filter([]byte("\x1b]8;;https://x\x07link\x1b]8;;\x07"))
	f.filter([]byte("\x1b]0;◐ cancelled\x18")) // CAN
	f.filter([]byte("\x1b]0;◐ broken\x1b[0m")) // ESC that is not ST
	if f.title != "✳ Claude Code" || f.changed {
		t.Fatalf("title = %q changed=%v", f.title, f.changed)
	}
	if cap(f.pay) > 2*titleCap {
		t.Fatalf("buffered %d bytes", cap(f.pay))
	}
}

func TestOSCFilterCapsTheTitleOnARune(t *testing.T) {
	t.Parallel()
	var f oscFilter
	long := strings.Repeat("◐", 200) // 600 bytes
	f.filter([]byte("\x1b]0;" + long + "\x07"))
	if len(f.title) > titleCap || !utf8.ValidString(f.title) || !strings.HasPrefix(long, f.title) || len(f.title) < titleCap-3 {
		t.Fatalf("capped title: %d bytes valid=%v", len(f.title), utf8.ValidString(f.title))
	}
}

// The emulator sees exactly what it saw before the recording existed.
func TestOSCFilterOutputUnchangedByRecording(t *testing.T) {
	t.Parallel()
	in := bytes.Join([][]byte{capClaudeIdle, []byte("é\x1b[1m"), capKimiBusy, capCodexWorking, []byte("\x1b]0;◐ x\x1b\\tail")}, nil)
	var f oscFilter
	got := string(f.filter(in))
	want := "\x1b]0; Claude Code\x07é\x1b[1m\x1b]9;4;3\x07\x1b]0; repo\x07\x1b]0; x\x1b\\tail"
	if got != want {
		t.Fatalf("filter output\n got %q\nwant %q", got, want)
	}
}

func TestOSCFilterCapturesClipboardWrites(t *testing.T) {
	t.Parallel()
	var f oscFilter
	// "hello\nworld" base64, split across two reads, BEL-terminated.
	f.filter([]byte("x\x1b]52;c;aGVsbG8K"))
	f.filter([]byte("d29ybGQ=\x07y"))
	if !f.clipChanged || f.clip != "hello\nworld" || f.clipSeq != 1 || f.clipOver {
		t.Fatalf("got changed=%v clip=%q seq=%d over=%v", f.clipChanged, f.clip, f.clipSeq, f.clipOver)
	}
	f.clipChanged = false
	f.filter([]byte("\x1b]52;;Zm9v\x1b\\")) // empty selection param, ST-terminated
	if !f.clipChanged || f.clip != "foo" || f.clipSeq != 2 {
		t.Fatalf("ST form: clip=%q seq=%d", f.clip, f.clipSeq)
	}
}

func TestOSCFilterIgnoresClipboardReadsAndJunk(t *testing.T) {
	t.Parallel()
	var f oscFilter
	f.filter([]byte("\x1b]52;c;?\x07"))   // a read request: never answered, never stored
	f.filter([]byte("\x1b]52;c;!!!\x07")) // not base64
	f.filter([]byte("\x1b]52;c\x07"))     // no data field
	f.filter([]byte("\x1b]52;c;\x07"))    // empty: xterm's "clear the selection" — never empties the user's clipboard
	if f.clipChanged || f.clipSeq != 0 {
		t.Fatalf("stored something: seq=%d clip=%q", f.clipSeq, f.clip)
	}
}

func TestOSCFilterDropsOversizedClipboard(t *testing.T) {
	t.Parallel()
	var f oscFilter
	big := strings.Repeat("QUFB", clipCap/4+10) // > clipCap base64 bytes
	f.filter([]byte("\x1b]52;c;" + big + "\x07"))
	if !f.clipChanged || !f.clipOver || f.clip != "" || f.clipSeq != 1 {
		t.Fatalf("over=%v clip len=%d seq=%d", f.clipOver, len(f.clip), f.clipSeq)
	}
	f.clipChanged = false
	f.filter([]byte("\x1b]52;c;Zm9v\x07")) // the next small one is fine again
	if f.clipOver || f.clip != "foo" {
		t.Fatalf("after oversize: over=%v clip=%q", f.clipOver, f.clip)
	}
}

func TestOSCFilterClipboardStaysOffScreen(t *testing.T) {
	t.Parallel()
	got := screenOf(40, 2, []byte("\x1b]52;c;aGVsbG8=\x07AB"))
	if got[0] != "AB" {
		t.Fatalf("row0 = %q", got[0])
	}
}
