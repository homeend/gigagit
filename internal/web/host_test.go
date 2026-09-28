package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

func repoInfoAt(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url + "/api/repo")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
		t.Fatal(err)
	}
	return m
}

func webPresenceOf(t *testing.T, svc *domain.Service) (steer.Presence, bool) {
	t.Helper()
	dir, _ := resolveSteerDir(context.Background(), svc)
	if dir == "" {
		t.Fatal("steering resolved off")
	}
	return steer.Live(dir, steer.WebPresence)
}

func startHost(t *testing.T, svc *domain.Service) (*Host, string) {
	t.Helper()
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(h.Close)
	return h, url
}

func TestHostStartServesHosted(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	h, url := startHost(t, svc)
	if !strings.HasPrefix(url, "http://127.0.0.1:") || h.URL() != url {
		t.Fatalf("url = %q", url)
	}
	if info := repoInfoAt(t, url); info["hosted"] != true {
		t.Fatalf("hosted = %v", info["hosted"])
	}
	if p, live := webPresenceOf(t, svc); !live || p.URL != url {
		t.Fatalf("web.json = %+v live=%v, want this URL", p, live)
	}
}

func TestHostRerootFollows(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	svc := domain.Open(dir)
	h, url := startHost(t, svc)
	next := domain.Open(wt)
	if err := h.Reroot(context.Background(), next); err != nil {
		t.Fatal(err)
	}
	if got := repoInfoAt(t, url)["worktree"]; got != wt {
		t.Fatalf("worktree after reroot = %v, want %s", got, wt)
	}
	if _, live := webPresenceOf(t, svc); live {
		t.Fatal("the old inbox must lose web.json")
	}
	if p, live := webPresenceOf(t, next); !live || p.URL != url {
		t.Fatalf("new inbox web.json = %+v live=%v", p, live)
	}
	// Twice in a row lands on the last one.
	if err := h.Reroot(context.Background(), svc); err != nil {
		t.Fatal(err)
	}
	if got := repoInfoAt(t, url)["worktree"]; got != svc.Root() {
		t.Fatalf("worktree after the second reroot = %v, want %s", got, svc.Root())
	}
	if _, live := webPresenceOf(t, next); live {
		t.Fatal("the middle inbox must lose web.json too")
	}
}

func TestHostRefusesForeignPage(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	dir, _ := resolveSteerDir(context.Background(), svc)
	if err := steer.Touch(dir, steer.WebPresence, steer.Presence{PID: 99999, URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	h := NewHost(svc, domain.Open, true)
	_, err := h.Start(context.Background(), "127.0.0.1:0")
	if err == nil {
		h.Close()
		t.Fatal("a foreign live page must refuse the start")
	}
	if !strings.Contains(err.Error(), "http://127.0.0.1:1") {
		t.Fatalf("err = %v, want a refusal naming the other page", err)
	}
	if p, live := steer.Live(dir, steer.WebPresence); !live || p.PID != 99999 {
		t.Fatal("the foreign presence must be left alone")
	}
}

func TestHostCloseAnnouncesShutdown(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got := make(chan []liveMsg, 1)
	go func() { got <- readLiveSSEFrom(t, url, 2, shutdownGrace+3*time.Second) }()
	time.Sleep(300 * time.Millisecond) // after the hello went out
	start := time.Now()
	h.Close()
	h.Close() // idempotent
	if took := time.Since(start); took > shutdownGrace+time.Second {
		t.Fatalf("Close took %v, must return within the grace", took)
	}
	msgs := <-got
	if len(msgs) != 2 || msgs[1].Reason != "shutdown" {
		t.Fatalf("want hello then shutdown, got %+v", msgs)
	}
	if _, live := webPresenceOf(t, svc); live {
		t.Fatal("Close removes web.json")
	}
	if _, err := http.Get(url + "/api/repo"); err == nil {
		t.Fatal("the port must be closed")
	}
}

// A re-root into a worktree another live page serves must not steal that
// page's web.json: the hosted page keeps serving with no presence there and
// Reroot reports the other URL.
func TestHostRerootLeavesAForeignPagesPresenceAlone(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	dir := newRepoDir(t, 2)
	wt := addWorktree(t, dir, "side")
	svc := domain.Open(dir)
	h, url := startHost(t, svc)
	next := domain.Open(wt)
	nextDir, _ := resolveSteerDir(context.Background(), next)
	if err := steer.Touch(nextDir, steer.WebPresence, steer.Presence{PID: 99999, URL: "http://127.0.0.1:1"}); err != nil {
		t.Fatal(err)
	}
	err := h.Reroot(context.Background(), next)
	if err == nil || !strings.Contains(err.Error(), "http://127.0.0.1:1") {
		t.Fatalf("Reroot = %v, want the foreign page named", err)
	}
	if got := repoInfoAt(t, url)["worktree"]; got != wt {
		t.Fatalf("the page must still follow the switch, worktree = %v", got)
	}
	if p, live := steer.Live(nextDir, steer.WebPresence); !live || p.PID != 99999 {
		t.Fatal("the foreign presence must be left alone")
	}
	if _, live := webPresenceOf(t, svc); live {
		t.Fatal("the old inbox must lose web.json")
	}
	time.Sleep(steerPresenceTick + 300*time.Millisecond)
	if p, _ := steer.Live(nextDir, steer.WebPresence); p.PID != 99999 {
		t.Fatal("the presence ticker must not overwrite the foreign web.json")
	}
}

// Quit with a console stream open: Close still returns well within the
// grace (the screen handler ends on the closing signal).
func TestHostCloseWithAScreenStreamAttached(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	sess := testSession(t, "sleep 5")
	svc := domain.Open(newRepoDir(t, 1))
	h := NewHost(svc, domain.Open, true)
	url, err := h.Start(context.Background(), "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url+"/api/session-screen?id="+string(sess.Info().ID), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	buf := make([]byte, 64)
	if _, err := resp.Body.Read(buf); err != nil { // the hello is on the wire
		t.Fatal(err)
	}
	start := time.Now()
	h.Close()
	if took := time.Since(start); took > shutdownGrace/2 {
		t.Fatalf("Close took %v with a screen stream open; the stream must end on the closing signal", took)
	}
}

func TestServeStandaloneIsNotHosted(t *testing.T) {
	isolateGlobal(t)
	isolateState(t)
	dir := newRepoDir(t, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	urlCh := make(chan string, 1)
	serveURLHook = func(u string) { urlCh <- u }
	t.Cleanup(func() { serveURLHook = nil })
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, dir, "127.0.0.1:0", false, nil) }()
	select {
	case u := <-urlCh:
		if repoInfoAt(t, u)["hosted"] != false {
			t.Fatal("standalone is not hosted")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Serve never bound")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v", err)
		}
	case <-time.After(shutdownGrace + 5*time.Second):
		t.Fatal("Serve did not return after cancel")
	}
}
