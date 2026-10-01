package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/theme"
)

// headlessRepo is a two-commit repo on main plus a "feature" branch at the
// FIRST commit, so a.txt differs between them (a dirty a.txt then makes a
// switch ask a decision).
func headlessRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q", "-b", "main")
	git("config", "user.name", "t")
	git("config", "user.email", "t@t")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", ".")
	git("commit", "-qm", "first")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("commit", "-qam", "second")
	git("branch", "feature", "HEAD~1")
	return dir
}

func newHeadless(t *testing.T, dir string) *Headless {
	t.Helper()
	h, err := NewHeadless(domain.OpenTUI(dir), HeadlessOptions{Width: 120, Height: 30, StatePath: filepath.Join(t.TempDir(), "repos.toml")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h
}

// Start-up settles to a fixed point (quiet mode starts nothing that never
// ends) and draws the real screen.
func TestHeadlessStartsAndDraws(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	s := h.Screen()
	for _, want := range []string{"second", "first", "main"} {
		if !strings.Contains(s, want) {
			t.Fatalf("screen lacks %q:\n%s", want, s)
		}
	}
	if n := len(strings.Split(s, "\n")); n != 30 {
		t.Fatalf("screen has %d rows, want 30", n)
	}
	// A pause right after start-up also settles (the heartbeat's work is
	// gated in quiet mode).
	if err := h.FireTimers(); err != nil {
		t.Fatal(err)
	}
}

// Keys move the real model: the cursor walks the Commits list.
func TestHeadlessPressMovesTheModel(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	if err := h.Press("right"); err != nil { // focus Commits
		t.Fatal(err)
	}
	before := h.Screen()
	if err := h.Press("down"); err != nil {
		t.Fatal(err)
	}
	if h.Screen() == before {
		t.Fatal("down changed nothing on screen")
	}
	if err := h.Press("<f1>"); err == nil {
		t.Fatal("a diagnostic token must be rejected")
	}
}

// Timers are parked, not fired, until FireTimers: a sticky notice stays on
// screen after the step that raised it.
func TestHeadlessParksTimers(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	var c tea.Cmd
	h.m, c = h.m.stickyNotice("sticky-probe")
	if err := h.settle([]tea.Cmd{c}); err != nil { // the expiry timer parks
		t.Fatal(err)
	}
	if !strings.Contains(h.Screen(), "sticky-probe") {
		t.Fatal("the notice must be visible before any timer fires")
	}
	if len(h.timers) == 0 {
		t.Fatal("the notice's expiry must be parked")
	}
	if err := h.FireTimers(); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(h.Screen(), "sticky-probe") {
		t.Fatal("FireTimers must expire the notice")
	}
}

// A line wider than the terminal clips at the right edge — it never wraps
// into the next row.
func TestPaintClipsWideLines(t *testing.T) {
	t.Parallel()
	got := paintScreen(strings.Repeat("x", 15)+"\nsecond", 10, 3)
	want := "xxxxxxxxxx\nsecond\n\n"
	if got != want {
		t.Fatalf("paint = %q, want %q", got, want)
	}
}

// A scenario may not change the process-global theme or language: parallel
// scenarios share them. Not parallel: it changes the global theme.
func TestHeadlessRefusesGlobalChanges(t *testing.T) {
	prev := activeTheme()
	defer setTheme(prev)
	h := newHeadless(t, headlessRepo(t))
	other := theme.Dark
	if prev.Name == other.Name {
		other = theme.Light
	}
	setTheme(other)
	if err := h.Press("down"); err == nil || !strings.Contains(err.Error(), "theme") {
		t.Fatalf("Press after a theme change = %v, want a theme error", err)
	}
}

// An op with no decision finishes inside the step: the loop reads the op
// waiter while the op works.
func TestHeadlessWaitsForAnOpToFinish(t *testing.T) {
	t.Parallel()
	dir := headlessRepo(t)
	h := newHeadless(t, dir)
	for _, k := range headlessSwitchKeys {
		if err := h.Press(k); err != nil {
			t.Fatal(err)
		}
	}
	if h.m.running || h.opWaitHeld() {
		t.Fatalf("the switch must have finished inside the step, screen:\n%s", h.Screen())
	}
	out, _ := exec.Command("git", "-C", dir, "branch", "--show-current").Output()
	if strings.TrimSpace(string(out)) != "feature" {
		t.Fatalf("current branch = %q, want feature; screen:\n%s", out, h.Screen())
	}
}

// An op blocked on an engine decision settles with the modal on screen;
// the next key answers it and the op resumes (the spec review's blocker:
// a synchronous loop deadlocked on the op waiter here).
func TestHeadlessSettlesOnADecision(t *testing.T) {
	t.Parallel()
	h := newHeadless(t, headlessRepo(t))
	// d on "feature" runs DeleteBranch, which asks "delete-branch" through
	// the engine Decider (a real engine decision, not a frontend confirm).
	for _, k := range []string{"up", "d"} {
		if err := h.Press(k); err != nil {
			t.Fatal(err)
		}
	}
	if !h.m.awaitingDecision() {
		t.Fatalf("want an open decision, screen:\n%s", h.Screen())
	}
	if err := h.Press("esc"); err != nil { // esc = abort
		t.Fatal(err)
	}
	if h.m.awaitingDecision() || h.m.running {
		t.Fatalf("esc must answer the decision and let the op end, screen:\n%s", h.Screen())
	}
}

// headlessSwitchKeys switches to "feature" from the start screen: the
// Branches panel is focused on main, feature is the row above it, s
// switches (SmartSwitch) after a y/n confirm.
var headlessSwitchKeys = []string{"up", "s", "y"}
