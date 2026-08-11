package web

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// isolateIdentity points the global git config and the gg profile state dir
// into the test's tempdir: an identity test must never read the developer's
// real ~/.gitconfig (machine-dependent asserts) nor write to it (a global
// apply). Returns the isolated global gitconfig path.
func isolateIdentity(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	gc := filepath.Join(dir, "gitconfig")
	t.Setenv("GIT_CONFIG_GLOBAL", gc)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return gc
}

type identityWire struct {
	Identity struct {
		GlobalName     string `json:"global_name"`
		GlobalEmail    string `json:"global_email"`
		GlobalSet      bool   `json:"global_set"`
		LocalName      string `json:"local_name"`
		LocalEmail     string `json:"local_email"`
		LocalSet       bool   `json:"local_set"`
		EffectiveName  string `json:"effective_name"`
		EffectiveEmail string `json:"effective_email"`
	} `json:"identity"`
	Profiles []struct {
		ID       string `json:"id"`
		Name     string `json:"name"`
		GitName  string `json:"git_name"`
		GitEmail string `json:"git_email"`
		Scope    string `json:"scope"`
	} `json:"profiles"`
}

func getIdentity(t *testing.T, ts *httptest.Server) identityWire {
	t.Helper()
	var got identityWire
	if code := getJSON(t, ts, "/api/identity", &got); code != http.StatusOK {
		t.Fatalf("GET /api/identity code = %d", code)
	}
	return got
}

