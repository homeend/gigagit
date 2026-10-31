package cli

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/steer"
)

// blockedTUIPresence writes the tui.json of a terminal the page took over:
// Inactive, carrying the page's URL.
func blockedTUIPresence(t *testing.T, dir, url string) {
	t.Helper()
	if err := steer.Touch(dir, steer.TUIPresence, steer.Presence{PID: 42, Worktree: dir, Inactive: true, URL: url}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, dir, steer.TUIPresence)
}

func blockedWebPresence(t *testing.T, dir, url string) {
	t.Helper()
	if err := steer.Touch(dir, steer.WebPresence, steer.Presence{PID: 7, Worktree: dir, Inactive: true, URL: url}); err != nil {
		t.Fatal(err)
	}
	keepPresenceAlive(t, dir, steer.WebPresence)
}

func TestRouteBlockedTUIReachesPage(t *testing.T) {
	inbox := t.TempDir()
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedTUIPresence(t, inbox, srv.URL)
	var out, errb strings.Builder
	code := sendSteer(sessDir{inbox: inbox}, steer.Command{Cmd: "focus"}, false, &out, &errb)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(fake.commands()) != 1 {
		t.Fatalf("posted = %+v", fake.commands())
	}
	if left := steer.Drain(inbox); len(left) != 0 {
		t.Fatalf("a blocked terminal's inbox got %+v", left)
	}
}

func TestGGInboxAfterSwitchReachesPage(t *testing.T) {
	inbox, here := t.TempDir(), t.TempDir()
	_, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedTUIPresence(t, inbox, srv.URL)
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return inbox
		}
		return ""
	}
	t.Cleanup(func() { sessionGetenv = old })
	for _, to := range []string{"", "web"} {
		if got := preferredInboxFor(here, to); got != inbox {
			t.Errorf("to=%q: inbox = %q, want the agent's own", to, got)
		}
	}
}

func TestRouteBlockedPageReachesTUI(t *testing.T) {
	inbox := t.TempDir()
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedWebPresence(t, inbox, srv.URL)
	livePresence(t, inbox)
	var out, errb strings.Builder
	if code := sendSteer(sessDir{inbox: inbox}, steer.Command{Cmd: "focus"}, true, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if len(fake.commands()) != 0 {
		t.Fatalf("a blocked page got %+v", fake.commands())
	}
	if left := steer.Drain(inbox); len(left) != 1 {
		t.Fatalf("the terminal's inbox has %+v", left)
	}
}

func TestToBlockedSideRefused(t *testing.T) {
	inbox := t.TempDir()
	_, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedTUIPresence(t, inbox, srv.URL)
	var out, errb strings.Builder
	if code := sendSteer(sessDir{inbox: inbox, to: "tui"}, steer.Command{Cmd: "focus"}, true, &out, &errb); code != 1 ||
		!strings.Contains(errb.String(), "the web UI is active in this gg") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}

	inbox = t.TempDir()
	blockedWebPresence(t, inbox, srv.URL)
	livePresence(t, inbox)
	errb.Reset()
	if code := sendSteer(sessDir{inbox: inbox, to: "web"}, steer.Command{Cmd: "focus"}, true, &out, &errb); code != 1 ||
		!strings.Contains(errb.String(), "the terminal UI is active in this gg") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestStatusReportsBlocked(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	_, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedTUIPresence(t, inbox, srv.URL)
	var out, errb strings.Builder
	if code := sessionStatus(inbox, svc, []string{"--json"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatal(err)
	}
	if got["tui_blocked"] != true {
		t.Fatalf("status = %s", out.String())
	}
	out.Reset()
	sessionStatus(inbox, svc, nil, &out, &errb)
	if !strings.Contains(out.String(), "tui: blocked (the web UI is active)") {
		t.Fatalf("status = %s", out.String())
	}
}

func TestOpenWebFlips(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	t.Cleanup(func() { steer.Discard(inbox) })
	livePresence(t, inbox)
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedWebPresence(t, inbox, srv.URL)
	sawServe := make(chan bool, 1)
	stop := answerServeRecording(t, inbox, srv.URL, sawServe)
	defer stop()
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	if !<-sawServe {
		t.Fatal("a blocked page must be activated through serve")
	}
	if len(fake.commands()) != 1 {
		t.Fatalf("posted = %+v", fake.commands())
	}
}

func TestOpenWebRefused(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	t.Cleanup(func() { steer.Discard(inbox) })
	livePresence(t, inbox)
	go func() {
		for i := 0; i < 250; i++ {
			for _, c := range steer.Drain(inbox) {
				if c.Cmd == "serve" {
					_ = steer.PostReply(inbox, steer.Reply{ID: c.ID, Error: "an operation is running"})
					return
				}
			}
			sleepMs(20)
		}
	}()
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 1 ||
		!strings.Contains(errb.String(), "an operation is running") {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
}

func TestOpenWithoutWebReachesPage(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	t.Cleanup(func() { steer.Discard(inbox) })
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	blockedTUIPresence(t, inbox, srv.URL)
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	got := fake.commands()
	if len(got) != 1 || got[0].Cmd != "navigate" {
		t.Fatalf("posted = %+v", got)
	}
	if left := steer.Drain(inbox); len(left) != 0 {
		t.Fatalf("nothing flips: no serve in the inbox, got %+v", left)
	}
}
