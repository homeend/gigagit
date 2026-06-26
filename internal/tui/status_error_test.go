package tui

import (
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/homeend/gigagit/internal/model"
)

// With GIT_TERMINAL_PROMPT=0 a push/pull/fetch that needs credentials no longer
// hangs the TUI — git fails fast. But its raw message ("could not read Username
// … terminal prompts disabled") reads like a gg defect, so friendlyOpError turns
// it into actionable guidance while staying an "error:" line (so it styles red).
func TestFriendlyOpErrorExplainsMissingCredentials(t *testing.T) {
	raw := errors.New("git push failed (exit 128): fatal: could not read Username for 'https://github.com': terminal prompts disabled")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("friendlyOpError(%q) = %q, want an error: line", raw, got)
	}
	if strings.Contains(got, "terminal prompts disabled") {
		t.Fatalf("friendly message still leaks the raw git noise: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "credential") {
		t.Fatalf("friendly message should mention credentials: %q", got)
	}
}

func TestFriendlyOpErrorPassesThroughOtherFailures(t *testing.T) {
	raw := errors.New("git stash apply failed (exit 1): some unrecognised git noise")
	got := friendlyOpError(raw)
	if !strings.Contains(got, "some unrecognised git noise") {
		t.Fatalf("unrelated failure should pass through, got: %q", got)
	}
}

// A non-fast-forward rejection (plain push) is git's most common push failure.
// The raw multi-line stderr ("! [rejected] … (non-fast-forward)" + a wall of
// hints) is useless in a one-line status bar, so friendlyOpError rewrites it
// into one actionable sentence — and must not leak the raw "(non-fast-forward)"
// token or the multi-line hint block.
func TestFriendlyOpErrorExplainsNonFastForward(t *testing.T) {
	raw := errors.New("git push failed (exit 1): To ssh://host/repo.git\n " +
		"! [rejected]        br -> br (non-fast-forward)\n" +
		"error: failed to push some refs to 'ssh://host/repo.git'\n" +
		"hint: Updates were rejected because the tip of your current branch is behind")
	got := friendlyOpError(raw)
	if !statusIsError(got) {
		t.Fatalf("want an error: line, got %q", got)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("status line must be a single line, got %q", got)
	}
	if strings.Contains(strings.ToLower(got), "non-fast-forward") || strings.Contains(got, "hint:") {
		t.Fatalf("friendly message still leaks raw git noise: %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "new commits") {
		t.Fatalf("message should explain the remote is ahead: %q", got)
	}
}

// "stale info" is what --force-with-lease reports when the remote moved since
// the last fetch — the safety net firing, not a defect. The user needs to know
// it means "fetch first", not raw git jargon.
func TestFriendlyOpErrorExplainsStaleInfo(t *testing.T) {
	raw := errors.New("git push failed (exit 1): To ssh://host/repo.git\n " +
		"! [rejected]        br -> br (stale info)\n" +
		"error: failed to push some refs to 'ssh://host/repo.git'")
	got := friendlyOpError(raw)
	if !statusIsError(got) || strings.Contains(got, "\n") {
		t.Fatalf("want a single error: line, got %q", got)
	}
	low := strings.ToLower(got)
	if !strings.Contains(low, "force-with-lease") || !strings.Contains(low, "fetch") {
		t.Fatalf("message should name force-with-lease and steer to fetch: %q", got)
	}
	if strings.Contains(low, "stale info") {
		t.Fatalf("friendly message still leaks the raw '(stale info)' token: %q", got)
	}
}

// A server-side rejection (protected branch / pre-receive hook) is not something
// pull-then-push fixes, so it gets its own message rather than the remote-ahead one.
func TestFriendlyOpErrorExplainsHookRejection(t *testing.T) {
	raw := errors.New("git push failed (exit 1): remote: error: GH006: Protected branch update failed\n" +
		"! [remote rejected] main -> main (pre-receive hook declined)")
	got := friendlyOpError(raw)
	if !statusIsError(got) || strings.Contains(got, "\n") {
		t.Fatalf("want a single error: line, got %q", got)
	}
	low := strings.ToLower(got)
	if !strings.Contains(low, "protected") && !strings.Contains(low, "hook") {
		t.Fatalf("message should name the server-side rejection: %q", got)
	}
}

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
	return Model{width: 120, height: 30, focus: panelFiles, sel: map[panel]int{}}
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

// TestStatusErrorLeadsConflictNotice guards the stash-apply-conflict workflow:
// when an error coexists with the conflict notice, the error must lead so a
// narrow terminal truncates the notice, not the error.
func TestStatusErrorLeadsConflictNotice(t *testing.T) {
	m := statusRenderModel()
	m.width = 50 // narrow enough that the whole line can't fit
	m.status = model.WorkingTreeStatus{Files: []model.FileStatus{
		{Path: "timing3.log", Kind: model.KindUnmerged, Staged: 'U', Unstaged: 'U'},
	}}
	m.statusMsg = "error: git stash apply failed (exit 1)"
	line := ansi.Strip(lastLine(m.View()))
	if !strings.Contains(line, "error:") {
		t.Fatalf("error must lead the status line, got: %q", line)
	}
	if strings.HasPrefix(strings.TrimSpace(line), "⚠") {
		t.Errorf("conflict notice should not lead while an error is shown: %q", line)
	}
}
