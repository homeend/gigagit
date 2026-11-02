package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// isolateGlobal points XDG_CONFIG_HOME (and the state dir via XDG_STATE_HOME/
// HOME on some platforms) into the test's tempdir so settings writes can
// never touch the developer's real global gg config. Returns the global
// config path the handlers will resolve.
func isolateGlobal(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(dir, "gg", "config.toml")
}

type settingsGet struct {
	ShowGraph          bool            `json:"show_graph"`
	CommitSort         string          `json:"commit_sort"`
	AutoRefresh        bool            `json:"auto_refresh"`
	RemoteTagsAuto     bool            `json:"remote_tags_auto"`
	OpLog              bool            `json:"op_log"`
	VersionsEnabled    bool            `json:"versions_enabled"`
	VersionsMaxAgeDays int             `json:"versions_max_age_days"`
	Refresh            map[string]int  `json:"refresh"`
	RefreshWatch       map[string]bool `json:"refresh_watch"`
	WatchSupported     bool            `json:"watch_supported"`
	Hook               string          `json:"hook"`
	RepoConfigPath     string          `json:"repo_config_path"`
	RepoConfigPrivate  bool            `json:"repo_config_private"`
	GlobalConfigPath   string          `json:"global_config_path"`
	CommitGraphKnown   bool            `json:"commit_graph_known"`
}

func getSettings(t *testing.T, ts *httptest.Server) settingsGet {
	t.Helper()
	var got settingsGet
	if code := getJSON(t, ts, "/api/settings", &got); code != http.StatusOK {
		t.Fatalf("GET /api/settings code = %d", code)
	}
	return got
}

func TestSettingsGetDefaults(t *testing.T) {
	global := isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	got := getSettings(t, ts)
	if !got.ShowGraph || got.CommitSort != "date-order" {
		t.Errorf("commits defaults = %+v", got)
	}
	if !got.VersionsEnabled || got.VersionsMaxAgeDays != 90 {
		t.Errorf("versions defaults = %+v", got)
	}
	if !got.RemoteTagsAuto || got.AutoRefresh || got.OpLog {
		t.Errorf("global defaults = %+v", got)
	}
	if got.GlobalConfigPath != global {
		t.Errorf("global path = %q, want the isolated %q", got.GlobalConfigPath, global)
	}
	if got.RepoConfigPath != filepath.Join(dir, ".gg.toml") || got.RepoConfigPrivate {
		t.Errorf("repo config = %q private=%v", got.RepoConfigPath, got.RepoConfigPrivate)
	}
	if len(got.Refresh) != 9 {
		t.Errorf("refresh map = %v, want 9 sources", got.Refresh)
	}
	if !got.CommitGraphKnown {
		t.Errorf("commit-graph health not read")
	}
}

