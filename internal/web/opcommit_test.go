package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPCommitRoundTrip(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "new.txt")
	ts := serve(t, New(domain.Open(dir)))

	var out struct {
		OpID string `json:"op_id"`
	}
	code := postJSON(t, ts, "/api/op", `{"op":"commit","message":"web commit\n\nbody line"}`, "application/json", "", &out)
	if code != http.StatusAccepted {
		t.Fatalf("commit start code = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	sum, _ := done["summary"].(string)
	if !strings.Contains(sum, "committed") || !strings.Contains(sum, "web commit") {
		t.Errorf("summary = %q", sum)
	}
	if subj := gitRun(t, dir, "log", "-1", "--format=%s"); subj != "web commit" {
		t.Errorf("committed subject = %q", subj)
	}
	if body := gitRun(t, dir, "log", "-1", "--format=%b"); !strings.Contains(body, "body line") {
		t.Errorf("committed body = %q", body)
	}
	if out := gitRun(t, dir, "status", "--porcelain"); strings.Contains(out, "new.txt") {
		t.Errorf("new.txt still pending after commit:\n%s", out)
	}
}

func TestOpHTTPCommitValidation(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	for _, body := range []string{
		`{"op":"commit","message":""}`,
		`{"op":"commit","message":"   \n\t "}`,
		`{"op":"commit"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", body, code)
		}
	}
}

func TestOpHTTPCommitNothingStaged(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1) // clean tree
	ts := serve(t, New(domain.Open(dir)))
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/op", `{"op":"commit","message":"nope"}`, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("start code = %d", code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false (git refuses an empty commit)", done)
	}
}
