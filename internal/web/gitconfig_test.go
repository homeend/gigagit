package web

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// configRepo builds a repo with the developer's real home ISOLATED: a global
// config write from this package must never touch ~/.gitconfig. HOME is what
// git reads on unix, USERPROFILE on Windows; both are set so the test is
// hermetic on either.
func configRepo(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// The package-wide gittest.Isolate() pins GIT_CONFIG_GLOBAL to a shared
	// file; global writes from THIS test must land in its own temp home.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(home, ".gitconfig"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	return newRepoDir(t, 1)
}

type gitConfigListResp struct {
	Catalog []gitConfigRow `json:"catalog"`
	Extra   []gitConfigRow `json:"extra"`
}

func configList(t *testing.T, ts *httptest.Server) gitConfigListResp {
	t.Helper()
	var out gitConfigListResp
	if code := getJSON(t, ts, "/api/gitconfig", &out); code != http.StatusOK {
		t.Fatalf("GET /api/gitconfig = %d", code)
	}
	return out
}

func findConfigRow(rows []gitConfigRow, key string) *gitConfigRow {
	for i := range rows {
		if rows[i].Key == key {
			return &rows[i]
		}
	}
	return nil
}

func TestGitConfigListCarriesCatalogFacts(t *testing.T) {
	dir := configRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	body := configList(t, ts)
	if len(body.Catalog) < 40 {
		t.Fatalf("catalog has %d rows, want the curated table", len(body.Catalog))
	}
	// A bool with a known git default, unset in this fixture.
	row := findConfigRow(body.Catalog, "fetch.prune")
	if row == nil {
		t.Fatal("fetch.prune missing from the catalog")
	}
	if row.Kind != "bool" || row.Default != "false" || row.Desc == "" {
		t.Errorf("fetch.prune = %+v, want kind=bool default=false with a description", *row)
	}
	if row.LocalSet || row.GlobalSet {
		t.Errorf("fetch.prune reads as set in a fresh repo: %+v", *row)
	}
	if row.Effective != "false" || row.Scope != "default" {
		t.Errorf("unset key = %q from %q, want the documented default", row.Effective, row.Scope)
	}
	if !row.Editable {
		t.Error("a catalog key must be editable")
	}
	// An enum carries its options, or the browser has no picker to show.
	if e := findConfigRow(body.Catalog, "diff.algorithm"); e == nil || len(e.Options) < 2 {
		t.Errorf("diff.algorithm = %+v, want an option list", e)
	}
}

func TestGitConfigListReportsScope(t *testing.T) {
	dir := configRepo(t)
	gitRun(t, dir, "config", "--local", "fetch.prune", "true")
	ts := serve(t, New(domain.Open(dir)))

	row := findConfigRow(configList(t, ts).Catalog, "fetch.prune")
	if row == nil {
		t.Fatal("fetch.prune missing")
	}
	if !row.LocalSet || row.Local != "true" {
		t.Fatalf("local value = %+v", *row)
	}
	if row.Effective != "true" || row.Scope != "repo" {
		t.Errorf("effective = %q from %q, want true from repo", row.Effective, row.Scope)
	}
}

// A key git knows about but the curated catalog does not is REPORTED (so the
// explorer tells the truth about the repo) but not editable from the browser.
func TestGitConfigListSeparatesUncuratedKeys(t *testing.T) {
	dir := configRepo(t)
	gitRun(t, dir, "config", "--local", "alias.lg", "log --oneline")
	ts := serve(t, New(domain.Open(dir)))

	body := configList(t, ts)
	if findConfigRow(body.Catalog, "alias.lg") != nil {
		t.Error("alias.lg must not appear in the editable catalog")
	}
	row := findConfigRow(body.Extra, "alias.lg")
	if row == nil {
		t.Fatal("alias.lg missing from the extra list")
	}
	if row.Editable {
		t.Error("an uncurated key must not be editable")
	}
	if row.Effective != "log --oneline" {
		t.Errorf("extra value = %q", row.Effective)
	}
}

