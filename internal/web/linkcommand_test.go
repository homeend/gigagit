package web

import (
	"net/http"
	"net/url"
	"path/filepath"
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
