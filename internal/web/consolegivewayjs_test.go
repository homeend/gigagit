package web

import (
	"strings"
	"testing"
)

// An agent that shows the user something — a review, a permanent overview,
// a temporary overview or file it focuses, a place on the panes — is asking
// for the screen: an agent console lying over the panes must give way. The
// review navigate (the end of /gg-review) used to open the review UNDER the
// console, which only yields to a plain pane change right after the user's
// own click: the user had to step out of the agent and close it to read it.
func TestAgentShowingSomethingClosesTheConsole(t *testing.T) {
	t.Parallel()
	giveWay := jsFunc(t, "live.js", "consoleGivesWay")
	if !strings.Contains(giveWay, `new CustomEvent("gg:panes", { detail: { force: true } })`) {
		t.Error("consoleGivesWay must send the FORCED pane event: the plain one closes a console only after the user's click")
	}
	for _, fn := range []string{"steerNavigateLand", "steerNavigateReview", "steerNavigateOverview", "steerFileFocus"} {
		if !strings.Contains(jsFunc(t, "live.js", fn), "consoleGivesWay();") {
			t.Errorf("%s opens what the agent shows without closing the agent console over it", fn)
		}
	}
}
