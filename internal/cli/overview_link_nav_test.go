package cli

import (
	"bytes"
	"context"
	"net/http"
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

// A session overview link goes to gg web like any other (spec
// 2026-10-28-web-overview-links §3); only the IDE is refused.
func TestSessionNavigateSessionOverviewToWebNotIDE(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	var out, errb bytes.Buffer
	code := runSession(wtInbox(t, main), domain.Open(main), []string{"navigate", "--to", "ide", link}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "a session overview link opens in the gg terminal or gg web, not the IDE") {
		t.Errorf("--to ide = %d %q", code, errb.String())
	}
	inbox := wtInbox(t, wt2)
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	liveWebPresence(t, inbox, srv.URL)
	t.Cleanup(func() { steer.Discard(inbox) })
	out.Reset()
	errb.Reset()
	if code := runSession(wtInbox(t, main), domain.Open(main), []string{"navigate", "--to", "web", "--no-wait", link}, &out, &errb); code != 0 {
		t.Fatalf("--to web = %d %q", code, errb.String())
	}
	got := fake.commands()
	if len(got) != 1 || got[0].Cmd != "navigate" || got[0].HintKind != model.OverviewHintKind || got[0].HintID != id || got[0].File != "" || got[0].Commit != "" {
		t.Fatalf("posted to the page = %+v", got)
	}
}

// With only the IDE live, a session overview link is refused as --to ide
// is: the IDE would be handed an overview it cannot show (review finding).
func TestSessionNavigateSessionOverviewIDEAloneRefused(t *testing.T) {
	t.Parallel()
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	inbox := wtInbox(t, wt2)
	liveIDEPresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	var out, errb bytes.Buffer
	code := runSession(wtInbox(t, main), domain.Open(main), []string{"navigate", "--no-wait", link}, &out, &errb)
	if code != 2 || !strings.Contains(errb.String(), "a session overview link opens in the gg terminal or gg web, not the IDE") {
		t.Fatalf("exit = %d out=%q err=%q", code, out.String(), errb.String())
	}
	if left := steer.Drain(steer.IDEDir(inbox)); len(left) != 0 {
		t.Errorf("posted to the IDE: %+v", left)
	}
}

// gg open --web with no live page starts gg web in the link's worktree, at
// the overview (a hint-only start-at). Serial: LaunchWeb is global.
func TestOpenWebSessionOverviewStartsGGWebThere(t *testing.T) {
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	var gotDir string
	var gotAt steer.Command
	LaunchWeb = func(dir string, at steer.Command) int { gotDir, gotAt = dir, at; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	if code, _, errb := runCLI(t, main, "open", "--web", link); code != 0 {
		t.Fatalf("open --web = %d %q", code, errb)
	}
	if !domain.SamePath(gotDir, wt2) || gotAt.Cmd != "navigate" || gotAt.HintKind != model.OverviewHintKind || gotAt.HintID != id || gotAt.File != "" || gotAt.Commit != "" {
		t.Fatalf("launched in %q at %+v", gotDir, gotAt)
	}
}

// Plain gg open reaches a lone live web page the way it reaches it for
// every link — no TUI is launched beside it. Serial: LaunchTUI is global.
func TestOpenSessionOverviewReachesALoneWebPage(t *testing.T) {
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	inbox := wtInbox(t, wt2)
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	liveWebPresence(t, inbox, srv.URL)
	t.Cleanup(func() { steer.Discard(inbox) })
	launched := 0
	LaunchTUI = func(string, model.Link) int { launched++; return 0 }
	t.Cleanup(func() { LaunchTUI = nil })
	if code, _, errb := runCLI(t, main, "open", "--no-wait", link); code != 0 {
		t.Fatalf("open = %d %q", code, errb)
	}
	if got := fake.commands(); len(got) != 1 || got[0].HintID != id || launched != 0 {
		t.Fatalf("posted %+v, TUI launched %d times", got, launched)
	}
}

// A lone page refusing the overview (gone) is the agent's error, not
// "web: sent" (spec 2026-10-29 §4).
func TestSessionNavigateWebRefusalIsPrinted(t *testing.T) {
	main, wt2, id, _ := sessOvWorktrees(t)
	link, _ := domain.SessionOverviewLink(wt2, id)
	inbox := wtInbox(t, wt2)
	_, srv := newSteerServer(t, http.StatusOK, `{"id":"1-1","ok":false,"error":"overview `+id+` no longer exists"}`)
	liveWebPresence(t, inbox, srv.URL)
	t.Cleanup(func() { steer.Discard(inbox) })
	code, out, errb := runCLI(t, main, "session", "navigate", "--no-wait", link)
	if code != 1 || strings.Contains(out, "web: sent") || !strings.Contains(errb, "web: overview "+id+" no longer exists") {
		t.Fatalf("exit = %d out=%q err=%q", code, out, errb)
	}
}
