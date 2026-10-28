package cli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/steer"
)

// wtInbox is checkout's session inbox (XDG state is pinned by TestMain).
func wtInbox(t *testing.T, checkout string) string {
	t.Helper()
	cd, err := domain.Open(checkout).GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return config.SessionSteerDir(cd, checkout)
}

func TestSessionNavigateSessionOverviewPostsToTUI(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	inbox := wtInbox(t, wt2)
	livePresence(t, inbox)
	seen := answer(t, inbox, func(c steer.Command) steer.Reply { return steer.Reply{ID: c.ID, OK: true} })
	var out, errb bytes.Buffer
	if code := runSession(wtInbox(t, main), domain.Open(main), []string{"navigate", "--no-wait", link}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d (stderr %q)", code, errb.String())
	}
	c, ok := <-seen
	if !ok || c.Cmd != "navigate" || c.HintKind != model.OverviewHintKind || c.HintID != id ||
		!domain.SamePath(c.Worktree, wt2) || c.Commit != "" || c.File != "" || c.Target != nil {
		t.Fatalf("posted %+v ok=%v", c, ok)
	}
}

func TestSessionNavigateSessionOverviewRefusesWebIDE(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	for _, to := range []string{"web", "ide"} {
		var out, errb bytes.Buffer
		code := runSession(wtInbox(t, main), domain.Open(main), []string{"navigate", "--to", to, link}, &out, &errb)
		if code != 2 || !strings.Contains(errb.String(), "a session overview link opens only in the gg terminal for now") {
			t.Errorf("--to %s = %d %q", to, code, errb.String())
		}
	}
}

func TestOpenSessionOverviewWebRefused(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	code, _, errb := runCLI(t, main, "open", "--web", link)
	if code != 2 || !strings.Contains(errb, "a session overview link opens only in the gg terminal for now") {
		t.Fatalf("open --web = %d %q", code, errb)
	}
}
