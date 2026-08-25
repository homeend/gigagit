package web

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPDeleteTag(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-tag","tag":"v1"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if !strings.Contains(done["summary"].(string), "deleted tag v1") {
		t.Errorf("summary = %v", done["summary"])
	}
	if out := gitRun(t, dir, "tag", "--list", "v1"); strings.TrimSpace(out) != "" {
		t.Errorf("tag still listed: %q", out)
	}
}

func TestOpHTTPDeleteTagMissing(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"delete-tag","tag":"nope"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != false {
		t.Fatalf("done = %v, want ok=false", done)
	}
	if done["error"] == nil || done["error"] == "" {
		t.Error("missing error detail")
	}
}

func TestOpHTTPDeleteTagBadName(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"op":"delete-tag"}`,
		`{"op":"delete-tag","tag":"-v1"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
