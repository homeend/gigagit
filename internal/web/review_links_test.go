package web

import (
	"net/url"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// GET /api/review/{id}/link hands the page a review's gg link (only the
// server knows the change a review compared); an unknown id is a 404.
func TestReviewLinkEndpoint(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDoc)
	var got struct{ Link string }
	if code := getJSON(t, ts, "/api/review/"+id+"/link", &got); code != 200 || !strings.HasSuffix(got.Link, "@"+sha+"?review="+id) {
		t.Fatalf("link = %d %q", code, got.Link)
	}
	if code := getJSON(t, ts, "/api/review/deadbeef/link", &got); code != 404 {
		t.Fatalf("unknown = %d, want 404", code)
	}
}

// GET /api/notes/row-link is a commit's Range review / Notes row link:
// {commit, path} = the file at the commit, {commit, scope} = the pair.
func TestNoteRowLinkEndpoint(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 3)
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	ts := serve(t, New(svc))
	head := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD"))
	base := strings.TrimSpace(gitOut(t, dir, "rev-parse", "HEAD~1"))
	var got struct{ Link string }
	q := func(v url.Values) int { got.Link = ""; return getJSON(t, ts, "/api/notes/row-link?"+v.Encode(), &got) }
	if code := q(url.Values{"commit": {head}, "path": {"f.txt"}}); code != 200 || !strings.HasSuffix(got.Link, "/f.txt@"+head) {
		t.Fatalf("path = %d %q", code, got.Link)
	}
	if code := q(url.Values{"commit": {head}, "scope": {base[:7] + ".." + head[:7]}}); code != 200 || !strings.Contains(got.Link, "@"+base+".."+head) {
		t.Fatalf("scope = %d %q", code, got.Link)
	}
	for _, v := range []url.Values{
		{"commit": {"HEAD~1"}, "path": {"f.txt"}},
		{"commit": {head}},
		{"commit": {head}, "path": {"f.txt"}, "scope": {"a..b"}},
	} {
		if code := q(v); code != 400 {
			t.Errorf("%v = %d, want 400", v, code)
		}
	}
}

// ?path= names a file of the review, ?n= one remark; both is a usage error.
func TestReviewLinkEndpointFileAndRemark(t *testing.T) {
	t.Parallel()
	ts, _, sha, id := reviewServer(t, webReviewDoc)
	var got struct{ Link string }
	if code := getJSON(t, ts, "/api/review/"+id+"/link?path=f.txt", &got); code != 200 || !strings.HasSuffix(got.Link, "/f.txt@"+sha+"?review="+id) {
		t.Fatalf("file = %d %q", code, got.Link)
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/link?n=0", &got); code != 200 || !strings.HasSuffix(got.Link, "/f.txt@"+sha+":1?review="+id) {
		t.Fatalf("remark = %d %q", code, got.Link)
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/link?n=99", &got); code != 404 {
		t.Fatalf("unknown remark = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/link?path=nope.txt", &got); code != 404 {
		t.Fatalf("a file the review does not hold = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/link?n=x", &got); code != 400 {
		t.Fatalf("bad n = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/link?n=0&path=f.txt", &got); code != 400 {
		t.Fatalf("both = %d, want 400", code)
	}
}

var reviewLinkRowsWiring = []struct{ file, want, why string }{
	{"files.js", "copy review link to this file", "a reviewed file row copies its review link"},
	{"files.js", `"/link?path="`, "the file link is the server's"},
	{"files.js", "copy remark link", "a remark copies its review link"},
	{"files.js", `"/link?n="`, "the remark link is the server's"},
	{"files.js", "copy remark id", "a remark copies the id gg note reply takes"},
	{"live.js", "await openNamedFile(state.files, s, \"review \"", "a review link with a file lands on it"},
	{"live.js", "await landLine(s)", "a review link with a line lands on the line, as every navigate does"},
	{"files.js", "|| n.parent_id", "a reply's thread root falls back to its parent (the stacked view keeps no state.notes)"},
}

func TestReviewLinkRowsAreWired(t *testing.T) {
	t.Parallel()
	for _, c := range reviewLinkRowsWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}

// Copy remark id asks the server first: the id comes back only while the
// review and its remark exist.
func TestReviewRemarkIDEndpoint(t *testing.T) {
	t.Parallel()
	ts, _, _, id := reviewServer(t, webReviewDoc)
	var got struct{ ID string }
	if code := getJSON(t, ts, "/api/review/"+id+"/remark-id?n=0", &got); code != 200 || got.ID != "review:"+id+":0" {
		t.Fatalf("remark 0 = %d %q", code, got.ID)
	}
	for _, u := range []string{"/api/review/" + id + "/remark-id?n=99", "/api/review/deadbeef/remark-id?n=0"} {
		if code := getJSON(t, ts, u, &got); code != 404 {
			t.Errorf("%s = %d, want 404", u, code)
		}
	}
	if code := getJSON(t, ts, "/api/review/"+id+"/remark-id?n=x", &got); code != 400 {
		t.Errorf("bad n = %d, want 400", code)
	}
}
