package web

import (
	"net/http"
	"os"
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

// When a machine-local private repo config exists, gg's config resolution
// reads THAT file — so the write must land there too, or health keeps
// reporting the old values and the banner never retires.
func TestUIConfigWritesActivePrivateConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	// PrivateRepoPath is keyed on the main worktree path as the SERVER will
	// resolve it (svc.TopLevel/Worktrees, which run through git and can
	// resolve symlinks a raw t.TempDir() path doesn't — the TestRepoEndpoint
	// precedent), so anchor on the same resolved path here.
	resolved := dir
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		resolved = real
	}
	priv := config.PrivateRepoPath(resolved)
	if err := os.MkdirAll(filepath.Dir(priv), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(priv, []byte("[ui]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/ui-config",
		`{"show_graph":"off","commit_sort":"plain"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("ui-config status = %d, want 200", code)
	}
	cfg, err := config.Load("", priv)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.UI.ShowGraph != "off" || cfg.UI.CommitSort != "plain" {
		t.Errorf("private config = %q/%q, want off/plain", cfg.UI.ShowGraph, cfg.UI.CommitSort)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gg.toml")); !os.IsNotExist(err) {
		t.Errorf("committed .gg.toml was created despite an active private config")
	}
}
