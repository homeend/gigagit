package web

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

func feedIsNil(srv *Server) bool {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	return srv.feed == nil
}

// Both keys land in the committed .gg.toml and a commit_sort write drops the
// cached feed so the next /api/commits rebuilds with the new sort.
func TestUIConfigWrite(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Fatalf("prime commits: %d", code)
	}
	if feedIsNil(srv) {
		t.Fatal("feed not built by /api/commits")
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off","commit_sort":"plain"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ui-config status = %d, want 200", code)
	}
	cfg, err := config.Load("", filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "off" || cfg.UI.CommitSort != "plain" {
		t.Errorf("written config = %q/%q, want off/plain", cfg.UI.ShowGraph, cfg.UI.CommitSort)
	}
	if !feedIsNil(srv) {
		t.Error("commit_sort write did not reset the feed")
	}
	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Errorf("commits after reset: %d", code)
	}
}

// A show_graph-only write must NOT reset the feed (sort unchanged; graph
// rendering is client-side).
func TestUIConfigShowGraphOnlyKeepsFeed(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := getJSON(t, ts, "/api/commits", nil); code != http.StatusOK {
		t.Fatalf("prime commits: %d", code)
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ui-config status = %d, want 200", code)
	}
	if feedIsNil(srv) {
		t.Error("show_graph-only write reset the feed")
	}
}

// The enum vocabulary is enforced; nothing outside it reaches the file.
func TestUIConfigRefusals(t *testing.T) {
	dir := newRepoDir(t, 1)
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	cases := []string{
		`{"show_graph":"maybe"}`,
		`{"commit_sort":"topo"}`,
		`{}`,
		`not json`,
	}
	for _, body := range cases {
		if code := postJSON(t, ts, "/api/ui-config", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, code)
		}
	}
	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("text/plain status = %d, want 415", code)
	}
	if code := getJSON(t, ts, "/api/ui-config", nil); code != http.StatusMethodNotAllowed {
		t.Errorf("GET status = %d, want 405", code)
	}
	if _, err := config.Load("", filepath.Join(dir, ".gg.toml")); err != nil {
		t.Fatalf("load after refusals: %v", err)
	}
	cfg, _ := config.Load("", filepath.Join(dir, ".gg.toml"))
	if cfg.UI.ShowGraph == "maybe" || cfg.UI.CommitSort == "topo" {
		t.Errorf("refused value reached the file: %q/%q", cfg.UI.ShowGraph, cfg.UI.CommitSort)
	}
}
