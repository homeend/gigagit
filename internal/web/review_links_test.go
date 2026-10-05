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
