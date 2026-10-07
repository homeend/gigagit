package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
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
	Names      []string
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

func startBody(root, rest string) string {
	return `{"worktree":` + strconv.Quote(root) + rest + `}`
}

func TestSessionStartApprovalGateThenStart(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","cols":90,"rows":30`))
	if cmd, _ := body["command"].(string); code != http.StatusForbidden || body["needs_approval"] != true || !strings.Contains(cmd, "sleep 30") {
		t.Fatalf("unapproved start = %d %v", code, body)
	}
	if n := len(domain.Sessions().List()); n != 0 {
		t.Fatalf("a refused start left %d sessions", n)
	}
	code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"cols":90,"rows":30`))
	if code != http.StatusOK {
		t.Fatalf("approved start = %d %v", code, body)
	}
	sess, _ := body["session"].(map[string]any)
	if sess["label"] != "Shell" || sess["worktree"] != root || sess["state"] != "running" {
		t.Fatalf("%v", sess)
	}
	// Remembered: the next start needs no approve flag.
	if code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`)); code != http.StatusOK {
		t.Fatalf("remembered start = %d %v", code, body)
	}
}

func TestSessionStartWithANameRecordsIt(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"name":"  viewer\n"`))
	sess, _ := body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "Shell [viewer]" || sess["name"] != "viewer" {
		t.Fatalf("%d %v", code, body)
	}
	var cmds sessionCommandsBody
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &cmds); code != http.StatusOK || len(cmds.Names) == 0 || cmds.Names[0] != "viewer" {
		t.Fatalf("names = %d %v", code, cmds.Names)
	}
	// An unnamed start records nothing and keeps the bare label.
	code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`))
	sess, _ = body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "Shell" || sess["name"] != nil {
		t.Fatalf("%d %v", code, body)
	}
	if code := getJSON(t, ts, "/api/session-commands?worktree="+url.QueryEscape(root), &cmds); code != http.StatusOK || len(cmds.Names) != 1 {
		t.Fatalf("an unnamed start must not grow the ring: %v", cmds.Names)
	}
}

func TestSessionStartTerminalNeedsNoApproval(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	t.Setenv("SHELL", "/bin/sh")
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"terminal":true`))
	sess, _ := body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "Terminal" {
		t.Fatalf("%d %v", code, body)
	}
	id, _ := sess["id"].(string)
	s, ok := domain.Sessions().Get(domain.SessionID(id))
	if !ok {
		t.Fatal("the started session is not in the manager")
	}
	if fr := s.ScreenRuns(); fr.Cols != 100 || fr.Rows != 30 {
		t.Fatalf("a start with no size = %dx%d, want 100x30", fr.Cols, fr.Rows)
	}
}

// Review focus 1: a path the repository does not own never becomes a cwd.
func TestSessionStartRefusesForeignPaths(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	for _, wt := range []string{"/etc", root + "/..", filepath.Join(root, "sub"), ""} {
		code, body := postJSONAny(t, ts, "/api/session-start", startBody(wt, `,"terminal":true`))
		if code != http.StatusBadRequest {
			t.Errorf("worktree %q = %d %v, want 400", wt, code, body)
		}
	}
	if n := len(domain.Sessions().List()); n != 0 {
		t.Fatalf("%d sessions started", n)
	}
}

func TestSessionStartUnknownToolAndBadBody(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	if code, _ := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Nope"`)); code != http.StatusBadRequest {
		t.Errorf("unknown tool = %d, want 400", code)
	}
	if code, _ := postJSONAny(t, ts, "/api/session-start", `{`); code != http.StatusBadRequest {
		t.Errorf("bad body = %d, want 400", code)
	}
}

