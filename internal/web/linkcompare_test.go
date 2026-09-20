package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/repos"
)

// linkRepo: main holds a.txt + b.txt; feat/x edits both. Returns the dir and
// the two tips; the checkout is left on main.
func linkRepo(t *testing.T) (dir, mainSha, featSha string) {
	t.Helper()
	isolateState(t)
	dir = newRepoDir(t, 1)
	write := func(body string) {
		for _, f := range []string{"a.txt", "b.txt"} {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	write("one\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "base")
	mainSha = gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "-b", "feat/x")
	write("two\n")
	gitRun(t, dir, "commit", "-am", "edit both")
	featSha = gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	return dir, mainSha, featSha
}

// linkServer wires a Server with its own saved-compare store and its OWN
// registry file — never the user's.
func linkServer(t *testing.T, dir string) (*Server, *domain.Service) {
	t.Helper()
	svc := domain.Open(dir)
	svc.UsePreviewsDir(t.TempDir())
	srv := New(svc)
	srv.reposPath = filepath.Join(t.TempDir(), "repos.toml")
	return srv, svc
}

func linkServe(t *testing.T, dir string) *httptest.Server {
	t.Helper()
	srv, _ := linkServer(t, dir)
	return serve(t, srv)
}

// localLink renders a LOCAL-form link: the checkout and the file sit
// undivided in the repo half, which only a locate can split.
func localLink(dir, path string, tg model.LinkTarget) string {
	abs := filepath.ToSlash(dir)
	if path != "" {
		abs += "/" + path
	}
	return model.Link{Repo: model.LinkRepo{Abs: abs}, Target: tg}.String()
}

func commitTarget(sha string) model.LinkTarget {
	return model.LinkTarget{State: model.StateCommitted, Commit: sha}
}

type linkCmpFile struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	OldPath   string `json:"old_path"`
	LeftSpec  string `json:"left_spec"`
	RightSpec string `json:"right_spec"`
}

type linkCmp struct {
	Left, Right struct{ Text, Desc, Spec string }
	Label       string
	Files       []linkCmpFile
	Error, Side string
}

// getAny GETs path and decodes the body WHATEVER the status: the error body
// (error + side) is half of this route's contract.
func getAny(t *testing.T, ts *httptest.Server, path string, out any) int {
	t.Helper()
	resp, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("%s: decode: %v", path, err)
		}
	}
	return resp.StatusCode
}

func cmpURL(l, r string) string {
	return "/api/compare-links?" + url.Values{"left": {l}, "right": {r}}.Encode()
}

// F4 for the web: a LOCAL-form FILE link must narrow to one file. Evaluated
// without a locate it reads as the whole tree and lists two.
func TestCompareLinksNarrowsALocalFormFileLink(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	ts := linkServe(t, dir)
	var got linkCmp
	code := getAny(t, ts, cmpURL(localLink(dir, "a.txt", commitTarget(mainSha)), localLink(dir, "", commitTarget(featSha))), &got)
	if code != http.StatusOK || len(got.Files) != 1 || got.Files[0].Path != "a.txt" || got.Files[0].Status != "M" {
		t.Fatalf("code=%d err=%q files=%+v, want exactly M a.txt", code, got.Error, got.Files)
	}
	if got.Left.Spec != "commit:"+mainSha || got.Right.Spec != "commit:"+featSha {
		t.Fatalf("specs = %q / %q", got.Left.Spec, got.Right.Spec)
	}
	if got.Left.Desc == "" || got.Left.Text == "" || got.Right.Desc == "" {
		t.Fatalf("a side carries its text and description: %+v / %+v", got.Left, got.Right)
	}
	if got.Files[0].LeftSpec != "" || got.Files[0].RightSpec != "" {
		t.Errorf("a member read from its side's own endpoint carries no spec of its own: %+v", got.Files[0])
	}
}

// One fixture, two arms that must DISAGREE: the bad link moves, side follows.
func TestCompareLinksNamesTheFailingSide(t *testing.T) {
	dir, mainSha, _ := linkRepo(t)
	ts := linkServe(t, dir)
	good, bad := localLink(dir, "", commitTarget(mainSha)), "gg://r@not a target"
	for _, c := range []struct{ l, r, side string }{{bad, good, "left"}, {good, bad, "right"}} {
		var got linkCmp
		if code := getAny(t, ts, cmpURL(c.l, c.r), &got); code != http.StatusBadRequest || got.Side != c.side || got.Error == "" {
			t.Errorf("%s: code=%d side=%q err=%q", c.side, code, got.Side, got.Error)
		}
		if strings.HasPrefix(got.Error, c.side+":") {
			t.Errorf("the side rides its own field, not the message: %q", got.Error)
		}
	}
}

// The web passes a registry (F10), so the cross-repository refusal is
// reachable here — unlike MCP. Without the registry the same link is merely
// unknown: the two arms must differ.
func TestCompareLinksRefusesAnotherCheckout(t *testing.T) {
	dir, mainSha, _ := linkRepo(t)
	other, otherSha, _ := linkRepo(t)
	srv, _ := linkServer(t, dir)
	ts := serve(t, srv)
	u := cmpURL(localLink(dir, "", commitTarget(mainSha)), localLink(other, "", commitTarget(otherSha)))

	var before linkCmp
	if code := getAny(t, ts, u, &before); code != http.StatusUnprocessableEntity || strings.Contains(before.Error, "cross-repository") {
		t.Fatalf("fixture: unregistered, code=%d err=%q — want 422 and NOT the cross-repository refusal", code, before.Error)
	}
	if err := repos.Touch(srv.reposPath, other, "", time.Unix(9000, 0)); err != nil {
		t.Fatal(err)
	}
	var got linkCmp
	code := getAny(t, ts, u, &got)
	if code != http.StatusUnprocessableEntity || got.Side != "right" || !strings.Contains(got.Error, "cross-repository") {
		t.Fatalf("code=%d side=%q err=%q", code, got.Side, got.Error)
	}
}

func TestCompareLinksWantsExactlyOneForm(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	ts := linkServe(t, dir)
	// Two VALID links, so only the form guard can answer 400: with garbage
	// here the grammar would refuse the request whether or not the guard ran.
	ok := url.Values{"left": {localLink(dir, "", commitTarget(mainSha))}, "right": {localLink(dir, "", commitTarget(featSha))}}
	if code := getAny(t, ts, "/api/compare-links?"+ok.Encode(), nil); code != http.StatusOK {
		t.Fatalf("fixture: the two links alone must compare, got %d", code)
	}
	with := func(k, v string) string {
		q := url.Values{}
		for key, vals := range ok {
			q[key] = vals
		}
		q.Set(k, v)
		return q.Encode()
	}
	for _, q := range []string{"", "left=" + url.QueryEscape(ok.Get("left")), with("id", "x"), with("a", mainSha), with("b", featSha)} {
		if code := getAny(t, ts, "/api/compare-links?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%q: %d, want 400", q, code)
		}
	}
}

// A kind the diff-spec vocabulary cannot spell is a server bug, never an
// empty `commit:` on the wire (commitEntrySide's rule).
func TestLinkSideSpecPerKind(t *testing.T) {
	t.Parallel()
	if _, err := linkSideSpec(model.Endpoint{}); err == nil {
		t.Error("the invalid endpoint must be refused")
	}
	sha := strings.Repeat("a", 40)
	if got, err := linkSideSpec(mustCommitEndpoint(sha)); err != nil || got != "commit:"+sha {
		t.Errorf("commit: %q, %v", got, err)
	}
}
