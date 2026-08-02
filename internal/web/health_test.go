package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

type healthOut struct {
	Big                 bool            `json:"big"`
	PackMB              int64           `json:"pack_mb"`
	HasCommitGraph      bool            `json:"has_commit_graph"`
	WriteCommitGraphSet bool            `json:"write_commit_graph_set"`
	ShowGraph           string          `json:"show_graph"`
	CommitSort          string          `json:"commit_sort"`
	Dismissed           map[string]bool `json:"dismissed"`
}

// A small loose-object repo under the real 100MB floor: not big, nothing
// set, defaults reported, both ids present and false.
func TestHealthEndpointDefaults(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if h.Big {
		t.Errorf("big = true for a tiny repo")
	}
	if h.HasCommitGraph || h.WriteCommitGraphSet {
		t.Errorf("flags = %v/%v, want false/false", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
	if h.ShowGraph != "on" || h.CommitSort != "date-order" {
		t.Errorf("defaults = %q/%q, want on/date-order", h.ShowGraph, h.CommitSort)
	}
	if len(h.Dismissed) != 2 || h.Dismissed["commit_graph_recommend"] || h.Dismissed["web_graph_off_suggest"] {
		t.Errorf("dismissed = %v, want both ids present and false", h.Dismissed)
	}
}

// The packThreshold seam: gc packs the objects, threshold 1 makes it "big".
func TestHealthBigViaSeam(t *testing.T) {
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "gc", "--quiet")
	srv := New(domain.Open(dir))
	srv.packThreshold = 1
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.Big {
		t.Errorf("big = false with a pack present and threshold 1")
	}
}

// Real flags + configured .gg.toml values are projected, not defaulted.
func TestHealthFlagsAndConfig(t *testing.T) {
	dir := newRepoDir(t, 3)
	gitRun(t, dir, "commit-graph", "write", "--reachable")
	gitRun(t, dir, "config", "fetch.writeCommitGraph", "true")
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"),
		[]byte("[ui]\nshow_graph = \"off\"\ncommit_sort = \"plain\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.HasCommitGraph || !h.WriteCommitGraphSet {
		t.Errorf("flags = %v/%v, want true/true", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
	if h.ShowGraph != "off" || h.CommitSort != "plain" {
		t.Errorf("config = %q/%q, want off/plain", h.ShowGraph, h.CommitSort)
	}
}

// A dismissal seeded in the shared prompts store (keyed by git common dir,
// the TUI's key) is reported; the other id stays false.
func TestHealthDismissed(t *testing.T) {
	dir := newRepoDir(t, 3)
	srv := New(domain.Open(dir))
	stateDir := t.TempDir()
	srv.reposPath = filepath.Join(stateDir, "repos.toml")

	key, err := domain.Open(dir).GitCommonDir(context.Background())
	if err != nil || key == "" {
		t.Fatalf("GitCommonDir: %v %q", err, key)
	}
	store := promptstate.NewFileStore(filepath.Join(stateDir, "prompts.toml"))
	if err := store.DismissNotice(key, "commit_graph_recommend"); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if !h.Dismissed["commit_graph_recommend"] {
		t.Errorf("seeded dismissal not reported: %v", h.Dismissed)
	}
	if h.Dismissed["web_graph_off_suggest"] {
		t.Errorf("unseeded id reported dismissed: %v", h.Dismissed)
	}
}
