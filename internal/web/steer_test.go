package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/steer"
)

// newSteerServer builds a Server with its inbox injected — the seam that keeps
// these tests parallel and out of the real state home.
func newSteerServer(t *testing.T) *Server {
	t.Helper()
	s := New(domain.Open(newRepoDir(t, 1)))
	s.steerDir = t.TempDir()
	return s
}

// steerPost drives one request through the real handler chain (hostGuard +
// writeGuard included) using the suite's own helpers. out is nil: a 202 has an
// EMPTY body, and postJSON decodes every 2xx it is handed a target for.
func steerPost(t *testing.T, s *Server, body, contentType string) int {
	t.Helper()
	ts := serve(t, s)
	return postJSON(t, ts, "/api/session/steer", body, contentType, "", nil)
}

func TestSteerEndpointAccepts(t *testing.T) {
	t.Parallel()
	code := steerPost(t, newSteerServer(t), `{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"unstaged"},"line":{"side":"new","no":12}}`, "application/json")
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202", code)
	}
}

func TestSteerEndpointRejectsBadValues(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"id":"1-1","cmd":"teleport"}`,
		`{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"elsewhere"}}`,
		`{"id":"1-1","cmd":"navigate","file":"a.txt","line":{"side":"middle","no":1}}`,
		`{"id":"1-1","cmd":"navigate","file":"--upload-pack=evil","target":{"state":"unstaged"}}`,
		`{"id":"1-1","cmd":"navigate","commit":"-x"}`,
		`{"id":"1-1","cmd":"highlight","file":"a.txt","start":1,"tone":"shout"}`,
		`{"id":"1-1","cmd":"reload","sources":["weather"]}`,
		`not json`,
	} {
		body := body
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			if code := steerPost(t, newSteerServer(t), body, "application/json"); code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400 for %s", code, body)
			}
		})
	}
}

func TestSteerEndpointIs404WhenSteeringIsOff(t *testing.T) {
	t.Parallel()
	s := New(domain.Open(newRepoDir(t, 1))) // steerDir "" = steering off
	if code := steerPost(t, s, `{"id":"1-1","cmd":"reload","sources":["notes"]}`, "application/json"); code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — with steering off the CLI must see no session", code)
	}
}

func TestSteerEndpointRequiresJSONContentType(t *testing.T) {
	t.Parallel()
	if code := steerPost(t, newSteerServer(t), `{"id":"1-1","cmd":"reload"}`, "text/plain"); code != http.StatusUnsupportedMediaType {
		t.Fatalf("status = %d, want 415 (writeGuard)", code)
	}
}

func TestSteerEndpointAnswers409WhileAnOpIsInFlight(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.cur = &opRun{} // zero value: done is false, so opInFlight() is true
	if code := steerPost(t, s, `{"id":"1-1","cmd":"reload","sources":["notes"]}`, "application/json"); code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 — a steer must never be dropped silently by the hub gate", code)
	}
}

func TestSteerEmissionBypassesTheHubGate(t *testing.T) {
	t.Parallel()
	h := newLiveHub(config.RefreshConfig{}, false, func() bool { return true }) // gate always closed
	defer h.close()
	ch, cancel := h.subscribe()
	defer cancel()
	h.emit(liveMsg{Changed: []string{"notes"}, Reason: "notes"})
	select {
	case got := <-ch:
		t.Fatalf("emit delivered %+v through a closed gate", got)
	default:
	}
	h.emitSteer(liveMsg{Changed: []string{}, Reason: "steer", Steer: &steerWire{Cmd: "reload", Sources: []string{"notes"}}})
	select {
	case got := <-ch:
		if got.Reason != "steer" || got.Steer == nil {
			t.Fatalf("got %+v, want a steer message", got)
		}
	default:
		t.Fatal("emitSteer did not deliver: a one-off instruction must not be dropped by the op gate")
	}
}

func TestWebPresenceLifecycle(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	s.steerURL = "http://127.0.0.1:7777"
	s.touchSteerPresence()
	p, ok := steer.Live(s.steerDir, steer.WebPresence)
	if !ok {
		t.Fatal("no live web presence after a touch")
	}
	if p.URL != "http://127.0.0.1:7777" {
		t.Errorf("url = %q, want the server's own address — the CLI POSTs to it", p.URL)
	}
	s.removeSteerPresence()
	if _, ok := steer.Live(s.steerDir, steer.WebPresence); ok {
		t.Error("presence survived removeSteerPresence")
	}
}

// A server that booted with steering OFF must still refresh a presence a
// re-root handed it later: the ticker is the whole liveness protocol, and
// without one web.json drops out of steer.LiveWindow five seconds after the
// re-root wrote it — `gg session status` would stop seeing the page.
func TestSteerPresenceTickerPicksUpAnInboxAcquiredAfterBoot(t *testing.T) {
	t.Parallel()
	s := New(domain.Open(newRepoDir(t, 1))) // steerDir "" — booted steering-off
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.steerPresenceLoop(ctx)
	// The re-root arrives later and hands the server an inbox.
	dir := t.TempDir()
	s.steerMu.Lock()
	s.steerDir, s.steerURL = dir, "http://127.0.0.1:9999"
	s.steerMu.Unlock()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		if p, ok := steer.Live(dir, steer.WebPresence); ok {
			if p.URL != "http://127.0.0.1:9999" {
				t.Fatalf("url = %q, want this server's address", p.URL)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the presence ticker never wrote an inbox the server acquired after boot")
}

// A claim must DROP whatever web.json it finds: Touch preserves a stale
// payload (it only refreshes the mtime), and gg web's port changes every run,
// so a leftover file would send `gg session` to a dead address.
func TestWebPresenceClaimDropsAStalePayload(t *testing.T) {
	t.Parallel()
	s := newSteerServer(t)
	stale, err := json.Marshal(steer.Presence{PID: 4242, URL: "http://127.0.0.1:1111"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(s.steerDir, steer.WebPresence), stale, 0o644); err != nil {
		t.Fatal(err)
	}
	s.steerURL = "http://127.0.0.1:2222"
	s.claimSteerPresence()
	p, ok := steer.Live(s.steerDir, steer.WebPresence)
	if !ok {
		t.Fatal("no live web presence after a claim")
	}
	if p.URL != "http://127.0.0.1:2222" {
		t.Errorf("url = %q, want this run's address — a claim must overwrite the previous run's file", p.URL)
	}
}
