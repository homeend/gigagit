package web

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func TestOverviewExportAgent(t *testing.T) {
	t.Parallel()
	s, root := noteSrv(t)
	o, err := s.docs.AddOverview(root, s.service().Root(), "My Tour", "see [here](f.txt:1)")
	if err != nil {
		t.Fatal(err)
	}
	code, h, raw := getRaw(t, serve(t, s), "/api/overview-export?id="+o.ID)
	body := string(raw)
	if code != http.StatusOK {
		t.Fatalf("code %d: %s", code, body)
	}
	if ct := h.Get("Content-Type"); ct != "text/html; charset=utf-8" {
		t.Errorf("content type %q", ct)
	}
	if cd := h.Get("Content-Disposition"); cd != `attachment; filename="my-tour.html"` {
		t.Errorf("disposition %q", cd)
	}
	if !strings.Contains(body, `id="gg-data"`) || !strings.Contains(body, `"path":"f.txt"`) {
		t.Errorf("body lacks the blob or the file")
	}
}

func TestOverviewExportErrors(t *testing.T) {
	t.Parallel()
	s, _ := noteSrv(t)
	ts := serve(t, s)
	other, _ := s.docs.AddOverview("/elsewhere", "/elsewhere", "T", "x")
	for path, want := range map[string]int{
		"/api/overview-export":                http.StatusBadRequest,
		"/api/overview-export?id=bogus":       http.StatusBadRequest,
		"/api/overview-export?id=f999999":     http.StatusNotFound,
		"/api/overview-export?id=" + other.ID: http.StatusNotFound,
		"/api/overview-export?id=f1&review=x": http.StatusBadRequest,
	} {
		if code, _, body := getRaw(t, ts, path); code != want {
			t.Errorf("%s: %d, want %d (%s)", path, code, want, body)
		}
	}
}

func TestOverviewExportStoredReview(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDocWithOverview)
	code, h, raw := getRaw(t, ts, "/api/overview-export?review="+id)
	if code != http.StatusOK {
		t.Fatalf("code %d: %s", code, raw)
	}
	if cd := h.Get("Content-Disposition"); cd != `attachment; filename="review-overview-`+sha[:7]+`.html"` {
		t.Errorf("disposition %q", cd)
	}
	if body := string(raw); !strings.Contains(body, `"path":"f.txt"`) || strings.Contains(body, `"summary":"A"`) {
		t.Errorf("want f.txt and no note in the page")
	}
	ts2, _, _, plain := reviewServer(t, webReviewDoc) // no overview
	for path, want := range map[string]int{
		"/api/overview-export?review=" + plain: http.StatusNotFound,
		"/api/overview-export?review=nope":     http.StatusNotFound,
		"/api/overview-export?review=a/b":      http.StatusBadRequest,
	} {
		if code, _, body := getRaw(t, ts2, path); code != want {
			t.Errorf("%s: %d, want %d (%s)", path, code, want, body)
		}
	}
}

func TestOverviewExportPermanent(t *testing.T) {
	t.Parallel()
	svc := domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	tg, err := svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude", Text: []byte(
		`{"version":1,"title":"Kept tour","overview":"see [here](f.txt:1)","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"zebra-remark"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(svc))
	code, h, raw := getRaw(t, ts, "/api/overview-export?overview="+o.ID)
	if code != http.StatusOK {
		t.Fatalf("code %d: %s", code, raw)
	}
	if cd := h.Get("Content-Disposition"); cd != `attachment; filename="overview-kept-tour.html"` {
		t.Errorf("disposition %q", cd)
	}
	if body := string(raw); !strings.Contains(body, `"path":"f.txt"`) || strings.Contains(body, "zebra-remark") {
		t.Errorf("want f.txt and no remark in the page")
	}
	for path, want := range map[string]int{
		"/api/overview-export?overview=nope":                  http.StatusNotFound,
		"/api/overview-export?overview=a/b":                   http.StatusBadRequest,
		"/api/overview-export?overview=" + o.ID + "&review=x": http.StatusBadRequest,
	} {
		if code, _, body := getRaw(t, ts, path); code != want {
			t.Errorf("%s: %d, want %d (%s)", path, code, want, body)
		}
	}
}

// E E (or E and the foot chip) must not download twice: the export runs
// through the shared runOnce gate, which drops a repeat start while the
// first is in flight.
func TestOverviewExportClientRunsOnce(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("static", "viewer.js"))
	if err != nil {
		t.Fatal(err)
	}
	body := funcBody(t, string(src), "function exportOverviewHTML()")
	if !strings.Contains(body, `runOnce("overview-export"`) {
		t.Errorf("exportOverviewHTML is not gated by runOnce:\n%s", body)
	}
}
