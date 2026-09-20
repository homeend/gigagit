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
	ID, Label, Kind, Link, A, B, State string
	Files                              int
	Left, Right                        string
	LeftDesc                           string `json:"left_desc"`
	RightDesc                          string `json:"right_desc"`
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
