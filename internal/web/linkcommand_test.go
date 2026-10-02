package web

import (
	"github.com/homeend/gigagit/internal/model"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/repos"
)

func TestLinkCommandContentLinkIsASteer(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	link := "gg://" + filepath.ToSlash(dir) + "/f.txt:1?view=content"
	var b struct {
		Steer    *steerWire `json:"steer"`
		Checkout string     `json:"checkout"`
	}
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(link), &b); code != http.StatusOK ||
		b.Steer == nil || b.Steer.Cmd != "navigate" || b.Steer.File != "f.txt" || b.Steer.HintKind != "view" || b.Steer.Line != 1 {
		t.Fatalf("code=%d body=%+v steer=%+v", code, b, b.Steer)
	}
}

func TestLinkCommandOtherCheckoutAndGarbage(t *testing.T) {
	t.Parallel()
	// Another checkout the resolver can place: one in this machine's gg
	// history (a local-form link to a repo gg never opened is a 422 — the
	// resolver's rule, reported as is).
	here, there := newRepoDir(t, 1), newRepoDir(t, 1)
	srv := New(domain.Open(here))
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	if err := repos.Touch(srv.reposPath, there, "", time.Now()); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, srv)
	var b struct {
		Steer    *steerWire `json:"steer"`
		Checkout string     `json:"checkout"`
	}
	link := "gg://" + filepath.ToSlash(there) + "/f.txt?view=content"
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(link), &b); code != http.StatusOK || b.Steer != nil || !domain.SamePath(b.Checkout, there) {
		t.Fatalf("other checkout: code=%d body=%+v", code, b)
	}
	var e map[string]any
	if code := getAny(t, ts, "/api/link-command?link=not-a-link", &e); code != http.StatusUnprocessableEntity {
		t.Fatalf("garbage: code %d, want 422", code)
	}
}

// A range link pasted into the page's # prompt comes back with its last line
// (the page bands first..last); a range of uncommitted lines that changed
// since the link was copied is refused in the resolver's own words.
func TestLinkCommandRangeLink(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("one\ntwo\nthree\nfour\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	base := "gg://" + filepath.ToSlash(dir) + "/f.txt:2-3~"
	var b struct {
		Steer *steerWire `json:"steer"`
	}
	link := base + model.BlockFingerprint([]string{"two", "three"})
	if code := getJSON(t, ts, "/api/link-command?link="+url.QueryEscape(link), &b); code != http.StatusOK ||
		b.Steer == nil || b.Steer.Line != 2 || b.Steer.EndLine != 3 {
		t.Fatalf("code=%d steer=%+v", code, b.Steer)
	}
	var e map[string]any
	stale := base + model.BlockFingerprint([]string{"two", "3"})
	if code := getAny(t, ts, "/api/link-command?link="+url.QueryEscape(stale), &e); code != http.StatusUnprocessableEntity {
		t.Fatalf("stale: code %d, want 422", code)
	}
	if msg, _ := e["error"].(string); !strings.Contains(msg, "the link is no longer valid: lines 2-3 of f.txt have changed") {
		t.Errorf("stale: error = %q", msg)
	}
}
