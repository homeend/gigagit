package tui

import (
	"strings"
	"testing"
)

func helpPlainText() string {
	var b strings.Builder
	for _, l := range helpContent() {
		b.WriteString(l.text + "\n")
	}
	return b.String()
}

func TestHelpDescribesSendingFromThePRView(t *testing.T) {
	t.Parallel()
	text := helpPlainText()
	for _, want := range []string{"Send as GitHub comment", "Reply & send", "Send to GitHub…", "Verdict…", "Review and send…", "Finish sending", "updated"} {
		if !strings.Contains(text, want) {
			t.Errorf("help lacks %q", want)
		}
	}
	if strings.Contains(text, "gg never posts to the forge") {
		t.Error("help still says gg never posts")
	}
	for _, gone := range []string{"Send review…", "Send my draft review…", "Send this AI review…"} {
		if strings.Contains(text, gone) {
			t.Errorf("help still mentions %q (R7, R9)", gone)
		}
	}
}
