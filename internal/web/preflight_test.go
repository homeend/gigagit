package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type preflightResp struct {
	Pending  []preflightMigrationRow `json:"pending"`
	Disabled []string                `json:"disabled"`
}

// readPreflightStatic reads a file under static/ for the JS/CSS/HTML wiring
// checks below.
func readPreflightStatic(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("static", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func getPreflight(t *testing.T, ts *httptest.Server) preflightResp {
	t.Helper()
	var out preflightResp
	if code := getJSON(t, ts, "/api/preflight", &out); code != http.StatusOK {
		t.Fatalf("GET /api/preflight = %d", code)
	}
	return out
}

// A current-build repository satisfies every declared feature and has
// nothing repairable, so this is what an ordinary launch sees: an empty
// panel that never renders. domain.Features() declares no Migrate today
// (the follow-up spec adds one), so PendingMigrations is always empty here
// — there is no way to drive the Migrate happy path from this package
// without reaching into domain's unexported preflight cache, which the
// domain package's own tests do and this one deliberately does not.
func TestPreflightReportsNothingOnAHealthyRepo(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	body := getPreflight(t, ts)
	if len(body.Pending) != 0 {
		t.Errorf("pending = %v, want none on a fresh repo", body.Pending)
	}
	if len(body.Disabled) != 0 {
		t.Errorf("disabled = %v, want none on a fresh repo", body.Disabled)
	}
}

// The migrate endpoint must refuse a feature with nothing pending rather
// than silently no-op — a client bug that posts a stale feature id must be
// visible, not swallowed.
func TestPreflightMigrateRefusesUnknownFeature(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	code := postJSON(t, ts, "/api/preflight/migrate", `{"feature":"versions"}`, "application/json", "", nil)
	if code != http.StatusNotFound {
		t.Errorf("migrate versions on a healthy repo = %d, want 404 (nothing pending)", code)
	}
}

func TestPreflightMigrateRequiresFeature(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	code := postJSON(t, ts, "/api/preflight/migrate", `{}`, "application/json", "", nil)
	if code != http.StatusBadRequest {
		t.Errorf("migrate with no feature = %d, want 400", code)
	}
}

// Both endpoints must keep the standing loopback and Host/Origin guards —
// they are what keeps a repository readable/writable only from this
// machine's own browser, and a new route is exactly the kind of place that
// forgets to wire writeGuard back in.
func TestPreflightMigrateRejectsCrossOriginRequest(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/preflight/migrate", `{"feature":"versions"}`, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin migrate = %d, want 403", code)
	}
}

func TestPreflightMigrateRejectsWrongContentType(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/preflight/migrate", `{"feature":"versions"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("wrong content-type migrate = %d, want 415", code)
	}
}

func TestPreflightRejectsForbiddenHost(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	req, err := http.NewRequest("GET", ts.URL+"/api/preflight", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "evil.example.com"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("forbidden-host GET /api/preflight = %d, want 403", resp.StatusCode)
	}
}

// The panel and its CSS visibility rule must actually exist: a new
// class="hidden" element with no matching #id.hidden rule renders visible
// regardless of the class (a bug this project has shipped before), so a
// panel present in index.html with no rule in style.css would ALWAYS show.
func TestPreflightPanelHasAMatchingHiddenRule(t *testing.T) {
	t.Parallel()
	html := readPreflightStatic(t, "index.html")
	css := readPreflightStatic(t, "style.css")
	if !strings.Contains(html, `id="preflight"`) {
		t.Fatal(`index.html missing id="preflight" — the consent panel markup`)
	}
	if !strings.Contains(html, `id="preflight"`) || !strings.Contains(html, `class="hidden"`) {
		t.Fatal("index.html preflight panel must start hidden")
	}
	if !strings.Contains(css, "#preflight.hidden") {
		t.Fatal("style.css missing #preflight.hidden rule — the panel would ALWAYS be visible (per-id hiding rule)")
	}
}

// preflight.js must gate boot on the panel, fetch its own data, and hide the
// versions entry points when that feature is reported disabled — a static
// wiring check in the previewsjs_test.go style, since there is no JS runtime
// in this test binary.
func TestPreflightJSIsWiredEverywhere(t *testing.T) {
	t.Parallel()
	checks := []struct{ file, want, why string }{
		{"preflight.js", `getJSON("/api/preflight")`, "preflight.js owns its fetch"},
		{"preflight.js", `/api/preflight/migrate`, "Migrate must POST to the migrate endpoint"},
		{"app.js", `./preflight.js`, "the module must be imported"},
		{"app.js", `preflightGate`, "boot must gate on the consent panel before anything else runs"},
		{"sidebar.js", `featureDisabled("versions")`, "the branch context menu's versions row must be gated"},
		{"palette.js", `featureDisabled("versions")`, "both the palette and the ☰ menu's versions rows must be gated"},
	}
	for _, c := range checks {
		if !strings.Contains(readPreflightStatic(t, c.file), c.want) {
			t.Errorf("%s: missing %q — %s", c.file, c.want, c.why)
		}
	}
}
