package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// blockedServer is a hosted server whose terminal is the active side.
func blockedServer(t *testing.T) *Server {
	t.Helper()
	srv := New(domain.Open(newRepoDir(t, 1)))
	t.Cleanup(srv.Close)
	srv.hosted = true
	srv.SetUIActive(false)
	return srv
}

func uiRequest(srv *Server, method, path, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Host = "127.0.0.1:1"
	if body != "" {
		r.Header.Set("Content-Type", "application/json")
	}
	if path == "/api/events" {
		ctx, cancel := context.WithCancel(r.Context())
		cancel() // the stream ends at once; only the gate's answer matters
		r = r.WithContext(ctx)
	}
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, r)
	return w
}

func refusedAsInactive(w *httptest.ResponseRecorder) bool {
	if w.Code != http.StatusConflict {
		return false
	}
	var out struct {
		Error      string `json:"error"`
		UIInactive bool   `json:"ui_inactive"`
	}
	return json.Unmarshal(w.Body.Bytes(), &out) == nil && out.UIInactive
}

func TestUIGateAllowlist(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	cases := []struct {
		method, path, body string
		refused            bool
	}{
		{"GET", "/", "", false},
		{"GET", "/static/app.js", "", false},
		{"GET", "/api/ping", "", false},
		{"GET", "/api/events", "", false},
		{"GET", "/api/ui-active", "", false},
		{"POST", "/api/ui-active", `{"side":"web"}`, false},
		{"GET", "/api/quit", "", false},
		{"GET", "/api/repo", "", true},
		{"GET", "/api/status", "", true},
		{"POST", "/api/op", `{}`, true},
		{"POST", "/api/steer", `{}`, true},
		{"PUT", "/api/uistate", `{}`, true},
	}
	for _, c := range cases {
		w := uiRequest(srv, c.method, c.path, c.body)
		if got := refusedAsInactive(w); got != c.refused {
			t.Errorf("%s %s: refused = %v, want %v (status %d, %s)", c.method, c.path, got, c.refused, w.Code, w.Body.String())
		}
	}
}

