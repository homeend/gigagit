package tui

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Quiet mode hands the loop a descriptor instead of sleeping: time never
// passes in a headless test, a step says when "the user paused".
func TestTickInQuietModeIsADescriptor(t *testing.T) {
	t.Parallel()
	var m Model
	m.quiet = true
	msg := m.tick(5*time.Second, func(time.Time) tea.Msg { return "fired" })()
	tm, ok := msg.(timerMsg)
	if !ok || tm.due != 5*time.Second || tm.fire(time.Time{}) != "fired" {
		t.Fatalf("quiet tick = %#v, want a timerMsg carrying due and fire", msg)
	}
}

// Every timer goes through m.tick, and no TUI command sleeps on the wall
// clock outside the allow-list — or a headless settle would wait on time.
func TestNoRawTimersOutsideTick(t *testing.T) {
	t.Parallel()
	raw := regexp.MustCompile(`tea\.(Tick|Every)\(|time\.(After|Sleep)\(`)
	allowed := map[string]string{
		"tick.go":       "the one tea.Tick call",
		"console.go":    "waitSessionCmd's sleep: agent consoles never open in quiet mode",
		"repo_popup.go": "probeReposCmd's 1 s deadline only bounds wedged fs probes; it returns as soon as every probe answers",
	}
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || allowed[f] != "" {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			if raw.MatchString(line) && !strings.HasPrefix(strings.TrimSpace(line), "//") {
				t.Errorf("%s:%d: raw timer %q — use m.tick (or allow-list it with a reason)", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
