package web

import (
	"strings"
	"testing"
)

// A navigate naming a file the target does not carry (or a line its diff
// does not have) must SAY so on the page. It used to return in silence and
// leave an empty diff pane — the swallowed failure ruling S7 names as this
// feature's signature bug. The page's steer is fire-and-forget (the CLI has
// printed "web: sent" long before), so the notice is the only report; the
// wording is the TUI's steerFail reasons.
func TestNavigateMissSaysSo(t *testing.T) {
	t.Parallel()
	land := jsFunc(t, "live.js", "steerNavigateLand")
	if strings.Contains(land, "if (i < 0) return;") {
		t.Error("steerNavigateLand still drops a missing file in silence")
	}
	if strings.Contains(land, "if (!tr) return;") {
		t.Error("steerNavigateLand still drops a missing line in silence")
	}
	live := readStatic(t, "live.js")
	for _, want := range []string{
		`navMiss(s.file + " is not in " + where)`,
		`"commit " + s.commit.slice(0, 8)`,
		`"the working-tree diff"`,
		`"preview " + s.target + "..." + s.source`,
		`"line " + line + " is not in " + file + "'s diff"`,
	} {
		if !strings.Contains(live, want) {
			t.Errorf("live.js is missing the notice %s", want)
		}
	}
}
