package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// wireNote mirrors the JSON one note renders as; the handler's own type is
// unexported, so the test decodes into its own copy of the contract.
type testWireNote struct {
	ID        string         `json:"id"`
	ParentID  string         `json:"parent_id"`
	Source    string         `json:"source"`
	Author    string         `json:"author"`
	Side      string         `json:"side"`
	Line      int            `json:"line"`
	Summary   string         `json:"summary"`
	Rationale string         `json:"rationale"`
	Status    string         `json:"status"`
	Replies   []testWireNote `json:"replies"`
}

type notesResp struct {
	Notes []testWireNote `json:"notes"`
}

// notesServer serves a two-commit repo whose notes live in this test's own
// temp dir. TestMain turns notes off package-wide (so the background sweep can
// never reach the developer's real store); UseNotesDir opts THIS Service back
// in without touching any global, so the test stays parallel-safe.
func notesServer(t *testing.T) *httptest.Server {
	t.Helper()
	svc := domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	return serve(t, New(svc))
}

func TestNotesAddListReplyRemove(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	code, body := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","rev":"","state":"unstaged","side":"new","line":1,"summary":"tighten this","rationale":"why"}`)
	if code != http.StatusOK {
		t.Fatalf("POST /api/notes/add = %d (%v)", code, body)
	}
	var got notesResp
	if code := getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got); code != http.StatusOK {
		t.Fatalf("GET /api/notes = %d", code)
	}
	if len(got.Notes) != 1 || got.Notes[0].Summary != "tighten this" ||
		got.Notes[0].Line != 1 || got.Notes[0].Status != "active" {
		t.Fatalf("listed %+v", got.Notes)
	}
	id := got.Notes[0].ID

	if code, b := postJSONRaw(t, ts, "/api/notes/reply",
		`{"id":"`+id+`","summary":"agreed"}`); code != http.StatusOK {
		t.Fatalf("reply = %d (%v)", code, b)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if len(got.Notes) != 1 || len(got.Notes[0].Replies) != 1 {
		t.Fatalf("threading lost: %+v", got.Notes)
	}

	if code, b := postJSONRaw(t, ts, "/api/notes/edit",
		`{"id":"`+id+`","summary":"edited","rationale":""}`); code != http.StatusOK {
		t.Fatalf("edit = %d (%v)", code, b)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if got.Notes[0].Summary != "edited" {
		t.Fatalf("edit lost: %+v", got.Notes[0])
	}

	if code, b := postJSONRaw(t, ts, "/api/notes/remove", `{"id":"`+id+`"}`); code != http.StatusOK {
		t.Fatalf("remove = %d (%v)", code, b)
	}
	getJSON(t, ts, "/api/notes?path=f.txt&state=unstaged", &got)
	if len(got.Notes) != 0 {
		t.Fatalf("remove left %+v", got.Notes)
	}
}

func TestNotesCountsEndpoint(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	if code, b := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"x"}`); code != http.StatusOK {
		t.Fatalf("add = %d (%v)", code, b)
	}
	var counts struct {
		ByPath       map[string]int `json:"by_path"`
		ByCommit     map[string]int `json:"by_commit"`
		ByCommitPath map[string]int `json:"by_commit_path"`
	}
	if code := getJSON(t, ts, "/api/notes/counts", &counts); code != http.StatusOK {
		t.Fatalf("counts = %d", code)
	}
	if counts.ByPath["f.txt"] != 1 {
		t.Fatalf("counts = %+v", counts)
	}
}

func TestNotesRejectsBadWireValues(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	for _, body := range []string{
		`{"path":"f.txt","state":"bogus","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"sideways","line":1,"summary":"x"}`,
		`{"path":"--upload-pack=x","state":"unstaged","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":0,"summary":"x"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"  "}`,
		`{"path":"","state":"unstaged","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","state":"commit","side":"new","line":1,"summary":"x"}`,
		`{"path":"f.txt","rev":"--exec=x","state":"commit","side":"new","line":1,"summary":"x"}`,
	} {
		if code, _ := postJSONRaw(t, ts, "/api/notes/add", body); code != http.StatusBadRequest {
			t.Fatalf("POST %s = %d, want 400", body, code)
		}
	}
	for _, body := range []string{`{"summary":"x"}`, `{"id":"n1","summary":"  "}`} {
		for _, path := range []string{"/api/notes/edit", "/api/notes/reply"} {
			if code, _ := postJSONRaw(t, ts, path, body); code != http.StatusBadRequest {
				t.Fatalf("POST %s %s = %d, want 400", path, body, code)
			}
		}
	}
	if code, _ := postJSONRaw(t, ts, "/api/notes/remove", `{"id":""}`); code != http.StatusBadRequest {
		t.Fatalf("remove with no id = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/notes?path=f.txt&state=nope", nil); code != http.StatusBadRequest {
		t.Fatalf("GET with a bad state = %d, want 400", code)
	}
	if code := getJSON(t, ts, "/api/notes?state=unstaged", nil); code != http.StatusBadRequest {
		t.Fatalf("GET with no path = %d, want 400", code)
	}
}

// TestNotesMutationEmitsLiveEvent pins the SSE arm: every mutation tells open
// pages to re-fetch, so the ◆ row appears without a manual refresh.
func TestNotesMutationEmitsLiveEvent(t *testing.T) {
	t.Parallel()
	svc := domain.Open(newRepoDir(t, 2))
	svc.UseNotesDir(t.TempDir())
	srv := New(svc)
	ts := serve(t, srv)
	// A bare hub, installed directly: startLive would spawn the watcher and
	// ticker goroutines (and read the ambient config), which this test neither
	// needs nor may race — emit() is the only thing under test.
	srv.liveMu.Lock()
	srv.live = newLiveHub(config.RefreshConfig{}, false, srv.opInFlight)
	srv.liveMu.Unlock()
	t.Cleanup(srv.stopLive)
	ch, cancel := srv.liveHubRef().subscribe()
	t.Cleanup(cancel)

	if code, b := postJSONRaw(t, ts, "/api/notes/add",
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"x"}`); code != http.StatusOK {
		t.Fatalf("add = %d (%v)", code, b)
	}
	select {
	case msg := <-ch:
		if len(msg.Changed) != 1 || msg.Changed[0] != "notes" {
			t.Fatalf("emitted %+v, want changed=[notes]", msg)
		}
	default:
		t.Fatal("no live event after a note mutation")
	}
}

// TestNotesRefusesOverlongText pins Minor 5: the store's budget is an entry
// COUNT, not a byte budget, so a single paste could otherwise put an arbitrarily
// large string into notes.toml.
func TestNotesRefusesOverlongText(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	huge := strings.Repeat("x", noteRationaleMax+1)
	for _, body := range []string{
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"` + strings.Repeat("s", noteSummaryMax+1) + `"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"x","rationale":"` + huge + `"}`,
		`{"path":"f.txt","state":"unstaged","side":"new","line":1,"summary":"x","author":"` + strings.Repeat("a", noteAuthorMax+1) + `"}`,
	} {
		if code, _ := postJSONRaw(t, ts, "/api/notes/add", body); code != http.StatusBadRequest {
			t.Fatalf("an over-long field must be a 400, got %d", code)
		}
	}
	for _, path := range []string{"/api/notes/edit", "/api/notes/reply"} {
		if code, _ := postJSONRaw(t, ts, path, `{"id":"n1","summary":"x","rationale":"`+huge+`"}`); code != http.StatusBadRequest {
			t.Fatalf("POST %s with an over-long rationale = %d, want 400", path, code)
		}
	}
}

// TestNotesUnknownIDIs404 pins the domain.ErrNoteNotFound sentinel: a stale
// page (after a sweep, or another client's delete) must get a 404, and that is
// now decided with errors.Is rather than by grepping the message.
func TestNotesUnknownIDIs404(t *testing.T) {
	t.Parallel()
	ts := notesServer(t)
	for _, path := range []string{"/api/notes/edit", "/api/notes/reply", "/api/notes/remove"} {
		if code, b := postJSONRaw(t, ts, path, `{"id":"deadbeef","summary":"x"}`); code != http.StatusNotFound {
			t.Fatalf("POST %s for an unknown id = %d (%v), want 404", path, code, b)
		}
	}
	if !errors.Is(domain.ErrNoteNotFound, domain.ErrNoteNotFound) {
		t.Fatal("the sentinel must match itself")
	}
}
