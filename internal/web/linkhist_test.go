package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/gittest"
)

// linkHistBody is the shape both /api/linkhist verbs answer with.
type linkHistBody struct {
	Links []struct {
		Link    string `json:"link"`
		Desc    string `json:"desc"`
		Created string `json:"created"`
	} `json:"links"`
}

// linkHistServer is a server on a real repo with an isolated state dir, so a
// test never touches the developer's own link history.
func linkHistServer(t *testing.T) (*httptest.Server, string) {
	t.Helper()
	isolateState(t)
	dir := gittest.BasicRepo(t, "hi\n")
	ts := httptest.NewServer(New(domain.Open(dir)).Handler())
	t.Cleanup(ts.Close)
	return ts, dir
}

func getLinkHist(t *testing.T, ts *httptest.Server) linkHistBody {
	t.Helper()
	resp, err := http.Get(ts.URL + "/api/linkhist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /api/linkhist: %d, want 200", resp.StatusCode)
	}
	var body linkHistBody
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	return body
}

// postLinkHist posts raw JSON and returns the status. origin "" sends no
// Origin header (the ordinary same-page fetch); a non-empty one exercises the
// cross-origin refusal.
func postLinkHist(t *testing.T, ts *httptest.Server, body, origin string) int {
	t.Helper()
	req, err := http.NewRequest("POST", ts.URL+"/api/linkhist", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// TestLinkHistEmptyIsAnArrayNotNull pins the shape the client iterates. A nil
// slice encodes as `null`, which would make `for (const e of body.links)`
// throw on a fresh repo — the state every user starts in.
func TestLinkHistEmptyIsAnArrayNotNull(t *testing.T) {
	ts, _ := linkHistServer(t)
	resp, err := http.Get(ts.URL + "/api/linkhist")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	if got := string(raw["links"]); got != "[]" {
		t.Errorf("links = %s, want [] (an empty ARRAY — null breaks the client's for-of)", got)
	}
}

func TestLinkHistPostThenGetReturnsTheRow(t *testing.T) {
	ts, dir := linkHistServer(t)
	link := "gg://" + dir + "/README.md:1"
	if code := postLinkHist(t, ts, `{"link":`+jsonStr(link)+`,"desc":"file: README.md"}`, ""); code != http.StatusOK {
		t.Fatalf("POST: %d, want 200", code)
	}
	body := getLinkHist(t, ts)
	if len(body.Links) != 1 {
		t.Fatalf("links = %+v, want exactly one row", body.Links)
	}
	if body.Links[0].Link != link {
		t.Errorf("link = %q, want %q", body.Links[0].Link, link)
	}
	if body.Links[0].Desc != "file: README.md" {
		t.Errorf("desc = %q, want %q", body.Links[0].Desc, "file: README.md")
	}
}

// TestLinkHistPostRefusesABadLink: the body is WIRE INPUT, so the link is
// validated here rather than trusted. A malformed link accepted into the ring
// would fail later, at `gg open` time, far from the cause — and it must be a
// 400 (the caller sent something wrong), never a 500.
func TestLinkHistPostRefusesABadLink(t *testing.T) {
	ts, _ := linkHistServer(t)
	for _, tc := range []struct{ name, body string }{
		{"unparseable", `{"link":"not-a-gg-link","desc":"x"}`},
		{"blank", `{"link":"","desc":"x"}`},
		{"missing", `{"desc":"x"}`},
		{"not json", `{`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if code := postLinkHist(t, ts, tc.body, ""); code != http.StatusBadRequest {
				t.Errorf("POST %s: %d, want 400", tc.body, code)
			}
		})
	}
	if got := getLinkHist(t, ts).Links; len(got) != 0 {
		t.Errorf("a refused POST still recorded %+v — the ring must be untouched", got)
	}
}

// TestLinkHistPostRefusesACrossOriginPost pins the writeGuard wrapping. The
// listener is loopback-only, so the threat is not a remote attacker: it is a
// page in the user's own browser POSTing to 127.0.0.1 on whatever port gg web
// happens to hold. Every other mutating route is wrapped; this one must be too.
func TestLinkHistPostRefusesACrossOriginPost(t *testing.T) {
	ts, dir := linkHistServer(t)
	body := `{"link":` + jsonStr("gg://"+dir+"/README.md:1") + `,"desc":"file: README.md"}`
	if code := postLinkHist(t, ts, body, "http://evil.example"); code != http.StatusForbidden {
		t.Errorf("cross-origin POST: %d, want 403", code)
	}
	if got := getLinkHist(t, ts).Links; len(got) != 0 {
		t.Errorf("a refused cross-origin POST still recorded %+v", got)
	}
}

// TestLinkHistPostRefusesAFormContentType is writeGuard's other half: without
// the JSON requirement a plain <form> POST would work, and a form needs no
// CORS preflight, which is what makes a cross-site POST reachable at all.
func TestLinkHistPostRefusesAFormContentType(t *testing.T) {
	ts, dir := linkHistServer(t)
	req, err := http.NewRequest("POST", ts.URL+"/api/linkhist",
		strings.NewReader(`{"link":`+jsonStr("gg://"+dir+"/README.md:1")+`}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnsupportedMediaType {
		t.Errorf("form-encoded POST: %d, want 415", resp.StatusCode)
	}
}

// TestLinkHistDedupsToTop: the ring's identity is the link TEXT, so copying
// the same link twice moves the row rather than adding one, and the newest
// Desc wins. This is linkhist.Store's contract, asserted through the wire so
// the handler cannot quietly bypass it.
func TestLinkHistDedupsToTop(t *testing.T) {
	ts, dir := linkHistServer(t)
	a := "gg://" + dir + "/README.md:1"
	b := "gg://" + dir + "/README.md:2"
	for _, p := range []struct{ link, desc string }{
		{a, "file: README.md"},
		{b, "file: README.md#2"},
		{a, "commit: abc1234 later label"},
	} {
		if code := postLinkHist(t, ts, `{"link":`+jsonStr(p.link)+`,"desc":`+jsonStr(p.desc)+`}`, ""); code != http.StatusOK {
			t.Fatalf("POST %s: %d", p.link, code)
		}
	}
	got := getLinkHist(t, ts).Links
	if len(got) != 2 {
		t.Fatalf("links = %+v, want 2 rows (a re-copy moves, never appends)", got)
	}
	if got[0].Link != a {
		t.Errorf("newest = %q, want the re-copied %q", got[0].Link, a)
	}
	if got[0].Desc != "commit: abc1234 later label" {
		t.Errorf("desc = %q, want the NEWEST label", got[0].Desc)
	}
}

// TestLinkHistRejectsOtherMethods: an unrouted method must 405, not fall
// through to the SPA's index.html (which would look like success to a client).
func TestLinkHistRejectsOtherMethods(t *testing.T) {
	ts, _ := linkHistServer(t)
	req, err := http.NewRequest("DELETE", ts.URL+"/api/linkhist", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("DELETE /api/linkhist: %d, want 405", resp.StatusCode)
	}
}

// jsonStr quotes s as a JSON string — the test links are absolute paths from
// t.TempDir(), which on Windows contain backslashes that must be escaped.
func jsonStr(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		panic(err)
	}
	return string(b)
}
