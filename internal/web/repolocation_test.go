package web

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func locationOf(t *testing.T, srv *Server) repoLocationWire {
	t.Helper()
	var got struct {
		RepoLocation repoLocationWire `json:"repo_location"`
	}
	if code := getJSON(t, serve(t, srv), "/api/settings", &got); code != http.StatusOK {
		t.Fatalf("GET /api/settings = %d", code)
	}
	return got.RepoLocation
}

// locationRepo: a repo whose committed .gg.toml holds content, its private
// path under an isolated XDG_CONFIG_HOME.
func locationRepo(t *testing.T) (dir, committed, private string, srv *Server) {
	t.Helper()
	isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir = newRepoDir(t, 1)
	committed = filepath.Join(dir, ".gg.toml")
	if err := os.WriteFile(committed, []byte("[ui]\ncommit_sort = \"plain\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv = New(domain.Open(dir))
	return dir, committed, "", srv
}

func TestRepoLocationGet(t *testing.T) {
	_, committed, _, srv := locationRepo(t)
	loc := locationOf(t, srv)
	if !samePath(loc.Committed.Path, committed) || !loc.Committed.Exists {
		t.Errorf("committed = %+v", loc.Committed)
	}
	if loc.Private.Path == "" || loc.Private.Exists {
		t.Errorf("private = %+v", loc.Private)
	}
	if !slices.Equal(loc.Actions, []string{"copy-to-private", "move-to-private"}) {
		t.Errorf("actions = %v", loc.Actions)
	}
}

func samePath(a, b string) bool {
	ea, _ := filepath.EvalSymlinks(a)
	eb, _ := filepath.EvalSymlinks(b)
	return ea != "" && ea == eb
}

func TestRepoLocationCopyConfirmMove(t *testing.T) {
	_, committed, _, srv := locationRepo(t)
	var changed []bool
	srv.SetConfigChanged(func(relocated bool) { changed = append(changed, relocated) })
	ts := serve(t, srv)
	private := locationOf(t, srv).Private.Path

	code, got := postJSONAny(t, ts, "/api/settings/repo-location", `{"action":"copy-to-private"}`)
	if code != http.StatusOK || got["msg"] != "repo settings: Copy to private (user dir) done" {
		t.Fatalf("copy = %d %v", code, got)
	}
	if b, err := os.ReadFile(private); err != nil || string(b) != "[ui]\ncommit_sort = \"plain\"\n" {
		t.Fatalf("private copy = %q %v", b, err)
	}
	if len(changed) != 1 || !changed[0] {
		t.Errorf("the terminal is told (relocated): %v", changed)
	}
	loc := locationOf(t, srv)
	if !loc.Private.Exists || len(loc.Actions) != 4 {
		t.Errorf("after the copy: %+v", loc)
	}

	// The destination holds content: ask first, write nothing.
	if err := os.WriteFile(committed, []byte("# committed edit\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, got = postJSONAny(t, ts, "/api/settings/repo-location", `{"action":"copy-to-committed"}`)
	if code != http.StatusConflict || got["confirm"] != "Overwrite "+committedPathOf(t, srv)+" ?" {
		t.Fatalf("unconfirmed overwrite = %d %v", code, got)
	}
	if b, _ := os.ReadFile(committed); string(b) != "# committed edit\n" {
		t.Errorf("an unconfirmed overwrite wrote: %q", b)
	}
	code, got = postJSONAny(t, ts, "/api/settings/repo-location", `{"action":"move-to-committed","overwrite":true}`)
	if code != http.StatusOK || got["msg"] != "repo settings: Move to committed (.gg.toml) done" {
		t.Fatalf("move = %d %v", code, got)
	}
	if _, err := os.Stat(private); !os.IsNotExist(err) {
		t.Errorf("a move keeps the source: %v", err)
	}
	if b, _ := os.ReadFile(committed); string(b) != "[ui]\ncommit_sort = \"plain\"\n" {
		t.Errorf("committed after the move = %q", b)
	}
	if len(changed) != 2 {
		t.Errorf("changed = %v", changed)
	}
}

func committedPathOf(t *testing.T, srv *Server) string {
	t.Helper()
	return locationOf(t, srv).Committed.Path
}

func TestRepoLocationRefusals(t *testing.T) {
	_, _, _, srv := locationRepo(t)
	ts := serve(t, srv)
	for _, body := range []string{
		`{"action":"copy-to-committed"}`, // no private file: not applicable
		`{"action":"rm"}`,
		`{"action":"copy-to-private","path":"/etc/passwd"}`,
		`nope`,
	} {
		if code, _ := postJSONAny(t, ts, "/api/settings/repo-location", body); code != http.StatusBadRequest {
			t.Errorf("%s → %d, want 400", body, code)
		}
	}
	if locationOf(t, srv).Private.Exists {
		t.Error("a refused request wrote the private file")
	}
}
