package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
)

type savedRow struct {
	ID, Label, Kind, Link, Desc, A, B, State string
	Files                                    int
	Left, Right                              string
	LeftDesc                                 string `json:"left_desc"`
	RightDesc                                string `json:"right_desc"`
}

type savedResp struct {
	Entry        savedRow
	Entries      []savedRow
	Disabled     bool
	Error, ID    string
	Label        string
	Ok           bool
	ErrorMessage string `json:"-"`
}

// postDecode POSTs JSON and decodes the body WHATEVER the status (a 409 carries
// the row that is already there).
func postDecode(t *testing.T, ts *httptest.Server, path, body, contentType string, out any) int {
	t.Helper()
	resp, err := http.Post(ts.URL+path, contentType, strings.NewReader(body))
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

func jsonBody(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSavedCompareAddComparison(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	srv, svc := linkServer(t, dir)
	ts := serve(t, srv)
	left, right := localLink(dir, "", commitTarget(mainSha)), localLink(dir, "a.txt", commitTarget(featSha))
	body := jsonBody(t, map[string]string{"left": left, "right": right, "label": ""})

	var added savedResp
	if code := postDecode(t, ts, "/api/saved-compares", body, "application/json", &added); code != http.StatusOK || added.Entry.Kind != "compare" || added.Entry.ID == "" {
		t.Fatalf("add: %d %+v", code, added)
	}
	// An EMPTY label takes the STORE's default. Ask domain what that is rather
	// than restating the rule: the same add, done directly, in a second store.
	ref := domain.Open(dir)
	ref.UsePreviewsDir(t.TempDir())
	want, err := ref.SavedCompareAdd(context.Background(), left, right, "")
	if err != nil {
		t.Fatal(err)
	}
	if added.Entry.Label != want.Label {
		t.Errorf("label = %q, want the store's default %q", added.Entry.Label, want.Label)
	}
	stored, err := svc.SavedCompareList(context.Background())
	if err != nil || len(stored) != 1 || stored[0].Left != left || stored[0].Right != right {
		t.Fatalf("stored = %+v, %v", stored, err)
	}

	var dup savedResp
	if code := postDecode(t, ts, "/api/saved-compares", body, "application/json", &dup); code != http.StatusConflict || dup.ID != added.Entry.ID || dup.Label != added.Entry.Label {
		t.Errorf("duplicate: %d %+v, want 409 naming the existing row", code, dup)
	}
	if code := postDecode(t, ts, "/api/saved-compares", body, "text/plain", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("writeGuard: %d, want 415", code)
	}
	for name, b := range map[string]string{
		"left alone":    jsonBody(t, map[string]string{"left": left}),
		"right alone":   jsonBody(t, map[string]string{"right": right}),
		"both forms":    jsonBody(t, map[string]string{"left": left, "right": right, "a": mainSha, "b": featSha}),
		"neither form":  `{"label":"x"}`,
		"not json":      `{`,
		"a short sha":   jsonBody(t, map[string]string{"a": mainSha[:9], "b": featSha}),
		"a branch name": jsonBody(t, map[string]string{"a": "main", "b": featSha}),
	} {
		if code := postDecode(t, ts, "/api/saved-compares", b, "application/json", nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
	if code := postDecode(t, ts, "/api/saved-compares", jsonBody(t, map[string]string{"left": "gg://r@not a target", "right": right}), "application/json", nil); code != http.StatusUnprocessableEntity {
		t.Errorf("an unparseable link: %d, want 422", code)
	}
	if got, _ := svc.SavedCompareList(context.Background()); len(got) != 1 {
		t.Errorf("a refused add must store nothing: %+v", got)
	}
}

func TestSavedCompareAddPair(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	srv, svc := linkServer(t, dir)
	ts := serve(t, srv)
	var added savedResp
	body := jsonBody(t, map[string]string{"a": mainSha, "b": featSha})
	if code := postDecode(t, ts, "/api/saved-compares", body, "application/json", &added); code != http.StatusOK || added.Entry.Kind != "pair" || added.Entry.A != mainSha || added.Entry.B != featSha {
		t.Fatalf("add: %d %+v", code, added)
	}
	pairs, err := svc.PairList(context.Background())
	if err != nil || len(pairs) != 1 || pairs[0].ID != added.Entry.ID {
		t.Fatalf("PairList = %+v, %v — the web's pair must be domain's pair", pairs, err)
	}
	var dup savedResp
	if code := postDecode(t, ts, "/api/saved-compares", body, "application/json", &dup); code != http.StatusConflict || dup.ID != added.Entry.ID {
		t.Errorf("duplicate: %d %+v", code, dup)
	}
}

// threeKinds fills ONE store with a merge preview, a commit pair and a
// comparison — the fixture every kind-routing assertion needs, since two
// look-alike arms only prove anything when they must disagree on one store.
// The pair's new side ADDS a file, so swapping its halves flips A to D.
func threeKinds(t *testing.T) (ts *httptest.Server, svc *domain.Service, dir, previewID, pairID, cmpID string) {
	t.Helper()
	dir, mainSha, _ := linkRepo(t)
	gitRun(t, dir, "checkout", "-q", "feat/x")
	write(t, dir, "added.txt", "new\n")
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "commit", "-m", "add a file")
	featSha := gitRun(t, dir, "rev-parse", "HEAD")
	gitRun(t, dir, "checkout", "-q", "main")
	srv, svc := linkServer(t, dir)
	ctx := context.Background()
	p, err := svc.PreviewAdd(ctx, "feat/x", "main", "the preview")
	if err != nil {
		t.Fatal(err)
	}
	pr, err := svc.PairAdd(ctx, mainSha, featSha, "the pair")
	if err != nil {
		t.Fatal(err)
	}
	c, err := svc.SavedCompareAdd(ctx, localLink(dir, "a.txt", commitTarget(mainSha)), localLink(dir, "", refTarget("feat/x")), "the comparison")
	if err != nil {
		t.Fatal(err)
	}
	return serve(t, srv), svc, dir, p.ID, pr.ID, c.ID
}

func labelsOf(t *testing.T, svc *domain.Service) map[string]string {
	t.Helper()
	all, err := svc.SavedCompareList(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, c := range all {
		out[c.ID] = c.Label
	}
	return out
}

func TestSavedComparesListsPairsAndComparisonsNotPreviews(t *testing.T) {
	ts, _, _, previewID, pairID, cmpID := threeKinds(t)
	var got savedResp
	if code := getAny(t, ts, "/api/saved-compares", &got); code != http.StatusOK || len(got.Entries) != 2 {
		t.Fatalf("code=%d entries=%+v, want the pair and the comparison only", code, got.Entries)
	}
	pair, cmp := got.Entries[0], got.Entries[1]
	if pair.Kind != "pair" || pair.ID != pairID || pair.State != "ok" || pair.Files != 3 || !isFullSha(pair.A) || !isFullSha(pair.B) {
		t.Errorf("pair row = %+v", pair)
	}
	// The row's link is what "copy gg link" copies: the stored @a..b link
	// plus the landing hint that reveals this row again.
	if !strings.HasSuffix(pair.Link, "@"+pair.A+".."+pair.B+"?preview="+pair.ID) {
		t.Errorf("pair link = %q, want its stored @a..b link + ?preview=%s", pair.Link, pair.ID)
	}
	if !strings.HasPrefix(pair.Desc, "pair: ") {
		t.Errorf("pair desc = %q, want the saved entry's label form", pair.Desc)
	}
	if cmp.Kind != "compare" || cmp.ID != cmpID || cmp.Left == "" || cmp.Right == "" || cmp.LeftDesc == "" || cmp.RightDesc == "" {
		t.Errorf("comparison row = %+v", cmp)
	}
	for _, e := range got.Entries {
		if e.ID == previewID {
			t.Errorf("a merge preview is /api/preview's row, not this route's: %+v", e)
		}
	}
	// …and the preview is still where it always was.
	var pv pvList
	if getJSON(t, ts, "/api/preview", &pv); len(pv.Entries) != 1 || pv.Entries[0].ID != previewID {
		t.Errorf("/api/preview = %+v", pv.Entries)
	}
}

// A pair whose old side is gone still LISTS, saying so; it must not vanish.
func TestSavedComparesListsAPairWithAMissingSide(t *testing.T) {
	dir, mainSha, featSha := linkRepo(t)
	srv, svc := linkServer(t, dir)
	if _, err := svc.PairAdd(context.Background(), featSha, mainSha, "gone"); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "branch", "-D", "feat/x")
	gitRun(t, dir, "reflog", "expire", "--expire=now", "--all")
	gitRun(t, dir, "gc", "-q", "--prune=now")
	if commitExists(t, dir, featSha) {
		t.Fatal("fixture: the commit survived gc")
	}
	var got savedResp
	if getAny(t, serve(t, srv), "/api/saved-compares", &got); len(got.Entries) != 1 || got.Entries[0].State != "missing-a" {
		t.Fatalf("entries = %+v, want one row in state missing-a", got.Entries)
	}
}

func linkStatusOf(files []linkCmpFile, path string) string {
	if f := fileNamed(files, path); f != nil {
		return f.Status
	}
	return ""
}

func TestCompareLinksByID(t *testing.T) {
	ts, _, _, previewID, pairID, cmpID := threeKinds(t)

	var cmp linkCmp
	if code := getAny(t, ts, "/api/compare-links?id="+cmpID, &cmp); code != http.StatusOK || cmp.Label != "the comparison" {
		t.Fatalf("comparison: %d %+v", code, cmp)
	}
	if len(cmp.Files) != 1 || cmp.Files[0].Path != "a.txt" {
		t.Errorf("comparison files = %+v, want a.txt alone (its left link names one file)", cmp.Files)
	}

	// The pair: the point A against A..B. Swapped halves would read added.txt
	// as D, so the STATUS is asserted, not only the path.
	var pair linkCmp
	if code := getAny(t, ts, "/api/compare-links?id="+pairID, &pair); code != http.StatusOK || pair.Label != "the pair" {
		t.Fatalf("pair: %d %+v", code, pair)
	}
	if len(pair.Files) != 3 || linkStatusOf(pair.Files, "added.txt") != "A" || linkStatusOf(pair.Files, "a.txt") != "M" {
		t.Errorf("pair files = %+v, want M a.txt, M b.txt, A added.txt", pair.Files)
	}

	for name, id := range map[string]string{"a merge preview's id": previewID, "an unknown id": "nope", "a LABEL, not an id": "the+pair"} {
		if code := getAny(t, ts, "/api/compare-links?id="+id, nil); code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404", name, code)
		}
	}
}

// Kind routing on ONE store: each write moves exactly its own row, and a merge
// preview's id — same store, another surface — is not found here.
func TestSavedCompareRenameRemoveRouteByKind(t *testing.T) {
	ts, svc, _, previewID, pairID, cmpID := threeKinds(t)
	rename := func(id, label string) int {
		return postDecode(t, ts, "/api/saved-compares/rename", jsonBody(t, map[string]string{"id": id, "label": label}), "application/json", nil)
	}
	if code := rename(cmpID, "renamed comparison"); code != http.StatusOK {
		t.Fatalf("rename the comparison: %d", code)
	}
	if got := labelsOf(t, svc); got[cmpID] != "renamed comparison" || got[pairID] != "the pair" || got[previewID] != "the preview" {
		t.Fatalf("after renaming the comparison: %+v", got)
	}
	if code := rename(pairID, "renamed pair"); code != http.StatusOK {
		t.Fatalf("rename the pair: %d", code)
	}
	if got := labelsOf(t, svc); got[pairID] != "renamed pair" || got[cmpID] != "renamed comparison" || got[previewID] != "the preview" {
		t.Fatalf("after renaming the pair: %+v", got)
	}
	if code := rename(previewID, "hijacked"); code != http.StatusNotFound {
		t.Errorf("rename a merge preview through this surface: %d, want 404", code)
	}
	if code := rename("renamed pair", "by label"); code != http.StatusNotFound {
		t.Errorf("a LABEL used as an id: %d, want 404", code)
	}
	if got := labelsOf(t, svc); got[previewID] != "the preview" || got[pairID] != "renamed pair" {
		t.Fatalf("a refused rename moved a label: %+v", got)
	}
	if code := postDecode(t, ts, "/api/saved-compares/rename", jsonBody(t, map[string]string{"id": cmpID, "label": "x"}), "text/plain", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("rename writeGuard: %d", code)
	}

	if code := deleteJSON(t, ts, "/api/saved-compares?id="+previewID); code != http.StatusNotFound {
		t.Errorf("remove a merge preview through this surface: %d, want 404", code)
	}
	if got := labelsOf(t, svc); len(got) != 3 {
		t.Fatalf("a refused remove deleted something: %+v", got)
	}
	if code := deleteJSON(t, ts, "/api/saved-compares?id="+pairID); code != http.StatusOK {
		t.Fatalf("remove the pair: %d", code)
	}
	if got := labelsOf(t, svc); len(got) != 2 || got[pairID] != "" || got[cmpID] == "" || got[previewID] == "" {
		t.Fatalf("after removing the pair: %+v", got)
	}
	if code := deleteJSON(t, ts, "/api/saved-compares?id="+cmpID); code != http.StatusOK {
		t.Fatalf("remove the comparison: %d", code)
	}
	if got := labelsOf(t, svc); len(got) != 1 || got[previewID] != "the preview" {
		t.Fatalf("after removing the comparison: %+v", got)
	}
	if code := deleteWithJSONless(t, ts, "/api/saved-compares?id="+previewID); code != http.StatusUnsupportedMediaType {
		t.Errorf("remove writeGuard: %d", code)
	}
}

func deleteWithJSONless(t *testing.T, ts *httptest.Server, path string) int {
	t.Helper()
	req, _ := http.NewRequest("DELETE", ts.URL+path, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// W7: a `@a..b` navigate lands through the door. The endpoint-shaped lane
// (/api/compare?revs=1) cannot see a `-u` stash's untracked file; the two
// lanes must DISAGREE on this fixture, or the test sees nothing.
func TestCompareLinksPairFormSeesAStashesUntrackedFile(t *testing.T) {
	dir, stashSha := stashLinkRepo(t)
	ts := linkServe(t, dir)
	parent := gitRun(t, dir, "rev-parse", stashSha+"^1")

	var old struct{ Files []linkCmpFile }
	if code := getJSON(t, ts, "/api/compare?a="+parent+"&b="+stashSha+"&revs=1", &old); code != http.StatusOK {
		t.Fatalf("fixture: /api/compare = %d", code)
	}
	if fileNamed(old.Files, "scratch.txt") != nil {
		t.Fatal("fixture: the endpoint-shaped lane already lists scratch.txt — the lanes do not differ")
	}
	var got linkCmp
	if code := getAny(t, ts, "/api/compare-links?a="+parent+"&b="+stashSha, &got); code != http.StatusOK {
		t.Fatalf("pair form: %d %s", code, got.Error)
	}
	if linkStatusOf(got.Files, "scratch.txt") != "A" || linkStatusOf(got.Files, "a.txt") != "M" {
		t.Errorf("files = %+v, want M a.txt and A scratch.txt", got.Files)
	}
	if fileNamed(got.Files, "b.txt") != nil {
		t.Errorf("b.txt did not change in the stash; a point-vs-point compare would not list it either, but a wrong right side might: %+v", got.Files)
	}
	for name, q := range map[string]string{"a short id": "a=" + parent[:9] + "&b=" + stashSha, "a alone": "a=" + parent, "a name": "a=main&b=" + stashSha} {
		if code := getAny(t, ts, "/api/compare-links?"+q, nil); code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, code)
		}
	}
}