// Review focus 2: an unreachable worktree is refused in words — BEFORE the
// approval question — and one recorded under the other environment's
// notation runs in its translated path.
func TestSessionStartPlace(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	srv.placeStat = func(string) error { return os.ErrNotExist }
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`))
	if msg, _ := body["error"].(string); code != http.StatusConflict || !strings.Contains(msg, "is not reachable from here") || body["needs_approval"] == true {
		t.Fatalf("unreachable = %d %v (want 409 before any approval question)", code, body)
	}
	// Foreign notation: a Windows gg seeing a WSL record. Only the translated
	// path "exists"; the session's identity stays the recorded path.
	srv.placeGOOS = "windows"
	srv.placeStat = func(p string) error {
		if strings.HasPrefix(p, `T:\`) {
			return nil
		}
		return os.ErrNotExist
	}
	if cwd, err := srv.sessionPlace("/mnt/t/others/wt"); err != nil || cwd != `T:\others\wt` {
		t.Fatalf("sessionPlace(foreign) = %q, %v", cwd, err)
	}
}

// A hosted page hands the start to the terminal's starter and reports its
// session; the server itself starts nothing.
func TestSessionStartDelegatesToTheStarter(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	var got domain.SessionStartRequest
	srv.SetSessionStarter(func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
		got = req
		s, err := domain.Sessions().Start(domain.SessionStartSpec{Label: "from-tui", Dir: req.Worktree, Argv: []string{"sh", "-c", "sleep 30"}, Cols: req.Cols, Rows: req.Rows})
		if err != nil {
			return "", err
		}
		return s.Info().ID, nil
	})
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"name":" worker ","cols":700,"rows":2`))
	sess, _ := body["session"].(map[string]any)
	if code != http.StatusOK || sess["label"] != "from-tui" {
		t.Fatalf("%d %v", code, body)
	}
	if got.Worktree != root || got.Command.Name != "Shell" || got.Terminal || got.Name != "worker" || got.Cols != 500 || got.Rows != 5 {
		t.Fatalf("starter got %+v (the size must arrive clamped)", got)
	}
	srv.SetSessionStarter(func(context.Context, domain.SessionStartRequest) (domain.SessionID, error) {
		return "", errors.New("the terminal is closing")
	})
	code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`))
	if msg, _ := body["error"].(string); code != http.StatusInternalServerError || !strings.Contains(msg, "the terminal is closing") {
		t.Fatalf("starter error = %d %v", code, body)
	}
}

func TestSessionStartIsWriteGuarded(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	ts := serve(t, srv)
	if code := postJSON(t, ts, "/api/session-start", startBody(root, `,"terminal":true`), "application/json", "http://evil.example", nil); code != http.StatusForbidden {
		t.Fatalf("a cross-origin start = %d, want 403", code)
	}
	if n := len(domain.Sessions().List()); n != 0 {
		t.Fatalf("%d sessions started", n)
	}
}

func waitExited(t *testing.T, s *domain.AgentSession) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the session never exited")
	}
}

// Review focus 4: a verb on a session in the wrong state is refused in words.
func TestSessionKillAndRemoveRules(t *testing.T) {
	s := testSession(t, "sleep 60")
	id := string(s.Info().ID)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	body := `{"id":` + strconv.Quote(id) + `}`

	code, b := postJSONAny(t, ts, "/api/session-remove", body)
	if msg, _ := b["error"].(string); code != http.StatusConflict || !strings.Contains(msg, "only an exited session can be removed") {
		t.Fatalf("remove of a running session = %d %v", code, b)
	}
	if code, b = postJSONAny(t, ts, "/api/session-kill", body); code != http.StatusOK {
		t.Fatalf("kill = %d %v", code, b)
	}
	waitExited(t, s)
	if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
		t.Fatal("a plain kill removed the session")
	}
	code, b = postJSONAny(t, ts, "/api/session-kill", body)
	if msg, _ := b["error"].(string); code != http.StatusConflict || !strings.Contains(msg, "already exited") {
		t.Fatalf("kill of an exited session = %d %v", code, b)
	}
	if code, b = postJSONAny(t, ts, "/api/session-remove", body); code != http.StatusOK {
		t.Fatalf("remove = %d %v", code, b)
	}
	for _, path := range []string{"/api/session-kill", "/api/session-remove"} {
		if code, _ := postJSONAny(t, ts, path, body); code != http.StatusNotFound {
			t.Errorf("%s on a removed id = %d, want 404", path, code)
		}
		if code, _ := postJSONAny(t, ts, path, `{`); code != http.StatusBadRequest {
			t.Errorf("%s bad body = %d, want 400", path, code)
		}
	}
}

func TestSessionKillAndRemoveInOne(t *testing.T) {
	s := testSession(t, "sleep 60")
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	if code, b := postJSONAny(t, ts, "/api/session-kill", `{"id":`+strconv.Quote(string(s.Info().ID))+`,"remove":true}`); code != http.StatusOK {
		t.Fatalf("%d %v", code, b)
	}
	waitExited(t, s)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("kill+remove left the session listed")
}

// Review focus 5: a wedged terminal must not hang the page's start forever.
func TestSessionStartTimesOutOnAWedgedStarter(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	srv.startTimeout = 100 * time.Millisecond
	srv.SetSessionStarter(func(ctx context.Context, _ domain.SessionStartRequest) (domain.SessionID, error) {
		<-ctx.Done() // the terminal never answers
		return "", ctx.Err()
	})
	ts := serve(t, srv)
	done := make(chan int, 1)
	go func() {
		code, _ := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"terminal":true`))
		done <- code
	}()
	select {
	case code := <-done:
		if code != http.StatusInternalServerError {
			t.Fatalf("code = %d, want 500", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the start hung on a wedged starter")
	}
}

// Open terminal never detects agents or writes the config: that is Start
// agent's dialog, where the page says what was added.
func TestSessionStartTerminalNeverDetects(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	t.Setenv("SHELL", "/bin/sh")
	detected := false
	srv.detectTools = func() []exttool.Detection {
		detected = true
		return []exttool.Detection{{Bin: "/usr/bin/claude", Tool: exttool.Tool{ID: "claude", Label: "Claude Code", Commands: []exttool.CommandTemplate{
			{Category: exttool.CatSession, Name: "Claude", Mode: "session", Command: "claude"},
		}}}}
	}
	ts := serve(t, srv)
	if code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"terminal":true`)); code != http.StatusOK {
		t.Fatalf("%d %v", code, body)
	}
	if code, _ := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Claude","approve":true`)); code != http.StatusBadRequest {
		t.Fatalf("a start naming an unconfigured tool = %d, want 400", code)
	}
	if _, err := os.Stat(config.DefaultGlobalPath()); detected || !os.IsNotExist(err) {
		t.Fatalf("detected=%v, global config stat err=%v: a start must not detect or write", detected, err)
	}
}