func TestIdentityGet(t *testing.T) {
	gc := isolateIdentity(t)
	if err := os.WriteFile(gc, []byte("[user]\n\tname = Glo Bal\n\temail = glo@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := newRepoDir(t, 1)
	gitRun(t, dir, "config", "user.name", "Lo Cal")
	gitRun(t, dir, "config", "user.email", "lo@example.com")
	ts := serve(t, New(domain.Open(dir)))

	got := getIdentity(t, ts)
	id := got.Identity
	if !id.GlobalSet || id.GlobalName != "Glo Bal" || id.GlobalEmail != "glo@example.com" {
		t.Errorf("global = %+v", id)
	}
	if !id.LocalSet || id.LocalName != "Lo Cal" || id.LocalEmail != "lo@example.com" {
		t.Errorf("local = %+v", id)
	}
	// local wins the effective merge
	if id.EffectiveName != "Lo Cal" || id.EffectiveEmail != "lo@example.com" {
		t.Errorf("effective = %+v", id)
	}
	if len(got.Profiles) != 0 {
		t.Errorf("profiles not empty on a fresh state dir: %+v", got.Profiles)
	}
}

// TestIdentityGetInheritsGlobal: a repo with no local identity reports
// local_set=false while the effective row carries the global value — the
// client renders the "inherits global" note from the raw flags.
func TestIdentityGetInheritsGlobal(t *testing.T) {
	gc := isolateIdentity(t)
	if err := os.WriteFile(gc, []byte("[user]\n\tname = Glo Bal\n\temail = glo@example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	id := getIdentity(t, ts).Identity
	if id.LocalSet {
		t.Errorf("local_set = true with no repo identity: %+v", id)
	}
	if !id.GlobalSet || id.EffectiveName != "Glo Bal" {
		t.Errorf("effective should inherit global: %+v", id)
	}
}

func postProfile(t *testing.T, ts *httptest.Server, body string) int {
	t.Helper()
	return postJSON(t, ts, "/api/profiles", body, "application/json", "", nil)
}

func TestProfilesAddRenameRemove(t *testing.T) {
	isolateIdentity(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	if code := postProfile(t, ts, `{"name":"Work","git_name":"W Orker","git_email":"w@corp.example","scope":"global"}`); code != http.StatusOK {
		t.Fatalf("add global code = %d", code)
	}
	if code := postProfile(t, ts, `{"name":"OSS","git_name":"O Ss","git_email":"o@oss.example","scope":"repo"}`); code != http.StatusOK {
		t.Fatalf("add repo code = %d", code)
	}
	got := getIdentity(t, ts)
	if len(got.Profiles) != 2 {
		t.Fatalf("profiles = %+v", got.Profiles)
	}
	scopes := map[string]string{}
	for _, p := range got.Profiles {
		scopes[p.Name] = p.Scope
	}
	if scopes["Work"] != "global" || scopes["OSS"] != "repo" {
		t.Errorf("scopes = %v", scopes)
	}

	// rename: add-first semantics; the original id goes away
	if code := postProfile(t, ts, `{"name":"Work Corp","git_name":"W Orker","git_email":"w@corp.example","scope":"global","rename_from":"work","rename_scope":"global"}`); code != http.StatusOK {
		t.Fatalf("rename code = %d", code)
	}
	got = getIdentity(t, ts)
	names := make([]string, 0, len(got.Profiles))
	for _, p := range got.Profiles {
		names = append(names, p.Name)
	}
	if len(got.Profiles) != 2 || strings.Contains(strings.Join(names, "|"), "|Work|") {
		t.Fatalf("after rename profiles = %v", names)
	}

	// remove by fresh-read identity (scope+id)
	if code := postJSON(t, ts, "/api/profiles/remove", `{"scope":"repo","id":"oss"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("remove code = %d", code)
	}
	got = getIdentity(t, ts)
	if len(got.Profiles) != 1 || got.Profiles[0].Name != "Work Corp" {
		t.Fatalf("after remove profiles = %+v", got.Profiles)
	}
}

func TestProfilesRefusals(t *testing.T) {
	isolateIdentity(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	bad := []string{
		`{"name":"","git_name":"N","git_email":"e@x","scope":"global"}`,                                          // empty display name
		`{"name":"P","git_name":"","git_email":"e@x","scope":"global"}`,                                          // empty git name
		`{"name":"P","git_name":"N","git_email":"","scope":"global"}`,                                            // empty git email
		`{"name":"P","git_name":"N","git_email":"e@x","scope":"everywhere"}`,                                     // unknown scope
		`{"name":"P","git_name":"N","git_email":"e@x","scope":"global","rename_from":"x","rename_scope":"nope"}`, // bad rename scope
	}
	for i, b := range bad {
		if code := postProfile(t, ts, b); code != http.StatusBadRequest {
			t.Errorf("bad[%d] code = %d, want 400", i, code)
		}
	}
	// nothing was stored by the refused requests
	if got := getIdentity(t, ts); len(got.Profiles) != 0 {
		t.Errorf("refused adds leaked: %+v", got.Profiles)
	}

	if code := postJSON(t, ts, "/api/profiles/remove", `{"scope":"global","id":"ghost"}`, "application/json", "", nil); code != http.StatusNotFound {
		t.Errorf("remove unknown code = %d, want 404", code)
	}
	if code := postJSON(t, ts, "/api/profiles/remove", `{"scope":"weird","id":"x"}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Errorf("remove bad scope code = %d, want 400", code)
	}
}

// TestOpSetIdentityLocal: the repo-scope apply writes .git/config and leaves
// the isolated global file untouched (the both-directions leak assert).
func TestOpSetIdentityLocal(t *testing.T) {
	gc := isolateIdentity(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"set-identity","name":"Re Po","email":"rp@example.com"}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	if got := gitRun(t, dir, "config", "--local", "user.name"); got != "Re Po" {
		t.Errorf("local user.name = %q", got)
	}
	if _, err := os.Stat(gc); err == nil {
		b, _ := os.ReadFile(gc)
		if strings.Contains(string(b), "Re Po") {
			t.Errorf("repo apply leaked into the global config: %s", b)
		}
	}
}

// TestOpSetIdentityGlobal: the global apply lands in GIT_CONFIG_GLOBAL and
// never in the fixture repo's .git/config.
func TestOpSetIdentityGlobal(t *testing.T) {
	gc := isolateIdentity(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	events := readSSE(t, ts, startOpBody(t, ts, `{"op":"set-identity","name":"Glo Bal","email":"gb@example.com","global":true}`), 30*time.Second)
	if done := events[len(events)-1]; done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	b, err := os.ReadFile(gc)
	if err != nil || !strings.Contains(string(b), "Glo Bal") {
		t.Errorf("global config = %q, err %v", b, err)
	}
	local, _ := os.ReadFile(filepath.Join(dir, ".git", "config"))
	if strings.Contains(string(local), "Glo Bal") {
		t.Errorf("global apply leaked into .git/config: %s", local)
	}
}

func TestOpSetIdentityRefusals(t *testing.T) {
	isolateIdentity(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for i, body := range []string{
		`{"op":"set-identity","name":"","email":"e@x"}`,
		`{"op":"set-identity","name":"N","email":""}`,
		`{"op":"set-identity","name":"--upload-pack=evil","email":"e@x"}`,
		`{"op":"set-identity","name":"N","email":"-evil"}`,
	} {
		var got map[string]string
		if code := postJSON(t, ts, "/api/op", body, "application/json", "", &got); code != http.StatusBadRequest {
			t.Errorf("bad[%d] code = %d, want 400", i, code)
		}
	}
}
