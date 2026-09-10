package steer

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestNotifyReloadPostsOnlyForALivePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()

	NotifyReload(dir, "notes")
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v with no live presence, want nothing", got)
	}

	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	got := Drain(dir)
	if len(got) != 1 {
		t.Fatalf("posted %+v, want one reload", got)
	}
	if got[0].Cmd != "reload" || len(got[0].Sources) != 1 || got[0].Sources[0] != "notes" {
		t.Errorf("posted %+v, want reload notes", got[0])
	}
	if got[0].Wait {
		t.Error("an automatic reload must be wait:false — nothing would read its reply")
	}
	if _, err := os.Stat(filepath.Join(dir, "reply-"+got[0].ID+".json")); !os.IsNotExist(err) {
		t.Errorf("a wait:false post must leave no reply file (err = %v)", err)
	}
}

func TestNotifyReloadSkipsAStalePresence(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := Touch(dir, TUIPresence, Presence{PID: 1, Worktree: "/w"}); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-LiveWindow - time.Second)
	if err := os.Chtimes(filepath.Join(dir, TUIPresence), old, old); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	if got := Drain(dir); len(got) != 0 {
		t.Fatalf("posted %+v for a stale presence, want nothing", got)
	}
}

func TestNotifyReloadEmptyDirIsANoOp(t *testing.T) {
	t.Parallel()
	NotifyReload("", "notes") // must not panic
}

func TestNotifyReloadReachesALiveWebPage(t *testing.T) {
	t.Parallel()
	seen := make(chan Command, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/session/steer" {
			http.NotFound(w, r)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json (the server's writeGuard requires it)", ct)
		}
		if o := r.Header.Get("Origin"); o != "" {
			t.Errorf("Origin = %q, want none from a non-browser client", o)
		}
		body, _ := io.ReadAll(r.Body)
		var c Command
		_ = json.Unmarshal(body, &c)
		seen <- c
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	dir := t.TempDir()
	if err := Touch(dir, WebPresence, Presence{PID: 2, Worktree: "/w", URL: srv.URL}); err != nil {
		t.Fatal(err)
	}
	NotifyReload(dir, "notes")
	select {
	case c := <-seen:
		if c.Cmd != "reload" || c.ID == "" {
			t.Errorf("web got %+v, want a reload with an id", c)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the live web page was never told")
	}
}

func TestPostHTTPMapsTheGateConflict(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
	}))
	defer srv.Close()
	err := PostHTTP(srv.URL, Command{ID: "1-1", Cmd: "reload"})
	if err == nil {
		t.Fatal("a 409 must be an error")
	}
	if err.Error() != "operation in flight" {
		t.Errorf("err = %q, want \"operation in flight\"", err)
	}
}
