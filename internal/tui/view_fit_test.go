package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// The rune-at-a-time references truncate, elideLeft and takeWidth replaced:
// the binary search must cut every string at exactly the same place.
func refTruncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

func refElideLeft(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r))+1 > n {
		r = r[1:]
	}
	return "…" + string(r)
}

func refTakeWidth(s string, w int) string {
	if w <= 0 {
		return ""
	}
	r := []rune(s)
	for len(r) > 0 && lipgloss.Width(string(r)) > w {
		r = r[:len(r)-1]
	}
	return string(r)
}

func TestWidthCutsMatchTheRuneAtATimeReference(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"",
		"a",
		"hello world",
		"漢字かな交じり文",
		"ab漢cd字ef",
		"étude café",
		"tab\there",
		"| `engine` | Operations + the `Event`/`Decider`/`Result` contract — 漢字 mixed in.",
	}
	for _, s := range inputs {
		for n := 0; n <= lipgloss.Width(s)+2; n++ {
			if got, want := truncate(s, n), refTruncate(s, n); got != want {
				t.Errorf("truncate(%q, %d) = %q, want %q", s, n, got, want)
			}
			if got, want := elideLeft(s, n), refElideLeft(s, n); got != want {
				t.Errorf("elideLeft(%q, %d) = %q, want %q", s, n, got, want)
			}
			if got, want := takeWidth(s, n), refTakeWidth(s, n); got != want {
				t.Errorf("takeWidth(%q, %d) = %q, want %q", s, n, got, want)
			}
		}
	}
}

// A long line (CLAUDE.md's package map rows run to 1,400 columns) cut to a
// screen's width: one cut must not re-measure the line once per dropped
// rune. Rune-at-a-time, these 60 cuts of a 3,300-rune line take many
// seconds; the bound is generous.
func TestWidthCutsOfALongLineAreFast(t *testing.T) {
	t.Parallel()
	long := strings.Repeat("abcdefghij漢", 300)
	t0 := time.Now()
	for i := 0; i < 20; i++ {
		_ = truncate(long, 120)
		_ = elideLeft(long, 120)
		_ = takeWidth(long, 120)
	}
	if d := time.Since(t0); d > time.Second {
		t.Fatalf("60 cuts of a %d-rune line took %v", len([]rune(long)), d)
	}
}
