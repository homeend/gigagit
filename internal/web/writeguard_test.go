package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
)

// routeLine matches one mux registration: the method, the path, and the
// handler expression it was given.
var routeLine = regexp.MustCompile(`mux\.HandleFunc\("([A-Z]+) ([^"]+)",\s*(.+?)\)\s*$`)

// mutatingMethods are the ones that change state and therefore need the
// Origin + Content-Type checks. GET/HEAD are reads and are covered by
// hostGuard alone.
var mutatingMethods = map[string]bool{
	"POST": true, "PUT": true, "PATCH": true, "DELETE": true,
}

// TestEveryMutatingRouteIsWriteGuarded pins the invariant the server's
// security posture rests on: every state-changing route goes through
// writeGuard, which requires `Content-Type: application/json` and refuses a
// non-loopback Origin.
//
// The listener binds loopback only, so the threat is not a remote attacker.
// It is a page in the user's own browser POSTing to 127.0.0.1 on whatever
// port gg web happens to hold. Without the Content-Type requirement a plain
// <form> POST reaches the handler, and a form needs no CORS preflight — which
// is exactly what makes a cross-site write reachable at all. Without the
// Origin check, a page that guesses the port can drive the API.
//
// This gate exists because the rule was enforced by HAND on ~22 routes and
// four had been missed: POST and DELETE for /api/bookmarks and /api/shelf.
// A convention applied per-call-site holds nowhere structurally, which is the
// same finding as archtest's enumerated store list (ruling S17).
func TestEveryMutatingRouteIsWriteGuarded(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, line := range strings.Split(string(src), "\n") {
		m := routeLine.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		method, path, handler := m[1], m[2], m[3]
		if !mutatingMethods[method] {
			continue
		}
		seen++
		if !strings.Contains(handler, "writeGuard(") {
			t.Errorf("%s %s is not wrapped in writeGuard — a cross-origin or "+
				"form-encoded request would reach it:\n  %s", method, path, strings.TrimSpace(line))
		}
	}
	// If the registration spelling ever changes, this gate would quietly
	// guard nothing. There are ~22 mutating routes; a count far below that
	// means the regex stopped matching.
	if seen < 15 {
		t.Errorf("only %d mutating routes matched — the mux registration spelling changed and this gate has stopped seeing them", seen)
	}
}

// TestBookmarkShelfWritesRefuseCrossOrigin is the runtime half of the gate
// above, on the four routes that were missed. The gate reads source; this
// drives the real mux, so a future refactor that keeps the writeGuard( text
// but breaks the wiring still fails.
func TestBookmarkShelfWritesRefuseCrossOrigin(t *testing.T) {
	isolateState(t)
	dir := gittest.BasicRepo(t, "hi\n")
	ts := httptest.NewServer(New(domain.Open(dir)).Handler())
	defer ts.Close()

	for _, tc := range []struct {
		name, method, path, body string
	}{
		{"bookmark add", "POST", "/api/bookmarks", `{"path":"README.md","state":"unstaged"}`},
		{"bookmark remove", "DELETE", "/api/bookmarks?id=x", ""},
		{"shelf add", "POST", "/api/shelf", `{"path":"README.md","state":"unstaged"}`},
		{"shelf remove", "DELETE", "/api/shelf?id=x", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// A cross-origin request must be refused outright...
			req, err := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Origin", "http://evil.example")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if resp.StatusCode != http.StatusForbidden {
				t.Errorf("cross-origin %s %s: %d, want 403", tc.method, tc.path, resp.StatusCode)
			}

			// ...and so must a form-encoded one, which is the shape that
			// needs no CORS preflight and so is reachable cross-site.
			req2, err := http.NewRequest(tc.method, ts.URL+tc.path, strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp2, err := http.DefaultClient.Do(req2)
			if err != nil {
				t.Fatal(err)
			}
			resp2.Body.Close()
			if resp2.StatusCode != http.StatusUnsupportedMediaType {
				t.Errorf("form-encoded %s %s: %d, want 415", tc.method, tc.path, resp2.StatusCode)
			}
		})
	}
}
