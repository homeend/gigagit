# Web attach — plan 2: session lifecycle Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task (this repo forbids implementer subagents — the session that wrote the plan executes it; the final whole-branch review may be a read-only subagent). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** The gg web page can start an agent or a terminal in a worktree, and kill, kill-and-remove and remove sessions — from the sidebar menus, the `ctrl+\` popup and the console itself.

**Architecture:** Three new endpoints sit beside the plan-1 ones in `internal/web` (`session-commands`, `session-start`, `session-kill`/`session-remove`). The server validates the worktree, the command and its approval, then hands the start to a **starter**: a standalone `gg web` starts the session itself; a page hosted by the TUI hands the request to the terminal through a `Host.SetSessionStarter` seam (the `SetSwitcher` shape), so a web-started agent is identical to a TUI-started one. The page gets one new module (`sessions.js`: start dialog + menu rows); kill/remove helpers live in `console.js`.

**Tech Stack:** Go 1.26 stdlib HTTP, Bubble Tea (TUI seam), vanilla ES modules, JS-in-Go pure-section tests (`runPureJS`), Playwright for the browser check.

**Spec:** `docs/superpowers/specs/2026-09-28-web-attach-design.md` (sections "`internal/web` — endpoints", "Sidebar", "Start agent dialog", "`ctrl+\` popup (web)", "Error handling") and `docs/superpowers/specs/2026-09-28-web-hosted-in-tui-design.md`.

## Rulings since the spec (user, 2026-10-02 — do not re-ask)

1. **A hosted page's start is the terminal's start.** The page asks the TUI to start the session, so the agent gets the same `GG_INBOX`, `GG_MCP_URL` + `GG_SESSION_TOKEN` (it can spawn workers) and the kept-inbox tracking (`m.childInbox`). The terminal shows a status line and does **not** open its own console. A standalone `gg web` starts it itself: `GG_INBOX` = its steer inbox, no agent channel.
2. **Finished-task results in the web viewer are out of this plan** (own follow-up, on the AI-tasks backlog). `enter` on a finished task keeps today's "not shown here yet" line.
3. **Kill and remove is on the page too** (the TUI's `X`): `k` kills (asks once), `x` removes an exited session, `X` kills and removes a running one — popup, sub-row menu, console.

## Already on main (do not rebuild)

`m` maximize, the exited toast (`exitToast`), running-age titles, sub-rows with click-to-open, the tabbed switcher, `sessions` live event. A manager change (start, exit, remove) already reaches every tab through `watchSessions` → `broadcastSessions`; the new handlers never broadcast themselves.

## Global Constraints

- All new endpoints: loopback + Host/Origin guards (inherited from the mux), every POST behind `writeGuard`, ids validated against `domain.Sessions()` before any work, the worktree validated against `svc.Worktrees(ctx)` (allowlist — a wire path never reaches `StartSpec.Dir` unchecked).
- Session commands offered to the page: `domain.SessionCommands(cfg, "web")` — never `"tui"`.
- Approval: the AI lanes' pattern verbatim — hash the **template** (`promptstate.CommandHash(tc.Command)`), show the **resolved** text, 403 + `needs_approval`, remember on `approve:true` (best-effort), store = `s.promptStore()`, key = `s.toolRepoKey(ctx, svc)` (shared with the TUI).
- Console size from the wire: `domain.ClampConsoleSize(cols, rows)`; a missing size (0) starts at 100×30.
- Menu labels carry no trailing "…" (user ruling): `Start agent in <wt>`, `Open terminal in <wt>`.
- `internal/web` never imports `internal/tui` or `internal/agentsession` (archtest); the request type shared with the TUI lives in `internal/domain`.
- gg web hides by ID: every new element that can hide gets its own `#id.hidden { display: none; }` rule.
- Every new user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (use the `adding-translations` skill).
- Tests that can reach the global config or the approval store pin `XDG_CONFIG_HOME` **and** `XDG_STATE_HOME` to temp dirs (`isolateGlobal(t)` + `t.Setenv("XDG_STATE_HOME", t.TempDir())`); session tests install a private manager (`domain.UseSessionManager`) and skip on Windows (`sh`-based).
- Work in the worktree `/work/gigagit/.claude/worktrees/web-session-lifecycle` (branch `feat/web-session-lifecycle`); `cd` there in every command; never `git add -A` (stage named paths).
- Commits: `gg add <paths>` then `git commit -F <msgfile>` (gg commit has no `-F`), ending with the session's attribution lines.

## Review Focus

1. **A worktree path the repo does not own** (`{"worktree":"/etc"}`, a path with `..`, a sub-directory of a worktree) → 400, no session. Test in Task 3.
2. **An unreachable or foreign-notation worktree** (a `/mnt/t/…` record seen from Windows; a deleted directory still listed by git) → started in the translated path, or 409 with "is not reachable from here" — never a raw chdir error. Test in Task 3 (the `stat`/`goos` seam).
3. **Start pressed twice / two tabs starting at once** → two sessions is correct (the TUI allows it); the dialog must close on the first answer so a held `enter` does not start a second. Test in Task 7 (pure `dialogStep`), checked live in Task 9.
4. **Kill or remove of a session that just changed state** (kill on an exited one, remove on a running one, any verb on a removed id) → 409 / 404 with a sentence the toast can show, the row refreshed from the live list. Test in Task 4.
5. **The hosted terminal is closing or wedged when the page asks for a start** → the request ends with an error within its context ("the terminal is closing" / the request timeout), never a hung fetch. Test in Task 5.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/domain/sessions.go` (modify) | `SessionStartRequest`, `SessionProgram(tc)` (the program word, behind `found`) |
| `internal/web/session_lifecycle.go` (create) | the four handlers, worktree allowlist, place probe, the standalone starter, `SetSessionStarter` |
| `internal/web/session_lifecycle_test.go` (create) | handler tests |
| `internal/web/host.go` (modify) | `Host.SetSessionStarter` |
| `internal/tui/websession.go` (create) | the hosted starter: request msg, wait cmd, start, status line |
| `internal/tui/websession_test.go` (create) | the seam's tests |
| `internal/tui/webhost.go`, `model.go` (modify) | interface method, channel, dispatch, arming |
| `internal/web/static/console.js` (modify) | `killSession`/`removeSession`, foot rows, `k`/`X`/`x` |
| `internal/web/static/sessions.js` (create) | start dialog, `Start agent`/`Open terminal`, menu rows |
| `internal/web/static/sidebar.js`, `menus.js`, `openfiles.js`, `app.js`, `style.css` (modify) | sub-row menu, `session` menu key, popup keys, boot import, dialog styles |
| `internal/web/sessionsjs_test.go` (create) | pure-section + wiring tests |
| `internal/web/attach_browser_test.go` (modify) | the browser-check host seeds a `sh` session command |

---

### Task 1: domain — the shared start request and the program word

**Files:**
- Modify: `internal/domain/sessions.go`
- Test: `internal/domain/sessions_test.go`

**Interfaces:**
- Produces: `type SessionStartRequest struct { Worktree string; Command config.ToolCommand; Terminal bool; Cols, Rows int }`; `func SessionProgram(tc config.ToolCommand) string`.

- [ ] **Step 1: Write the failing test** (append to `internal/domain/sessions_test.go`)

```go
func TestSessionProgram(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ cmd, want string }{
		{"claude --resume", "claude"},
		{`"C:\Program Files\Claude\claude.exe" --x`, `C:\Program Files\Claude\claude.exe`},
		{"  codex  ", "codex"},
		{"", ""},
	} {
		if got := SessionProgram(config.ToolCommand{Command: c.cmd}); got != c.want {
			t.Errorf("SessionProgram(%q) = %q, want %q", c.cmd, got, c.want)
		}
	}
}
```

- [ ] **Step 2: Run it** — `cd /work/gigagit/.claude/worktrees/web-session-lifecycle && rtk go test ./internal/domain -run TestSessionProgram` → FAIL: `undefined: SessionProgram`.

- [ ] **Step 3: Implement.** In `sessions.go`, lift the program parsing out of `agentIDFor` and add the request type:

```go
// SessionStartRequest is a frontend asking for a session in a worktree: the
// page's start (web) handed to whoever owns the start — the web server
// itself, or the terminal hosting the page. Command is ignored for a
// terminal.
type SessionStartRequest struct {
	Worktree   string
	Command    config.ToolCommand
	Terminal   bool
	Cols, Rows int
}

// SessionProgram is the program a session command runs: its first word, or
// the double-quoted first word of a Windows install path. "" for an empty
// command.
func SessionProgram(tc config.ToolCommand) string {
	prog := strings.TrimSpace(tc.Command)
	if strings.HasPrefix(prog, `"`) {
		if end := strings.Index(prog[1:], `"`); end >= 0 {
			return prog[1 : 1+end]
		}
		return prog
	}
	if f := strings.Fields(prog); len(f) > 0 {
		return f[0]
	}
	return ""
}
```

and make `agentIDFor` start with `prog := SessionProgram(tc)` (delete its own parsing; the `if prog == "" { return "" }` and everything after stay).

- [ ] **Step 4: Run** `rtk go test ./internal/domain -run 'TestSessionProgram|TestAgentID'` → PASS (the existing agent-id tests still pass).

- [ ] **Step 5: Commit** — `gg add internal/domain/sessions.go internal/domain/sessions_test.go`, message `feat(domain): SessionStartRequest, SessionProgram — the web start's shared shapes`.

---

### Task 2: `GET /api/session-commands`

**Files:**
- Create: `internal/web/session_lifecycle.go`, `internal/web/session_lifecycle_test.go`
- Modify: `internal/web/server.go` (one seam field)

**Interfaces:**
- Consumes: `domain.SessionCommands`, `domain.EnsureSessionCommands`, `domain.SessionProgram`, `s.detections()`, `s.effectiveConfig`, `s.promptStore()`, `s.toolRepoKey`.
- Produces: wire `{"commands":[{"name","command","approved","found"}],"added":[…],"config_path":"…"}`; `func (s *Server) sessionWorktree(ctx, svc, path) (string, error)`; `func (s *Server) sessionCommandsFor(ctx, svc) (cfg config.Config, cmds []config.ToolCommand, added []string, err error)`; seam `s.lookPath func(string) (string, error)` (nil = `exec.LookPath`).

- [ ] **Step 1: Write the failing tests**

```go
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
```

Add to `sessions_http_test.go` (and make `testSession` call it instead of its own two lines):

