package web

import (
	"net/http"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOpHTTPCheckoutTagDetached(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "tag", "v1", "HEAD~1")
	target := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-tag","tag":"v1"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if ref := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); ref != "HEAD" {
		t.Errorf("HEAD = %q, want detached", ref)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != target {
		t.Errorf("HEAD at %s, want the tag target %s", at, target)
	}
}

func TestOpHTTPCheckoutTagNewBranch(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "tag", "v1", "HEAD~1")
	target := gitRun(t, dir, "rev-parse", "HEAD~1")
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"checkout-tag","tag":"v1","name":"from-v1"}`), 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if ref := gitRun(t, dir, "rev-parse", "--abbrev-ref", "HEAD"); ref != "from-v1" {
		t.Errorf("HEAD = %q, want from-v1", ref)
	}
	if at := gitRun(t, dir, "rev-parse", "HEAD"); at != target {
		t.Errorf("HEAD at %s, want %s", at, target)
	}
}

func TestOpHTTPCheckoutTagRefusals(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "tag", "v1")
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/op", `{"op":"checkout-tag","tag":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("unknown tag: code = %d, want 404", code)
	}
	for _, body := range []string{
		`{"op":"checkout-tag"}`,
		`{"op":"checkout-tag","tag":"-v1"}`,
		`{"op":"checkout-tag","tag":"v1","name":"-x"}`,
	} {
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
}
