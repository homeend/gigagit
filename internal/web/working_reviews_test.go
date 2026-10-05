package web

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

func sha1Blob(s string) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(s))
	h.Write([]byte(s))
	return hex.EncodeToString(h.Sum(nil))
}

// workingReviewServer: f.txt edited to "edited\n" and an untracked u.txt,
// one working review whose document notes f.txt line 1; the files given
// (default: both, matching) are its fingerprints.
func workingReviewServer(t *testing.T) (*httptest.Server, *domain.Service, string, string) {
	t.Helper()
	dir := newRepoDir(t, 2)
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("u\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	doc := `{"version":1,"summary":"ok","files":[{"path":"f.txt","annotations":[{"newRange":[1,1],"summary":"on f"}]}]}`
	id, _, err := svc.SaveReview(context.Background(), domain.SaveReview{Target: domain.WorkingReviewTarget(), Agent: "Claude", Text: doc,
		Files: []model.NoteFile{{Path: "f.txt", Blob: sha1Blob("edited\n")}, {Path: "u.txt", Blob: sha1Blob("u\n")}}})
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, New(svc)), svc, dir, id
}

func TestNotesCountsCarryWorkingReviews(t *testing.T) {
	t.Parallel()
	ts, _, _, id := workingReviewServer(t)
	var got struct {
		Working []struct {
			ID      string   `json:"id"`
			Current bool     `json:"current"`
			Matches []string `json:"matches"`
		} `json:"working_reviews"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	if len(got.Working) != 1 || got.Working[0].ID != id || !got.Working[0].Current ||
		strings.Join(got.Working[0].Matches, ",") != "f.txt,u.txt" {
		t.Fatalf("working_reviews = %+v", got.Working)
	}
}

func TestReviewEndpointServesAWorkingReview(t *testing.T) {
	t.Parallel()
	ts, _, dir, id := workingReviewServer(t)
	if err := os.WriteFile(filepath.Join(dir, "u.txt"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Working bool                    `json:"working"`
		Base    string                  `json:"base"`
		Tip     string                  `json:"tip"`
		States  map[string]string       `json:"states"`
		Files   []struct{ Path string } `json:"files"`
		Counts  map[string]int          `json:"counts"`
	}
	if code := getJSON(t, ts, "/api/review/"+id, &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	paths := map[string]bool{}
	for _, f := range got.Files {
		paths[f.Path] = true
	}
	if !got.Working || len(got.Base) != 40 || got.Tip != "" || !paths["f.txt"] || !paths["u.txt"] {
		t.Fatalf("review = %+v", got)
	}
	if got.States["f.txt"] != "matches" || got.States["u.txt"] != "changed" || got.Counts["f.txt"] != 1 {
		t.Fatalf("states %v counts %v", got.States, got.Counts)
	}
}

func TestReviewNotesServesAWorkingReviewsNotesWhileTheFileMatches(t *testing.T) {
	t.Parallel()
	ts, _, dir, id := workingReviewServer(t)
	type resp struct {
		Notes []struct {
			ID   string `json:"id"`
			Line int    `json:"line"`
		} `json:"notes"`
	}
	var got resp
	if code := getJSON(t, ts, "/api/review/notes?id="+id+"&path=f.txt&status=M", &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].Line != 1 {
		t.Fatalf("notes = %+v", got.Notes)
	}
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("edited again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var after resp
	getJSON(t, ts, "/api/review/notes?id="+id+"&path=f.txt&status=M", &after)
	if len(after.Notes) != 0 {
		t.Fatalf("an edited file still shows %+v", after.Notes)
	}
}

func TestDiffHeadLaneComparesHeadToTheWorkingTree(t *testing.T) {
	t.Parallel()
	ts, _, _, _ := workingReviewServer(t)
	type row struct {
		Left  string `json:"left"`
		Right string `json:"right"`
	}
	var got struct {
		Rows []row `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/diff?wt=head&path=f.txt", &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	text := fmt.Sprint(got.Rows)
	if !strings.Contains(text, "content 2") || !strings.Contains(text, "edited") {
		t.Fatalf("rows = %+v, want HEAD's text against the working text", got.Rows)
	}
	var add struct {
		Rows []row `json:"rows"`
	}
	if code := getJSON(t, ts, "/api/diff?wt=head&path=u.txt", &add); code != http.StatusOK {
		t.Fatalf("untracked GET = %d", code)
	}
	if len(add.Rows) != 1 || add.Rows[0].Left != "" || add.Rows[0].Right != "u" {
		t.Fatalf("untracked rows = %+v, want a pure add", add.Rows)
	}
}

func TestAllNotesWireCarriesWorkingReviews(t *testing.T) {
	t.Parallel()
	ts, _, dir, _ := workingReviewServer(t)
	for _, f := range []string{"f.txt", "u.txt"} { // nothing matches: outdated
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var got struct {
		Count   int `json:"count"`
		Working []struct {
			ID       string `json:"id"`
			Kind     string `json:"kind"`
			Outdated bool   `json:"outdated"`
		} `json:"working_reviews"`
	}
	if code := getJSON(t, ts, "/api/notes/overview", &got); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	if len(got.Working) != 1 || !got.Working[0].Outdated || got.Working[0].Kind != "working" || got.Count != 1 {
		t.Fatalf("working_reviews = %+v (count %d)", got.Working, got.Count)
	}
}
