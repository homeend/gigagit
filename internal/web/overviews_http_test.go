package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// overviewServer serves newRepoDir's two-commit repo (f.txt "content 2" at the
// tip) holding one permanent overview on HEAD: a link to f.txt:1 with a remark.
func overviewServer(t *testing.T) (ts *httptest.Server, svc *domain.Service, id string) {
	t.Helper()
	svc = domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	tg, err := svc.HeadOverviewTarget(ctx)
	if err != nil {
		t.Fatal(err)
	}
	got, _, err := svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"How f works","overview":"Start at [f](f.txt:1), then [gone](zzz.go:1).","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"the content"}]}]}`)})
	if err == nil {
		t.Fatal("zzz.go is not in the commit: the save must be refused")
	}
	got, _, err = svc.SaveOverview(ctx, domain.SaveOverview{Target: tg, Agent: "Claude Code", Text: []byte(
		`{"version":1,"title":"How f works","overview":"Start at [f](f.txt:1).","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"the content"}]}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, New(svc)), svc, got.ID
}

func TestOverviewDocument(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	var got struct {
		ID, Title, Commit, Agent, Text string
		PR                             int
		Blocks                         []any
		Anchors                        []overviewAnchor
	}
	if code := getJSON(t, ts, "/api/overview/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	if got.ID != id || got.Title != "How f works" || got.Commit != strings.TrimSpace(head) || got.PR != 0 || len(got.Blocks) == 0 {
		t.Fatalf("doc = %+v", got)
	}
	if len(got.Anchors) != 1 || got.Anchors[0].Path != "f.txt" || got.Anchors[0].Plain {
		t.Fatalf("anchors = %+v", got.Anchors)
	}
	var link struct{ Link string }
	if code := getJSON(t, ts, "/api/overview/"+id+"/link", &link); code != 200 || !strings.Contains(link.Link, "?overview="+id) {
		t.Fatalf("link = %d %+v", code, link)
	}
}

func TestOverviewRemarksRideTheLanes(t *testing.T) {
	t.Parallel()
	ts, svc, id := overviewServer(t)
	head, _, _ := svc.ResolveRev(context.Background(), "HEAD")
	var fc struct {
		Remarks []noteRow `json:"remarks"`
	}
	if code := getJSON(t, ts, "/api/file-content?src=commit&rev="+strings.TrimSpace(head)+"&path=f.txt&overview="+id, &fc); code != 200 ||
		len(fc.Remarks) != 1 || fc.Remarks[0].Summary != "the content" || !fc.Remarks[0].ReadOnly || fc.Remarks[0].Kind != "overview" {
		t.Fatalf("file-content = %d %+v", code, fc.Remarks)
	}
	// R9: without the lane the same file carries none.
	fc.Remarks = nil
	if getJSON(t, ts, "/api/file-content?src=commit&rev="+strings.TrimSpace(head)+"&path=f.txt", &fc); len(fc.Remarks) != 0 {
		t.Fatalf("remarks leaked: %+v", fc.Remarks)
	}
	var d struct {
		Changed bool      `json:"changed"`
		Remarks []noteRow `json:"remarks"`
	}
	if code := getJSON(t, ts, "/api/diff?overview="+id+"&path=f.txt", &d); code != 200 || !d.Changed || len(d.Remarks) != 1 || d.Remarks[0].Kind != "overview" {
		t.Fatalf("diff = %d %+v", code, d)
	}
	resp, err := http.Get(ts.URL + "/api/overview-export?overview=" + id)
	if err != nil || resp.StatusCode != 200 || !strings.Contains(resp.Header.Get("Content-Disposition"), "overview-how-f-works.html") {
		t.Fatalf("export = %v %+v", err, resp)
	}
	resp.Body.Close()
}

func TestOverviewRoutesRefuseBadIDs(t *testing.T) {
	t.Parallel()
	ts, _, _ := overviewServer(t)
	for _, p := range []string{
		"/api/overview/..%2Fx", "/api/overview/" + strings.Repeat("a", 200),
		"/api/diff?overview=..%2Fx&path=f.txt", "/api/overview-export?overview=..%2Fx",
		"/api/file-content?src=worktree&path=f.txt&overview=..%2Fx",
	} {
		if code := getJSON(t, ts, p, nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", p, code)
		}
	}
	for _, p := range []string{"/api/overview/nope", "/api/overview/nope/link", "/api/diff?overview=nope&path=f.txt", "/api/overview-export?overview=nope"} {
		if code := getJSON(t, ts, p, nil); code != http.StatusNotFound {
			t.Errorf("%s = %d, want 404", p, code)
		}
	}
}