// setConfig posts a write and drains its op, returning the terminal event.
func setConfig(t *testing.T, ts *httptest.Server, body string) wireEvent {
	t.Helper()
	var out struct {
		OpID string `json:"op_id"`
	}
	if code := postJSON(t, ts, "/api/gitconfig", body, "application/json", "", &out); code != http.StatusAccepted {
		t.Fatalf("POST /api/gitconfig %s = %d", body, code)
	}
	events := readSSE(t, ts, out.OpID, 20*time.Second)
	return events[len(events)-1]
}

func TestGitConfigSetWritesChosenScope(t *testing.T) {
	dir := configRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	done := setConfig(t, ts, `{"key":"fetch.prune","value":"true"}`)
	if done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "config", "--local", "fetch.prune"); got != "true" {
		t.Errorf("local fetch.prune = %q", got)
	}

	// The global scope is the OTHER file, and the repo's value must stay put.
	done = setConfig(t, ts, `{"key":"diff.algorithm","value":"histogram","global":true}`)
	if done["ok"] != true {
		t.Fatalf("global write done = %v", done)
	}
	if got := gitRun(t, dir, "config", "--global", "diff.algorithm"); got != "histogram" {
		t.Errorf("global diff.algorithm = %q", got)
	}
	// `git config --get` EXITS 1 on a missing key, which gitRun turns into a
	// test failure — so absence is asserted from the local file's listing.
	if out := gitRun(t, dir, "config", "--local", "--list"); strings.Contains(out, "diff.algorithm") {
		t.Errorf("global write leaked into the repo:\n%s", out)
	}
}

func TestGitConfigUnset(t *testing.T) {
	dir := configRepo(t)
	gitRun(t, dir, "config", "--local", "fetch.prune", "true")
	ts := serve(t, New(domain.Open(dir)))

	if done := setConfig(t, ts, `{"key":"fetch.prune","unset":true}`); done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if row := findConfigRow(configList(t, ts).Catalog, "fetch.prune"); row == nil || row.LocalSet {
		t.Errorf("fetch.prune still set after unset: %+v", row)
	}
}

// The security boundary: a key the catalog does not know is refused before any
// write, whatever the value. This is what keeps a loopback page from setting
// core.pager or credential.helper — arbitrary execution the next time git runs.
func TestGitConfigSetRefusesKeyOffTheWire(t *testing.T) {
	dir := configRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"key":"alias.pwn","value":"!sh -c 'echo owned'"}`,
		`{"key":"core.gitproxy","value":"/tmp/evil"}`,
		`{"key":"","value":"x"}`,
	} {
		if code := postJSON(t, ts, "/api/gitconfig", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, code)
		}
	}
	if out := gitRun(t, dir, "config", "--local", "--list"); strings.Contains(out, "alias.pwn") {
		t.Fatalf("refused key was written anyway:\n%s", out)
	}
}

// A closed value set is enforced too: a bool that is not a bool, an enum
// outside its options.
func TestGitConfigSetRefusesValueOutsideClosedSet(t *testing.T) {
	dir := configRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{"key":"fetch.prune","value":"yes please"}`,
		`{"key":"diff.algorithm","value":"nonesuch"}`,
	} {
		if code := postJSON(t, ts, "/api/gitconfig", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400", body, code)
		}
	}
	// A free-text key still takes free text.
	if done := setConfig(t, ts, `{"key":"core.editor","value":"vim -f"}`); done["ok"] != true {
		t.Fatalf("string key refused: %v", done)
	}
}

// The catalog's spelling is what reaches git: a request may not vary the case
// to make the stored key something other than the curated one.
func TestGitConfigSetNormalisesKeyToCatalogSpelling(t *testing.T) {
	dir := configRepo(t)
	ts := serve(t, New(domain.Open(dir)))

	if done := setConfig(t, ts, `{"key":"FETCH.PRUNE","value":"true"}`); done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "config", "--local", "fetch.prune"); got != "true" {
		t.Errorf("fetch.prune = %q, want the catalog key written", got)
	}
}
