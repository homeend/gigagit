package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// putJSON is postJSON's PUT twin (the layout endpoint is a replace, not a
// command), with an overridable content type for the guard test.
func putJSON(t *testing.T, ts *httptest.Server, path, body, contentType string, out any) int {
	t.Helper()
	req, err := http.NewRequest("PUT", ts.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PUT %s: %v", path, err)
	}
	defer resp.Body.Close()
	if out != nil && resp.StatusCode >= 200 && resp.StatusCode < 300 {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", path, err)
		}
	}
	return resp.StatusCode
}

// uiServer serves a repo with the machine-local state redirected into the
// test's temp dir, so the layout file never touches the real one.
func uiServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	srv := New(domain.Open(newRepoDir(t, 1)))
	return srv
}

func getUIState(t *testing.T, ts *httptest.Server) uiStateWire {
	t.Helper()
	var out uiStateWire
	if code := getJSON(t, ts, "/api/uistate", &out); code != http.StatusOK {
		t.Fatalf("GET /api/uistate: code = %d", code)
	}
	return out
}

// A first run reports saved=false, which is what tells the client to apply its
// own defaults instead of treating an empty list as "nothing folded".
func TestUIStateStartsUnsaved(t *testing.T) {
	ts := serve(t, uiServer(t))
	st := getUIState(t, ts)
	if st.Saved {
		t.Fatalf("fresh state reports saved=true: %+v", st)
	}
	if len(st.Sections) != 0 {
		t.Fatalf("fresh sections = %v", st.Sections)
	}
}

func TestUIStateRoundTrips(t *testing.T) {
	ts := serve(t, uiServer(t))
	body := `{"sections":["tags","reflog"],"sidebar_hidden":true,"sidebar_width":310,"files_width":420,"graph":"off"}`
	var put uiStateWire
	if code := putJSON(t, ts, "/api/uistate", body, "", &put); code != http.StatusOK {
		t.Fatalf("PUT code = %d", code)
	}
	if !put.Saved {
		t.Fatal("PUT response must report saved=true")
	}
	st := getUIState(t, ts)
	if !st.Saved || st.SidebarWidth != 310 || st.FilesWidth != 420 || !st.SidebarHidden || st.Graph != "off" {
		t.Fatalf("round trip = %+v", st)
	}
	if len(st.Sections) != 2 || st.Sections[0] != "tags" || st.Sections[1] != "reflog" {
		t.Fatalf("sections = %v", st.Sections)
	}
}

// The layout must outlive the server: this is the whole point — a NEW process
// (on a new random port) has to serve back what the last one stored.
func TestUIStateSurvivesAServerRestart(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	repo := newRepoDir(t, 1)

	ts1 := serve(t, New(domain.Open(repo)))
	body := `{"sections":["stashes"],"sidebar_width":275,"sorts":{"branches":"name-asc"}}`
	if code := putJSON(t, ts1, "/api/uistate", body, "", nil); code != http.StatusOK {
		t.Fatalf("PUT code = %d", code)
	}

	ts2 := serve(t, New(domain.Open(repo))) // a second server, as a restart would be
	st := getUIState(t, ts2)
	if st.Sorts["branches"] != "name-asc" {
		t.Errorf("sorts after a restart = %v", st.Sorts)
	}
	if !st.Saved || len(st.Sections) != 1 || st.Sections[0] != "stashes" || st.SidebarWidth != 275 {
		t.Fatalf("state after a restart = %+v (file: %s)", st, filepath.Join(dir, "gg", "prompts.toml"))
	}
}

// Wire values are resolved against an allowlist, like everywhere else in this
// frontend: unknown sections drop, the order is canonical, junk graph modes
// fall back, and widths are bounded.
func TestUIStateSanitizesInput(t *testing.T) {
	ts := serve(t, uiServer(t))
	body := `{"sections":["reflog","bogus","tags","tags"],"sidebar_width":-40,"files_width":99999,"graph":"; rm -rf /"}`
	var put uiStateWire
	if code := putJSON(t, ts, "/api/uistate", body, "", &put); code != http.StatusOK {
		t.Fatalf("PUT code = %d", code)
	}
	if len(put.Sections) != 2 || put.Sections[0] != "tags" || put.Sections[1] != "reflog" {
		t.Fatalf("sections = %v, want the known two in canonical order", put.Sections)
	}
	if put.SidebarWidth != 0 {
		t.Fatalf("negative width stored as %d", put.SidebarWidth)
	}
	if put.FilesWidth != uiMaxPaneWidth {
		t.Fatalf("oversized width stored as %d", put.FilesWidth)
	}
	if put.Graph != "svg" {
		t.Fatalf("graph = %q, want the svg fallback", put.Graph)
	}
}

// The per-list sort modes ride the same layout record: a browser on a random
// port cannot remember them itself.
func TestUIStateSortsRoundTrip(t *testing.T) {
	ts := serve(t, uiServer(t))
	body := `{"sections":[],"sorts":{"branches":"date-desc","tags":"name-asc"}}`
	var put uiStateWire
	if code := putJSON(t, ts, "/api/uistate", body, "", &put); code != http.StatusOK {
		t.Fatalf("PUT code = %d", code)
	}
	if put.Sorts["branches"] != "date-desc" || put.Sorts["tags"] != "name-asc" {
		t.Fatalf("PUT response sorts = %v", put.Sorts)
	}
	st := getUIState(t, ts)
	if st.Sorts["branches"] != "date-desc" || st.Sorts["tags"] != "name-asc" {
		t.Fatalf("stored sorts = %v", st.Sorts)
	}
}

// Both dimensions are allowlisted — the list name AND the mode — like
// allowedSections/allowedGraph. An explicit "default" is kept: the files list
// does not START on git's order, so choosing it is a real choice.
func TestUIStateSanitizesSorts(t *testing.T) {
	ts := serve(t, uiServer(t))
	body := `{"sections":[],"sorts":{"branches":"bogus","bogus":"name-asc","worktrees":"date-asc","files":"default"}}`
	var put uiStateWire
	if code := putJSON(t, ts, "/api/uistate", body, "", &put); code != http.StatusOK {
		t.Fatalf("PUT code = %d", code)
	}
	if _, ok := put.Sorts["bogus"]; ok {
		t.Errorf("unknown list survived: %v", put.Sorts)
	}
	if put.Sorts["branches"] != "default" {
		t.Errorf("unknown mode = %q, want the default fallback", put.Sorts["branches"])
	}
	if put.Sorts["files"] != "default" {
		t.Errorf("an explicit default must survive: %v", put.Sorts)
	}
	if put.Sorts["worktrees"] != "date-asc" {
		t.Errorf("sorts = %v, want worktrees=date-asc kept", put.Sorts)
	}
}

func TestUIStateRejectsGarbage(t *testing.T) {
	ts := serve(t, uiServer(t))
	if code := putJSON(t, ts, "/api/uistate", `not json`, "", nil); code != http.StatusBadRequest {
		t.Fatalf("bad body code = %d, want 400", code)
	}
	if code := putJSON(t, ts, "/api/uistate", `{}`, "text/plain", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("non-JSON content type code = %d, want 415", code)
	}
}