// Per-key file routing: repo keys land in .gg.toml, global keys in the
// isolated global file — never crossed.
func TestSettingsSetRoutesFiles(t *testing.T) {
	global := isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	repoCfg := filepath.Join(dir, ".gg.toml")

	body := `{"show_graph":"off","commit_sort":"plain","versions_enabled":false,"versions_max_age_days":30,` +
		`"refresh":{"status":45},"hook":"echo hi","auto_refresh":true,"remote_tags_auto":false,"op_log":true}`
	if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST code = %d", code)
	}

	repo, err := os.ReadFile(repoCfg)
	if err != nil {
		t.Fatalf("repo config not written: %v", err)
	}
	// The hook lands as a TOML multi-line literal ('''…'''), so assert the
	// key and the script text separately.
	for _, want := range []string{`show_graph = "off"`, `commit_sort = "plain"`, "disabled = true", "max_age_days = 30", "status = 45", "post_create_hook = '''", "echo hi"} {
		if !strings.Contains(string(repo), want) {
			t.Errorf("repo config missing %q:\n%s", want, repo)
		}
	}
	for _, wrong := range []string{"log_operations", "enabled = true", "disable_remote_tags_auto"} {
		if strings.Contains(string(repo), wrong) {
			t.Errorf("GLOBAL key %q leaked into the repo config:\n%s", wrong, repo)
		}
	}

	glob, err := os.ReadFile(global)
	if err != nil {
		t.Fatalf("global config not written: %v", err)
	}
	for _, want := range []string{"log_operations = true", "enabled = true", "disable_remote_tags_auto = true"} {
		if !strings.Contains(string(glob), want) {
			t.Errorf("global config missing %q:\n%s", want, glob)
		}
	}
	for _, wrong := range []string{"show_graph", "commit_sort", "max_age_days", "post_create_hook"} {
		if strings.Contains(string(glob), wrong) {
			t.Errorf("REPO key %q leaked into the global config:\n%s", wrong, glob)
		}
	}

	// And the GET reflects every write.
	got := getSettings(t, ts)
	// The multi-line literal round-trips with a trailing newline; the client
	// textarea is newline-tolerant, so compare trimmed.
	if got.ShowGraph || got.CommitSort != "plain" || got.VersionsEnabled || got.VersionsMaxAgeDays != 30 ||
		got.Refresh["status"] != 45 || strings.TrimRight(got.Hook, "\n") != "echo hi" || !got.AutoRefresh || got.RemoteTagsAuto || !got.OpLog {
		t.Errorf("GET after writes = %+v", got)
	}
}

func TestSettingsSetRefusals(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	for _, body := range []string{
		`{}`,
		`{"show_graph":"maybe"}`,
		`{"commit_sort":"topo"}`,
		`{"versions_max_age_days":0}`,
		`{"versions_max_age_days":-2}`,
		`{"refresh":{"bogus":10}}`,
		`{"refresh":{"status":-5}}`,
	} {
		if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("body %s: code = %d, want 400", body, code)
		}
	}
	// A request with one bad member writes NOTHING (validate-then-write).
	if code := postJSON(t, ts, "/api/settings", `{"show_graph":"off","refresh":{"bogus":10}}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("mixed body code = %d, want 400", code)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gg.toml")); !os.IsNotExist(err) {
		t.Errorf("repo config written despite a refused request")
	}
}

// The versions write is picked up LIVE by the long-running server: deleting
// a branch normally snapshots it under refs/gg/versions first; after
// versions_enabled=false lands through the settings endpoint, the very next
// delete must leave no version ref — without a server restart.
func TestSettingsVersionsPolicyLive(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 2)
	gitRun(t, dir, "branch", "target")
	srv := New(domain.Open(dir))
	ts := serve(t, srv)

	if code := postJSON(t, ts, "/api/settings", `{"versions_enabled":false}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST code = %d", code)
	}
	opID := startOpBody(t, ts, `{"op":"delete-branch","branch":"target"}`)
	run := srv.opByID(opID)
	if run == nil {
		t.Fatal("run not found")
	}
	waitDecision(t, run)
	if code := postJSON(t, ts, "/api/op/"+opID+"/decide", `{"option":"delete"}`, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("decide code = %d", code)
	}
	events := readSSE(t, ts, opID, 30*time.Second)
	done := events[len(events)-1]
	if done["ok"] != true || done["changed"] != true {
		t.Fatalf("done = %v", done)
	}
	if out := gitRun(t, dir, "for-each-ref", "refs/gg/versions"); strings.TrimSpace(out) != "" {
		t.Errorf("version snapshot recorded despite the live disable:\n%s", out)
	}
}

func TestSettingsRefreshWatchRoundTrip(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))

	got := getSettings(t, ts)
	if len(got.RefreshWatch) != 4 || got.RefreshWatch["branches"] {
		t.Fatalf("refresh_watch defaults = %v, want 4 false entries", got.RefreshWatch)
	}
	body := `{"refresh_watch":{"branches":true,"reflog":true}}`
	if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST code = %d", code)
	}
	repo, err := os.ReadFile(filepath.Join(dir, ".gg.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"branches_watch = true", "reflog_watch = true"} {
		if !strings.Contains(string(repo), want) {
			t.Errorf("repo config missing %q:\n%s", want, repo)
		}
	}
	got = getSettings(t, ts)
	if !got.RefreshWatch["branches"] || !got.RefreshWatch["reflog"] || got.RefreshWatch["remotes"] {
		t.Fatalf("refresh_watch after write = %v", got.RefreshWatch)
	}
	if code := postJSON(t, ts, "/api/settings", `{"refresh_watch":{"status":true}}`, "application/json", "", nil); code != http.StatusBadRequest {
		t.Fatalf("ineligible watch source must be 400, got %d", code)
	}
}

