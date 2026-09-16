package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

// The file-list stage draws the commit's date under its title. The feed row
// already carries a time, but the by-hash open (sidebar tags) has no row — so
// the endpoint itself must carry the date, one source for both paths.
func TestCommitFilesEndpointCarriesDateAndAuthor(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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

// The file-list header draws the first lines of the commit's DESCRIPTION under
// the date, and its "show full message…" button opens the whole message — so
// the endpoint that opens a commit carries the message itself. One source for
// the feed-row open and the by-hash open, and the same text the reword prompt
// prefills with: /api/commit-message is the reference, both must agree.
func TestCommitFilesEndpointCarriesTheFullMessage(t *testing.T) {
	t.Parallel()
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, "g.txt"), []byte("g\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "subject line", "-m", "first body paragraph\n\nsecond body paragraph")
	sha := gitRun(t, dir, "rev-parse", "HEAD")
	ts := serve(t, New(domain.Open(dir)))
	var got struct {
		Message string `json:"message"`
	}
	if code := getJSON(t, ts, "/api/commit/"+sha, &got); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	var ref struct {
		Message string `json:"message"`
	}
	if code := getJSON(t, ts, "/api/commit-message?rev="+sha, &ref); code != http.StatusOK {
		t.Fatalf("/api/commit-message status = %d, want 200", code)
	}
	if ref.Message == "" || !strings.Contains(ref.Message, "second body paragraph") {
		t.Fatalf("/api/commit-message = %q, want the full message", ref.Message)
	}
	if got.Message != ref.Message {
		t.Errorf("commit endpoint message = %q, /api/commit-message = %q; they must agree", got.Message, ref.Message)
	}
}
