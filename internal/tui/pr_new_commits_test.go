package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestPRUpdatedText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n, c int
		want string
	}{
		{7, 0, "PR #7 updated: new commits"},
		{7, 1, "PR #7 updated: 1 new commit"},
		{7, 2, "PR #7 updated: 2 new commits"},
	} {
		if got := prUpdatedText(tc.n, tc.c); got != tc.want {
			t.Errorf("prUpdatedText(%d, %d) = %q, want %q", tc.n, tc.c, got, tc.want)
		}
	}
}

// Item 7: a moved-head reopen counts the commits against the head that was
// on screen (the reland's from). Serial: prSendModel.
func TestAMovedReopenCountsTheNewCommits(t *testing.T) {
	m, dir, _ := prSendModel(t)
	m.prReland = &prReland{n: 7, from: gitOut(t, dir, "rev-parse", "main")}
	msg := m.openPRPreviewCmd(model.PullRequest{Number: 7, State: "open", Target: "main"})().(previewOpenMsg)
	if msg.newCommits != 1 {
		t.Fatalf("newCommits = %d, want 1", msg.newCommits)
	}
}
