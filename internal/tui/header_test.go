package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/gigagit/gg/internal/model"
)

func TestElideMiddlePath(t *testing.T) {
	const p = "/mnt/t/others/gigagit"
	t.Run("fits unchanged", func(t *testing.T) {
		if got := elideMiddlePath(p, 40); got != p {
			t.Fatalf("got %q, want unchanged", got)
		}
	})
	t.Run("middle-elided keeps head and leaf", func(t *testing.T) {
		got := elideMiddlePath(p, 15)
		if lipgloss.Width(got) > 15 {
			t.Fatalf("width %d > 15: %q", lipgloss.Width(got), got)
		}
		if !strings.Contains(got, "…") {
			t.Fatalf("expected a middle ellipsis, got %q", got)
		}
		if !strings.HasSuffix(got, "gigagit") {
			t.Fatalf("repo dir name must stay at the end, got %q", got)
		}
		if !strings.HasPrefix(got, "/") {
			t.Fatalf("path head should stay visible, got %q", got)
		}
	})
	t.Run("leaf too long falls back to leading ellipsis", func(t *testing.T) {
		got := elideMiddlePath("/very/deep/some-extremely-long-repo-directory-name", 10)
		if lipgloss.Width(got) > 10 {
			t.Fatalf("width %d > 10: %q", lipgloss.Width(got), got)
		}
		if !strings.HasPrefix(got, "…") {
			t.Fatalf("expected a leading ellipsis fallback, got %q", got)
		}
	})
}

func TestPathLeaf(t *testing.T) {
	for in, want := range map[string]string{
		"/mnt/t/others/gigagit":  "gigagit",
		"/mnt/t/others/gigagit/": "gigagit",
		`C:\repos\gigagit`:       "gigagit",
		"gigagit":                "gigagit",
	} {
		if got := pathLeaf(in); got != want {
			t.Errorf("pathLeaf(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestHeaderLinePathRightAligned(t *testing.T) {
	m := Model{
		status:          model.WorkingTreeStatus{Branch: "main"},
		currentWorktree: "/mnt/t/others/gigagit",
	}
	const w = 60
	line := m.headerLine(w)
	plain := ansi.Strip(line)
	if lipgloss.Width(line) > w {
		t.Fatalf("header width %d > %d: %q", lipgloss.Width(line), w, plain)
	}
	if !strings.HasPrefix(plain, "gigagit  branch main") {
		t.Fatalf("left side malformed: %q", plain)
	}
	// Full path fits at width 60 → right edge is the repo dir name, flush right.
	if !strings.HasSuffix(strings.TrimRight(plain, " "), "/mnt/t/others/gigagit") {
		t.Fatalf("path should be right-aligned ending in the full path: %q", plain)
	}
}

func TestHeaderLineMiddleElidesWhenTight(t *testing.T) {
	m := Model{
		status:          model.WorkingTreeStatus{Branch: "main"},
		currentWorktree: "/mnt/t/others/some/deep/path/gigagit",
	}
	const w = 40
	line := m.headerLine(w)
	plain := ansi.Strip(line)
	if lipgloss.Width(line) > w {
		t.Fatalf("header width %d > %d: %q", lipgloss.Width(line), w, plain)
	}
	if !strings.Contains(plain, "…") || !strings.HasSuffix(plain, "gigagit") {
		t.Fatalf("tight header should middle-elide and keep the dir name: %q", plain)
	}
}

func TestHeaderLineNoPathWhenTooNarrow(t *testing.T) {
	m := Model{
		status:          model.WorkingTreeStatus{Branch: "main"},
		currentWorktree: "/mnt/t/others/gigagit",
	}
	line := m.headerLine(18) // only room for the left side
	plain := ansi.Strip(line)
	if lipgloss.Width(line) > 18 {
		t.Fatalf("header width %d > 18: %q", lipgloss.Width(line), plain)
	}
	if strings.Contains(plain, "gigagit") && strings.Contains(plain, "/mnt") {
		t.Fatalf("no room for the path, it should be dropped: %q", plain)
	}
}

// The top-left title is the current directory name (the worktree's leaf), not a
// hard-coded brand, so it tracks whichever repo/worktree is open.
func TestHeaderLineTitleIsCurrentDirName(t *testing.T) {
	m := Model{
		status:          model.WorkingTreeStatus{Branch: "main"},
		currentWorktree: "/home/me/projects/coolrepo",
	}
	plain := ansi.Strip(m.headerLine(80))
	if !strings.HasPrefix(plain, "coolrepo  branch main") {
		t.Fatalf("title should be the current dir name: %q", plain)
	}
}

// With no path yet (e.g. before the first snapshot loads) the title falls back
// to the gigagit brand rather than rendering empty.
func TestHeaderLineTitleFallsBackWhenNoPath(t *testing.T) {
	m := Model{status: model.WorkingTreeStatus{Branch: "main"}}
	plain := ansi.Strip(m.headerLine(80))
	if !strings.HasPrefix(plain, "gigagit  branch main") {
		t.Fatalf("title should fall back to gigagit when no path: %q", plain)
	}
}
