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

type textTemplatesWire struct {
	Templates []textTemplateRow `json:"templates"`
}

func getTextTemplates(t *testing.T, ts *httptest.Server) []textTemplateRow {
	t.Helper()
	var got textTemplatesWire
	if code := getJSON(t, ts, "/api/text-templates", &got); code != http.StatusOK {
		t.Fatalf("GET /api/text-templates code = %d", code)
	}
	return got.Templates
}

func ttPost(t *testing.T, ts *httptest.Server, path, body string, out any) int {
	t.Helper()
	return postJSON(t, ts, "/api/text-templates"+path, body, "application/json", "", out)
}

func TestTextTemplatesAddListUpdateRemove(t *testing.T) {
	isolatePrefixes(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))

	if got := getTextTemplates(t, ts); got == nil || len(got) != 0 {
		t.Fatalf("a fresh list must be an empty array, got %#v", got)
	}
	var row textTemplateRow
	if code := ttPost(t, ts, "", `{"title":"Note","body":"Hi <user:n> <br> <date>\n","scope":"global"}`, &row); code != http.StatusOK {
		t.Fatalf("add code = %d", code)
	}
	if row.ID != "note" || row.Scope != "global" || len(row.UserLabels) != 1 || row.UserLabels[0] != "n" || len(row.Automatic) != 1 || row.Automatic[0] != "<date>" {
		t.Fatalf("added row = %+v", row)
	}
	if got := getTextTemplates(t, ts); len(got) != 1 || got[0].Body != "Hi <user:n> <br> <date>" {
		t.Fatalf("list = %+v", got)
	}
	if code := ttPost(t, ts, "/update", `{"id":"note","scope":"global","title":"Memo","body":"plain"}`, &row); code != http.StatusOK || row.ID != "memo" || row.UserLabels == nil || len(row.UserLabels) != 0 {
		t.Fatalf("update code = %d row = %+v", code, row)
	}
	if code := ttPost(t, ts, "/remove", `{"id":"memo","scope":"global"}`, nil); code != http.StatusOK {
		t.Fatalf("remove code = %d", code)
	}
	if got := getTextTemplates(t, ts); len(got) != 0 {
		t.Fatalf("after remove = %+v", got)
	}
}

