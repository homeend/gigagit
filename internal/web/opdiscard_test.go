package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// postJSONRaw POSTs body and decodes the JSON response body into a
// string map regardless of status code — used where the error message
// itself is part of the assertion (postJSON only decodes 2xx bodies).
func postJSONRaw(t *testing.T, ts *httptest.Server, path, body string) (int, map[string]string) {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	var out map[string]string
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestOpDiscard(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("clean\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "c1")

	// worktree edits: f.txt (tracked, unstaged) + new.txt (untracked)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	ts := serve(t, New(domain.Open(dir)))

	// 1. discard the tracked file: restored to its committed content, the
	// untracked file is left alone.
	opID := startOpBody(t, ts, `{"op":"discard","path":"f.txt"}`)
	events := readSSE(t, ts, opID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	got, err := os.ReadFile(filepath.Join(dir, "f.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "clean\n" {
		t.Errorf("f.txt = %q, want %q", got, "clean\n")
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); err != nil {
		t.Errorf("new.txt missing after discarding f.txt: %v", err)
	}

	// 2. discard the untracked file: removed from disk.
	opID2 := startOpBody(t, ts, `{"op":"discard","path":"new.txt"}`)
	events2 := readSSE(t, ts, opID2, 20*time.Second)
	done2 := events2[len(events2)-1]
	if done2["ok"] != true || done2["changed"] != true {
		t.Fatalf("done2 = %v", done2)
	}
	if _, err := os.Stat(filepath.Join(dir, "new.txt")); !os.IsNotExist(err) {
		t.Errorf("new.txt still on disk after discard: err=%v", err)
	}

	// 3. a path the server's own status doesn't know about 404s instead of
	// discarding the wrong thing (a stale client row).
	if code := postJSON(t, ts, "/api/op", `{"op":"discard","path":"nope.txt"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown path code = %d, want 404", code)
	}

	// 4. an empty or flag-shaped path is rejected before it ever reaches a
	// status read / git argv.
	for _, body := range []string{
		`{"op":"discard","path":""}`,
		`{"op":"discard","path":"-x"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}

func TestOpDiscardConflicted(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // f.txt committed as "content 1\n"
	gitRun(t, dir, "checkout", "-b", "side")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("side\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "side")
	gitRun(t, dir, "checkout", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("main2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "main2")
	gitTry(dir, "merge", "side") // conflicts on f.txt by construction

	ts := serve(t, New(domain.Open(dir)))

	code, out := postJSONRaw(t, ts, "/api/op", `{"op":"discard","path":"f.txt"}`)
	if code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, want 422 (body %v)", code, out)
	}
	if !strings.Contains(out["error"], "conflicted") {
		t.Errorf("error = %v", out)
	}
}
