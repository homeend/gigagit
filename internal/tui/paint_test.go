package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// NOTE: serial (no t.Parallel) — lipgloss.SetColorProfile is process-global.
func withTrueColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

// sgr is the single combined SGR lipgloss emits for fg #CCCCCC on bg #0C0C0C
// under TrueColor — fg params then bg params in one escape (verified against
// real lipgloss output; not the two-escape bgSeq+fgSeq the brief guessed).
const (
	sgr   = "\x1b[38;2;204;204;204;48;2;12;12;12m"
	reset = "\x1b[0m"
)

func TestPaintFrameNoThemeIsIdentity(t *testing.T) {
	t.Parallel()
	in := "ab\n" + reset + "c\n"
	if got := paintFrame(in, 10, 5, "", ""); got != in {
		t.Fatalf("empty colours must return the frame unchanged:\n%q\n%q", in, got)
	}
}

func TestPaintFramePadsWidthAndHeight(t *testing.T) {
	withTrueColor(t)
	got := paintFrame("ab\ncd", 4, 3, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	lines := strings.Split(got, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (padded to h), got %d: %q", len(lines), got)
	}
	want0 := sgr + "ab  " + reset
	if lines[0] != want0 {
		t.Fatalf("line0 = %q, want %q", lines[0], want0)
	}
	want2 := sgr + "    " + reset
	if lines[2] != want2 {
		t.Fatalf("padded line = %q, want %q", lines[2], want2)
	}
}

func TestPaintFrameReassertsAfterReset(t *testing.T) {
	withTrueColor(t)
	in := "\x1b[1mbold" + reset + "tail"
	got := paintFrame(in, 8, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	want := sgr + "\x1b[1mbold" + reset + sgr + "tail" + reset
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPaintFramePadsByDisplayWidth(t *testing.T) {
	withTrueColor(t)
	// 日本 is 4 cells wide; padding to 6 adds two spaces, not four.
	got := paintFrame("日本", 6, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	want := sgr + "日本  " + reset
	if got != want {
		t.Fatalf("got  %q\nwant %q", got, want)
	}
}

func TestPaintFrameNeverTruncates(t *testing.T) {
	withTrueColor(t)
	got := paintFrame("abcdef", 3, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
	if !strings.Contains(got, "abcdef") {
		t.Fatalf("over-long line must be left intact: %q", got)
	}
}

// bareReset is the bare SGR reset ("\x1b[m", no "0") that cellbuf.Wrap
// injects (ansi.ResetStyle) when a Width-bearing style wraps already-styled
// input — distinct from the "\x1b[0m" form lipgloss.Style.Render emits on
// its own.
const bareReset = "\x1b[m"

func TestPaintFrameReassertsAfterBareReset(t *testing.T) {
	withTrueColor(t)

	t.Run("bare", func(t *testing.T) {
		in := "\x1b[1mbold" + bareReset + "tail"
		got := paintFrame(in, 8, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
		want := sgr + "\x1b[1mbold" + bareReset + sgr + "tail" + reset
		if got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("mixed", func(t *testing.T) {
		// One line, both reset forms: a full "\x1b[0m" reset followed later
		// by a bare "\x1b[m" reset — both must re-assert the SGR.
		in := "\x1b[1mA" + reset + "B" + bareReset + "C"
		got := paintFrame(in, 3, 1, lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC"))
		want := sgr + "\x1b[1mA" + reset + sgr + "B" + bareReset + sgr + "C" + reset
		if got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})
}

func TestPaintFrameDegenerateSizes(t *testing.T) {
	withTrueColor(t)
	bg, fg := lipgloss.Color("#0C0C0C"), lipgloss.Color("#CCCCCC")

	t.Run("empty frame padded to height", func(t *testing.T) {
		got := paintFrame("", 4, 2, bg, fg)
		lines := strings.Split(got, "\n")
		if len(lines) != 2 {
			t.Fatalf("want 2 lines, got %d: %q", len(lines), got)
		}
		want := sgr + "    " + reset
		for i, l := range lines {
			if l != want {
				t.Fatalf("line%d = %q, want %q", i, l, want)
			}
		}
	})

	t.Run("zero w and h never panics or pads", func(t *testing.T) {
		got := paintFrame("ab", 0, 0, bg, fg)
		want := sgr + "ab" + reset
		if got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	})

	t.Run("h never truncates extra lines", func(t *testing.T) {
		got := paintFrame("ab\ncd\nef", 3, 1, bg, fg)
		lines := strings.Split(got, "\n")
		if len(lines) != 3 {
			t.Fatalf("want all 3 lines kept, got %d: %q", len(lines), got)
		}
	})
}
