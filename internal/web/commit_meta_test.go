package web

import (
	"net/http"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The file-list stage draws the commit's date under its title. The feed row
// already carries a time, but the by-hash open (sidebar tags) has no row — so
// the endpoint itself must carry the date, one source for both paths.
func TestCommitFilesEndpointCarriesDateAndAuthor(t *testing.T) {
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Sha    string `json:"sha"`
		Time   int64  `json:"time"`
		Author string `json:"author"`
	}
	if code := getJSON(t, ts, "/api/commit/"+sha, &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if got.Time == 0 {
		t.Error("time = 0, want the commit's author time")
	}
	if got.Author == "" {
		t.Error("author = \"\", want the commit's author")
	}
	// The date must match the feed's, or the file list and the commit row above
	// it would disagree about the same commit.
	var feed struct {
		Rows []struct {
			Hash   string `json:"hash"`
			Time   int64  `json:"time"`
			Author string `json:"author"`
		} `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/commits", &feed); code != http.StatusOK {
		t.Fatalf("/api/commits status = %d, want 200", code)
	}
	var found bool
	for _, r := range feed.Rows {
		if r.Hash != sha {
			continue
		}
		found = true
		if r.Time != got.Time || r.Author != got.Author {
			t.Errorf("commit endpoint = (%d, %q), feed row = (%d, %q); they must agree",
				got.Time, got.Author, r.Time, r.Author)
		}
	}
	if !found {
		t.Fatalf("sha %s not in the feed; the agreement check did not run", sha)
	}
}

// An unresolvable date must not take the file list down with it: the files are
// the payload, the date is decoration.
func TestCommitFilesEndpointStillServesFilesWithoutADate(t *testing.T) {
	dir := newRepoDir(t, 2)
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if code := getJSON(t, ts, "/api/commit/"+sha, &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if len(got.Files) != 1 || got.Files[0].Path != "f.txt" {
		t.Fatalf("files = %+v, want [{f.txt}] — the date must not displace the payload", got.Files)
	}
}