```go
// testSessionManager installs a private manager for the test.
func testSessionManager(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(t.Context()); restore() })
}
```

If `exttool.CommandTemplate`/`Detection` field names differ from `fakeDetections` in `exttools_test.go`, copy that fixture's shape — it is the reference. If `InstallTemplates` needs a real binary for a `--version` probe, the package `TestMain` already sets `domain.ToolStatusesDisabled`.

- [ ] **Step 2: Run** `rtk go test ./internal/web -run TestSessionCommands` → FAIL (404 / `srv.lookPath undefined`).

- [ ] **Step 3: Implement.** `server.go`: add beside `detectTools`:

```go
	// lookPath finds a session command's program (tests); nil = exec.LookPath.
	lookPath func(string) (string, error)
```

`internal/web/session_lifecycle.go`:

```go
package web

// Session lifecycle on the web (web attach, plan 2): the commands a start
// can run, the start itself, kill and remove.

import (
	"context"
	"errors"
	"net/http"
	"os/exec"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/session-commands", s.handleSessionCommands)
	})
}

type sessionCommandWire struct {
	Name     string `json:"name"`
	Command  string `json:"command"` // resolved for the worktree: what will run
	Approved bool   `json:"approved"`
	Found    bool   `json:"found"`
}

// sessionWorktree answers the worktree a wire path names — one of the
// repository's own, as git lists it. Anything else is refused: the path
// becomes a process's working directory.
func (s *Server) sessionWorktree(ctx context.Context, svc *domain.Service, path string) (string, error) {
	wts, err := svc.Worktrees(ctx)
	if err != nil {
		return "", err
	}
	for _, w := range wts {
		if w.Path != "" && w.Path == path && !w.Bare {
			return w.Path, nil
		}
	}
	return "", errors.New("not a worktree of this repository")
}

// sessionCommandsFor is the effective config's web session commands. With
// no session command configured at all it runs the first-run detect, which
// appends the installed agents' safe templates to the GLOBAL config, and
// reloads; added names what it wrote (nil = nothing written).
func (s *Server) sessionCommandsFor(ctx context.Context, svc *domain.Service) (config.Config, []config.ToolCommand, []string, error) {
	cfg, err := s.effectiveConfig(ctx, svc)
	if err != nil {
		return cfg, nil, nil, err
	}
	added, err := domain.EnsureSessionCommands(cfg, config.DefaultGlobalPath(), s.detections)
	if err != nil {
		return cfg, nil, nil, err
	}
	if added != nil {
		if cfg, err = s.effectiveConfig(ctx, svc); err != nil {
			return cfg, nil, nil, err
		}
	}
	return cfg, domain.SessionCommands(cfg, "web"), added, nil
}

func (s *Server) programFound(tc config.ToolCommand) bool {
	look := s.lookPath
	if look == nil {
		look = exec.LookPath
	}
	prog := domain.SessionProgram(tc)
	if prog == "" {
		return false
	}
	_, err := look(prog)
	return err == nil
}

// handleSessionCommands lists what Start agent can run in ?worktree=. The
// first call on a machine with no session command detects the installed
// agents inside the request — the page shows its busy notice while it waits.
func (s *Server) handleSessionCommands(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	dir, err := s.sessionWorktree(r.Context(), svc, r.URL.Query().Get("worktree"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	_, cmds, added, err := s.sessionCommandsFor(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	approved := map[string]bool{}
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(r.Context(), svc))
	}
	out := make([]sessionCommandWire, 0, len(cmds))
	for _, tc := range cmds {
		resolved, rerr := template.ResolveCommand(tc.Command, nil, template.CmdCtx{Repo: dir})
		if rerr != nil {
			continue // inert, as in every tool lane
		}
		out = append(out, sessionCommandWire{Name: tc.Name, Command: resolved,
			Approved: approved[promptstate.CommandHash(tc.Command)], Found: s.programFound(tc)})
	}
	writeJSON(w, map[string]any{"commands": out, "added": added, "config_path": config.DefaultGlobalPath()})
}
```

Check `model.Worktree`'s bare field name (`Bare`) before compiling; `s.detections` is a method value of the right type (`func() []exttool.Detection`).

- [ ] **Step 4: Run** `rtk go test ./internal/web -run 'TestSessionCommands|TestSessions'` → PASS.

- [ ] **Step 5: Commit** — paths: the two new files, `server.go`, `sessions_http_test.go`; message `feat(web): GET /api/session-commands — the start dialog's list, first-run detect`.

---

### Task 3: `POST /api/session-start`

**Files:**
- Modify: `internal/web/session_lifecycle.go`, `internal/web/server.go`, `internal/web/host.go`
- Test: `internal/web/session_lifecycle_test.go`

**Interfaces:**
- Consumes: Task 1's `domain.SessionStartRequest`; Task 2's `sessionWorktree`, `sessionCommandsFor`.
- Produces: `func (s *Server) SetSessionStarter(fn func(context.Context, domain.SessionStartRequest) (domain.SessionID, error))`, `func (h *Host) SetSessionStarter(...)` (same signature); wire request `{worktree, tool, terminal, approve, cols, rows}`, response `{"session": sessionWire}`; seams `s.placeStat func(string) error`, `s.placeGOOS string`.

- [ ] **Step 1: Write the failing tests** (append)

```go
func startBody(root, rest string) string {
	return `{"worktree":` + strconv.Quote(root) + rest + `}`
}

func TestSessionStartApprovalGateThenStart(t *testing.T) {
	srv, root := lifecycleServer(t, shSessionTool)
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","cols":90,"rows":30`))
	if code != http.StatusForbidden || body["needs_approval"] != true || !strings.Contains(body["command"].(string), "sleep 30") {
		t.Fatalf("unapproved start = %d %v", code, body)
	}
	if n := len(domain.Sessions().List()); n != 0 {
		t.Fatalf("a refused start left %d sessions", n)
	}
	code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"cols":90,"rows":30`))
	if code != http.StatusOK {
		t.Fatalf("approved start = %d %v", code, body)
	}
	sess := body["session"].(map[string]any)
	if sess["label"] != "Shell" || sess["worktree"] != root || sess["state"] != "running" {
		t.Fatalf("%v", sess)
	}
	// Remembered: the next start needs no approve flag.
	if code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`)); code != http.StatusOK {
		t.Fatalf("remembered start = %d %v", code, body)
	}
}

func TestSessionStartTerminalNeedsNoApproval(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	t.Setenv("SHELL", "/bin/sh")
	ts := serve(t, srv)
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"terminal":true`))
	if code != http.StatusOK || body["session"].(map[string]any)["label"] != "Terminal" {
		t.Fatalf("%d %v", code, body)
	}
	if info := domain.Sessions().List()[0]; info.Cols != 100 || info.Rows != 30 {
		t.Fatalf("a start with no size = %dx%d, want 100x30", info.Cols, info.Rows)
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
	if code != http.StatusConflict || !strings.Contains(body["error"].(string), "is not reachable from here") || body["needs_approval"] == true {
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
	code, body := postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell","approve":true,"cols":700,"rows":2`))
	if code != http.StatusOK || body["session"].(map[string]any)["label"] != "from-tui" {
		t.Fatalf("%d %v", code, body)
	}
	if got.Worktree != root || got.Command.Name != "Shell" || got.Terminal || got.Cols != 500 || got.Rows != 5 {
		t.Fatalf("starter got %+v (the size must arrive clamped)", got)
	}
	srv.SetSessionStarter(func(context.Context, domain.SessionStartRequest) (domain.SessionID, error) {
		return "", errors.New("the terminal is closing")
	})
	if code, body = postJSONAny(t, ts, "/api/session-start", startBody(root, `,"tool":"Shell"`)); code != http.StatusInternalServerError || !strings.Contains(body["error"].(string), "the terminal is closing") {
		t.Fatalf("starter error = %d %v", code, body)
	}
}

func TestSessionStartIsWriteGuarded(t *testing.T) {
	srv, root := lifecycleServer(t, "")
	ts := serve(t, srv)
	var out map[string]any
	if code := postJSON(t, ts, "/api/session-start", startBody(root, `,"terminal":true`), "application/json", "http://evil.example", &out); code != http.StatusForbidden {
		t.Fatalf("a cross-origin start = %d, want 403", code)
	}
}
```

`postJSONAny` is the `map[string]any` twin of `postJSONRaw` — if the package has no such helper (check `opdiscard_test.go` and `review_test.go`'s `startReview`), add it next to `postJSONRaw`:

```go
func postJSONAny(t *testing.T, ts *httptest.Server, path, body string) (int, map[string]any) {
	t.Helper()
	resp, err := http.Post(ts.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}
```

(If `writeGuard` requires an Origin/extra header that `postJSONRaw` sets, copy that header here. `SessionInfo` may not expose `Cols`/`Rows`: then assert the size through `sess.Screen()`'s dimensions or `ScreenRuns().Cols/Rows` instead.)

- [ ] **Step 2: Run** `rtk go test ./internal/web -run TestSessionStart` → FAIL (404, undefined seams).

- [ ] **Step 3: Implement.** `server.go` fields + setter (beside `switcher`, guarded by `mu`):

```go
	// starter starts a session for the page (Host.SetSessionStarter): the
	// terminal's own start when a TUI hosts the page, so a web-started agent
	// gets the terminal's inbox and agent channel. nil = this server starts
	// it itself (standalone gg web). Guarded by mu.
	starter func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)
	// placeStat / placeGOOS: the reachability probe's seams (tests); zero =
	// os.Stat and runtime.GOOS.
	placeStat func(string) error
	placeGOOS string
```

```go
// SetSessionStarter installs the terminal's start (see starter).
func (s *Server) SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)) {
	s.mu.Lock()
	s.starter = fn
	s.mu.Unlock()
}

func (s *Server) sessionStarter() func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.starter
}
```

`host.go`:

```go
// SetSessionStarter installs the terminal's session start: the page's Start
// agent / Open terminal is validated and approved here, then fn starts it
// the way the terminal does (its inbox, its agent channel) and returns the
// new session's id. Without it the page's server starts sessions itself.
func (h *Host) SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error)) {
	h.srv.SetSessionStarter(fn)
}
```

`session_lifecycle.go` — route `mux.HandleFunc("POST /api/session-start", writeGuard(s.handleSessionStart))` and:

```go
type sessionStartReq struct {
	Worktree string `json:"worktree"`
	Tool     string `json:"tool"`
	Terminal bool   `json:"terminal"`
	Approve  bool   `json:"approve"` // the user just approved this command
	Cols     int    `json:"cols"`
	Rows     int    `json:"rows"`
}

