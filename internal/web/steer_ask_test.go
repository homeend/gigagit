package web

import (
	"context"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func steerAskNoticeOf(t *testing.T, s *Server) *noticeWire {
	t.Helper()
	var got centreAns
	getJSON(t, serve(t, s), "/api/notifications", &got)
	for i := range got.Notices {
		if got.Notices[i].ID == noticeSteerSwitch {
			return &got.Notices[i]
		}
	}
	return nil
}

// A navigate for a worktree the page is not showing is not applied: it is
// parked behind a notice, and the agent hears so at once (the TUI's ask).
func TestSteerAskParksOtherWorktree(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	other := filepath.Join(t.TempDir(), "wt-b")
	code, rep := steerAsk(t, s, `{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"unstaged"},"line":{"side":"new","no":12},"worktree":"`+filepath.ToSlash(other)+`"}`)
	want := "gg web is showing worktree " + s.service().Root() + "; asked the user to switch to " + filepath.ToSlash(other)
	if code != http.StatusOK || rep.OK || rep.Error != want {
		t.Fatalf("code=%d rep=%+v, want the ask reply %q", code, rep, want)
	}
	n := steerAskNoticeOf(t, s)
	if n == nil {
		t.Fatal("no steer_switch notice")
	}
	if n.Title != "an agent wants to show a.txt:12 in wt-b" || len(n.Actions) != 2 ||
		n.Actions[0].Label != "Switch to wt-b and show" || n.Actions[0].Steer != "switch" || !n.Actions[0].Keep ||
		n.Actions[1].Label != "Ignore" || n.Actions[1].Steer != "ignore" || !n.Actions[1].Keep {
		t.Errorf("notice = %+v", n)
	}
}

// The page's own worktree, or a command that reads the same everywhere, is
// applied as before.
func TestSteerAskOnlyForAnotherWorktree(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	root := filepath.ToSlash(s.service().Root())
	if code := steerPost(t, s, `{"id":"1-1","cmd":"navigate","file":"a.txt","worktree":"`+root+`"}`, "application/json"); code != http.StatusAccepted {
		t.Errorf("own worktree = %d, want 202", code)
	}
	if code := steerPost(t, s, `{"id":"1-2","cmd":"focus","panel":"commits","worktree":"/elsewhere"}`, "application/json"); code != http.StatusAccepted {
		t.Errorf("focus = %d, want 202 (not worktree-bound)", code)
	}
	if steerAskNoticeOf(t, s) != nil {
		t.Error("an ask was parked")
	}
}

// Ignore drops the parked command; there is then nothing to answer.
func TestSteerAskIgnore(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	steerAsk(t, s, `{"id":"1-1","cmd":"navigate","file":"a.txt","worktree":"/elsewhere/wt"}`)
	ts := serve(t, s)
	if code := postJSON(t, ts, "/api/notifications/steer", `{"act":"ignore"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ignore = %d", code)
	}
	if steerAskNoticeOf(t, s) != nil {
		t.Error("the ask survived Ignore")
	}
	if code := postJSON(t, ts, "/api/notifications/steer", `{"act":"switch"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Errorf("a second answer = %d, want 409", code)
	}
	if code := postJSON(t, ts, "/api/notifications/steer", `{"act":"maybe"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a bad act = %d, want 400", code)
	}
}

// Switch hands the page the worktree to re-root to; the navigate is replayed
// (as the page's start-at) only once the re-root lands THERE.
func TestSteerAskSwitchReplaysAfterReroot(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	for _, landsThere := range []bool{true, false} {
		s := newSteerServer(t)
		target := newRepoDir(t, 1)
		steerAsk(t, s, `{"id":"1-1","cmd":"navigate","file":"a.txt","line":{"no":3},"worktree":"`+filepath.ToSlash(target)+`","from":"inbox-x","wait":true}`)
		var ans struct {
			Worktree string `json:"worktree"`
		}
		ts := serve(t, s)
		if code := postJSON(t, ts, "/api/notifications/steer", `{"act":"switch"}`, "application/json", "", &ans); code != http.StatusOK || ans.Worktree != filepath.ToSlash(target) {
			t.Fatalf("switch = %d %+v", code, ans)
		}
		if steerAskNoticeOf(t, s) != nil {
			t.Error("the ask survived Switch")
		}
		landed := target
		if !landsThere {
			landed = newRepoDir(t, 1)
		}
		if err := s.adoptService(ctx, domain.Open(landed)); err != nil {
			t.Fatal(err)
		}
		var at startAtBody
		getJSON(t, ts, "/api/session/start-at", &at)
		if !landsThere {
			if at.Steer != nil {
				t.Errorf("a re-root elsewhere replays %+v", at.Steer)
			}
			continue
		}
		if at.Steer == nil || at.Steer.Cmd != "navigate" || at.Steer.File != "a.txt" {
			t.Fatalf("start-at = %+v, want the parked navigate", at.Steer)
		}
	}
}

// The page answers the ask from the centre, re-roots on Switch, and re-reads
// the centre the moment an ask is parked.
func TestSteerAskWired(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "notifications.js")
	for _, want := range []string{`postJSON("/api/notifications/steer", { act: a.steer })`, "doReroot(ans.worktree)", `"gg:notices"`} {
		if !strings.Contains(js, want) {
			t.Errorf("notifications.js: missing %q", want)
		}
	}
	if !strings.Contains(readStatic(t, "live.js"), `msg.reason === "notices"`) {
		t.Error("live.js: the notices reason is not handled")
	}
}
