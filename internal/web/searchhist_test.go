package web

import (
	"net/http"
	"strings"
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

// A search phrase is a short line: an oversized body or phrase is refused
// rather than stored in the ring every frontend reads back.
func TestSearchHistoryBounded(t *testing.T) {
	isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	long := strings.Repeat("x", maxSearchPhrase+1)
	if code := postJSON(t, ts, "/api/search-history", `{"scope":"panel","phrase":"`+long+`"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("a phrase over %d runes = %d, want 400", maxSearchPhrase, code)
	}
	huge := strings.Repeat(" ", 8<<10)
	if code := postJSON(t, ts, "/api/search-history", `{"scope":"panel","phrase":"x"`+huge+`}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("an oversized body = %d, want 400", code)
	}
	var got struct {
		Rings map[string][]string `json:"rings"`
	}
	getJSON(t, ts, "/api/search-history", &got)
	if len(got.Rings["panel"]) != 0 {
		t.Errorf("a refused phrase was stored: %v", got.Rings["panel"])
	}
}
