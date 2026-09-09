package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
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
		Error                            string `json:"error"`
	} `json:"entries"`
}

// fakePreviewSvc builds a domain.Service over a FakeRunner (no real repo
// needed: UsePreviewsDir injects the preview store directly, and every git
// call the test exercises is stubbed below). The handler names come from
// internal/git's span names — see resolve.go, mergebase.go, preview.go.
//
//   - "git rev-parse verify commit (resolve)" resolves feat/x and main to
//     distinct fixed hashes (ResolveRev, called for both source and target).
//   - "git merge-base" and "git rev-list --left-right --count" succeed
//     (ahead=1), so PreviewSummary reaches the files step.
//   - "git diff --name-only (range)" is the injected failure: a transient
//     git error at the LAST step of PreviewSummary, the one case that makes
//     PreviewSummary itself return a non-nil error (a merge-base failure
//     alone maps to the no-base STATE, not a Go error).
func fakePreviewSvc(t *testing.T, diffErr error) *domain.Service {
	t.Helper()
	f := gitexec.NewFakeRunner()
	f.SetHandler("git rev-parse verify commit (resolve)", func(_ context.Context, argv []string) (gitexec.Result, error) {
		ref := strings.TrimSuffix(argv[len(argv)-1], "^{commit}")
		switch ref {
		case "feat/x":
			return gitexec.Result{Stdout: strings.Repeat("a", 40) + "\n"}, nil
		case "main":
			return gitexec.Result{Stdout: strings.Repeat("b", 40) + "\n"}, nil
		}
		return gitexec.Result{}, fmt.Errorf("fake: unknown ref %q", ref)
	})
	f.SetResponse("git merge-base", gitexec.Result{Stdout: strings.Repeat("c", 40) + "\n"})
	f.SetResponse("git rev-list --left-right --count", gitexec.Result{Stdout: "0 1\n"})
	// Minimal Branches()/RemoteBranches() fixtures so handlePreviewAdd's
	// knownRefName allowlist check finds feat/x and main (ParseBranches
	// needs only HEAD\0name\0upstream\0hash).
	f.SetResponse("git for-each-ref", gitexec.Result{
		Stdout: " \x00feat/x\x00\x00" + strings.Repeat("a", 7) + "\n \x00main\x00\x00" + strings.Repeat("b", 7) + "\n",
	})
	f.SetResponse("git for-each-ref (remotes)", gitexec.Result{})
	if diffErr != nil {
		f.SetError("git diff --name-only (range)", diffErr)
	} else {
		f.SetResponse("git diff --name-only (range)", gitexec.Result{Stdout: "a.txt\x00"})
	}
	svc := domain.New(&git.Repo{Runner: f})
	svc.UsePreviewsDir(t.TempDir())
	return svc
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

// TestPreviewRenameRemoveUnknownID: an unknown id must 404 on both mutation
// routes (a stale page after another client's remove), never fall through
// to a 500. The known-id 200 case is already covered by TestPreviewCRUDAndOpen.
func TestPreviewRenameRemoveUnknownID(t *testing.T) {
	dir := previewRepo(t)
	ts := previewServe(t, dir)
	if code := postJSON(t, ts, "/api/preview/rename", `{"id":"nope","label":"x"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Fatalf("rename unknown id: %d, want 404", code)
	}
	if code := deleteWithJSON(t, ts, "/api/preview?id=nope"); code != http.StatusNotFound {
		t.Fatalf("remove unknown id: %d, want 404", code)
	}
}

// TestPreviewListDegradesPerRowOnSummaryError: one pair's PreviewSummary
// failure (a transient git error, not a resolvable state) must degrade that
// row to state:"error" with a message, not 500 the whole list.
func TestPreviewListDegradesPerRowOnSummaryError(t *testing.T) {
	svc := fakePreviewSvc(t, errors.New("boom: transient git failure"))
	if _, err := svc.PreviewAdd(context.Background(), "feat/x", "main", ""); err != nil {
		t.Fatalf("seed add: %v", err)
	}
	ts := serve(t, New(svc))

	var got pvList
	if code := getJSON(t, ts, "/api/preview", &got); code != http.StatusOK {
		t.Fatalf("list: %d, want 200", code)
	}
	if len(got.Entries) != 1 {
		t.Fatalf("entries = %+v, want 1", got.Entries)
	}
	row := got.Entries[0]
	if row.State != "error" || row.Error == "" || row.Files != 0 || row.Ahead != 0 || row.SourceHash != "" {
		t.Fatalf("degraded row = %+v", row)
	}
}

// TestPreviewAddDegradesRowOnSummaryError: the add itself must still succeed
// (200, the record is stored and emitted) even when the immediate summary
// fails — it must never fabricate state:"ok" for a summary that errored.
func TestPreviewAddDegradesRowOnSummaryError(t *testing.T) {
	svc := fakePreviewSvc(t, errors.New("boom: transient git failure"))
	ts := serve(t, New(svc))

	var added struct {
		Entry struct {
			ID, State, Error string
		}
	}
	if code := postJSON(t, ts, "/api/preview", `{"source":"feat/x","target":"main"}`, "application/json", "", &added); code != http.StatusOK {
		t.Fatalf("add: %d, want 200", code)
	}
	if added.Entry.State != "error" || added.Entry.Error == "" {
		t.Fatalf("add entry = %+v", added.Entry)
	}
	if l, err := svc.PreviewList(context.Background()); err != nil || len(l) != 1 {
		t.Fatalf("the record must still be stored: %v, %v", l, err)
	}
}

func TestPreviewUISectionPersists(t *testing.T) {
	t.Parallel()
	if !strings.Contains(strings.Join(uiSections, ","), "previews") {
		t.Fatal("previews must be an allowlisted sidebar section or its fold state is dropped")
	}
}