func TestUIGateNamesTheActiveSide(t *testing.T) {
	t.Parallel()
	w := uiRequest(blockedServer(t), "GET", "/api/status", "")
	if !strings.Contains(w.Body.String(), "the terminal UI is active in this gg") {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestUIGateOpensWhenActive(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.SetUIActive(true)
	if w := uiRequest(srv, "GET", "/api/repo", ""); w.Code != http.StatusOK {
		t.Fatalf("status %d: %s", w.Code, w.Body.String())
	}
}

func TestUIGateStandaloneNeverBlocks(t *testing.T) {
	t.Parallel()
	srv := New(domain.Open(newRepoDir(t, 1)))
	t.Cleanup(srv.Close)
	srv.SetUIActive(false) // no terminal to be active: the flag must not bind
	w := uiRequest(srv, "GET", "/api/ui-active", "")
	var out struct {
		Active, Hosted bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Active || out.Hosted {
		t.Fatalf("ui-active = %+v", out)
	}
	if w := uiRequest(srv, "GET", "/api/repo", ""); w.Code != http.StatusOK {
		t.Fatalf("repo status %d", w.Code)
	}
}

func TestUIActiveReadSaysBlocked(t *testing.T) {
	t.Parallel()
	w := uiRequest(blockedServer(t), "GET", "/api/ui-active", "")
	var out struct {
		Active, Hosted bool
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Active || !out.Hosted {
		t.Fatalf("ui-active = %+v", out)
	}
}

func TestUIActiveFlipFansOut(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.startLive(context.Background())
	ch, cancel := srv.liveHubRef().subscribe()
	defer cancel()
	srv.SetUIActive(true)
	select {
	case msg := <-ch:
		if msg.Reason != "ui_active" || msg.Active == nil || !*msg.Active {
			t.Fatalf("msg = %+v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no flip event")
	}
}

func TestUIActivatePost(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	var side string
	srv.SetActivator(func(_ context.Context, s string) error {
		side = s
		return errors.New("an operation is running")
	})
	w := uiRequest(srv, "POST", "/api/ui-active", `{"side":"web"}`)
	if side != "web" || w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "an operation is running") {
		t.Fatalf("side %q status %d body %s", side, w.Code, w.Body.String())
	}
	srv.SetActivator(func(_ context.Context, s string) error { side = s; return nil })
	if w := uiRequest(srv, "POST", "/api/ui-active", `{"side":"web"}`); w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}
	if w := uiRequest(srv, "POST", "/api/ui-active", `{"side":"x"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad side status %d", w.Code)
	}
	srv.SetActivator(nil)
	if w := uiRequest(srv, "POST", "/api/ui-active", `{"side":"web"}`); w.Code != http.StatusConflict {
		t.Fatalf("no activator status %d", w.Code)
	}
}

func TestUIActivateToTUIRefusedWhileOpRuns(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.SetUIActive(true)
	srv.SetActivator(func(context.Context, string) error {
		t.Error("the terminal must not be asked while an op runs here")
		return nil
	})
	srv.opMu.Lock()
	srv.cur = &opRun{} // a live op (not done)
	srv.opMu.Unlock()
	if got := srv.uiBusy(); got != "an operation is running" {
		t.Fatalf("busy = %q", got)
	}
	if w := uiRequest(srv, "POST", "/api/ui-active", `{"side":"tui"}`); w.Code != http.StatusConflict {
		t.Fatalf("status %d", w.Code)
	}
}

func TestUIActiveHubSuspended(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.startLive(context.Background())
	h := srv.liveHubRef()
	ch, cancel := h.subscribe()
	defer cancel()
	h.emit(liveMsg{Changed: []string{"status"}, Reason: "watch"})
	select {
	case msg := <-ch:
		t.Fatalf("a blocked page's hub emitted %+v", msg)
	case <-time.After(50 * time.Millisecond):
	}
	srv.SetUIActive(true)
	<-ch // the flip
	h.emit(liveMsg{Changed: []string{"status"}, Reason: "watch"})
	select {
	case msg := <-ch:
		if msg.Reason != "watch" {
			t.Fatalf("msg = %+v", msg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the hub did not resume")
	}
}

func TestUIActiveClients(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.startLive(context.Background())
	if n := srv.uiClients(); n != 0 {
		t.Fatalf("clients = %d", n)
	}
	_, cancel := srv.liveHubRef().subscribe()
	if n := srv.uiClients(); n != 1 {
		t.Fatalf("clients = %d", n)
	}
	cancel()
	if n := srv.uiClients(); n != 0 {
		t.Fatalf("clients after cancel = %d", n)
	}
}

func TestUIActivePresence(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	h.SetActive(false)
	if _, err := h.Start(context.Background(), "127.0.0.1:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	dir, _ := resolveSteerDir(context.Background(), svc)
	p, live := steer.Live(dir, steer.WebPresence)
	if !live || !p.Inactive {
		t.Fatalf("blocked presence = %+v live %v", p, live)
	}
	h.SetActive(true)
	p, live = steer.Live(dir, steer.WebPresence)
	if !live || p.Inactive {
		t.Fatalf("active presence = %+v live %v", p, live)
	}
}

// `gg open --web` with no tab: the page is opened, and the command is posted
// before it connects. The new tab must still land on the link.
func TestSteerWithNoTabBecomesStartAt(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	if err := steer.PostHTTP(url, steer.Command{ID: "n-1", Cmd: "navigate", File: "a.txt"}); err != nil {
		t.Fatal(err)
	}
	if h.srv.startAt == nil {
		t.Fatal("the command was lost: no tab to hear it, no start-at for the next one")
	}
}

// The agent window a tab shows travels with a flip: the tab reports it, the
// terminal reads it, and a blocked tab can't overwrite what the terminal left.
func TestUIConsoleCarried(t *testing.T) {
	t.Parallel()
	srv := blockedServer(t)
	srv.SetUIActive(true)
	if w := uiRequest(srv, "POST", "/api/ui-console", `{"id":"s1"}`); w.Code != http.StatusOK {
		t.Fatalf("report = %d %s", w.Code, w.Body)
	}
	if got := srv.ShownConsole(); got != "s1" {
		t.Fatalf("shown = %q", got)
	}
	srv.SetUIActive(false)
	if w := uiRequest(srv, "POST", "/api/ui-console", `{"id":""}`); !refusedAsInactive(w) {
		t.Fatalf("blocked report = %d", w.Code)
	}
	if got := srv.ShownConsole(); got != "s1" {
		t.Fatalf("a blocked tab overwrote it: %q", got)
	}
	var out struct {
		Console string `json:"console"`
	}
	if err := json.Unmarshal(uiRequest(srv, "GET", "/api/ui-active", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Console != "s1" {
		t.Fatalf("read = %+v", out)
	}
}

func TestConsoleFollowsFlipWired(t *testing.T) {
	t.Parallel()
	con := readStaticSrc(t, "console.js")
	for _, want := range []string{`postJSON("/api/ui-console", { id: con.id })`, "if (!state.hosted || state.uiBlocked) return;", "openConsole(q.console, { quiet: true })"} {
		if !strings.Contains(con, want) {
			t.Errorf("console.js lacks %s", want)
		}
	}
	if !strings.Contains(readStaticSrc(t, "live.js"), "showCarriedConsole();") {
		t.Error("live.js: a tab becoming active does not take the agent window")
	}
	if !strings.Contains(readStaticSrc(t, "app.js"), "await showCarriedConsole();") {
		t.Error("app.js: a tab opened by the switch does not take the agent window")
	}
}
