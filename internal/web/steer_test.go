package web

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
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
		// A panel gg does not have: the protocol's nine names are the whole
		// vocabulary, and an agent that invents one should hear so rather than
		// have the command silently do nothing.
		`{"id":"1-1","cmd":"focus","panel":"weather"}`,
		`{"id":"1-1","cmd":"focus","panel":"diff"}`,
		// state "commit" with no sha names no commit at all — the page would
		// fetch /api/commit/ and 404 on its own.
		`{"id":"1-1","cmd":"navigate","file":"a.txt","target":{"state":"commit"}}`,
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

// The nine protocol panel names (internal/tui's panelProtoName) must ALL be
// accepted: the page only has two panes, but the endpoint speaks the protocol,
// not the page's subset — a name the page cannot act on is a no-op there, never
// a refusal here.
func TestSteerEndpointAcceptsEveryProtocolPanel(t *testing.T) {
	t.Parallel()
	for _, panel := range []string{"branches", "worktrees", "remotes", "files", "staged", "commits", "tags", "reflog", "previews"} {
		panel := panel
		t.Run(panel, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"1-1","cmd":"focus","panel":"` + panel + `"}`
			if code := steerPost(t, newSteerServer(t), body, "application/json"); code != http.StatusAccepted {
				t.Errorf("status = %d, want 202 for panel %q", code, panel)
			}
		})
	}
}

// Only `highlight` carries a band. A navigate that happened to arrive with
// tone/start/end must not hand the page a band to paint — the page keys its
// marks off exactly these fields.
func TestSteerWireCarriesBandFieldsOnlyForHighlight(t *testing.T) {
	t.Parallel()
	w, err := toSteerWire(steer.Command{Cmd: "navigate", File: "a.txt", Start: 3, End: 9, Tone: "error"})
	if err != nil {
		t.Fatalf("toSteerWire: %v", err)
	}
	if w.Start != 0 || w.End != 0 || w.Tone != "" {
		t.Errorf("navigate wire = {start:%d end:%d tone:%q}, want all zero — a band belongs to highlight alone", w.Start, w.End, w.Tone)
	}
	h, err := toSteerWire(steer.Command{Cmd: "highlight", File: "a.txt", Start: 3, Tone: "error"})
	if err != nil {
		t.Fatalf("toSteerWire(highlight): %v", err)
	}
	if h.Start != 3 || h.End != 3 || h.Tone != "error" {
		t.Errorf("highlight wire = {start:%d end:%d tone:%q}, want {3 3 error}", h.Start, h.End, h.Tone)
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
	// The dir is captured HERE: removeSteerPresence clears s.steerDir (so a
	// late tick cannot resurrect the file), and re-reading the field after the
	// remove would stat "web.json" in the process's own working directory —
	// never ok, whether or not the remove did anything.
	dir := s.steerDir
	s.steerURL = "http://127.0.0.1:7777"
	s.touchSteerPresence()
	p, ok := steer.Live(dir, steer.WebPresence)
	if !ok {
		t.Fatal("no live web presence after a touch")
	}
	if p.URL != "http://127.0.0.1:7777" {
		t.Errorf("url = %q, want the server's own address — the CLI POSTs to it", p.URL)
	}
	s.removeSteerPresence()
	if _, ok := steer.Live(dir, steer.WebPresence); ok {
		t.Error("presence survived removeSteerPresence")
	}
	if got := s.steerInbox(); got != "" {
		t.Errorf("steerDir = %q after removeSteerPresence, want it cleared", got)
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

// The page keeps the TUI's attention-mark lifetime rule: a `status`/`all`
// reload rebuilds the diff geometry the bands are anchored against and drops
// them; a `notes`-only reload — which every note mutation auto-posts — must
// leave them exactly where the agent put them. The two halves live in
// different languages, so this pins the JS side by source.
func TestLiveJSDropsMarksOnlyForAStatusReload(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if !strings.Contains(src, `if (want.has("status")) state.attention.clear();`) {
		t.Error(`live.js must gate state.attention.clear() on want.has("status") — a notes-only reload keeps the bands`)
	}
	if strings.Contains(src, "\n  state.attention.clear();") {
		t.Error("live.js still clears the attention marks unconditionally in steerReload")
	}
}

func TestSteerWireAcceptsThePreviewTarget(t *testing.T) {
	t.Parallel()
	w, err := toSteerWire(steer.Command{
		Cmd:    "navigate",
		File:   "a.txt",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 4},
	})
	if err != nil {
		t.Fatalf("toSteerWire: %v", err)
	}
	if w.State != "preview" || w.Source != "feat/x" || w.Target != "main" {
		t.Errorf("wire = {state:%q source:%q target:%q}, want the preview triple", w.State, w.Source, w.Target)
	}
	if w.Commit != "" {
		t.Errorf("wire.commit = %q, want empty — the consumer resolves the tip itself", w.Commit)
	}
	if w.Side != "new" || w.Line != 4 {
		t.Errorf("wire line = %s:%d", w.Side, w.Line)
	}
}

// A preview navigate with NO file reveals the Previews entry; the "navigate
// needs a file, a commit or a step" rule must not reject it.
func TestSteerWireAcceptsAPreviewRevealWithNoFile(t *testing.T) {
	t.Parallel()
	if _, err := toSteerWire(steer.Command{
		Cmd:    "navigate",
		Target: &steer.Target{State: "preview", Source: "feat/x", Target: "main"},
	}); err != nil {
		t.Fatalf("toSteerWire: %v", err)
	}
}

func TestSteerWireRefusesABadPreviewTarget(t *testing.T) {
	t.Parallel()
	for _, tg := range []*steer.Target{
		{State: "preview", Source: "feat/x"},                          // no target half
		{State: "preview", Target: "main"},                            // no source half
		{State: "preview", Source: "--upload-pack=x", Target: "main"}, // argv injection
		{State: "preview", Source: "feat/x", Target: "--evil"},
	} {
		if _, err := toSteerWire(steer.Command{Cmd: "navigate", File: "a.txt", Target: tg}); err == nil {
			t.Errorf("toSteerWire(%+v) = nil error, want a refusal", *tg)
		}
	}
}
