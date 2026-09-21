package web

import (
	"net/http"
	"testing"
)

// The prompt's branch hints: the TUI's ranking (fuzzy.Rank over local then
// remote-tracking names, top 5), and nothing for an empty query.
func TestBranchSuggest(t *testing.T) {
	dir, _, _ := linkRepo(t) // main, feat/x
	gitRun(t, dir, "branch", "feat/yak")
	srv, _ := linkServer(t, dir)
	ts := serve(t, srv)
	var got struct{ Names []string }
	if code := getJSON(t, ts, "/api/branch-suggest?q=fx", &got); code != http.StatusOK || len(got.Names) == 0 || got.Names[0] != "feat/x" {
		t.Fatalf("q=fx: %d %v, want feat/x first", code, got.Names)
	}
	got.Names = nil
	if code := getJSON(t, ts, "/api/branch-suggest?q=feat", &got); code != http.StatusOK || len(got.Names) != 2 {
		t.Fatalf("q=feat: %d %v, want both feat/ branches", code, got.Names)
	}
	got.Names = []string{"stale"}
	if code := getJSON(t, ts, "/api/branch-suggest?q=", &got); code != http.StatusOK || len(got.Names) != 0 {
		t.Fatalf("empty q: %d %v, want no names", code, got.Names)
	}
}
