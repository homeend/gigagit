package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestStatusIsError(t *testing.T) {
	errs := []string{
		"error: git stash apply failed (exit 1)",
		"files: boom", "commits: boom", "amend: boom",
		"interactive rebase: boom", "cannot create: boom",
	}
	for _, s := range errs {
		if !statusIsError(s) {
			t.Errorf("statusIsError(%q) = false, want true", s)
		}
	}
	ok := []string{
		"", "working…", "nothing to stash", "title required",
		"resolve conflicts first", "agent skills: 1 installed, 0 refreshed",
		"resolved foo (kept theirs)", "merge continued",
	}
	for _, s := range ok {
		if statusIsError(s) {
			t.Errorf("statusIsError(%q) = true, want false", s)
		}
	}
}

func statusRenderModel() Model {
	return Model{width: 120, height: 30, focus: panelStatus, sel: map[panel]int{}}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}

// TestStatusErrorLinePreservesText guards that the error-styling path does not
// corrupt or drop the message (the color itself is verified visually — lipgloss
// strips color in the non-TTY test environment).
func TestStatusErrorLinePreservesText(t *testing.T) {
	m := statusRenderModel()
	m.statusMsg = "error: git stash apply failed (exit 1)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "error: git stash apply failed") {
		t.Fatalf("error status line lost its text: %q", line)
	}
}

func TestStatusNormalLinePreservesText(t *testing.T) {
	m := statusRenderModel()
	m.statusMsg = "resolved foo (kept theirs)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "resolved foo (kept theirs)") {
		t.Fatalf("normal status line lost its text: %q", line)
	}
}
