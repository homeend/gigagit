package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func startOpJSON(t *testing.T, ts *httptest.Server, body string) string {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", body, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("start %s: code = %d", body, code)
	}
	return out.OpID
}

// Fetch takes no arguments and asks nothing, so the whole contract is: it
// runs, and the remote-tracking ref catches up to work pushed elsewhere.
func TestOpHTTPFetch(t *testing.T) {
	origin, clone := cloneWithOrigin(t)
	before := gitRun(t, clone, "rev-parse", "origin/main")
	pushRemoteCommit(t, origin, "r.txt", "r\n", "remote work")
	ts := serve(t, New(domain.Open(clone)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"fetch"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "fetched") {
		t.Errorf("summary = %v", done["summary"])
	}

	after := gitRun(t, clone, "rev-parse", "origin/main")
	if after == before {
		t.Errorf("origin/main did not move: still %s", before)
	}
	// Fetch must not touch the working branch — that is what pull is for.
	local := gitRun(t, clone, "rev-parse", "main")
	if local == after {
		t.Errorf("fetch moved local main to %s; it should only move the tracking ref", local)
	}
}
