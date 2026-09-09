package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

func previewRepo(t *testing.T) string {
	t.Helper()
	isolateState(t)
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a\n"), 0o644)
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "add a")
	gitRun(t, dir, "checkout", "-q", "main")
	return dir
}

// previewServe wires a Server whose Service has its own injected preview
// store. domain.PreviewsDisabled is set true process-wide by this package's
// TestMain (the tui/web test seam), so a plain domain.Open(dir) here would
// resolve NO store and every call would answer {"disabled":true} — the
// lazy-resolution guard in previewStore only accepts an override VIA
// UsePreviewsDir, it does not read PreviewsDisabled for an already-injected
// store. Inject one so these tests exercise real CRUD/summary behaviour.
func previewServe(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	svc := domain.Open(dir)
	svc.UsePreviewsDir(t.TempDir())
	return serve(t, New(svc))
}

func deleteWithJSON(t *testing.T, ts *httptest.Server, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", ts.URL+path, nil)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

type pvList struct {
	Entries []struct {
		ID, Label, Source, Target, State string
		Files, Ahead                     int
		SourceHash                       string `json:"source_hash"`
	} `json:"entries"`
}

func TestPreviewCRUDAndOpen(t *testing.T) {
	dir := previewRepo(t)
	ts := previewServe(t, dir)
	var added struct{ Entry struct{ ID string } }
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main","label":"login"}`, "application/json", "", &added); code != http.StatusOK {
		t.Fatalf("add: %d", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main"}`, "application/json", "", nil); code != http.StatusConflict {
		t.Fatalf("duplicate: %d, want 409", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Fatalf("writeGuard: %d", code)
	}
	var got pvList
	getJSON(t, ts, "/api/preview", &got)
	if len(got.Entries) != 1 || got.Entries[0].State != "ok" || got.Entries[0].Files != 1 || got.Entries[0].Ahead != 1 || len(got.Entries[0].SourceHash) != 40 {
		t.Fatalf("list = %+v", got)
	}
	var open struct{ State, Left, Right, Source, Target string }
	if code := getJSON(t, ts, "/api/preview/open?id="+added.Entry.ID, &open); code != http.StatusOK || open.State != "ok" || open.Left == "" || open.Right != got.Entries[0].SourceHash {
		t.Fatalf("open = %d %+v", code, open)
	}
	if code := postJSON(t, ts, "/api/preview/rename", `{"id":"`+added.Entry.ID+`","label":"renamed"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("rename: %d", code)
	}
	if code := deleteWithJSON(t, ts, "/api/preview?id="+added.Entry.ID); code != http.StatusOK {
		t.Fatalf("delete: %d", code)
	}
	got = pvList{}
	getJSON(t, ts, "/api/preview", &got)
	if len(got.Entries) != 0 {
		t.Fatal("not removed")
	}
}

func TestPreviewDiffOnceAllowlistsNames(t *testing.T) {
	dir := previewRepo(t)
	ts := previewServe(t, dir)
	var open struct{ State, Left, Right string }
	if code := getJSON(t, ts, "/api/preview/diff?source=feat/x&target=main", &open); code != http.StatusOK || open.State != "ok" {
		t.Fatalf("diff = %d %+v", code, open)
	}
	if code := getJSON(t, ts, "/api/preview/diff?source=nope&target=main", nil); code != http.StatusNotFound {
		t.Fatalf("unknown name = %d, want 404", code)
	}
	if code := getJSON(t, ts, "/api/preview/diff?source=--exec&target=main", nil); code != http.StatusBadRequest {
		t.Fatalf("flag-shaped name = %d, want 400", code)
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"nope","target":"main"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("add unknown = %d", code)
	}
}

func TestPreviewUISectionPersists(t *testing.T) {
	t.Parallel()
	if !strings.Contains(strings.Join(uiSections, ","), "previews") {
		t.Fatal("previews must be an allowlisted sidebar section or its fold state is dropped")
	}
}