// settingsWorkspace is a workspace of a and b, a on screen, with b the settings
// repository.
func settingsWorkspace(t *testing.T) (*Server, string, string) {
	t.Helper()
	isolateGlobal(t)
	a, b := newRepoDir(t, 1), newRepoDir(t, 1)
	srv := New(domain.Open(a))
	t.Cleanup(srv.Close)
	srv.SetWorkspace(&domain.WorkspaceInfo{Name: "shop", SettingsRepo: b, Members: []domain.WorkspaceMember{
		{Path: a, Label: "a", Svc: domain.Open(a)}, {Path: b, Label: "b", Svc: domain.Open(b)},
	}})
	return srv, a, b
}

func fileHolds(t *testing.T, path, want string) bool {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return strings.Contains(string(data), want)
}

func TestSettingsProcessWideGoesToSettingsRepo(t *testing.T) {
	srv, a, b := settingsWorkspace(t)
	if w := uiRequest(srv, http.MethodPost, "/api/settings", `{"commit_sort":"plain"}`); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !fileHolds(t, filepath.Join(b, ".gg.toml"), "plain") {
		t.Fatal("[ui] must reach the settings repository's file")
	}
	if fileHolds(t, filepath.Join(a, ".gg.toml"), "plain") {
		t.Fatal("[ui] must not reach the member on screen")
	}
}

func TestSettingsMemberSectionGoesToLive(t *testing.T) {
	srv, a, b := settingsWorkspace(t)
	if w := uiRequest(srv, http.MethodPost, "/api/settings", `{"refresh":{"status":77}}`); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !fileHolds(t, filepath.Join(a, ".gg.toml"), "77") {
		t.Fatal("[refresh] must reach the member on screen")
	}
	if fileHolds(t, filepath.Join(b, ".gg.toml"), "77") {
		t.Fatal("[refresh] must not reach the settings repository")
	}
}

