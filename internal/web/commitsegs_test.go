package web

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// segDivergedRepo: feature adds ONE commit off main, then main moves on past the
// fork — the case where no decoration inside feature's history marks the
// fork, so only merge-base can find the territory boundary.
func segDivergedRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main", ".")
	gitRun(t, dir, "config", "user.email", "t@example.com")
	gitRun(t, dir, "config", "user.name", "t")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "main 1")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "main 2")
	gitRun(t, dir, "checkout", "-b", "feature")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "feature 1")
	gitRun(t, dir, "checkout", "main")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "main 3")
	return dir
}

type segRow struct {
	Subject string `json:"subject"`
	Seg     *int   `json:"seg"`
}

func commitSegRows(t *testing.T, ts *httptest.Server, path string) []segRow {
	t.Helper()
	var out struct {
		Rows []segRow `json:"rows"`
	}
	if code := getJSON(t, ts, path, &out); code != http.StatusOK {
		t.Fatalf("%s code = %d", path, code)
	}
	return out.Rows
}

func TestCommitsSoloRowsCarryTerritorySegments(t *testing.T) {
	t.Parallel()
	ts := serve(t, New(domain.Open(segDivergedRepo(t))))

	// Unsoloed: no segment field at all.
	for _, r := range commitSegRows(t, ts, "/api/commits") {
		if r.Seg != nil {
			t.Fatalf("unsoloed row %q carries seg=%d, want none", r.Subject, *r.Seg)
		}
	}

	if code := setSoloHTTP(t, ts, `{"branch":"feature"}`); code != http.StatusOK {
		t.Fatalf("solo code = %d", code)
	}
	rows := commitSegRows(t, ts, "/api/commits")
	if len(rows) != 3 || rows[0].Subject != "feature 1" {
		t.Fatalf("soloed rows = %+v", rows)
	}
	for _, r := range rows {
		if r.Seg == nil {
			t.Fatalf("soloed row %q has no seg", r.Subject)
		}
	}
	if *rows[0].Seg == *rows[1].Seg {
		t.Fatalf("feature's own commit and inherited main 2 share seg %d — fork point not found", *rows[0].Seg)
	}
	if *rows[1].Seg != *rows[2].Seg {
		t.Fatalf("main's territory split: main 2 seg %d, main 1 seg %d", *rows[1].Seg, *rows[2].Seg)
	}

	// A content filter lists a non-contiguous subset: no segments (nor lanes).
	for _, r := range commitSegRows(t, ts, "/api/commits?grep=main") {
		if r.Seg != nil {
			t.Fatalf("filtered row %q carries seg, want none", r.Subject)
		}
	}
}
