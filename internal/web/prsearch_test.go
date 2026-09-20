package web

import (
	"errors"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

type prSearchResp struct {
	Query *struct {
		Text  string `json:"text"`
		State string `json:"state"`
	} `json:"query"`
	PRs []struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		Fetched bool   `json:"fetched"`
	} `json:"prs"`
	More bool `json:"more"`
}

func searchForge() *fakeForge {
	merged := openPR(5, "Fix login redirect")
	merged.State, merged.Updated = model.PRStateMerged, time.Unix(1_600_000_500, 0).UTC()
	closed := openPR(3, "Drop legacy login")
	closed.State, closed.Updated = model.PRStateClosed, time.Unix(1_600_000_300, 0).UTC()
	return &fakeForge{open: []model.PullRequest{openPR(7, "Add a thing")},
		search: []model.PullRequest{closed, merged}, searchMore: true}
}

func (f *fakeForge) searches() []domain.PRQuery {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.PRQuery(nil), f.searchCalls...)
}

// R2: the GET is the session's last answer and never reaches the forge; the
// POST is the one call that does.
func TestPRSearchGetNeverCallsTheForge(t *testing.T) {
	t.Parallel()
	f := searchForge()
	ts, _ := prServe(t, newRepoDir(t, 1), f)
	waitPRsLoaded(t, ts)
	var out prSearchResp
	if code := getJSON(t, ts, "/api/pr/search", &out); code != 200 || out.Query != nil || out.PRs == nil || len(out.PRs) != 0 {
		t.Fatalf("GET before any search = %d %+v", code, out)
	}
	if n := len(f.searches()); n != 0 {
		t.Fatalf("a GET searched the forge (%d calls)", n)
	}
	if code := postJSON(t, ts, "/api/pr/search", `{"text":" login ","state":"merged"}`, "application/json", "", &out); code != 200 {
		t.Fatalf("POST = %d", code)
	}
	want := domain.PRQuery{State: domain.PRStateFilterMerged, Text: "login", Limit: domain.PRSearchDefaultLimit}
	if calls := f.searches(); len(calls) != 1 || calls[0] != want {
		t.Fatalf("provider calls = %+v", calls)
	}
	check := func(what string, out prSearchResp) {
		t.Helper()
		if out.Query == nil || out.Query.Text != "login" || out.Query.State != "merged" || !out.More ||
			len(out.PRs) != 2 || out.PRs[0].Number != 5 || out.PRs[0].State != "merged" || out.PRs[1].Number != 3 || out.PRs[0].Fetched {
			t.Fatalf("%s = %+v", what, out)
		}
	}
	check("POST", out)
	var again prSearchResp
	getJSON(t, ts, "/api/pr/search", &again)
	check("GET after", again)
	if n := len(f.searches()); n != 1 {
		t.Fatalf("the GET after spent a forge call (%d)", n)
	}
}

func TestPRSearchRefusals(t *testing.T) {
	t.Parallel()
	f := searchForge()
	ts, _ := prServe(t, newRepoDir(t, 1), f)
	waitPRsLoaded(t, ts)
	if code := postJSON(t, ts, "/api/pr/search", `{}`, "application/json", "https://evil.example", nil); code != 403 {
		t.Errorf("cross-origin = %d, want 403", code)
	}
	if code := postJSON(t, ts, "/api/pr/search", `{}`, "text/plain", "", nil); code != 415 {
		t.Errorf("non-JSON = %d, want 415", code)
	}
	if code := postJSON(t, ts, "/api/pr/search", `{"state":"draft"}`, "application/json", "", nil); code != 400 {
		t.Errorf("bad state = %d, want 400", code)
	}
	if n := len(f.searches()); n != 0 {
		t.Fatalf("a refused search reached the forge (%d)", n)
	}
	f.mu.Lock()
	f.searchErr = errors.New("HTTP 502")
	f.mu.Unlock()
	if code := postJSON(t, ts, "/api/pr/search", `{}`, "application/json", "", nil); code != 502 {
		t.Errorf("forge failure = %d, want 502", code)
	}
	noForge, _ := prServe(t, newRepoDir(t, 1), &fakeForge{detect: errors.New("no gh")})
	if code := postJSON(t, noForge, "/api/pr/search", `{}`, "application/json", "", nil); code != 404 {
		t.Errorf("no forge = %d, want 404", code)
	}
}

// R1: a searched-only pull request opens by its NUMBER — the polled list never
// had it, the search result is what vouches for it.
func TestPROpenKnowsASearchedRow(t *testing.T) {
	t.Parallel()
	ts, _ := prServe(t, newRepoDir(t, 1), searchForge())
	waitPRsLoaded(t, ts)
	if code := getJSON(t, ts, "/api/pr/open?n=5", nil); code != 404 {
		t.Fatalf("before the search #5 = %d, want 404", code)
	}
	postJSON(t, ts, "/api/pr/search", `{}`, "application/json", "", nil)
	var out struct {
		State string `json:"state"`
		PR    int    `json:"pr"`
		Label string `json:"label"`
	}
	if code := getJSON(t, ts, "/api/pr/open?n=5", &out); code != 200 || out.State != "unfetched" || out.PR != 5 || out.Label != "PR #5 · Fix login redirect" {
		t.Fatalf("searched #5 = %d %+v", code, out)
	}
	if code := getJSON(t, ts, "/api/pr/open?n=99", nil); code != 404 {
		t.Errorf("an unknown number = %d, want 404", code)
	}
}
