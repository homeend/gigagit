package web

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// op:"commit-graph" chains WriteCommitGraph → SetGitConfig server-side: after
// done, the graph file (or chain dir) exists AND fetch.writeCommitGraph is
// true — and /api/health reports both flags flipped, which is what retires
// the banner group.
func TestOpCommitGraph(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpJSON(t, ts, `{"op":"commit-graph"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}

	gitDir := gitRun(t, dir, "rev-parse", "--absolute-git-dir")
	_, ferr := os.Stat(filepath.Join(gitDir, "objects", "info", "commit-graph"))
	_, cerr := os.Stat(filepath.Join(gitDir, "objects", "info", "commit-graphs"))
	if ferr != nil && cerr != nil {
		t.Errorf("no commit-graph file or chain dir: %v / %v", ferr, cerr)
	}
	if v := gitRun(t, dir, "config", "fetch.writeCommitGraph"); v != "true" {
		t.Errorf("fetch.writeCommitGraph = %q, want true", v)
	}

	var h healthOut
	if code := getJSON(t, ts, "/api/health", &h); code != http.StatusOK {
		t.Fatalf("health status = %d", code)
	}
	if !h.HasCommitGraph || !h.WriteCommitGraphSet {
		t.Errorf("health flags = %v/%v, want true/true", h.HasCommitGraph, h.WriteCommitGraphSet)
	}
}
