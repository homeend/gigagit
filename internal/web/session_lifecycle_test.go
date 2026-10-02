package web

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
)

const shSessionTool = `
[[tools.command]]
category = "session"
mode = "session"
name = "Shell"
command = "sh -c 'echo hi; sleep 30'"
`

// lifecycleServer: an isolated config + state home, a repo carrying tools in
// its .gg.toml, a private session manager.
func lifecycleServer(t *testing.T, tools string) (*Server, string) {
	t.Helper()
	testSessionManager(t)
	isolateGlobal(t)
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := newRepoDir(t, 1)
	if tools != "" {
		if err := os.WriteFile(filepath.Join(dir, ".gg.toml"), []byte(tools), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv := New(domain.Open(dir))
	t.Cleanup(srv.Close)
	return srv, srv.service().Root()
}

type sessionCommandsBody struct {
	Commands []struct {
		Name, Command   string
		Approved, Found bool
	}
	Added      []string
	ConfigPath string `json:"config_path"`
}

func TestSessionCommandsListsWebCommands(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	srv.lookPath = func(p string) (string, error) { return "/bin/" + p, nil }
	ts := serve(t, srv)
	var body sessionCommandsBody
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Commands) != 1 || body.Commands[0].Name != "Shell" || body.Commands[0].Approved || !body.Commands[0].Found || !strings.Contains(body.Commands[0].Command, "sleep 30") || body.Added != nil {
		t.Fatalf("%+v", body)
	}
}

func TestSessionCommandsRefusesAForeignWorktree(t *testing.T) {
	srv, _ := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	var body map[string]any
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(t.TempDir()), &body); code != http.StatusBadRequest {
		t.Fatalf("code = %d, want 400 (%v)", code, body)
	}
}

func TestSessionCommandsFirstRunDetectsAndWrites(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	srv.detectTools = func() []exttool.Detection {
		return []exttool.Detection{{Bin: "/usr/bin/claude", Tool: exttool.Tool{ID: "claude", Label: "Claude Code", Commands: []exttool.CommandTemplate{
			{Category: exttool.CatSession, Name: "Claude", Mode: "session", Command: "claude"},
			{Category: exttool.CatSession, Name: "Claude (yolo)", Mode: "session", OptIn: true, Command: "claude --dangerously-skip-permissions"},
		}}}}
	}
	ts := serve(t, srv)
	var body sessionCommandsBody
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &body); code != http.StatusOK {
		t.Fatalf("code = %d", code)
	}
	if len(body.Added) != 1 || body.Added[0] != "Claude" || len(body.Commands) != 1 || body.ConfigPath == "" {
		t.Fatalf("first run: %+v", body) // the yolo template is opt-in: never auto-added
	}
	raw, err := os.ReadFile(body.ConfigPath)
	if err != nil || !strings.Contains(string(raw), `category = "session"`) {
		t.Fatalf("global config not written: %v %q", err, raw)
	}
	var again sessionCommandsBody
	getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &again)
	if again.Added != nil || len(again.Commands) != 1 {
		t.Fatalf("second call detected again: %+v", again)
	}
}

func TestSessionCommandsNothingDetected(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	srv.detectTools = func() []exttool.Detection { return nil }
	ts := serve(t, srv)
	var body sessionCommandsBody
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &body); code != http.StatusOK || len(body.Commands) != 0 || body.Added != nil {
		t.Fatalf("code=%d %+v", code, body)
	}
}
