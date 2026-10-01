package tui

import (
	"fmt"
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

// rawTimerAllowed lists each raw timer LINE (file → trimmed line → reason)
// that may sleep on the wall clock. Keyed by line, not file, so a second
// timer added to an allow-listed file is still caught.
var rawTimerAllowed = map[string]map[string]string{
	"tick.go":       {"return tea.Tick(d, fn)": "the one tea.Tick call"},
	"console.go":    {"time.Sleep(consoleRepaint)": "waitSessionCmd's sleep: agent consoles never open in quiet mode"},
	"headless.go":   {"case <-time.After(d):": "the driver's own settle guard: it bounds a blocking command, it is never a UI timer"},
	"repo_popup.go": {"deadline := time.After(time.Second)": "probeReposCmd's 1 s deadline only bounds wedged fs probes; it returns as soon as every probe answers"},
}

var rawTimer = regexp.MustCompile(`tea\.(Tick|Every)\(|time\.(After|Sleep)\(`)

// rawTimers reports each raw timer line of src not on the allow-list.
func rawTimers(file, src string) []string {
	var out []string
	for i, line := range strings.Split(src, "\n") {
		l := strings.TrimSpace(line)
		if !rawTimer.MatchString(l) || strings.HasPrefix(l, "//") || rawTimerAllowed[file][l] != "" {
			continue
		}
		out = append(out, fmt.Sprintf("%s:%d: raw timer %q — use m.tick (or allow-list the line with a reason)", file, i+1, l))
	}
	return out
}

// Every timer goes through m.tick, and no TUI command sleeps on the wall
// clock outside the allow-list — or a headless settle would wait on time.
func TestNoRawTimersOutsideTick(t *testing.T) {
	t.Parallel()
	files, _ := filepath.Glob("*.go")
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range rawTimers(f, string(b)) {
			t.Error(v)
		}
	}
}

// A second timer in an allow-listed file is not covered by the first's entry.
func TestRawTimerAllowListIsPerLine(t *testing.T) {
	t.Parallel()
	src := "\ttime.Sleep(consoleRepaint)\n\ttime.Sleep(time.Second)\n"
	if got := rawTimers("console.go", src); len(got) != 1 || !strings.Contains(got[0], "console.go:2") {
		t.Fatalf("rawTimers = %q, want only line 2", got)
	}
}
