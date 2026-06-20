package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

// forceColor makes lipgloss emit ANSI in the non-TTY test environment.
func forceColor(t *testing.T) {
	t.Helper()
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(prev) })
}

func TestLaneColorRecycles(t *testing.T) {
	if laneColor(0) != lanePalette[0] {
		t.Fatalf("lane 0 color")
	}
	if laneColor(len(lanePalette)) != lanePalette[0] {
		t.Fatalf("color should recycle modulo palette length")
	}
}

func TestCommitDotDecoratorColorsNodeOnlyOnFirstLine(t *testing.T) {
	forceColor(t)
	deco := commitDotDecorator(2, lipgloss.Color("40")) // ● at column 2
	line := "  ● 1234567 subject"
	out := deco(line, 0, 0)
	if !strings.Contains(out, "\x1b[") {
		t.Fatalf("expected an ANSI color escape around the node: %q", out)
	}
	if lipgloss.Width(out) != lipgloss.Width(line) {
		t.Fatalf("decorator changed visible width: %d vs %d", lipgloss.Width(out), lipgloss.Width(line))
	}
	// wrap continuation line: untouched.
	if got := deco(line, 0, 1); got != line {
		t.Fatalf("continuation line should be untouched")
	}
	// scrolled past the node: untouched.
	if got := deco(line, 5, 0); got != line {
		t.Fatalf("scrolled-off node should be untouched")
	}
}

func TestCommitDotDecoratorIgnoresNonNode(t *testing.T) {
	deco := commitDotDecorator(2, lipgloss.Color("40"))
	line := "  X 1234567 subject" // no ● at column 2
	if got := deco(line, 0, 0); got != line {
		t.Fatalf("a non-● glyph at nodeCol must not be colored: %q", got)
	}
}