// startSize clamps the page's measured grid; a start that sends none gets a
// roomy default rather than the clamp's minimum.
func startSize(cols, rows int) (int, int) {
	if cols <= 0 || rows <= 0 {
		return 100, 30
	}
	return domain.ClampConsoleSize(cols, rows)
}

// sessionPlace is the TUI's sessionPlace, wire-shaped: where a session for
// dir runs. cwd "" = in dir; a worktree recorded under the other
// environment's notation runs in its translated path; an unreachable one is
// refused.
func (s *Server) sessionPlace(dir string) (cwd string, err error) {
	stat, goos := s.placeStat, s.placeGOOS
	if stat == nil {
		stat = func(p string) error { _, err := os.Stat(p); return err }
	}
	if goos == "" {
		goos = runtime.GOOS
	}
	switch verdict, translated := worktree.CheckSwitchTarget(stat, goos, dir); verdict {
	case worktree.SwitchOK:
		return "", nil
	case worktree.SwitchRepairable:
		return translated, nil
	}
	return "", fmt.Errorf("cannot start here: %s is not reachable from here", dir)
}

// startSessionHere is the standalone server's own start: the child gets this
// page's steer inbox and no agent channel (no terminal hosts one).
func (s *Server) startSessionHere(ctx context.Context, svc *domain.Service, cfg config.Config, req domain.SessionStartRequest, cwd string) (domain.SessionID, error) {
	var env []string
	if inbox := s.steerInbox(); inbox != "" {
		env = []string{"GG_INBOX=" + inbox}
	}
	// The session outlives the request: never its context.
	bg := context.WithoutCancel(ctx)
	if req.Terminal {
		sess, err := svc.StartTerminal(bg, cfg.Console.Shell, req.Worktree, cwd, req.Cols, req.Rows, env)
		if err != nil {
			return "", err
		}
		return sess.Info().ID, nil
	}
	sess, _, err := svc.StartAgentSession(bg, req.Command, req.Worktree, cwd, req.Cols, req.Rows, env, "", domain.SpawnRecord{}, "")
	if err != nil {
		return "", err
	}
	return sess.Info().ID, nil
}

// handleSessionStart starts an agent (tool) or a terminal in a worktree. An
// unapproved command is refused with 403 + needs_approval and the resolved
// text; the page asks, then repeats with approve=true. A terminal runs the
// user's own shell and needs no approval, as in the TUI.
func (s *Server) handleSessionStart(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var q sessionStartReq
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	dir, err := s.sessionWorktree(r.Context(), svc, q.Worktree)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cfg, cmds, _, err := s.sessionCommandsFor(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Reachability first: nobody should approve a command and then be told
	// the worktree is not there (the TUI's startAgentFor order).
	cwd, err := s.sessionPlace(dir)
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	req := domain.SessionStartRequest{Worktree: dir, Terminal: q.Terminal}
	req.Cols, req.Rows = startSize(q.Cols, q.Rows)
	if !q.Terminal {
		found := false
		for _, tc := range cmds {
			if tc.Name == q.Tool {
				req.Command, found = tc, true
				break
			}
		}
		if !found {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("no session command named %q", q.Tool))
			return
		}
		resolved, rerr := template.ResolveCommand(req.Command.Command, nil, template.CmdCtx{Repo: dir})
		if rerr != nil {
			writeErr(w, http.StatusBadRequest, rerr)
			return
		}
		key, store := s.toolRepoKey(r.Context(), svc), s.promptStore()
		hash := promptstate.CommandHash(req.Command.Command)
		if approved := store != nil && store.ApprovedToolCommands(key)[hash]; !approved {
			if !q.Approve {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(http.StatusForbidden)
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error": "this command has not been approved yet", "needs_approval": true,
					"tool": req.Command.Name, "command": resolved,
				})
				return
			}
			if store != nil {
				_ = store.ApproveToolCommand(key, hash) // best-effort: only costs another prompt
			}
		}
	}
	var id domain.SessionID
	if start := s.sessionStarter(); start != nil {
		id, err = start(r.Context(), req)
	} else {
		id, err = s.startSessionHere(r.Context(), svc, cfg, req, cwd)
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	for _, sw := range s.sessionsNow() {
		if sw.ID == string(id) {
			writeJSON(w, map[string]any{"session": sw})
			return
		}
	}
	writeErr(w, http.StatusInternalServerError, errors.New("the session ended before it could be shown"))
}
```

Imports grow by `encoding/json`, `fmt`, `os`, `runtime`, `internal/worktree`. The place probe runs before the approval gate and before the starter, in both modes, so the page's refusal sentence is the same and nobody approves a command for an unreachable worktree; the terminal re-derives `cwd` itself (Task 5). `context.WithoutCancel` is only belt-and-braces: check that `StartAgentSession` does not tie the child to `ctx` (it passes it to `RepoName` only) and keep the comment honest.

- [ ] **Step 4: Run** `rtk go test ./internal/web -run 'TestSessionStart|TestSessionCommands'` → PASS. Then `rtk go test ./internal/archtest` → PASS (web → worktree is already an edge: `reroot.go`).

- [ ] **Step 5: Commit** — `feat(web): POST /api/session-start — approval gate, worktree allowlist, the starter seam`.

---

### Task 4: `POST /api/session-kill` and `/api/session-remove`

**Files:**
- Modify: `internal/web/session_lifecycle.go`
- Test: `internal/web/session_lifecycle_test.go`

**Interfaces:**
- Produces: `POST /api/session-kill {id, remove}` → `{"ok":true}`; `POST /api/session-remove {id}` → `{"ok":true}`; 404 unknown id, 409 wrong state.

- [ ] **Step 1: Write the failing tests** (Review focus 4)

```go
func waitExited(t *testing.T, s *domain.AgentSession) {
	t.Helper()
	select {
	case <-s.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("the session never exited")
	}
}

