package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/gigagit/gg/internal/model"
)

// The rendered frame must never exceed the terminal: at most height lines, and
// no line wider than width — even with far more commits than fit and an
// over-long subject. This guards against the overflow that broke the layout.
func TestRenderNeverExceedsTerminal(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 80, 24

	// Many commits (must be windowed, not dumped) and a too-wide subject.
	m.commits = make([]model.Commit, 100)
	for i := range m.commits {
		m.commits[i] = model.Commit{Hash: "abcdef0123", Subject: "commit subject"}
	}
	m.commits[0].Subject = strings.Repeat("x", 400)

	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) > m.height {
		t.Fatalf("render produced %d lines, want <= %d", len(lines), m.height)
	}
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d: %q", i, w, m.width, ln)
		}
	}
}

// A tiny terminal must still render without panic and stay within bounds.
func TestRenderTinyTerminal(t *testing.T) {
	m := loadedModel(t)
	m.width, m.height = 20, 8
	out := m.View()
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	for i, ln := range lines {
		if w := lipgloss.Width(ln); w > m.width {
			t.Fatalf("line %d is %d cols wide, want <= %d", i, w, m.width)
		}
	}
}
