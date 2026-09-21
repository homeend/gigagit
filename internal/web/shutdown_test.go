package web

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// A page must learn the server is going away while the stream can still
// carry it: the last message on /api/events is a "shutdown", so the tab
// paints its server-down bar at once instead of probing for seconds.
func TestEventsAnnounceShutdown(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond) // after the hello went out
		srv.announceShutdown()
		srv.announceShutdown() // idempotent: the exit path may race a second signal
	}()
	msgs := readLiveSSE(t, ts, 2, 3*time.Second)
	if msgs[0].Reason != "hello" || msgs[1].Reason != "shutdown" {
		t.Fatalf("want hello then shutdown, got %+v", msgs)
	}
}

// A stream with no hub (live refresh off) still hears the shutdown.
func TestEventsAnnounceShutdownWithoutHub(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond)
		srv.announceShutdown()
	}()
	msgs := readLiveSSE(t, ts, 2, 3*time.Second)
	if msgs[1].Reason != "shutdown" {
		t.Fatalf("want shutdown, got %+v", msgs)
	}
}

// A replaced hub (re-root, refresh-settings write) ends the stream too, and
// that is NOT a shutdown: the tab reconnects to a fresh hello. A shutdown
// message there would paint the server-down bar on every repo switch.
func TestHubReplaceIsNotAShutdown(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	writeRepoRefresh(t, dir, "enabled = true\n")
	srv := New(domain.Open(dir))
	srv.startLive(context.Background())
	t.Cleanup(srv.Close)
	ts := serve(t, srv)
	go func() {
		time.Sleep(200 * time.Millisecond)
		srv.stopLive()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	for _, m := range drainLiveSSE(t, ctx, ts) {
		if m.Reason == "shutdown" {
			t.Fatalf("a stopped hub announced a shutdown: %+v", m)
		}
	}
}

// The liveness probe answers without touching the repository: reads
// serialise under the per-repo gate, so a probe that queued behind a long
// operation would read as a dead server.
func TestPingNeedsNoRepo(t *testing.T) {
	isolateGlobal(t)
	ts := serve(t, New(domain.Open(t.TempDir()))) // not a repository at all
	resp, err := http.Get(ts.URL + "/api/ping")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ping status = %d", resp.StatusCode)
	}
}

// drainLiveSSE reads /api/events until the server ends the stream.
func drainLiveSSE(t *testing.T, ctx context.Context, ts *httptest.Server) []liveMsg {
	t.Helper()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	var out []liveMsg
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var m liveMsg
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m); err != nil {
			t.Fatalf("bad SSE json %q: %v", line, err)
		}
		out = append(out, m)
	}
	if ctx.Err() != nil {
		t.Fatalf("the stream never ended: %v", out)
	}
	return out
}