func TestSessionKillAndRemoveRules(t *testing.T) {
	s := testSession(t, "sleep 60")
	id := string(s.Info().ID)
	ts := serve(t, New(domain.Open(newRepoDir(t, 1))))
	body := `{"id":` + strconv.Quote(id) + `}`

	if code, b := postJSONAny(t, ts, "/api/session-remove", body); code != http.StatusConflict || !strings.Contains(b["error"].(string), "only an exited session can be removed") {
		t.Fatalf("remove of a running session = %d %v", code, b)
	}
	if code, b := postJSONAny(t, ts, "/api/session-kill", body); code != http.StatusOK {
		t.Fatalf("kill = %d %v", code, b)
	}
	waitExited(t, s)
	if _, ok := domain.Sessions().Get(s.Info().ID); !ok {
		t.Fatal("a plain kill removed the session")
	}
	if code, b := postJSONAny(t, ts, "/api/session-kill", body); code != http.StatusConflict || !strings.Contains(b["error"].(string), "already exited") {
		t.Fatalf("kill of an exited session = %d %v", code, b)
	}
	if code, b := postJSONAny(t, ts, "/api/session-remove", body); code != http.StatusOK {
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
```

(`AgentSession.Done()` exists — `Manager.KillAndRemove` uses it.)

- [ ] **Step 2: Run** `rtk go test ./internal/web -run TestSessionKill` → FAIL (404 for the routes).

- [ ] **Step 3: Implement.** Routes `mux.HandleFunc("POST /api/session-kill", writeGuard(s.handleSessionKill))` and `…/session-remove`; reuse `sessionByID` from `console_input.go`:

```go
// handleSessionKill ends a running session; remove also forgets it once its
// exit is recorded (the TUI's X). The list change reaches every tab through
// the manager's signal.
func (s *Server) handleSessionKill(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID     string `json:"id"`
		Remove bool   `json:"remove"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	if sess.Info().State != domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("the session has already exited"))
		return
	}
	id := domain.SessionID(q.ID)
	var err error
	if q.Remove {
		err = domain.Sessions().KillAndRemove(id)
	} else {
		err = domain.Sessions().Kill(id)
	}
	if err != nil {
		writeErr(w, http.StatusNotFound, err) // removed between the lookup and the kill
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}

// handleSessionRemove forgets an exited session.
func (s *Server) handleSessionRemove(w http.ResponseWriter, r *http.Request) {
	var q struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&q); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	sess, ok := sessionByID(w, q.ID)
	if !ok {
		return
	}
	if sess.Info().State == domain.SessionRunning {
		writeErr(w, http.StatusConflict, errors.New("only an exited session can be removed — kill it first"))
		return
	}
	if err := domain.Sessions().Remove(domain.SessionID(q.ID)); err != nil {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeJSON(w, map[string]any{"ok": true})
}
```

- [ ] **Step 4: Run** `rtk go test ./internal/web -run 'TestSession'` → PASS.

- [ ] **Step 5: Commit** — `feat(web): POST /api/session-kill and /api/session-remove`.

---

### Task 5: TUI — the hosted page's start is the terminal's start

**Files:**
- Create: `internal/tui/websession.go`, `internal/tui/websession_test.go`
- Modify: `internal/tui/webhost.go` (interface, state, `startWebCmd`, `onWebStarted`), `internal/tui/model.go` (dispatch), `internal/tui/webhost_test.go` (`fakeWebHost`), the four i18n bundles

**Interfaces:**
- Consumes: `domain.SessionStartRequest`; the TUI's `sessionPlace`, `m.childEnv()`, `m.childInboxDir()`, `m.agentURL()`, `m.cfg.Console.Shell`.
- Produces: `WebHost.SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error))` (satisfied by `*web.Host` from Task 3 — `cmd/gg` needs no change).

- [ ] **Step 1: Write the failing tests** — `internal/tui/websession_test.go`. Fixtures already in the package: `loadedModel(t)` (a Model over a real temp repo), `installFakeHost(t)` and `runOne` (`webhost_test.go`). The start cmd is a `tea.Batch` whose second child is a blocking wait — never `drainCmds` it; the tests below call the start leaf directly through `webSessionStartLeaf`, a tiny seam the implementation exposes for this.

```go
package tui

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

// The page's start reaches Update, starts the session the terminal's way,
// answers with its id, records the child's inbox and never opens a console.
func TestWebSessionRequestStartsLikeTheTerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	installFakeHost(t)
	m := loadedModel(t)
	m = m.ensureWeb()
	dir := m.svc.Root()
	reply := make(chan webSessionReply, 1)
	req := domain.SessionStartRequest{Worktree: dir, Cols: 90, Rows: 30,
		Command: config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}}
	m, leaf, why := m.webSessionStartLeaf(webSessionRequestMsg{req: req, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	m, _ = m.onWebSessionStarted(leaf().(webSessionStartedMsg))
	r := <-reply
	if r.err != nil || r.id == "" {
		t.Fatalf("reply = %+v", r)
	}
	s, ok := domain.Sessions().Get(r.id)
	if !ok || s.Info().Label != "Shell" || s.Info().Dir != dir {
		t.Fatalf("session = %+v", s)
	}
	if m.console != nil {
		t.Fatal("a web start opened the terminal's console")
	}
	if got := m.childInbox[r.id]; got != m.childInboxDir() {
		t.Fatalf("childInbox = %q, want the terminal's inbox %q", got, m.childInboxDir())
	}
	if !strings.Contains(m.statusMsg, "Shell") || !strings.Contains(m.statusMsg, "web page") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestWebSessionRequestRefusesAnUnreachableWorktree(t *testing.T) {
	m := loadedModel(t).ensureWeb()
	old := guardStat
	guardStat = func(string) error { return errors.New("gone") }
	t.Cleanup(func() { guardStat = old })
	reply := make(chan webSessionReply, 1)
	m, _ = m.onWebSessionRequest(webSessionRequestMsg{req: domain.SessionStartRequest{Worktree: "/nowhere", Terminal: true}, reply: reply})
	if r := <-reply; r.err == nil || !strings.Contains(r.err.Error(), "not reachable") {
		t.Fatalf("reply = %+v", r)
	}
}

// Review focus 5: a closing or wedged terminal ends the page's request.
func TestSessionStarterEndsWhenTheTerminalCloses(t *testing.T) {
	t.Parallel()
	w := newWebHostState()
	start := sessionStarterFor(w)
	close(w.stop)
	if _, err := start(context.Background(), domain.SessionStartRequest{}); err == nil || !strings.Contains(err.Error(), "closing") {
		t.Fatalf("err = %v", err)
	}
	w2 := newWebHostState()
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := sessionStarterFor(w2)(ctx, domain.SessionStartRequest{}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a wedged terminal: err = %v", err)
	}
}
```

Add the stale-config test (the page's first-run detect wrote the global config from the web request; the terminal must not detect and append a second time):

```go
// The page's first-run detect wrote session commands the terminal has not
// loaded: a web start reloads the terminal's config, so its own Start agent
// lists them instead of detecting (and appending) again.
func TestWebSessionRequestReloadsAStaleConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sh-based")
	}
	restore := domain.UseSessionManager(agentsession.NewManager())
	t.Cleanup(func() { domain.Sessions().KillAll(context.Background()); restore() })
	m := loadedModel(t).ensureWeb()
	global := filepath.Join(t.TempDir(), "config.toml")
	old := agentGlobalConfigPath
	agentGlobalConfigPath = func() string { return global }
	t.Cleanup(func() { agentGlobalConfigPath = old })
	tc := config.ToolCommand{Name: "Shell", Category: "session", Mode: "session", Command: "sh -c 'sleep 30'"}
	if err := config.AppendToolCommands(global, []config.ToolCommand{tc}); err != nil {
		t.Fatal(err)
	}
	if n := len(domain.SessionCommands(m.cfg, "tui")); n != 0 {
		t.Fatalf("fixture: the model already has %d session commands", n)
	}
	reply := make(chan webSessionReply, 1)
	m, leaf, why := m.webSessionStartLeaf(webSessionRequestMsg{req: domain.SessionStartRequest{Worktree: m.svc.Root(), Command: tc, Cols: 80, Rows: 24}, reply: reply})
	if why != "" {
		t.Fatal(why)
	}
	m, _ = m.onWebSessionStarted(leaf().(webSessionStartedMsg))
	if n := len(domain.SessionCommands(m.cfg, "tui")); n != 1 {
		t.Fatalf("the terminal still sees %d session commands", n)
	}
}
```

(imports grow by `path/filepath`). `loadedModel`'s steering may be off in tests — then `m.childInboxDir()` is "" and the inbox assertion compares "" with a missing map entry, which holds.

- [ ] **Step 2: Run** `rtk go test ./internal/tui -run 'TestWebSession|TestSessionStarter'` → FAIL (undefined). After Step 3, see the reload test fail for the right reason once: comment the `if !hasSessionCommand(m.cfg) {…}` block out, run it (FAIL: "still sees 0"), restore it by re-typing the block (never `git checkout --`).

- [ ] **Step 3: Implement** `internal/tui/websession.go`:

```go
package tui

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// A session started from the hosted web page: the page's server validates
// and approves the request, then hands it here so the child is the
// terminal's own — its inbox, its agent channel, its kept-inbox tracking.
// The terminal says so on the status line and never opens its console (the
// page shows it).

type webSessionReply struct {
	id  domain.SessionID
	err error
}

// webSessionRequestMsg is the page asking the terminal to start a session.
// reply is buffered: Update never blocks answering it.
type webSessionRequestMsg struct {
	req   domain.SessionStartRequest
	reply chan webSessionReply
}

type webSessionStartedMsg struct {
	id    domain.SessionID
	name  string
	dir   string
	inbox string
	err   error
	reply chan webSessionReply
}

// sessionStarterFor is the function the host calls for a page start: it
// hands the request to Update and waits for the answer (bounded by the
// page's request context).
func sessionStarterFor(w *webHostState) func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
	reqs, stop := w.sessions, w.stop
	return func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error) {
		reply := make(chan webSessionReply, 1)
		select {
		case reqs <- webSessionRequestMsg{req: req, reply: reply}:
		case <-stop:
			return "", errors.New("the terminal is closing")
		case <-ctx.Done():
			return "", ctx.Err()
		}
		select {
		case r := <-reply:
			return r.id, r.err
		case <-stop:
			return "", errors.New("the terminal is closing")
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

// waitWebSessionCmd waits for the page's next start request; nil once the
// page is closed.
func waitWebSessionCmd(w *webHostState) tea.Cmd {
	reqs, stop := w.sessions, w.stop
	return func() tea.Msg {
		select {
		case req := <-reqs:
			return req
		case <-stop:
			return nil
		}
	}
}

// onWebSessionRequest starts the session off the UI thread with what only
// the terminal knows (child env, agent channel, shell), at the size the page
// measured; the wait re-arms at once.
func (m Model) onWebSessionRequest(msg webSessionRequestMsg) (Model, tea.Cmd) {
	var rearm tea.Cmd
	if m.web != nil {
		rearm = waitWebSessionCmd(m.web)
	}
	m, start, why := m.webSessionStartLeaf(msg)
	if why != "" {
		msg.reply <- webSessionReply{err: errors.New(why)}
		return m, rearm
	}
	return m, tea.Batch(start, rearm)
}

// hasSessionCommand: EnsureSessionCommands' own "already configured" test.
func hasSessionCommand(cfg config.Config) bool {
	for _, tc := range cfg.Tools.Command {
		if tc.Category == string(exttool.CatSession) {
			return true
		}
	}
	return false
}

// webSessionStartLeaf builds the start itself (no wait attached — tests run
// it directly); why is the English refusal for the page. The page's first
// start on a machine may have just WRITTEN the session commands (its
// first-run detect runs in the web request): a terminal that still sees
// none reloads its config here, or its own Start agent would detect and
// append them a second time.
func (m Model) webSessionStartLeaf(msg webSessionRequestMsg) (Model, tea.Cmd, string) {
	req := msg.req
	if !hasSessionCommand(m.cfg) {
		if nc, err := config.Load(agentGlobalConfigPath(), m.repoConfigPath); err == nil {
			m.cfg = nc
		}
	}
	cwd, _, refusal := sessionPlace(req.Worktree)
	if refusal != "" {
		return m, nil, "cannot start here: " + req.Worktree + " is not reachable from here"
	}
	svc, shell, env, inbox, url := m.svc, m.cfg.Console.Shell, m.childEnv(), m.childInboxDir(), m.agentURL()
	name := req.Command.Name
	if req.Terminal {
		name = i18n.T("Terminal")
	}
	start := func() tea.Msg {
		out := webSessionStartedMsg{name: name, dir: req.Worktree, inbox: inbox, reply: msg.reply}
		if req.Terminal {
			s, err := svc.StartTerminal(context.Background(), shell, req.Worktree, cwd, req.Cols, req.Rows, env)
			if err != nil {
				out.err = err
				return out
			}
			out.id = s.Info().ID
			return out
		}
		s, _, err := svc.StartAgentSession(context.Background(), req.Command, req.Worktree, cwd, req.Cols, req.Rows, env, url, domain.SpawnRecord{}, "")
		if err != nil {
			out.err = err
			return out
		}
		out.id = s.Info().ID
		return out
	}
	return m, start, ""
}

// onWebSessionStarted answers the page and keeps the child's inbox answered.
func (m Model) onWebSessionStarted(msg webSessionStartedMsg) (Model, tea.Cmd) {
	msg.reply <- webSessionReply{id: msg.id, err: msg.err}
	if msg.err != nil {
		m.statusMsg = i18n.T("could not start %s: %s", msg.name, msg.err.Error())
		return m, nil
	}
	if msg.inbox != "" {
		if m.childInbox == nil {
			m.childInbox = map[domain.SessionID]string{}
		}
		m.childInbox[msg.id] = msg.inbox
	}
	m.statusMsg = i18n.T("%s started in %s from the web page", msg.name, shortWorktreeName(msg.dir))
	return m, nil
}
```

Imports grow by `internal/config` and `internal/exttool` (both already imported elsewhere in `internal/tui`). The refusal sent to the page stays English (the page is English; `sessionPlace`'s own text is translated for the terminal). `webhost.go`: add to the `WebHost` interface

```go
	// SetSessionStarter installs the page's way to start a session: the
	// terminal starts it (its inbox, its agent channel) and returns the id.
	SetSessionStarter(fn func(ctx context.Context, req domain.SessionStartRequest) (domain.SessionID, error))
```

add `sessions chan webSessionRequestMsg` to `webHostState` (comment: "the page's start requests, HTTP goroutine → Update (waitWebSessionCmd)") and `sessions: make(chan webSessionRequestMsg)` in `newWebHostState`; in `startWebCmd` build `starter := sessionStarterFor(w)` beside `switcher` and call `h.SetSessionStarter(starter)` after `h.SetSwitcher(switcher)`; in `onWebStarted` append `waitWebSessionCmd(m.web)` next to `waitWebSwitchCmd(m.web)`. `model.go` Update, after `case webSwitchRequestMsg:`:

```go
	case webSessionRequestMsg:
		return m.onWebSessionRequest(msg)
	case webSessionStartedMsg:
		return m.onWebSessionStarted(msg)
```

`fakeWebHost` (webhost_test.go) gains the method storing the fn. Any existing test that counts `onWebStarted`'s batch children must expect one more.

- [ ] **Step 4: i18n.** Invoke the `adding-translations` skill; add the key `"%s started in %s from the web page"` to the ja/ko/zh/ru bundles (the other strings used above already exist: `Terminal`, `could not start %s: %s`). Run `rtk go test ./internal/tui -run 'TestI18n|TestWebSession|TestSessionStarter|TestWebHost'` → PASS.

- [ ] **Step 5: Run** `rtk go build ./... && rtk go test ./internal/tui ./internal/archtest` → PASS (the build proves `*web.Host` still satisfies `tui.WebHost` in `cmd/gg`).

- [ ] **Step 6: Commit** — `feat(tui): a hosted page's Start agent is the terminal's own start`.

---

### Task 6: page — kill and remove (console.js)

**Files:**
- Modify: `internal/web/static/console.js`
- Test: `internal/web/consolejs_test.go`

**Interfaces:**
- Produces (exports): `killSession(s, remove)`, `removeSession(s)` where `s` is a `/api/sessions` row `{id, label, worktree, state}`; pure `killPrompt(s, remove)`, `wtName(path)`.

- [ ] **Step 1: Write the failing test.** In `TestConsoleModelJS` append to the driver, before `console.log`:

```js
r.push(killPrompt({ label: "claude", worktree: "/a/b/wt" }, false), killPrompt({ label: "claude", worktree: "C:\\x\\wt2" }, true));
```

and to `want`: `|Kill claude in wt?|Kill claude in wt2 and remove it from the list?`. Add wiring rows to `consoleWiring`:

```go
	{"console.js", "/api/session-kill", "kill goes to its endpoint"},
	{"console.js", "/api/session-remove", "remove goes to its endpoint"},
	{"console.js", `k kill`, "the unfocused foot offers kill"},
	{"console.js", `X kill + remove`, "…and kill and remove"},
	{"console.js", `x remove`, "the exited foot offers remove"},
```

- [ ] **Step 2: Run** `rtk go test ./internal/web -run 'TestConsole'` → FAIL.

- [ ] **Step 3: Implement.** Pure section (before `// --- end console model ---`):

```js
function wtName(path) {
  return String(path).split(/[\\/]/).filter(Boolean).pop() || String(path);
}

function killPrompt(s, remove) {
  return "Kill " + s.label + " in " + wtName(s.worktree) + (remove ? " and remove it from the list?" : "?");
}
```

Import `showLocalConfirm` from `./ops.js`. This closes an import cycle (console.js → ops.js → sidebar.js → console.js); the page already lives with such cycles (ops ↔ sidebar ↔ layers) and it is safe here because `showLocalConfirm` is only CALLED from event handlers, never at module-evaluation time — keep it that way (no top-level use of an ops.js binding in console.js). Impure section:

```js
// killSession asks once, then kills (remove = the TUI's X: the session leaves
// the list when its exit is recorded). The row and any console on it follow
// the live sessions event; nothing is repainted here.
function killSession(s, remove) {
  if (!s || s.state === "exited") return remove ? removeSession(s) : toast(s ? s.label + " has already exited" : "that session is gone");
  const yes = remove ? "kill and remove" : "kill";
  showLocalConfirm(killPrompt(s, remove), [yes, "cancel"], async (o) => {
    if (o !== yes) return; // cancel, esc, a backdrop click — whatever the modal hands back
    try {
      await postJSON("/api/session-kill", { id: s.id, remove: !!remove });
      toast("killing " + s.label + " in " + wtName(s.worktree) + "…");
    } catch (e) {
      toast("kill: " + (e.message || e), { err: true });
    }
  });
}

async function removeSession(s) {
  if (!s) return;
  if (s.state !== "exited") return toast("only an exited session can be removed — X kills and removes a running one");
  try {
    await postJSON("/api/session-remove", { id: s.id });
  } catch (e) {
    toast("remove: " + (e.message || e), { err: true });
  }
}
```

`answerModal(option)` hands the callback the clicked option's text; a dismissed modal goes through `hideModal`, which drops the callback — so only an explicit click on the first option kills. Check that `"kill"`/`"kill and remove"` render as danger (add them to `DANGER_OPTIONS` in `core.js` if that set is how the modal colours options).

Foot + keys:

```js
const FOOT_UNFOCUSED = `<button data-cact="focus">enter focus</button><button data-cact="max">m maximize</button><button data-cact="kill">k kill</button><button data-cact="killrm">X kill + remove</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc / ctrl+] close</button>`;
const FOOT_EXITED = `<button data-cact="remove">x remove</button><button data-cact="sessions">ctrl+\\ sessions</button><button data-cact="close">esc / ctrl+] close</button>`;
```

the foot click map gains `kill: () => killSession(con.info, false), killrm: () => killSession(con.info, true), remove: () => removeSession(con.info)`; `consoleKey`'s unfocused switch gains

```js
    case "k": if (con.info.state !== "exited") killSession(con.info, false); break;
    case "X": if (con.info.state !== "exited") killSession(con.info, true); break;
    case "x": if (con.info.state === "exited") removeSession(con.info); break;
```

(an exited console is never focused, so these sit in the same switch; a focused console still sends `k`/`x` to the agent). Update the help row: "Unfocused: **enter** focus, **m** maximize, **k** kill, **X** kill and remove, **esc** … ; an exited one: **x** removes it." Export `killSession, removeSession`.

- [ ] **Step 4: Run** `rtk go test ./internal/web -run 'TestConsole'` → PASS.

- [ ] **Step 5: Commit** — `feat(web): kill, kill and remove, remove from the console`.

---

### Task 7: page — Start agent / Open terminal (sessions.js) and the menus

**Files:**
- Create: `internal/web/static/sessions.js`, `internal/web/sessionsjs_test.go`
- Modify: `internal/web/static/app.js`, `menus.js`, `sidebar.js`, `style.css`

**Interfaces:**
- Consumes: `/api/session-commands`, `/api/session-start`; `openConsole`, `killSession`, `removeSession` (console.js); `registerRows`, `extraRows` (menus.js); `worktreePathForBranch` (sidebar.js).
- Produces: exports `startAgent(path)`, `openTerminal(path)`; a new menu key `"session"`; pure `commandRowState(c)`, `dialogStep(d, key)`, `startRows(path)`, `sessionMenuRows(s)`.

- [ ] **Step 1: Write the failing tests** — `internal/web/sessionsjs_test.go`:

```go
package web

import (
	"strings"
	"testing"
)

func TestSessionsModelJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "sessions.js", "// --- sessions model (pure; guarded against Go) ---", "// --- end sessions model ---", `
const r = [];
r.push(commandRowState({ approved: true, found: true }), commandRowState({ approved: false, found: true }), commandRowState({ approved: true, found: false }));
const cmds = [{ name: "Claude", approved: true, found: true }, { name: "Codex", approved: false, found: true }, { name: "Junie", approved: true, found: false }];
const d = { phase: "choose", cmds, sel: 0 };
r.push(JSON.stringify(dialogStep(d, "ArrowDown")), JSON.stringify(dialogStep({ ...d, sel: 2 }, "ArrowDown")), JSON.stringify(dialogStep(d, "ArrowUp")));
r.push(JSON.stringify(dialogStep(d, "Enter")));                    // approved → start
r.push(JSON.stringify(dialogStep(d, "2")));                        // unapproved → approve phase
r.push(JSON.stringify(dialogStep(d, "9")));                        // no such row → nothing
r.push(JSON.stringify(dialogStep({ ...d, sel: 2 }, "Enter")));     // not found still starts: the server reports the failure
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Enter")));   // approve → start with approve
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds, sel: 1 }, "Escape")));  // back to the list
r.push(JSON.stringify(dialogStep({ phase: "approve", cmds: [cmds[1]], sel: 0 }, "Escape"))); // one command: esc closes
r.push(JSON.stringify(dialogStep(d, "Escape")));
r.push(JSON.stringify(dialogStep({ phase: "starting", cmds, sel: 0 }, "Enter")));  // a held enter starts nothing twice
r.push(JSON.stringify(dialogStep({ phase: "detecting", cmds: [], sel: 0 }, "Escape")));
r.push(JSON.stringify(startRows("/a/b/wt").map((x) => x.label)));
r.push(JSON.stringify(sessionMenuRows({ id: "s1", state: "running" }).map((x) => x.label)), JSON.stringify(sessionMenuRows({ id: "s2", state: "exited" }).map((x) => x.label)));
console.log(r.join("|"));
`)
	want := `approved|approve on start|not found|` +
		`{"sel":1}|{"sel":2}|{"sel":0}|` +
		`{"start":0,"approve":false}|{"sel":1,"phase":"approve"}|{}|{"start":2,"approve":false}|` +
		`{"start":1,"approve":true}|{"phase":"choose"}|{"close":true}|{"close":true}|{}|{"close":true}|` +
		`["Start agent in wt","Open terminal in wt"]|["Kill session","Kill and remove session"]|["Remove session"]`
	if out != want {
		t.Fatalf("got  %s\nwant %s", out, want)
	}
}

var sessionsWiring = []struct{ file, want, why string }{
	{"app.js", "./sessions.js", "the module must be imported at boot"},
	{"sessions.js", "/api/session-commands?worktree=", "the dialog lists the worktree's commands"},
	{"sessions.js", "/api/session-start", "starting goes to its endpoint"},
	{"sessions.js", "needs_approval", "a 403 falls back to the approval step"},
	{"sessions.js", "Detecting installed agents…", "the first run says what it is doing"},
	{"sessions.js", `registerRows("worktree"`, "worktree rows get Start agent / Open terminal"},
	{"sessions.js", `registerRows("branch"`, "branch rows with a worktree get them too"},
	{"sessions.js", `registerRows("session"`, "sub-rows get Kill / Remove"},
	{"sessions.js", "worktreePathForBranch(", "the branch gate is the sidebar's own lookup"},
	{"menus.js", `"session"`, "session is a registered menu key"},
	{"sidebar.js", `extraRows("session"`, "the sub-row menu collects the session rows"},
	{"style.css", "#sessstart.hidden", "hidden by id, never a global .hidden"},
}

func TestSessionsJSIsWired(t *testing.T) {
	t.Parallel()
	for _, c := range sessionsWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
	if strings.Contains(readStatic(t, "sessions.js"), "Start agent…") || strings.Contains(readStatic(t, "sessions.js"), "terminal…") {
		t.Error("menu labels carry no trailing … (user ruling)")
	}
}
```

- [ ] **Step 2: Run** `rtk go test ./internal/web -run 'TestSessionsModelJS|TestSessionsJSIsWired'` → FAIL (no such file).

- [ ] **Step 3: Implement `sessions.js`.**

```js
// sessions.js — starting sessions from the page (web attach, plan 2): the
// Start agent dialog (first-run detect, list, approval), Open terminal, and
// the menu rows that reach them. Kill and remove live in console.js.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { closeLayer, mountOverlay, popFoot, pushFoot, pushLayer } from "./layers.js";
import { registerHelp, registerRows } from "./menus.js";
import { toast } from "./toast.js";
import { killSession, openConsole, removeSession } from "./console.js";
import { worktreePathForBranch } from "./sidebar.js";

// --- sessions model (pure; guarded against Go) ---
function commandRowState(c) {
  return !c.found ? "not found" : c.approved ? "approved" : "approve on start";
}

function wtLeaf(path) {
  return String(path).split(/[\\/]/).filter(Boolean).pop() || String(path);
}

// dialogStep: what a key does to the dialog — a patch ({sel}, {phase}), a
// {start: i, approve} order, {close: true}, or {} for nothing. Pure: the
// dialog applies it. A dialog that is detecting or starting only closes.
function dialogStep(d, key) {
  if (d.phase === "detecting" || d.phase === "starting") return key === "Escape" && d.phase === "detecting" ? { close: true } : {};
  if (d.phase === "approve") {
    if (key === "Enter") return { start: d.sel, approve: true };
    if (key === "Escape") return d.cmds.length > 1 ? { phase: "choose" } : { close: true };
    return {};
  }
  const pick = (i) => (d.cmds[i].approved ? { start: i, approve: false } : { sel: i, phase: "approve" });
  if (key === "Escape") return { close: true };
  if (key === "ArrowDown" || key === "j") return { sel: Math.min(d.cmds.length - 1, d.sel + 1) };
  if (key === "ArrowUp" || key === "k") return { sel: Math.max(0, d.sel - 1) };
  if (key === "Enter") return d.cmds.length ? pick(d.sel) : {};
  if (key.length === 1 && key >= "1" && key <= "9") return Number(key) <= d.cmds.length ? pick(Number(key) - 1) : {};
  return {};
}

// startRows / sessionMenuRows: the menu rows as {label, id}; the impure side
// attaches the actions by id.
function startRows(path) {
  const n = wtLeaf(path);
  return [{ id: "agent", label: "Start agent in " + n }, { id: "terminal", label: "Open terminal in " + n }];
}

function sessionMenuRows(s) {
  return s.state === "exited"
    ? [{ id: "remove", label: "Remove session" }]
    : [{ id: "kill", label: "Kill session" }, { id: "killrm", label: "Kill and remove session" }];
}
// --- end sessions model ---

let dlg = null; // { phase, path, cmds, sel }
const root = mountOverlay("sessstart");
root.innerHTML = `<div id="sessstart-box"><div id="sessstart-title"></div><div id="sessstart-body"></div></div>`;
root.addEventListener("click", (e) => {
  if (e.target.id === "sessstart") return closeDialog();
  const li = e.target.closest("li[data-i]");
  if (li && dlg && dlg.phase === "choose") apply(dialogStep({ ...dlg, sel: Number(li.dataset.i) }, "Enter"), Number(li.dataset.i));
  if (e.target.closest("button[data-run]") && dlg && dlg.phase === "approve") apply(dialogStep(dlg, "Enter"));
});

const FOOT = { detecting: `<span>esc cancel</span>`, choose: `<span>↑↓ move · 1–9 / enter start · esc cancel</span>`, approve: `<span>enter run · esc back</span>`, starting: `<span>starting…</span>` };

function render() {
  if (!dlg) return;
  $("sessstart-title").textContent = dlg.phase === "approve" ? "Start this agent?  (" + dlg.cmds[dlg.sel].name + ")" : "Start agent in " + wtLeaf(dlg.path);
  const body = $("sessstart-body");
  if (dlg.phase === "detecting") body.innerHTML = `<div class="rnote">⏳ Detecting installed agents…</div>`;
  else if (dlg.phase === "starting") body.innerHTML = `<div class="rnote">starting ${esc(dlg.cmds[dlg.sel].name)}…</div>`;
  else if (dlg.phase === "approve")
    body.innerHTML =
      `<div class="rcmd">${esc(dlg.cmds[dlg.sel].command)}</div>` +
      `<div class="rnote">This runs on your machine with your permissions. Approval is remembered for this repo until the command text changes.</div>` +
      `<button data-run="1">run</button>`;
  else
    body.innerHTML =
      "<ul>" +
      dlg.cmds.map((c, i) => `<li data-i="${i}"${i === dlg.sel ? ' class="sel"' : ""}>${i + 1}  ${esc(c.name)}<span class="detail">${commandRowState(c)}</span></li>`).join("") +
      "</ul>";
  pushFoot("sessstart", FOOT[dlg.phase]);
}

function closeDialog() {
  dlg = null;
  closeLayer("sessstart");
  popFoot("sessstart");
}

function apply(step, sel) {
  if (!dlg) return;
  if (sel !== undefined) dlg.sel = sel;
  if (step.close) return closeDialog();
  if (step.start !== undefined) return run(step.start, step.approve);
  Object.assign(dlg, step);
  render();
}

// measure: the grid the console layer will have, so the session starts at
// the size its first viewer shows (no default-then-resize flash).
function measure() {
  const probe = document.createElement("span");
  probe.textContent = "M".repeat(20);
  probe.style.cssText = "visibility:hidden;position:absolute;font:12px/16px ui-monospace, Menlo, Consolas, monospace";
  document.body.append(probe);
  const r = probe.getBoundingClientRect();
  probe.remove();
  const panes = $("panes").getBoundingClientRect();
  const side = $("branches-pane");
  const left = side && side.offsetWidth ? side.getBoundingClientRect().right + 5 : panes.left;
  const h = $("foot").getBoundingClientRect().top - panes.top;
  return { cols: Math.max(20, Math.floor((window.innerWidth - left - 32) / (r.width / 20))), rows: Math.max(5, Math.floor((h - 50) / r.height)) };
}

async function post(body) {
  const resp = await postJSON("/api/session-start", Object.assign(body, measure()));
  openConsole(resp.session.id);
}

async function run(i, approve) {
  const d = dlg;
  d.sel = i;
  d.phase = "starting";
  render();
  try {
    await post({ worktree: d.path, tool: d.cmds[i].name, approve });
    if (dlg === d) closeDialog();
  } catch (e) {
    if (dlg !== d) return;
    if (e.data && e.data.needs_approval) {
      // The server's word wins over the list's: approve what will RUN.
      d.cmds[i] = { ...d.cmds[i], approved: false, command: e.data.command || d.cmds[i].command };
      d.phase = "approve";
      return render();
    }
    closeDialog();
    toast("could not start " + d.cmds[i].name + ": " + (e.message || e), { err: true });
  }
}

async function startAgent(path) {
  if (dlg) return;
  const d = (dlg = { phase: "detecting", path, cmds: [], sel: 0 });
  pushLayer("sessstart", root, { onKey: (e) => { apply(dialogStep(dlg, e.key)); e.preventDefault(); return true; } });
  render();
  let body;
  try {
    body = await getJSON("/api/session-commands?worktree=" + encodeURIComponent(path));
  } catch (e) {
    if (dlg === d) closeDialog();
    return toast("start agent: " + (e.message || e), { err: true });
  }
  if (dlg !== d) return; // closed while detecting
  if (body.added && body.added.length) toast("Added " + body.added.join(", ") + " to " + body.config_path + " — edit there or in Settings → External tools");
  d.cmds = body.commands || [];
  if (!d.cmds.length) {
    closeDialog();
    return toast('no agent found — add a [[tools.command]] block with category = "session" and mode = "session"', { err: true });
  }
  d.phase = "choose";
  if (d.cmds.length === 1) return apply(dialogStep(d, "Enter"));
  render();
}

async function openTerminal(path) {
  try {
    await post({ worktree: path, terminal: true });
  } catch (e) {
    toast("could not start a terminal: " + (e.message || e), { err: true });
  }
}

const startActs = (path) => startRows(path).map((r) => ({ label: r.label, act: () => (r.id === "agent" ? startAgent(path) : openTerminal(path)) }));

registerRows("worktree", (w) => (w && w.path && !w.bare ? startActs(w.path) : []));
registerRows("branch", (b) => {
  const path = b && worktreePathForBranch(b.name);
  return path ? startActs(path) : [];
});
registerRows("session", (s) =>
  sessionMenuRows(s).map((r) => ({
    label: r.label,
    danger: r.id !== "remove",
    act: () => (r.id === "remove" ? removeSession(s) : killSession(s, r.id === "killrm")),
  })));

registerHelp({
  key: "starting agents",
  html:
    "a worktree's menu (and a branch's, when it is checked out in a worktree) offers <b>Start agent in …</b> and <b>Open terminal in …</b>; the new session opens as a focused console. " +
    "A command runs only after you approve it once per repository. A session's own row offers <b>Kill</b>, <b>Kill and remove</b> and, once exited, <b>Remove</b>.",
});

export { openTerminal, startAgent };
```

Notes for the implementer: confirm `mountOverlay`'s return and hidden-class behaviour against `console.js`/`openfiles.js`; confirm the worktree wire field for a bare worktree is `bare`; `e.key === "j"/"k"` in the list is fine because the dialog owns the keyboard. `state` is imported only if used — drop it if not. The dialog closes (phase `starting`) on the first answer: review focus 3.

`menus.js`: add `"session"` to `MENUS`. `app.js`: `import "./sessions.js";` after `./console.js`. `sidebar.js`: import `showCtxMenu` is already there; replace the worktrees `contextmenu` handler's first lines so a sub-row gets its menu:

```js
$("worktrees-list").addEventListener("contextmenu", (e) => {
  const li = e.target.closest("li");
  if (!li) return;
  if (li.classList.contains("wsess")) {
    const s = (state.sessions || []).find((x) => x.id === li.dataset.sid);
    if (!s) return;
    e.preventDefault();
    return showCtxMenu([{ label: "Open session", act: () => openConsole(s.id) }, ...extraRows("session", s)], e.clientX, e.clientY);
  }
  if (!li.dataset.p) return;
  e.preventDefault();
  const w = state.worktrees.find((x) => x.path === li.dataset.p);
  if (w) showWorktreeMenu(w, e.clientX, e.clientY);
});
```

`sessions.js` imports `sidebar.js` and `sidebar.js` does **not** import `sessions.js` (it reaches the rows through `extraRows`) — no cycle. If the sidebar has a keyboard path to a row's menu (`.` in `keys.js`), check whether it can land on a `li.wsess`; if sub-rows are not keyboard-selectable today, leave that as is (note it in the CHANGELOG entry's scope, not a new feature).

`style.css` (after the `#console` block), reusing the review lane's look:

```css
/* The Start agent dialog (sessions.js, web attach plan 2). */
#sessstart { position: fixed; top: 0; left: 0; right: 0; bottom: var(--foot-h, 25px); background: rgba(0,0,0,.45); display: flex; align-items: flex-start; justify-content: center; padding-top: 14vh; z-index: 23; }
#sessstart.hidden { display: none; }
#sessstart-box { background: var(--bg-alt); border: 1px solid var(--accent); border-radius: 6px; width: min(640px, 92vw); max-height: 60vh; display: flex; flex-direction: column; }
#sessstart-title { padding: 8px 12px; border-bottom: 1px solid var(--border); }
#sessstart-body { overflow: auto; padding: 6px 0; font-family: ui-monospace, monospace; font-size: 12px; }
#sessstart-body ul { list-style: none; margin: 0; padding: 0; }
#sessstart-body li { padding: 2px 12px; cursor: pointer; white-space: nowrap; }
#sessstart-body li.sel { background: var(--sel); }
#sessstart-body li .detail { color: var(--dim); margin-left: 2ch; }
#sessstart-body .rcmd { margin: 6px 12px; padding: 6px 8px; background: var(--bg); border: 1px solid var(--border); white-space: pre-wrap; overflow-wrap: anywhere; }
#sessstart-body .rnote { margin: 6px 12px; color: var(--dim); font-family: inherit; }
#sessstart-body button { margin: 4px 12px 8px; }
```

(Check the variable names `--sel`, `--bg-alt`, `--dim` against the `#openfiles` rules above them — they are the reference.)

- [ ] **Step 4: Run** `rtk go test ./internal/web -run 'TestSessionsModelJS|TestSessionsJSIsWired|TestSidebar|TestConsole'` → PASS.

- [ ] **Step 5: Commit** — `feat(web): Start agent dialog, Open terminal, session rows in the worktree, branch and sub-row menus`.

---

### Task 8: page — the `ctrl+\` popup's Agents tab kills and removes

**Files:**
- Modify: `internal/web/static/openfiles.js`
- Test: `internal/web/openfilesjs_test.go` (the file holding the switcher's pure/wiring tests — find it with `grep -l "switcher model" internal/web/*_test.go`)

**Interfaces:**
- Consumes: `killSession`, `removeSession` from console.js.

TUI parity (its popup): `k` kills, `x` removes an exited session, `j`/`↓` and `↑` move. So on the **Agents** tab `k` stops being "up" (it stays "up" on the other two tabs).

- [ ] **Step 1: Write the failing test** — wiring rows in the switcher's test table (or a new `TestSwitcherLifecycleWired`):

```go
var switcherLifecycleWiring = []struct{ file, want, why string }{
	{"openfiles.js", "killSession(", "k / X on the Agents tab kill"},
	{"openfiles.js", "removeSession(", "x on the Agents tab removes an exited session"},
	{"openfiles.js", `k kill`, "the Agents foot offers kill"},
	{"openfiles.js", `x remove`, "…and remove"},
}

func TestSwitcherLifecycleWired(t *testing.T) {
	t.Parallel()
	for _, c := range switcherLifecycleWiring {
		if !strings.Contains(readStatic(t, c.file), c.want) {
			t.Errorf("%s lacks %q: %s", c.file, c.want, c.why)
		}
	}
}
```

and a pure test of the new key table (add `agentKey` to the pure section):

```go
func TestSwitcherAgentKeyJS(t *testing.T) {
	t.Parallel()
	out := runPureJS(t, "openfiles.js", "// --- switcher model (pure; guarded against Go) ---", "// --- end switcher model ---", `
console.log([agentKey("k", "running"), agentKey("X", "running"), agentKey("x", "exited"), agentKey("x", "running"), agentKey("k", "exited"), agentKey("X", "exited"), agentKey("j", "running")].join("|"));
`)
	if want := "kill|killrm|remove|refuse-remove|none|remove|none"; out != want {
		t.Fatalf("got %s want %s", out, want)
	}
}
```

- [ ] **Step 2: Run** `rtk go test ./internal/web -run 'TestSwitcher'` → FAIL.

- [ ] **Step 3: Implement.** Pure section:

```js
// agentKey: what k / X / x mean on a session row (the TUI popup's keys, plus
// its X). An exited row cannot be killed; X on one is x.
function agentKey(key, st) {
  if (key === "k") return st === "exited" ? "none" : "kill";
  if (key === "X") return st === "exited" ? "remove" : "killrm";
  if (key === "x") return st === "exited" ? "remove" : "refuse-remove";
  return "none";
}
```

Import `killSession, removeSession` from `./console.js`. Add:

```js
function agentAct(key) {
  const s = visibleSessions()[sw.sel];
  if (!s) return;
  const what = agentKey(key, s.state);
  if (what === "kill" || what === "killrm") {
    closeSwitcher(); // the confirm is a modal: one layer asks at a time
    killSession(s, what === "killrm");
  } else if (what === "remove") removeSession(s);
  else if (what === "refuse-remove") opLine("only an exited session can be removed — kill it first (k)", true);
}
```

`FOOT_AGENTS` becomes `↑↓ j move` + `enter open console` + `<button data-oact="kill">k kill</button><button data-oact="killrm">X kill + remove</button><button data-oact="x">x remove</button>` + filter/tab/esc; the foot click handler routes `kill`/`killrm` to `agentAct("k"/"X")` and `x` to `sw.tab === "agents" ? agentAct("x") : closeSelected()`. In `switcherKey`'s non-typing switch:

```js
    case "ArrowUp": sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); break;
    case "k":
      if (sw.tab === "agents") agentAct("k");
      else { sw.sel = clampSel(sw.sel - 1, currentCount()); renderSwitcher(); }
      break;
    case "X": if (sw.tab === "agents") agentAct("X"); break;
    case "x": if (sw.tab === "agents") agentAct("x"); else closeSelected(); break;
```

(`ArrowUp` loses its shared `case "k"` label.) A removed row leaves through `switcherSessions` (the live event), which already clamps the selection. Update the module header comment (Agents: "k kills, X kills and removes, x removes an exited one") and the empty-state line to `no agent sessions — a worktree's menu starts one`.

- [ ] **Step 4: Run** `rtk go test ./internal/web` → PASS (whole package: the header/foot strings are pinned by older wiring tests — fix any pin that named `j k move` for the Agents foot).

- [ ] **Step 5: Commit** — `feat(web): the sessions popup kills and removes — k, X, x on the Agents tab`.

---

### Task 9: the browser check

**Files:**
- Modify: `internal/web/attach_browser_test.go`
- Create (outside the repo): `<scratchpad>/checklifecycle.mjs` — never committed; playwright via the `node_modules` of `~/.claude/jobs/3683fa84/tmp` if still present, else `npm i playwright && npx playwright install chromium` in the scratchpad.

- [ ] **Step 1: Seed a session command in the host.** In `TestAttachBrowserHost`, after `isolateGlobal(t)` capture its return (`global := isolateGlobal(t)`), add `t.Setenv("XDG_STATE_HOME", t.TempDir())`, and write the global config before `New`:

```go
	if err := os.MkdirAll(filepath.Dir(global), 0o755); err != nil {
		t.Fatal(err)
	}
	// A session command that needs no detection and no real agent.
	const seeded = "[[tools.command]]\ncategory = \"session\"\nmode = \"session\"\nname = \"Shell\"\n" +
		"command = '''sh -c 'while true; do printf \"$ \"; read l || exit 0; eval \"$l\"; done' '''\n"
	if err := os.WriteFile(global, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}
```

Keep the pre-started `sh` session (plan 1's checks still use it). Run the host once by hand to confirm the config parses: `GG_BROWSER_CHECK=1 GG_BROWSER_DONE=/dev/null go test ./internal/web -run TestAttachBrowserHost -count=1` prints `ATTACH_URL=…` and returns at once.

- [ ] **Step 2: Write `checklifecycle.mjs`** (same helpers as plan 1's `checkattach.mjs`: `vis`, `txt`, `ok`, `wait`, `key`, `gridHas`; every step asserts **visibility**, not presence):

```js
import { chromium } from "playwright";
const [url] = process.argv.slice(2);
const b = await chromium.launch();
const p = await (await b.newContext({ viewport: { width: 1400, height: 800 } })).newPage();
const errs = [];
p.on("pageerror", (e) => errs.push(String(e)));
const vis = (id) => p.evaluate((id) => { const e = document.getElementById(id); if (!e) return false; const cs = getComputedStyle(e), r = e.getBoundingClientRect(); return cs.display !== "none" && cs.visibility !== "hidden" && r.width > 0 && r.height > 0; }, id);
const txt = (id) => p.evaluate((id) => (document.getElementById(id) || {}).textContent || "", id);
const res = [];
const ok = (name, cond, extra = "") => res.push((cond ? "PASS " : "FAIL ") + name + (extra ? "  " + extra : ""));
const wait = async (fn, ms) => { const t = Date.now(); while (Date.now() - t < ms) { if (await fn()) return true; await new Promise((r) => setTimeout(r, 100)); } return false; };
const key = async (k, ms = 300) => { await p.keyboard.press(k); await p.waitForTimeout(ms); };
const subRows = () => p.evaluate(() => [...document.querySelectorAll("#worktrees-list li.wsess")].map((x) => x.textContent));
const menuClick = async (label) => { const it = p.locator("#ctx-menu >> text=" + label).first(); if (!(await it.isVisible())) return false; await it.click(); return true; };

await p.goto(url);
await p.waitForSelector(".crow", { timeout: 15000 });
await p.waitForTimeout(800);
try {
  // 1: the worktree menu offers the two start rows (labels without …)
  await p.locator("#worktrees-list li[data-p]").first().click({ button: "right" });
  const menu = await txt("ctx-menu");
  ok("1 worktree menu offers Start agent in / Open terminal in", /Start agent in \S+/.test(menu) && /Open terminal in \S+/.test(menu) && !menu.includes("…"), JSON.stringify(menu.slice(0, 200)));
  // 2: Start agent → the approval step shows the command (one command: the list is skipped)
  ok("2a click Start agent", await menuClick("Start agent in"));
  ok("2 the approval step is visible with the command", (await wait(async () => (await vis("sessstart")) && (await txt("sessstart-body")).includes("read l"), 4000)), JSON.stringify(await txt("sessstart-body")));
  // 3: enter approves → the console opens focused with the prompt, the dialog is gone
  await key("Enter", 1500);
  ok("3 approved start opens a focused console", (await vis("console")) && !(await vis("sessstart")) && (await txt("foot")).includes("ctrl+] step out") && (await wait(async () => (await txt("console-grid")).includes("$"), 3000)), JSON.stringify(await txt("console-label")));
  // 4: typing reaches the started session
  await p.keyboard.type("echo life-cycle");
  await key("Enter", 300);
  ok("4 typed text echoes", await wait(async () => ((await txt("console-grid")).match(/life-cycle/g) || []).length >= 2, 3000));
  // 5: two sub-rows now (the pre-started sh + Shell)
  ok("5 the sidebar lists the new session", await wait(async () => (await subRows()).some((r) => r.includes("Shell") && r.includes("running")), 3000), JSON.stringify(await subRows()));
  // 6: step out, k asks once, kill → exited title + exited foot
  await key("Control+BracketRight", 400);
  await key("k", 500);
  ok("6a k asks once", (await vis("modal")) && (await txt("modal")).includes("Kill Shell in"), JSON.stringify(await txt("modal")));
  await p.locator("#modal >> text=kill").first().click();
  ok("6 killed: exited title and the remove foot", await wait(async () => (await txt("console-label")).includes("exited") && (await txt("foot")).includes("x remove"), 6000), JSON.stringify({ t: await txt("console-label"), f: await txt("foot") }));
  // 7: x removes it — the layer closes, the sub-row leaves
  await key("x", 800);
  ok("7 x removes: console closed, sub-row gone", !(await vis("console")) && (await wait(async () => !(await subRows()).some((r) => r.includes("Shell")), 3000)), JSON.stringify(await subRows()));
  // 8: a second start needs no approval (remembered) — straight to the console
  await p.locator("#worktrees-list li[data-p]").first().click({ button: "right" });
  await menuClick("Start agent in");
  ok("8 remembered approval: straight to the console", await wait(async () => (await vis("console")) && !(await vis("sessstart")), 4000));
  // 9: ctrl+\ → Agents; X on the running Shell kills and removes
  await key("Control+BracketRight", 300);
  await key("Control+Backslash", 600);
  const rows = await p.evaluate(() => [...document.querySelectorAll("#openfiles-list .ofrow")].map((x) => x.textContent));
  const idx = rows.findIndex((r) => r.includes("Shell"));
  for (let i = 0; i < 4; i++) await key("ArrowUp", 60);
  for (let i = 0; i < idx; i++) await key("j", 60);
  await key("Shift+X", 500);
  ok("9a X asks once", (await vis("modal")) && (await txt("modal")).includes("remove it from the list"), JSON.stringify(await txt("modal")));
  await p.locator("#modal >> text=kill and remove").first().click();
  ok("9 kill and remove: the session leaves every list", await wait(async () => !(await subRows()).some((r) => r.includes("Shell")), 6000), JSON.stringify(await subRows()));
  // 10: Open terminal from a sub-row's worktree menu starts without a dialog
  await key("Escape", 300);
  await p.locator("#worktrees-list li[data-p]").first().click({ button: "right" });
  await menuClick("Open terminal in");
  ok("10 Open terminal opens a console with no dialog", await wait(async () => (await vis("console")) && (await txt("console-label")).includes("Terminal"), 4000) && !(await vis("sessstart")));
  // 11: the sub-row's own menu
  await key("Control+BracketRight", 300);
  await key("Escape", 300);
  await p.locator("#worktrees-list li.wsess", { hasText: "Terminal" }).first().click({ button: "right" });
  const sm = await txt("ctx-menu");
  ok("11 a sub-row's menu: Open, Kill, Kill and remove", sm.includes("Open session") && sm.includes("Kill session") && sm.includes("Kill and remove session"), JSON.stringify(sm));
} catch (e) {
  ok("script ran to the end", false, String(e));
}
ok("no page errors", errs.length === 0, errs.join(" ; "));
console.log(res.join("\n"));
await b.close();
process.exit(res.some((r) => r.startsWith("FAIL")) ? 1 : 0);
```

Adjust selectors (`#modal`, `#ctx-menu`) to the ids the page really uses (`index.html`).

- [ ] **Step 3: Run it against the UNFIXED build first** (memory: a check must be seen failing). From the main checkout `/work/gigagit` (still at `014f0118`, no lifecycle code) run the host with the **seeded test file copied in only to a scratch copy** — simpler: `git stash`-free alternative is to run the host from this worktree at the commit **before Task 2** via a second temporary worktree: `git -C /work/gigagit worktree add --detach <scratchpad>/unfixed <merge-base>`, copy this branch's `attach_browser_test.go` over it, start `GG_BROWSER_CHECK=1 GG_BROWSER_DONE=<scratchpad>/done-unfixed go test ./internal/web -run TestAttachBrowserHost -count=1` in the background there, run `node checklifecycle.mjs <ATTACH_URL>`. Expected: step 1 FAILS (no start rows) and everything after it. Touch the done file; `git -C /work/gigagit worktree remove --force <scratchpad>/unfixed`.

- [ ] **Step 4: Run it against this branch.** Same host command from the feature worktree, same script. Expected: every line PASS. A failure here is a bug in Tasks 2–8 — fix it there (with a unit test when it is pure logic), not in the script.

- [ ] **Step 5: Hosted check (ruling 1), by hand with tui-capture.** Build `rtk go build -o bin/gg ./cmd/gg` (never `git add` `bin/`). In an isolated XDG home with the same seeded config, run the TUI with `--web` under `./tui-capture.sh` (pass the **binary path**, see the `driving-tui-headless` skill), read the served URL from the status line / `web.json`, then `curl -s -X POST <url>/api/session-start -H 'Content-Type: application/json' -H 'Origin: <url>' -d '{"worktree":"<wt>","tool":"Shell","approve":true,"cols":90,"rows":30}'`. Expected: 200 with the session; the TUI snapshot's status line reads `Shell started in <wt> from the web page`; the Worktrees tab shows the sub-row; no console docked. Then check the child's environment — type into it through `/api/session-input` (`env | grep GG_` + enter) and read `/api/session-screen`'s hello frame: `GG_INBOX`, `GG_MCP_URL` and `GG_SESSION_TOKEN` are all set. In a standalone `gg web` the same probe shows `GG_INBOX` only.

- [ ] **Step 6: Commit** — `test(web): the browser-check host seeds a sh session command`.

---

### Task 10: docs, gates, review

**Files:**
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, the plan-1 memory file after merge.

- [ ] **Step 1: CHANGELOG** — one entry: the page starts agents and terminals from the worktree and branch menus (dialog, first-run detect, approval), kills / kills and removes / removes from the console, the popup (`k`, `X`, `x` — `k` no longer moves up on the Agents tab) and the sub-row menu; a hosted page's start is the terminal's own (inbox + agent channel), a standalone `gg web`'s has no agent channel.
- [ ] **Step 2: README** — the gg web console section: starting, the dialog's row states (`approved` / `approve on start` / `not found`), the three verbs and where they are; the standalone-vs-hosted difference in one sentence.
- [ ] **Step 3: `docs/CLAUDE-details.md`** — web attach section: the four endpoints (wire shapes, status codes), the starter seam (`Host.SetSessionStarter` ↔ `tui/websession.go`), the worktree allowlist, "handlers never broadcast — the manager's signal does". No CLAUDE.md change (no new package, no new convention); no skill bump (no CLI change).
- [ ] **Step 4: Gates.** `cd /work/gigagit/.claude/worktrees/web-session-lifecycle && ./test.sh > <scratchpad>/gate.log 2>&1; tail -5 <scratchpad>/gate.log` then `./test.sh race > <scratchpad>/race.log 2>&1` — green ONLY on the literal "all green" line in each log (never trust `| tail`'s exit code).
- [ ] **Step 5: Commit** docs — `docs: web session lifecycle`.
- [ ] **Step 6: Whole-branch review** — a fresh read-only review subagent on the most capable model over `git diff main...feat/web-session-lifecycle` with the spec and this plan; fix Critical/Important findings (TDD), list minors for the user.
- [ ] **Step 7: Hand off** — report to the user with the verify binary's absolute path (`/work/gigagit/.claude/worktrees/web-session-lifecycle/bin/gg`, rebuilt after the last change); ASK before merging (`gg merge -F <msgfile> --into main feat/web-session-lifecycle`), then `./build.sh install`, `./build.sh web`, check main is clean, update the memory file (`agent-sessions-feature.md`: plan 2 merged, rulings 1–3 above, deferred: finished-task results in the viewer; NEXT = plan 3 states).

---

## Out of scope (recorded so nobody re-derives it)

- Finished AI-task results in the web viewer (ruling 2).
- Session states / badges / notices (plan 3).
- Keyboard selection of sidebar sub-rows (they are click / right-click today).
- The cross-environment *repair* offer on start: an other-notation worktree starts in its translated path, as in the TUI; repairing stays the switch flow's job.
- `console.js` `flush()` tests `e.status !== 409` but `postJSON` errors carry no `status` (so an input to a just-exited session toasts "input failed"). Pre-existing; mention to the user, fix only if asked.
