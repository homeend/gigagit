package cli

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/steer"
)

// `gg open --web`: the browser is the frontend the flag names. (Serial, like
// every test on the previewRepo fixture — it uses t.Setenv.)

// A live web page in the link's checkout is steered — and ONLY the page: a
// TUI live beside it is left alone, unlike plain `gg open`, which reaches
// both.
func TestOpenWebSteersOnlyTheLivePage(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	liveWebPresence(t, inbox, srv.URL)
	t.Cleanup(func() { steer.Discard(inbox) })
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb)
	if code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "web: sent") || !strings.Contains(out.String(), "steered: ") {
		t.Errorf("stdout = %q", out.String())
	}
	got := fake.commands()
	if len(got) != 1 || got[0].Target == nil || got[0].Target.State != "preview" || got[0].Line == nil || got[0].Line.No != 1 {
		t.Fatalf("posted to the page = %+v, want one preview navigate at line 1", got)
	}
	if left := steer.Drain(inbox); len(left) != 0 {
		t.Errorf("--web must not write the TUI inbox, posted %+v", left)
	}
	if calls != 0 {
		t.Errorf("a live page must be steered, not a new server started (launcher calls = %d)", calls)
	}
}

// Only a TUI live: --web still means a browser, so a new gg web is started
// there positioned on the link (the TUI is untouched).
func TestOpenWebAsksALiveTUIToServe(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	fake, srv := newSteerServer(t, http.StatusAccepted, "")
	// A stand-in TUI: answer the serve command with the page's URL and claim
	// web.json, as the real one does.
	stop := answerServe(t, inbox, srv.URL)
	defer stop()
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if !strings.Contains(out.String(), "web: sent") {
		t.Errorf("stdout = %q", out.String())
	}
	got := fake.commands()
	if len(got) != 1 || got[0].Cmd != "navigate" || got[0].File != "a.txt" || got[0].Line == nil || got[0].Line.No != 1 {
		t.Fatalf("posted to the page = %+v, want one navigate at line 1", got)
	}
	if calls != 0 {
		t.Error("a live TUI serves the page; no standalone server is started")
	}
}

// answerServe plays the TUI's side of "serve": drain the inbox until the
// command arrives, claim web.json with url, reply OK with the URL.
func answerServe(t *testing.T, inbox, url string) func() {
	t.Helper()
	done := make(chan struct{})
	go func() {
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case <-done:
				return
			default:
			}
			for _, c := range steer.Drain(inbox) {
				if c.Cmd == "serve" {
					liveWebPresence(t, inbox, url)
					_ = steer.PostReply(inbox, steer.Reply{ID: c.ID, OK: true, Detail: url})
					return
				}
			}
			time.Sleep(20 * time.Millisecond)
		}
	}()
	return func() { close(done) }
}

// A live TUI that never answers: no second server beside it, exit 1.
func TestOpenWebServeTimeoutExitsOne(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	livePresence(t, inbox)
	t.Cleanup(func() { steer.Discard(inbox) })
	old := steerReplyWaitForTest
	steerReplyWaitForTest = 200 * time.Millisecond
	t.Cleanup(func() { steerReplyWaitForTest = old })
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + "#1"}, &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "did not start its web page") || calls != 0 {
		t.Fatalf("exit=%d stderr=%q calls=%d", code, errb.String(), calls)
	}
}

// A bare repository link with --web just serves the page in that checkout —
// no start-at — or, when a page is already live there, prints its URL.
func TestOpenWebBareLinkServesThePage(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	var gotAt steer.Command
	calls := 0
	LaunchWeb = func(_ string, at steer.Command) int { calls++; gotAt = at; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	bare := "gg://" + filepath.ToSlash(dir)
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", bare}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if calls != 1 || gotAt.Cmd != "" {
		t.Errorf("calls = %d start-at = %+v, want one launch with no command", calls, gotAt)
	}
	// A page already live there: nothing to start, its URL is the answer.
	liveWebPresence(t, inbox, "http://127.0.0.1:4242")
	t.Cleanup(func() { steer.Discard(inbox) })
	out.Reset()
	if code := cmdOpen(svc, []string{"--web", bare}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if calls != 1 || !strings.Contains(out.String(), "http://127.0.0.1:4242") {
		t.Errorf("live page: calls = %d stdout = %q, want no launch and the URL", calls, out.String())
	}
}

// A presence whose server is GONE (its web.json is still within the liveness
// window) is a transport failure: a fresh server is started instead. A page
// that ANSWERS with an error is alive, and --web does not start a second one.
func TestOpenWebFallsBackToALaunchWhenThePageIsGone(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	inbox := steerDirFor(svc)
	_, srv := newSteerServer(t, http.StatusAccepted, "")
	dead := srv.URL
	srv.Close()
	liveWebPresence(t, inbox, dead)
	t.Cleanup(func() { steer.Discard(inbox) })
	calls := 0
	LaunchWeb = func(string, steer.Command) int { calls++; return 0 }
	t.Cleanup(func() { LaunchWeb = nil })
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb); code != 0 {
		t.Fatalf("exit = %d: %s", code, errb.String())
	}
	if calls != 1 || !strings.Contains(errb.String(), "starting one") {
		t.Errorf("calls = %d stderr = %q, want one launch after the dead presence", calls, errb.String())
	}
	// Alive but refusing (an operation in flight): exit 1, no launch. (Touch
	// only refreshes an existing presence's mtime, so the dead one goes first.)
	_, busy := newSteerServer(t, http.StatusConflict, "")
	steer.Remove(inbox, steer.WebPresence)
	liveWebPresence(t, inbox, busy.URL)
	errb.Reset()
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb); code != 1 {
		t.Errorf("busy page: exit = %d, want 1", code)
	}
	if calls != 1 || !strings.Contains(errb.String(), "operation in flight") {
		t.Errorf("busy page: calls = %d stderr = %q, want no launch and the page's answer", calls, errb.String())
	}
}

// Without the launcher seam (every test's default) --web says so.
func TestOpenWebWithoutALauncherExitsOne(t *testing.T) {
	dir := previewRepo(t)
	svc := openCLIService(t, dir)
	LaunchWeb = nil
	var out, errb strings.Builder
	if code := cmdOpen(svc, []string{"--web", previewLinkFor(dir, "a.txt") + ":1"}, &out, &errb); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
	if !strings.Contains(errb.String(), "no live gg web page in") || !strings.Contains(errb.String(), "launcher is unavailable") {
		t.Errorf("stderr = %q", errb.String())
	}
}
