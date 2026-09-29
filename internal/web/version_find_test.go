package web

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// A two-branch version row carries its preview link (server-built, like a
// saved pair's) and the description its copy records.
func TestVersionsRowCarriesThePreviewLink(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	var body struct {
		Versions []struct {
			Ref  string `json:"ref"`
			Link string `json:"link"`
			Desc string `json:"desc"`
		} `json:"versions"`
	}
	if code := getJSON(t, ts, "/api/versions?branch=main", &body); code != http.StatusOK || len(body.Versions) != 1 {
		t.Fatalf("GET /api/versions = %d, %+v", code, body)
	}
	v := body.Versions[0]
	id := v.Ref[strings.LastIndex(v.Ref, "/")+1:]
	if !strings.HasPrefix(v.Link, "gg:///") || !strings.HasSuffix(v.Link, "?version="+id) {
		t.Fatalf("link = %q, want the local-form pair link with ?version=%s", v.Link, id)
	}
	if !strings.HasPrefix(v.Desc, "version: main · merge · ") {
		t.Fatalf("desc = %q", v.Desc)
	}
}

// /api/version-find answers a landed hint: by id + pair, or a miss.
func TestVersionFindHitAndMiss(t *testing.T) {
	t.Parallel()
	dir := driftRepo(t)
	ts := serve(t, New(domain.Open(dir)))
	runOpOK(t, ts, `{"op":"merge","branch":"feature","onto":"main"}`)
	var list struct {
		Versions []struct {
			Ref  string `json:"ref"`
			Link string `json:"link"`
		} `json:"versions"`
	}
	getJSON(t, ts, "/api/versions?branch=main", &list)
	if len(list.Versions) != 1 || list.Versions[0].Link == "" {
		t.Fatalf("versions = %+v, want one row with a link", list.Versions)
	}
	l, err := model.ParseLink(list.Versions[0].Link)
	if err != nil {
		t.Fatal(err)
	}
	q := url.Values{"id": {l.Hint.ID}, "a": {l.Target.Pair.A}, "b": {l.Target.Pair.B}}
	var out struct {
		Found  bool   `json:"found"`
		Branch string `json:"branch"`
		Ref    string `json:"ref"`
	}
	if code := getJSON(t, ts, "/api/version-find?"+q.Encode(), &out); code != http.StatusOK || !out.Found || out.Branch != "main" || out.Ref != list.Versions[0].Ref {
		t.Fatalf("find hit = %d %+v", code, out)
	}
	q.Set("id", "1600000000-rebase")
	q.Set("b", strings.Repeat("f", 40))
	out.Found = true
	if code := getJSON(t, ts, "/api/version-find?"+q.Encode(), &out); code != http.StatusOK || out.Found {
		t.Fatalf("find miss = %d %+v, want 200 found:false", code, out)
	}
	if code := getJSON(t, ts, "/api/version-find?id=x&a=nope&b=nope", &out); code != http.StatusBadRequest {
		t.Fatalf("bad shas = %d, want 400", code)
	}
}
