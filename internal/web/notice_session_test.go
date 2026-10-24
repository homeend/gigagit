package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

type centreAns struct {
	Notices []noticeWire `json:"notices"`
	Unread  bool         `json:"unread"`
}

// centreServer is a server whose centre carries the test's own notices (the
// seam), each with the TUI's Not now / Never rows.
func centreServer(t *testing.T, ids *atomic.Value) *Server {
	t.Helper()
	srv := New(domain.Open(newRepoDir(t, 1)))
	srv.noticeExtra = []NoticeBuilder{func(context.Context, *Server, *domain.Service, model.RepoHealth, map[string]bool) []noticeWire {
		var out []noticeWire
		for _, id := range ids.Load().([]string) {
			out = append(out, noticeWire{ID: id, Title: id, Actions: notNowNever(id)})
		}
		return out
	}}
	return srv
}

// "Not now" (and every other action) hides a notice for the session — a
// re-read must not resurrect it; nothing is persisted.
func TestNoticeSessionDismissHides(t *testing.T) {
	t.Parallel()
	var ids atomic.Value
	ids.Store([]string{"t_one", "t_two"})
	ts := serve(t, centreServer(t, &ids))
	var got centreAns
	getJSON(t, ts, "/api/notifications", &got)
	if len(got.Notices) != 2 {
		t.Fatalf("notices = %+v, want 2", got.Notices)
	}
	if code := postJSON(t, ts, "/api/notifications/session", `{"id":"t_one"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("session dismiss = %d", code)
	}
	got = centreAns{}
	getJSON(t, ts, "/api/notifications", &got)
	if len(got.Notices) != 1 || got.Notices[0].ID != "t_two" {
		t.Fatalf("after Not now: %+v, want only t_two", got.Notices)
	}
	if code := postJSON(t, ts, "/api/notifications/session", `{"id":"../nope"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a malformed id = %d, want 400", code)
	}
}

// Unread is the TUI's armBlinkForNew: an id the previous read did not carry
// marks the centre unread until it is opened (shared by every tab).
func TestNoticeUnreadOnNewID(t *testing.T) {
	t.Parallel()
	var ids atomic.Value
	ids.Store([]string{"t_one"})
	ts := serve(t, centreServer(t, &ids))
	read := func() bool {
		var got centreAns
		getJSON(t, ts, "/api/notifications", &got)
		return got.Unread
	}
	if !read() {
		t.Fatal("a first notice is not unread")
	}
	if !read() {
		t.Fatal("unread cleared without the centre being opened")
	}
	if code := postJSON(t, ts, "/api/notifications/read", `{}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("read = %d", code)
	}
	if read() {
		t.Fatal("still unread after opening")
	}
	ids.Store([]string{"t_one", "t_two"})
	if !read() {
		t.Fatal("a new id did not mark the centre unread")
	}
}

// Every notice offers the TUI's two dismissal rows, in its words.
func TestNoticeRowsTUILabels(t *testing.T) {
	t.Parallel()
	n := narrowRefspecNotice(context.Background(), nil, nil, model.RepoHealth{UnmappedBranches: []string{"a", "b"}}, nil)
	if len(n) != 1 {
		t.Fatalf("refspec notice = %+v", n)
	}
	var notNow, never, pushOffer bool
	for _, a := range n[0].Actions {
		switch {
		case a.Session && a.Label == "Not now (ask again next load)":
			notNow = true
		case a.Dismiss == noticeNarrowRefspec && a.Label == "Never for this repo":
			never = true
		case a.Dismiss == noticePushMapOffer:
			pushOffer = a.Keep // a suppression of the push question, not of the finding
		case a.Branch != "":
			if !a.Keep {
				t.Errorf("per-branch fix %q removes the whole notice", a.Label)
			}
		}
	}
	if !notNow || !never || !pushOffer {
		t.Errorf("refspec rows: not now %v, never %v, push offer kept %v: %+v", notNow, never, pushOffer, n[0].Actions)
	}
}

// An op that fails on a git lock brings the lock notice back even after a
// Not now (maybeStaleLockNotice), and says so on the done event.
func TestLockErrorClearsSessionDismiss(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	key, err := srv.service().GitCommonDir(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	srv.notices.dismiss(key, noticeStaleLock)
	lock := filepath.Join(dir, ".git", "refs", "heads", "locked.lock")
	if err := os.WriteFile(lock, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"create-branch","name":"locked"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] == true {
		t.Fatalf("create-branch over a ref lock succeeded: %v", done)
	}
	if done["lock_error"] != true {
		t.Errorf("done = %v, want lock_error", done)
	}
	if srv.notices.isDismissed(key, noticeStaleLock) {
		t.Error("the lock notice is still dismissed for the session after a lock failure")
	}
}

// The page acts the TUI's lifecycle: every action but a kept one hides the
// notice for the session first, opening the centre marks it read, the bell
// blinks while unread, and a lock failure points at the centre.
func TestNoticeLifecycleWired(t *testing.T) {
	t.Parallel()
	js := readStatic(t, "notifications.js")
	for _, want := range []string{
		`postJSON("/api/notifications/session"`, "if (!a.keep)",
		`postJSON("/api/notifications/read"`,
		`chip.classList.toggle("unread"`,
		"#notice-chip.unread", "@keyframes",
		`a.open === "exttools"`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("notifications.js: missing %q", want)
		}
	}
	if strings.Contains(js, `data-dismiss="1"`) {
		t.Error("notifications.js: the old catch-all dismiss button is back (the notices carry the TUI's rows)")
	}
	if !strings.Contains(readStatic(t, "ops.js"), "press ! to remove the lock") {
		t.Error("ops.js: a lock failure does not point at the centre")
	}
}

// A feature id carries hyphens (note-edits): its Not now must not be refused.
func TestNoticeSessionHyphenatedID(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	if code := postJSON(t, ts, "/api/notifications/session", `{"id":"`+noticeNoteEdits+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("session dismiss of %s = %d, want 200", noticeNoteEdits, code)
	}
}

// A repo switch starts the centre's session afresh, as the TUI's reRoot does:
// A→B→A brings back A's notices (and their blink).
func TestNoticeSessionResetOnReroot(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	key, _ := s.service().GitCommonDir(context.Background())
	s.notices.dismiss(key, noticeStaleLock)
	s.notices.observe(key, []string{noticeStaleLock})
	s.notices.read(key)
	back := s.service().Root()
	if err := s.adoptService(context.Background(), domain.Open(newRepoDir(t, 1))); err != nil {
		t.Fatal(err)
	}
	if err := s.adoptService(context.Background(), domain.Open(back)); err != nil {
		t.Fatal(err)
	}
	if s.notices.isDismissed(key, noticeStaleLock) {
		t.Error("a Not now survived the switch away and back")
	}
	if !s.notices.observe(key, []string{noticeStaleLock}) {
		t.Error("a notice is not unread again after the switch")
	}
}

// Marking read and a background tool-status read both make every tab re-read
// the centre; the load runs under the server's background tracking.
func TestNoticeRefreshFanOuts(t *testing.T) {
	t.Parallel()
	src := readStatic(t, "../notice_session.go") + readStatic(t, "../notice_builders.go")
	if strings.Count(src, `Reason: "notices"`) < 2 {
		t.Error("read / tool-status load do not tell the tabs to re-read the centre")
	}
	if strings.Contains(readStatic(t, "../notice_builders.go"), "\t\tgo func() {") {
		t.Error("the tool-status load is a bare goroutine (use goBackground)")
	}
}

// Small page wiring: a clicked key-line button gives focus back (Enter must
// not close the overlay), and Enable auto-refresh only re-reads the banner.
func TestF3ReviewWiring(t *testing.T) {
	t.Parallel()
	det := readStatic(t, "prdetails.js")
	if !strings.Contains(det, "b.blur();") {
		t.Error("prdetails.js: a clicked key-line button keeps focus")
	}
	if !strings.Contains(readStatic(t, "ops.js"), `kind === "commit-graph-auto"`) {
		t.Error("ops.js: commit-graph-auto does not refresh the big-repo banner")
	}
	js := readStatic(t, "notifications.js")
	if !strings.Contains(js, "another operation is running — switch when it ends") {
		t.Error("notifications.js: a Switch during an op is not refused out loud")
	}
}