func TestSettingsReadsSplitTheSameWay(t *testing.T) {
	srv, a, b := settingsWorkspace(t)
	if err := os.WriteFile(filepath.Join(b, ".gg.toml"), []byte("[ui]\ncommit_sort = \"plain\"\n[refresh]\nstatus = 11\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(a, ".gg.toml"), []byte("[refresh]\nstatus = 22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var out settingsPayload
	if err := json.Unmarshal(uiRequest(srv, http.MethodGet, "/api/settings", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.CommitSort != "plain" || out.Refresh["status"] != 22 {
		t.Fatalf("commit sort %q, status %d", out.CommitSort, out.Refresh["status"])
	}
}

func TestSettingsNoWorkspaceUnchanged(t *testing.T) {
	isolateGlobal(t)
	a := newRepoDir(t, 1)
	srv := New(domain.Open(a))
	t.Cleanup(srv.Close)
	if w := uiRequest(srv, http.MethodPost, "/api/settings", `{"commit_sort":"plain"}`); w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
	if !fileHolds(t, filepath.Join(a, ".gg.toml"), "plain") {
		t.Fatal("no workspace: the repository's own file")
	}
	var out settingsPayload
	if err := json.Unmarshal(uiRequest(srv, http.MethodGet, "/api/settings", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.SectionRepos) != 0 {
		t.Fatalf("no workspace names no repository: %v", out.SectionRepos)
	}
}

func TestSettingsSectionsNameRepo(t *testing.T) {
	srv, _, _ := settingsWorkspace(t)
	var out settingsPayload
	if err := json.Unmarshal(uiRequest(srv, http.MethodGet, "/api/settings", "").Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.SectionRepos["commits"] != "b" || out.SectionRepos["refresh"] != "a" || out.SectionRepos["history"] != "a" || out.SectionRepos["branch filters"] != "a" || out.SectionRepos["repo"] != "a" {
		t.Fatalf("%v", out.SectionRepos)
	}
}

type toolbarWire struct {
	Toolbar []string `json:"toolbar"`
}

func TestSettingsToolbarRoundTrip(t *testing.T) {
	global := isolateGlobal(t)
	dir := newRepoDir(t, 1)
	ts := serve(t, New(domain.Open(dir)))
	var got toolbarWire
	if getJSON(t, ts, "/api/repo", &got); got.Toolbar == nil || len(got.Toolbar) != 0 {
		t.Fatalf("unset /api/repo toolbar = %#v, want an empty list, never null", got.Toolbar)
	}
	body := `{"toolbar":["fetch all remotes","view all notes…"]}`
	if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusOK {
		t.Fatalf("POST code = %d", code)
	}
	for _, path := range []string{"/api/repo", "/api/settings"} {
		got = toolbarWire{}
		getJSON(t, ts, path, &got)
		if strings.Join(got.Toolbar, "|") != "fetch all remotes|view all notes…" {
			t.Fatalf("%s toolbar = %q", path, got.Toolbar)
		}
	}
	raw, err := os.ReadFile(global)
	if err != nil || !strings.Contains(string(raw), "toolbar") {
		t.Fatalf("global config: %v\n%s", err, raw)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gg.toml")); err == nil {
		t.Fatal("a toolbar write must not touch the repository's config")
	}
}

func TestSettingsToolbarIgnoresRepoConfig(t *testing.T) {
	isolateGlobal(t)
	dir := newRepoDir(t, 1)
	if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte("[web]\ntoolbar = [\"quit gg…\"]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(dir)))
	for _, path := range []string{"/api/repo", "/api/settings"} {
		var got toolbarWire
		getJSON(t, ts, path, &got)
		if len(got.Toolbar) != 0 {
			t.Fatalf("%s toolbar = %q, a repository must not set it", path, got.Toolbar)
		}
	}
}

func TestSettingsToolbarRefusals(t *testing.T) {
	global := isolateGlobal(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	eleven := `["a","b","c","d","e","f","g","h","i","j","k"]`
	for name, list := range map[string]string{"eleven": eleven, "blank": `["a",""]`, "control": `["a\u0007"]`} {
		if code := postJSON(t, ts, "/api/settings", `{"toolbar":`+list+`}`, "application/json", "", nil); code != http.StatusBadRequest {
			t.Errorf("%s: code = %d, want 400", name, code)
		}
	}
	if _, err := os.Stat(global); err == nil {
		t.Fatal("a refused list wrote the config")
	}
}

func TestSettingsToolbarEmptyClears(t *testing.T) {
	isolateGlobal(t)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	for _, body := range []string{`{"toolbar":["a"]}`, `{"toolbar":[]}`} {
		if code := postJSON(t, ts, "/api/settings", body, "application/json", "", nil); code != http.StatusOK {
			t.Fatalf("%s: code = %d", body, code)
		}
	}
	var got toolbarWire
	getJSON(t, ts, "/api/settings", &got)
	if got.Toolbar == nil || len(got.Toolbar) != 0 {
		t.Fatalf("toolbar = %#v", got.Toolbar)
	}
}

// A hand-edited file is read forgivingly: the first 10, first of a duplicate.
func TestSettingsToolbarNormalisesTheFile(t *testing.T) {
	global := isolateGlobal(t)
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `[web]
toolbar = ["a","a","b","c","d","e","f","g","h","i","j","k"]
`
	if err := os.WriteFile(global, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	var got toolbarWire
	getJSON(t, ts, "/api/repo", &got)
	if strings.Join(got.Toolbar, "") != "abcdefghij" {
		t.Fatalf("toolbar = %q", got.Toolbar)
	}
}
