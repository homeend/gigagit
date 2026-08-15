package web

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// The registries exist so a feature can be written in its own file. These
// tests use throwaway registrations to prove the plumbing, then remove them —
// the maps are package globals, so a test that leaked one would change the
// behaviour of every later test in the package.

func withOp(t *testing.T, name string, b OpBuilder) {
	t.Helper()
	RegisterOp(name, b)
	t.Cleanup(func() { delete(opRegistry, name) })
}

// A registered op runs through POST /api/op exactly like a built-in one.
func TestRegisteredOpRuns(t *testing.T) {
	dir := newRepoDir(t, 1)
	withOp(t, "test-write", func(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
		return engine.WriteFile{Path: req.Path, Data: []byte("from a registered op\n")}, nil, 0, nil
	})
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"test-write","path":"registered.txt"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "registered.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "from a registered op\n" {
		t.Errorf("wrote %q", got)
	}
}

// A builder's refusal carries its own status code — a feature must be able to
// answer 404 or 422, not have everything flattened to 500.
func TestRegisteredOpRefusalKeepsItsStatus(t *testing.T) {
	dir := newRepoDir(t, 1)
	withOp(t, "test-refuse", func(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
		return nil, nil, http.StatusUnprocessableEntity, errors.New("nope")
	})
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"test-refuse"}`, "application/json", "", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("code = %d, want 422", code)
	}
}

// The cleanup hook runs after the operation finishes — the shelved-commit
// patch lane depends on it to remove its temp file.
func TestRegisteredOpCleanupRuns(t *testing.T) {
	dir := newRepoDir(t, 1)
	done := make(chan struct{})
	withOp(t, "test-cleanup", func(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
		return engine.WriteFile{Path: "cleanup.txt", Data: []byte("x\n")}, func() { close(done) }, 0, nil
	})
	ts := serve(t, New(domain.Open(dir)))

	readSSE(t, ts, startOpBody(t, ts, `{"op":"test-cleanup"}`), 30*time.Second)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup never ran")
	}
}

// An unregistered name still falls through to the built-in switch, and an
// unknown one is still a 400 — the registry adds a lane, it does not replace
// the 43 operations that already work.
func TestUnknownOpStillRefused(t *testing.T) {
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	if code := postJSON(t, ts, "/api/op", `{"op":"no-such-op"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("code = %d, want 400", code)
	}
}

// Two features answering to one wire name must fail loudly at startup rather
// than letting whichever init() ran last win.
func TestRegisterOpRejectsDuplicates(t *testing.T) {
	b := func(s *Server, r *http.Request, req opStartRequest) (engine.Operation, func(), int, error) {
		return nil, nil, 0, nil
	}
	withOp(t, "test-dup", b)
	defer func() {
		if recover() == nil {
			t.Error("a duplicate registration did not panic")
		}
	}()
	RegisterOp("test-dup", b)
}

// A route declared from a feature's own file is served.
func TestRegisteredRouteServed(t *testing.T) {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/test-registered", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, map[string]any{"ok": true})
		})
	})
	t.Cleanup(func() { routeRegistry = routeRegistry[:len(routeRegistry)-1] })

	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		OK bool `json:"ok"`
	}
	if code := getJSON(t, ts, "/api/test-registered", &got); code != http.StatusOK || !got.OK {
		t.Errorf("code = %d, body = %+v", code, got)
	}
}
