package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// isolatePrefixes points the gg state dir into the tempdir BEFORE the first
// service call (the prefix stores cache on first use, like profiles).
func isolatePrefixes(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
}

type prefixesWire struct {
	Prefixes []struct {
		ID         string   `json:"id"`
		Value      string   `json:"value"`
		Scope      string   `json:"scope"`
		UserLabels []string `json:"user_labels"`
	} `json:"prefixes"`
}

func getPrefixes(t *testing.T, ts *httptest.Server) prefixesWire {
	t.Helper()
	var got prefixesWire
	if code := getJSON(t, ts, "/api/prefixes", &got); code != http.StatusOK {
		t.Fatalf("GET /api/prefixes code = %d", code)
	}
	return got
}

func resolvePrefix(t *testing.T, ts *httptest.Server, body string) (int, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest("POST", ts.URL+"/api/prefixes/resolve", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestPrefixesAddListRemove(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if got := getPrefixes(t, ts); len(got.Prefixes) != 0 {
		t.Fatalf("fresh list = %+v", got.Prefixes)
	}
	if code := postJSON(t, ts, "/api/prefixes", `{"value":"feat/<user:ticket>-","scope":"global"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add global code = %d", code)
	}
	if code := postJSON(t, ts, "/api/prefixes", `{"value":"wt/<seq:ctr:3>/","scope":"repo"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("add repo code = %d", code)
	}
	got := getPrefixes(t, ts)
	if len(got.Prefixes) != 2 {
		t.Fatalf("list = %+v", got.Prefixes)
	}
	byVal := map[string]int{}
	for i, p := range got.Prefixes {
		byVal[p.Value] = i
	}
	g := got.Prefixes[byVal["feat/<user:ticket>-"]]
	if g.Scope != "global" || len(g.UserLabels) != 1 || g.UserLabels[0] != "ticket" {
		t.Errorf("global row = %+v", g)
	}
	r := got.Prefixes[byVal["wt/<seq:ctr:3>/"]]
	if r.Scope != "repo" || len(r.UserLabels) != 0 {
		t.Errorf("repo row = %+v", r)
	}

	if code := postJSON(t, ts, "/api/prefixes/remove", `{"scope":"repo","id":"`+r.ID+`"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("remove code = %d", code)
	}
	if got := getPrefixes(t, ts); len(got.Prefixes) != 1 || got.Prefixes[0].Scope != "global" {
		t.Fatalf("after remove = %+v", got.Prefixes)
	}
}

func TestPrefixesRefusals(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	bad := []string{
		`{"value":"","scope":"global"}`,           // empty
		`{"value":"x/<branch>","scope":"global"}`, // forbidden token
		`{"value":"x/<nope:y>","scope":"global"}`, // unknown token
		`{"value":"ok/","scope":"everywhere"}`,    // unknown scope
	}
	for i, b := range bad {
		if code := postJSON(t, ts, "/api/prefixes", b, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("bad[%d] code = %d, want 400", i, code)
		}
	}
	if got := getPrefixes(t, ts); len(got.Prefixes) != 0 {
		t.Errorf("refused adds leaked: %+v", got.Prefixes)
	}
	if code := postJSON(t, ts, "/api/prefixes/remove", `{"scope":"global","id":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("remove unknown code = %d, want 404", code)
	}
}

// TestPrefixResolvePeeks: resolving is a preview — two resolves of a seq
// prefix return the SAME number; user inputs substitute.
func TestPrefixResolvePeeks(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/prefixes", `{"value":"x-<seq:ctr:3>-<user:who>/","scope":"repo"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("add failed")
	}
	id := getPrefixes(t, ts).Prefixes[0].ID
	code, out := resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`","inputs":{"who":"me"}}`)
	if code != http.StatusOK || out["resolved"] != "x-001-me/" {
		t.Fatalf("resolve = %d %v", code, out)
	}
	code, out = resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`","inputs":{"who":"me"}}`)
	if code != http.StatusOK || out["resolved"] != "x-001-me/" {
		t.Fatalf("second resolve = %d %v — resolve must not consume the counter", code, out)
	}

	if code, _ := resolvePrefix(t, ts, `{"scope":"repo","id":"ghost"}`); code != http.StatusNotFound {
		t.Errorf("resolve unknown code = %d, want 404", code)
	}
	if code, _ := resolvePrefix(t, ts, `{"scope":"weird","id":"`+id+`"}`); code != http.StatusBadRequest {
		t.Errorf("resolve bad scope code = %d, want 400", code)
	}
}

// TestCreateBranchBumpsPrefixSeq: a create-branch carrying the picked prefix
// consumes its counters ON SUCCESS — and only then.
func TestCreateBranchBumpsPrefixSeq(t *testing.T) {
	isolatePrefixes(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postJSON(t, ts, "/api/prefixes", `{"value":"n-<seq:ctr:3>","scope":"repo"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatal("add failed")
	}
	id := getPrefixes(t, ts).Prefixes[0].ID
	_, out := resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`"}`)
	name := out["resolved"].(string) // n-001

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"create-branch","name":"`+name+`","prefix_id":"`+id+`","prefix_scope":"repo"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	// success consumed the counter: the next preview moves on
	_, out = resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`"}`)
	if out["resolved"] != "n-002" {
		t.Fatalf("post-create resolve = %v, want n-002", out)
	}

	// a FAILED create (name already exists) must not consume
	events = readSSE(t, ts, startOpBody(t, ts, `{"op":"create-branch","name":"n-001","prefix_id":"`+id+`","prefix_scope":"repo"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] == true {
		t.Fatalf("duplicate create should fail, done = %v", done)
	}
	_, out = resolvePrefix(t, ts, `{"scope":"repo","id":"`+id+`"}`)
	if out["resolved"] != "n-002" {
		t.Fatalf("post-failure resolve = %v — a failed create must not bump", out)
	}

	// an unknown prefix id on create is a clean refusal before any op starts
	var got map[string]string
	if code := postJSON(t, ts, "/api/op", `{"op":"create-branch","name":"z","prefix_id":"ghost","prefix_scope":"repo"}`, "application/json", "", &got); code != http.StatusNotFound {
		t.Errorf("create with unknown prefix code = %d, want 404", code)
	}
}