func TestTextTemplatesRefusals(t *testing.T) {
	isolatePrefixes(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	if code := ttPost(t, ts, "", `{"title":"Note","body":"x","scope":"repo"}`, nil); code != http.StatusOK {
		t.Fatalf("seed add code = %d", code)
	}
	for name, c := range map[string]struct {
		path, body string
		want       int
	}{
		"bad scope":          {"", `{"title":"A","body":"x","scope":"nope"}`, http.StatusBadRequest},
		"empty title":        {"", `{"title":" ","body":"x","scope":"repo"}`, http.StatusBadRequest},
		"malformed token":    {"", `{"title":"A","body":"x <seq> y","scope":"repo"}`, http.StatusBadRequest},
		"symbol-only title":  {"", `{"title":"!!!","body":"x","scope":"repo"}`, http.StatusBadRequest},
		"duplicate title":    {"", `{"title":"note","body":"x","scope":"repo"}`, http.StatusConflict},
		"update unknown":     {"/update", `{"id":"zzz","scope":"repo","title":"Z","body":"x"}`, http.StatusNotFound},
		"update other scope": {"/update", `{"id":"note","scope":"global","title":"Z","body":"x"}`, http.StatusNotFound},
		"remove unknown":     {"/remove", `{"id":"zzz","scope":"repo"}`, http.StatusNotFound},
		"render unknown":     {"/render", `{"id":"zzz","scope":"repo"}`, http.StatusNotFound},
		"render id prefix":   {"/render", `{"id":"no","scope":"repo"}`, http.StatusNotFound},
		"take bad name":      {"/take", `{"seq_names":["../x"]}`, http.StatusBadRequest},
	} {
		if code := ttPost(t, ts, c.path, c.body, nil); code != c.want {
			t.Errorf("%s: code = %d, want %d", name, code, c.want)
		}
	}
	// A write without the JSON content type is refused by the guard.
	if code := postJSON(t, ts, "/api/text-templates", `{"title":"B","body":"x","scope":"repo"}`, "text/plain", "", nil); code != http.StatusUnsupportedMediaType {
		t.Errorf("guard: code = %d", code)
	}
	if got := getTextTemplates(t, ts); len(got) != 1 {
		t.Fatalf("a refusal stored something: %+v", got)
	}
}

func TestTextTemplateRenderPeeksTakeBumps(t *testing.T) {
	isolatePrefixes(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	if code := ttPost(t, ts, "", `{"title":"Seq","body":"#<seq:my w:2> <user:n>","scope":"repo"}`, nil); code != http.StatusOK {
		t.Fatalf("add code = %d", code)
	}
	var out struct {
		Text     string   `json:"text"`
		SeqNames []string `json:"seq_names"`
	}
	for i := 0; i < 2; i++ {
		if code := ttPost(t, ts, "/render", `{"id":"seq","scope":"repo","inputs":{"n":"a"}}`, &out); code != http.StatusOK || out.Text != "#01 a" || len(out.SeqNames) != 1 || out.SeqNames[0] != "my w" {
			t.Fatalf("render %d: code %d out %+v", i, code, out)
		}
	}
	// The names come from the stored text, never from the wire: a counter the
	// template does not use cannot be bumped through /take.
	if code := ttPost(t, ts, "/take", `{"id":"seq","scope":"repo","seq_names":["other"]}`, nil); code != http.StatusOK {
		t.Fatalf("take code = %d", code)
	}
	if code := ttPost(t, ts, "/render", `{"id":"seq","scope":"repo","inputs":{"n":"a"}}`, &out); code != http.StatusOK || out.Text != "#02 a" {
		t.Fatalf("after take: code %d out %+v", code, out)
	}
	if code := ttPost(t, ts, "/render", `{"id":"seq","scope":"repo","inputs":{}}`, nil); code != http.StatusBadRequest {
		t.Fatalf("missing input: code = %d", code)
	}
}

// Titles collide by id: the 409 names the id and the title that holds it.
func TestTextTemplateDuplicateNamesTheID(t *testing.T) {
	isolatePrefixes(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	if code := ttPost(t, ts, "", `{"title":"Bug report","body":"x","scope":"repo"}`, nil); code != http.StatusOK {
		t.Fatalf("seed add code = %d", code)
	}
	code, out := postJSONRaw(t, ts, "/api/text-templates", `{"title":"bug-report","body":"x","scope":"repo"}`)
	if code != http.StatusConflict || !strings.Contains(out["error"], `"bug-report"`) || !strings.Contains(out["error"], `"Bug report"`) || strings.Contains(out["error"], "text template:") {
		t.Fatalf("code %d error %q", code, out["error"])
	}
}

// A damaged scope file does not empty the list: the other scope's rows come
// with the reason.
func TestTextTemplatesDamagedScopeStillListsTheOther(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, body := range []string{`{"title":"G","body":"x","scope":"global"}`, `{"title":"R","body":"x","scope":"repo"}`} {
		if code := ttPost(t, ts, "", body, nil); code != http.StatusOK {
			t.Fatalf("seed add code = %d", code)
		}
	}
	var files []string
	_ = filepath.WalkDir(state, func(p string, d os.DirEntry, _ error) error {
		if d != nil && d.Name() == "texttemplates.toml" && filepath.Base(filepath.Dir(p)) == "global" {
			files = append(files, p)
		}
		return nil
	})
	if len(files) != 1 {
		t.Fatalf("global store file not found: %v", files)
	}
	if err := os.WriteFile(files[0], []byte("[[templates]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out struct {
		Templates []textTemplateRow `json:"templates"`
		Error     string            `json:"error"`
	}
	if code := getJSON(t, ts, "/api/text-templates", &out); code != http.StatusOK {
		t.Fatalf("list code = %d", code)
	}
	if len(out.Templates) != 1 || out.Templates[0].Title != "R" || !strings.Contains(out.Error, "damaged") {
		t.Fatalf("list = %+v", out)
	}
}
