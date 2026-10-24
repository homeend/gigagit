package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// Search recall (the TUI's alt+↑/↓): an Enter-confirmed phrase is recorded
// in its scope's ring — the same per-repo rings the TUI reads — and the page
// reads them back newest first. Only the scopes the page searches in.
func TestSearchHistory(t *testing.T) {
	isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, p := range []string{"first", "second"} {
		if code := postJSON(t, ts, "/api/search-history", `{"scope":"panel","phrase":"`+p+`"}`, "application/json", "", nil); code != http.StatusOK {
			t.Fatalf("record = %d", code)
		}
	}
	var got struct {
		Rings map[string][]string `json:"rings"`
	}
	if code := getJSON(t, ts, "/api/search-history", &got); code != http.StatusOK {
		t.Fatalf("read = %d", code)
	}
	if r := got.Rings["panel"]; len(r) != 2 || r[0] != "second" {
		t.Fatalf("panel ring = %v, want [second first]", r)
	}
	if code := postJSON(t, ts, "/api/search-history", `{"scope":"shellcmd","phrase":"x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a scope the page does not search = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/search-history", `{"scope":"panel","phrase":"x"}`, "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Errorf("cross-origin = %d, want 403", code)
	}
}
