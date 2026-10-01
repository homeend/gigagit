# Agent Spawn — Plan B (reach) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (this repo forbids implementer subagents — CLAUDE.md). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Agents actually reach the TUI's agent channel: `gg mcp` forwards the six agent tools when it runs inside a gg console, `gg agent …` CLI twins exist, the discovery files name the channel, `gg init --mcp` registers `gg mcp` with Claude Code, and the docs describe it all.

**Architecture:** A new leaf `internal/agentlink` is the one MCP client of the TUI channel (bearer header, lazy connect, one reconnect). `internal/mcp` uses it to register forwarding tools; `internal/cli` uses it for `gg agent`. Discovery adds a URL field to `sessionreg.Registry` and `steer.Presence`. Registration shells out to `claude mcp add -s user`.

**Tech Stack:** Go 1.26, go-sdk v1.6.1 (`mcp` client: `StreamableClientTransport`), `claude` CLI (registration only).

**Spec:** `docs/superpowers/specs/2026-10-01-agent-spawn-design.md` (§9 "Plan B — reach"). Plan A (core) is implemented on this branch (`feat/agent-spawn`).

## Global Constraints

- Frontends never import `agentsession`, `sessionreg`, `wtclaim`, `git` in non-test code (archtest).
- `agentlink` is a DAG leaf: stdlib + go-sdk only (no gigagit package).
- Agent tools inside `gg mcp` exist only when BOTH `GG_MCP_URL` and `GG_SESSION_TOKEN` are set; outside gg they are absent (MCP) or refused with exit 2 (CLI): "run this inside a gg console (GG_MCP_URL and GG_SESSION_TOKEN are unset)".
- CLI exit codes: 0 ok, 1 transport/IO failure, 2 refusal (a tool error) or usage.
- The token is never printed, logged or written to disk by any of this.
- `gg init --mcp` never edits Claude's files directly — it runs `claude mcp add -s user gg -- <absolute gg path> mcp`, and leaves an existing `gg` entry alone.
- Docs: CHANGELOG always, README, `internal/agentskill/using-gg.md` + `agentskill.Version` 113 → 114, the dogfood `.claude/skills/using-gg/SKILL.md` regenerated from the embedded skill (never by running an installed `gg init --update`), `docs/CLAUDE-details.md` section, CLAUDE.md map rows (one line each).
- Tests that call `t.Setenv` are not `t.Parallel()`.
- Commit with `git -C /work/gigagit/.claude/worktrees/agent-spawn …`; never `git add -A`.

## Rulings made while planning (ledger them at execution start)

- R7: registration is an explicit `gg init --mcp`, not part of the skill-install flow the spec's §3.1 row implied — it changes the user's Claude configuration, so it must be asked for by name — cost if wrong: one extra flag to learn (the README and skill say it).
- R8: the `gg mcp` forwarder reuses the host's input structs and tool definitions (definition funcs, a fresh `*sdk.Tool` per registration) and returns the remote result as-is — no second schema to drift.
- R9: `gg agent list` outside gg prints the registry files only (no MCP call: no token) — spec §6.

## Review Focus

1. `gg mcp` started inside a console whose TUI has quit — every agent tool returns a clear "gg's agent channel is not reachable" error, the repo tools keep working (Task 2 `TestForwarderReportsUnreachableHost`).
2. A forwarded tool whose remote call is a refusal (IsError) — the agent sees the refusal text, not a transport error (Task 2 `TestForwarderPassesRefusalsThrough`).
3. `gg agent send <id>` with no text and no keys — usage error, exit 2, nothing typed (Task 4 `TestAgentSendNeedsTextOrKeys`).
4. `gg agent start --prompt-file -` reading the brief from stdin (Task 4 `TestAgentStartReadsPromptFromStdin`).
5. `gg init --mcp` with `claude` missing from PATH — a message naming the manual command, exit 1, nothing else attempted (Task 5 `TestRegisterClaudeMCPNoClaude`).

---

### Task 0: spike — does a Claude stdio MCP child inherit the console's env? (throwaway, STOP and report)

**Files:** scratch only (`$SCRATCH=/tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/spike-mcp`); nothing committed.

- [ ] **Step 1: Write a probe MCP server config**

```bash
SP=/tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/spike-mcp
mkdir -p $SP && cd /work/gigagit/.claude/worktrees/agent-spawn && go build -o $SP/gg ./cmd/gg
cat > $SP/probe.sh <<EOF
#!/bin/sh
env | grep '^GG_PROBE' > $SP/env.txt
exec $SP/gg mcp
EOF
chmod +x $SP/probe.sh
printf '{"mcpServers":{"probe":{"command":"%s/probe.sh","args":[]}}}' $SP > $SP/mcp.json
```

- [ ] **Step 2: Run Claude with the probe, an env var set in its environment**

Run: `cd $SP && GG_PROBE_URL=http://x GG_PROBE_TOKEN=abc timeout 120 claude -p "Reply with the single word ok." --mcp-config $SP/mcp.json --strict-mcp-config; cat $SP/env.txt`
Expected: `env.txt` lists `GG_PROBE_URL=http://x` and `GG_PROBE_TOKEN=abc`.

- [ ] **Step 3: Check `claude mcp get` on a missing name**

Run: `claude mcp get gg-spike-nonexistent; echo "exit=$?"`
Expected: a non-zero exit (Task 5 relies on it to detect "not registered").

- [ ] **Step 4: Ledger and decide**

Both match → ledger `Task 0: spike PASS (env inherited; mcp get missing → exit N)` and continue. Env NOT inherited → STOP and ask the user (the design's identity premise fails; options: an explicit `env` block in the registration using `${GG_MCP_URL}` expansion, or per-spawn `--mcp-config`). `mcp get` exits 0 for a missing name → Task 5 parses its stdout instead (ruling).

---

### Task 1: `internal/agentlink` — the channel client

**Files:**
- Create: `internal/agentlink/agentlink.go`, `internal/agentlink/agentlink_test.go`
- Modify: `internal/archtest/import_guard_test.go` (leaf entry)

**Interfaces:**
- Produces:
  - `type Client struct` (unexported fields)
  - `func FromEnv(getenv func(string) string) (*Client, bool)` — false unless both `GG_MCP_URL` and `GG_SESSION_TOKEN` are non-empty
  - `func New(url, token string) *Client`
  - `func (c *Client) Call(ctx context.Context, tool string, args any) (*sdk.CallToolResult, error)` — connects lazily; on a failed call it drops the session, reconnects once and retries
  - `func (c *Client) Close()`
  - `func ResultText(res *sdk.CallToolResult) string` — the result's text content joined by newlines
  - `func Decode(res *sdk.CallToolResult, out any) error` — `res.StructuredContent` → out (JSON round trip)
  - `var ErrUnreachable` wrapping every connect failure: message "gg's agent channel is not reachable"

- [ ] **Step 1: Write the failing test**

```go
package agentlink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type echoIn struct {
	Say string `json:"say"`
}
type echoOut struct {
	Said string `json:"said"`
}

// channel serves one "echo" tool behind a fixed bearer token.
func channel(t *testing.T) *httptest.Server {
	t.Helper()
	srv := sdk.NewServer(&sdk.Implementation{Name: "t", Version: "0"}, nil)
	sdk.AddTool(srv, &sdk.Tool{Name: "echo"}, func(_ context.Context, _ *sdk.CallToolRequest, in echoIn) (*sdk.CallToolResult, echoOut, error) {
		if in.Say == "no" {
			return nil, echoOut{}, errors.New("refused")
		}
		return nil, echoOut{Said: in.Say}, nil
	})
	h := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv }, nil)
	verify := func(_ context.Context, tok string, _ *http.Request) (*auth.TokenInfo, error) {
		if tok != "good" {
			return nil, auth.ErrInvalidToken
		}
		return &auth.TokenInfo{UserID: "p/s1", Expiration: time.Now().Add(time.Hour)}, nil
	}
	ts := httptest.NewServer(auth.RequireBearerToken(verify, nil)(h))
	t.Cleanup(ts.Close)
	return ts
}

func TestFromEnvNeedsBoth(t *testing.T) {
	env := map[string]string{"GG_MCP_URL": "http://x"}
	if _, ok := FromEnv(func(k string) string { return env[k] }); ok {
		t.Fatal("a URL without a token must not count as inside gg")
	}
	env["GG_SESSION_TOKEN"] = "t"
	if _, ok := FromEnv(func(k string) string { return env[k] }); !ok {
		t.Fatal("both set = inside gg")
	}
}

func TestCallDecodesAndPassesRefusals(t *testing.T) {
	ts := channel(t)
	c := New(ts.URL, "good")
	defer c.Close()
	res, err := c.Call(context.Background(), "echo", echoIn{Say: "hi"})
	if err != nil || res.IsError {
		t.Fatalf("call = %v %+v", err, res)
	}
	var out echoOut
	if err := Decode(res, &out); err != nil || out.Said != "hi" {
		t.Fatalf("decode = %+v %v", out, err)
	}
	res, err = c.Call(context.Background(), "echo", echoIn{Say: "no"})
	if err != nil || !res.IsError || !strings.Contains(ResultText(res), "refused") {
		t.Fatalf("a tool refusal is a result, not a transport error: %v %+v", err, res)
	}
}

func TestCallReconnectsOnce(t *testing.T) {
	ts := channel(t)
	c := New(ts.URL, "good")
	defer c.Close()
	if _, err := c.Call(context.Background(), "echo", echoIn{Say: "a"}); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	c.cs.Close() // the session died (TUI restarted its listener, network blip)
	c.mu.Unlock()
	if _, err := c.Call(context.Background(), "echo", echoIn{Say: "b"}); err != nil {
		t.Fatalf("one reconnect must recover: %v", err)
	}
}

func TestBadTokenIsUnreachable(t *testing.T) {
	ts := channel(t)
	c := New(ts.URL, "bad")
	defer c.Close()
	if _, err := c.Call(context.Background(), "echo", echoIn{Say: "a"}); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd /work/gigagit/.claude/worktrees/agent-spawn && go test ./internal/agentlink`
Expected: FAIL — `undefined: New`, `FromEnv`, …

- [ ] **Step 3: Implement `agentlink.go`**

```go
// Package agentlink is the one client of a gg TUI's agent channel (the MCP
// server the TUI hosts on loopback): `gg mcp` forwards its agent tools
// through it and `gg agent …` calls it. It reads GG_MCP_URL and
// GG_SESSION_TOKEN, sends the token as a bearer header on every request,
// connects lazily and reconnects once. DAG leaf: stdlib + go-sdk.
package agentlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrUnreachable wraps every failure to reach the channel.
var ErrUnreachable = errors.New("gg's agent channel is not reachable")

type Client struct {
	url, token string
	mu         sync.Mutex
	cs         *sdk.ClientSession
}

func New(url, token string) *Client { return &Client{url: url, token: token} }

// FromEnv is the client of the TUI that started this process; false
// outside gg (either variable unset).
func FromEnv(getenv func(string) string) (*Client, bool) {
	url, tok := getenv("GG_MCP_URL"), getenv("GG_SESSION_TOKEN")
	if url == "" || tok == "" {
		return nil, false
	}
	return New(url, tok), true
}

type bearer struct {
	token string
	base  http.RoundTripper
}

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(r)
}

func (c *Client) session(ctx context.Context) (*sdk.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cs != nil {
		return c.cs, nil
	}
	tr := &sdk.StreamableClientTransport{Endpoint: c.url, MaxRetries: -1, DisableStandaloneSSE: true,
		HTTPClient: &http.Client{Transport: bearer{token: c.token, base: http.DefaultTransport}}}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "gg", Version: "agentlink"}, nil).Connect(ctx, tr, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	c.cs = cs
	return cs, nil
}

func (c *Client) drop(cs *sdk.ClientSession) {
	c.mu.Lock()
	if c.cs == cs {
		c.cs = nil
	}
	c.mu.Unlock()
	_ = cs.Close()
}

// Call runs tool with args. A tool refusal comes back as a result with
// IsError set; an error means the channel itself failed (after one retry).
func (c *Client) Call(ctx context.Context, tool string, args any) (*sdk.CallToolResult, error) {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		cs, err := c.session(ctx)
		if err != nil {
			return nil, err
		}
		res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
		if err == nil {
			return res, nil
		}
		last = err
		c.drop(cs)
	}
	return nil, fmt.Errorf("%w: %v", ErrUnreachable, last)
}

func (c *Client) Close() {
	c.mu.Lock()
	cs := c.cs
	c.cs = nil
	c.mu.Unlock()
	if cs != nil {
		_ = cs.Close()
	}
}

// ResultText joins a result's text content.
func ResultText(res *sdk.CallToolResult) string {
	var parts []string
	for _, ct := range res.Content {
		if t, ok := ct.(*sdk.TextContent); ok {
			parts = append(parts, t.Text)
		}
	}
	return strings.Join(parts, "\n")
}

// Decode copies a result's structured content into out.
func Decode(res *sdk.CallToolResult, out any) error {
	data, err := json.Marshal(res.StructuredContent)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
```

In `internal/archtest/import_guard_test.go` `TestLayeringDAG` cases add:

```go
		// agentlink is a leaf: the agent-channel client knows no gigagit layer.
		"agentlink": {"config", "model", "git", "engine", "domain", "tui", "cli", "mcp", "web", "app", "steer", "agentsession", "sessionreg"},
```

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/agentlink ./internal/archtest`
Expected: PASS. (If `TestCallReconnectsOnce` shows the first `Call` after `Close` returning a non-error result with no reconnect, the SDK reports a closed session lazily — keep the test: it must pass either way, as both paths end in a good result.)

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/agentlink internal/archtest
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(agentlink): the agent-channel client — bearer header, lazy connect, one reconnect"
```

---

### Task 2: `gg mcp` forwards the agent tools inside a gg console

**Files:**
- Modify: `internal/mcp/agenttools.go` (export-free tool defs shared with the forwarder)
- Create: `internal/mcp/agentforward.go`, `internal/mcp/agentforward_test.go`
- Modify: `internal/mcp/server.go` (`Server.agent`, `New`, `sdkServer`)

**Interfaces:**
- Consumes: Task 1 `agentlink.FromEnv`, `Client.Call`; plan A `NewAgentHost().Handler`, `domain.StartAgentSession`.
- Produces: `var agentEnv = os.Getenv` (test seam in package mcp); `func (s *Server) registerAgentForwarders(srv *sdk.Server)`; tool definition funcs `toolAgentStart() *sdk.Tool` … `toolAgentTask() *sdk.Tool` (fresh value per call — `sdk.AddTool` fills the schema in place).

- [ ] **Step 1: Write the failing tests** (`agentforward_test.go`)

```go
package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// stdioWith connects an in-memory client to a gg mcp server built with env.
func stdioWith(t *testing.T, dir string, env map[string]string) *sdk.ClientSession {
	t.Helper()
	prev := agentEnv
	agentEnv = func(k string) string { return env[k] }
	t.Cleanup(func() { agentEnv = prev })
	svc := domainOpenForTest(t, dir)
	srv := New(svc).sdkServer()
	ct, st := sdk.NewInMemoryTransports()
	if _, err := srv.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return cs
}

func toolNames(t *testing.T, cs *sdk.ClientSession) map[string]bool {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, tl := range res.Tools {
		out[tl.Name] = true
	}
	return out
}

func TestForwarderAbsentOutsideGG(t *testing.T) {
	url, _, _ := hostEnv(t, nil)
	cs := stdioWith(t, t.TempDir(), map[string]string{"GG_MCP_URL": url}) // no token
	if toolNames(t, cs)["agent_list"] {
		t.Fatal("agent tools must be absent without a session token")
	}
}

func TestForwarderCallsTheTUI(t *testing.T) {
	url, tok, full := hostEnv(t, nil)
	cs := stdioWith(t, t.TempDir(), map[string]string{"GG_MCP_URL": url, "GG_SESSION_TOKEN": tok})
	names := toolNames(t, cs)
	for _, n := range []string{"agent_start", "agent_list", "agent_screen", "agent_send", "agent_kill", "agent_task"} {
		if !names[n] {
			t.Fatalf("%s missing from the forwarder", n)
		}
	}
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}})
	if err != nil || res.IsError || !strings.Contains(resultText(res), full) {
		t.Fatalf("forwarded agent_list = %v %s", err, resultText(res))
	}
}

func TestForwarderPassesRefusalsThrough(t *testing.T) {
	url, tok, _ := hostEnv(t, nil)
	cs := stdioWith(t, t.TempDir(), map[string]string{"GG_MCP_URL": url, "GG_SESSION_TOKEN": tok})
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_task", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), "not started by an agent") {
		t.Fatalf("the remote refusal must reach the agent verbatim: %v %s", err, resultText(res))
	}
}

func TestForwarderReportsUnreachableHost(t *testing.T) {
	cs := stdioWith(t, t.TempDir(), map[string]string{"GG_MCP_URL": "http://127.0.0.1:1/mcp", "GG_SESSION_TOKEN": "x"})
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), "not reachable") {
		t.Fatalf("an unreachable TUI = %v %s", err, resultText(res))
	}
	if !toolNames(t, cs)["gg_ui_state"] {
		t.Fatal("the repo tools stay registered")
	}
}
```

Add the helper to the same file:

```go
// domainOpenForTest opens dir (a repo or not) the way Serve does.
func domainOpenForTest(t *testing.T, dir string) *domain.Service {
	t.Helper()
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	return svc
}
```

(import `github.com/homeend/gigagit/internal/domain`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/mcp -run Forwarder`
Expected: FAIL — `undefined: agentEnv`.

- [ ] **Step 3: Implement**

In `agenttools.go`, replace each inline `&sdk.Tool{…}` in `RegisterAgentTools` with a call to a definition func, and add the funcs:

```go
func toolAgentStart() *sdk.Tool {
	return &sdk.Tool{Name: "agent_start", Description: "Start a worker agent in a worktree with a task; the worktree claim passes to the worker."}
}
func toolAgentList() *sdk.Tool {
	return &sdk.Tool{Name: "agent_list", Description: "Every agent session of this gg; mine = started by you.", Annotations: readOnlyAnnotations()}
}
func toolAgentScreen() *sdk.Tool {
	return &sdk.Tool{Name: "agent_screen", Description: "A session's visible console text.", Annotations: readOnlyAnnotations()}
}
func toolAgentSend() *sdk.Tool {
	return &sdk.Tool{Name: "agent_send", Description: "Type into an agent you started: paste text, Enter (default), then keys."}
}
func toolAgentKill() *sdk.Tool {
	return &sdk.Tool{Name: "agent_kill", Description: "End an agent you started."}
}
func toolAgentTask() *sdk.Tool {
	return &sdk.Tool{Name: "agent_task", Description: "Your own task, when an agent started you.", Annotations: readOnlyAnnotations()}
}
```

`agentforward.go`:

```go
package mcp

import (
	"context"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/agentlink"
)

// agentEnv reads the console's environment (a test seam).
var agentEnv = os.Getenv

// addForward registers t on srv, forwarding each call to the TUI's channel
// as-is: a remote refusal stays a refusal, a dead channel is a tool error.
func addForward[In any](srv *sdk.Server, c *agentlink.Client, t *sdk.Tool) {
	sdk.AddTool(srv, t, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, any, error) {
		res, err := c.Call(ctx, t.Name, in)
		if err != nil {
			return nil, nil, err
		}
		return res, nil, nil
	})
}

// registerAgentForwarders adds the agent tools of the TUI that started this
// gg mcp (GG_MCP_URL + GG_SESSION_TOKEN); a no-op outside gg.
func (s *Server) registerAgentForwarders(srv *sdk.Server) {
	if s.agent == nil {
		return
	}
	addForward[agentStartIn](srv, s.agent, toolAgentStart())
	addForward[struct{}](srv, s.agent, toolAgentList())
	addForward[agentIDIn](srv, s.agent, toolAgentScreen())
	addForward[agentSendIn](srv, s.agent, toolAgentSend())
	addForward[agentKillIn](srv, s.agent, toolAgentKill())
	addForward[struct{}](srv, s.agent, toolAgentTask())
}
```

`server.go`: add the field `agent *agentlink.Client // the TUI's agent channel; nil outside gg`; at the top of `New` (before the repo resolution, so it works outside a repo too) `if c, ok := agentlink.FromEnv(agentEnv); ok { s.agent = c }` (declare `s` first); in `sdkServer` create the server with instructions when `s.agent != nil`:

```go
	var opts *sdk.ServerOptions
	if s.agent != nil {
		opts = &sdk.ServerOptions{Instructions: agentInstructions}
	}
	srv := sdk.NewServer(&sdk.Implementation{Name: "gg", Version: buildinfo.Version}, opts)
```

and call `s.registerAgentForwarders(srv)` after `s.registerNoteTools(srv)`.

- [ ] **Step 4: Run tests**

Run: `go test -race ./internal/mcp ./internal/archtest`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/mcp
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(mcp): gg mcp inside a gg console forwards the agent tools to the TUI's channel"
```

---

### Task 3: discovery — the files name the channel; who is running

**Files:**
- Modify: `internal/sessionreg/sessionreg.go` (Registry.MCP)
- Modify: `internal/steer/steer.go` (Presence.MCP)
- Modify: `internal/domain/sessionpublish.go` (PublishSessions/snapshotRegistry take the URL; `LiveAgentHosts`)
- Modify: `internal/tui/run.go`, `internal/tui/steer.go`, `internal/tui/steer_kept.go` (presence + publish carry `m.agentURL()`; host starts before the inbox)
- Test: `internal/domain/sessionpublish_test.go`

**Interfaces:**
- Produces:
  - `sessionreg.Registry.MCP string` (`json:"mcp,omitempty"`), `steer.Presence.MCP string` (`json:"mcp,omitempty"`)
  - `func PublishSessions(ctx context.Context, dir string, worktree func() string, mcpURL string)`
  - `type AgentHostInfo struct { PID int; Worktree, MCP string; Sessions []AgentHostSession }`, `type AgentHostSession struct { ID, Agent, Label, Dir, State string }` (json snake_case)
  - `func LiveAgentHosts() []AgentHostInfo` (reads `SessionRegistryDir()`)
  - `func liveAgentHostsIn(dir string) []AgentHostInfo` (test seam)

- [ ] **Step 1: Write the failing test** (append to `sessionpublish_test.go`)

```go
func TestRegistryCarriesTheChannelURL(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { PublishSessions(ctx, dir, func() string { return "/wt" }, "http://127.0.0.1:9/mcp"); close(done) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		hosts := liveAgentHostsIn(dir)
		if len(hosts) == 1 && hosts[0].MCP == "http://127.0.0.1:9/mcp" && hosts[0].Worktree == "/wt" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("hosts = %+v", hosts)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain -run TestRegistryCarriesTheChannelURL`
Expected: FAIL — too many arguments to `PublishSessions` / `undefined: liveAgentHostsIn`.

- [ ] **Step 3: Implement**

`sessionreg.Registry` gains `MCP string \`json:"mcp,omitempty"\` // the TUI's agent channel URL ("" = none); never a token`. `steer.Presence` gains `MCP string \`json:"mcp,omitempty"\` // tui.json: the agent channel URL`.

`sessionpublish.go`: `PublishSessions(ctx, dir, worktree, mcpURL)`; `snapshotRegistry(started, wt, mcpURL string)` sets `MCP: mcpURL`; update every call (`write`, and `readLive`'s `snapshotRegistry("", "", "")`). Add:

```go
// AgentHostInfo is one live gg TUI as its registry file describes it.
type AgentHostInfo struct {
	PID      int                `json:"pid"`
	Worktree string             `json:"worktree"`
	MCP      string             `json:"mcp,omitempty"`
	Sessions []AgentHostSession `json:"sessions"`
}

type AgentHostSession struct {
	ID    string `json:"id"`
	Agent string `json:"agent,omitempty"`
	Label string `json:"label,omitempty"`
	Dir   string `json:"dir"`
	State string `json:"state"`
}

// LiveAgentHosts lists every live gg TUI on this machine (read-only, no
// channel call): what `gg agent list` shows outside a gg console.
func LiveAgentHosts() []AgentHostInfo { return liveAgentHostsIn(SessionRegistryDir()) }

func liveAgentHostsIn(dir string) []AgentHostInfo {
	var out []AgentHostInfo
	for _, r := range sessionreg.Live(dir) {
		h := AgentHostInfo{PID: r.PID, Worktree: r.Worktree, MCP: r.MCP}
		for _, e := range r.Sessions {
			h.Sessions = append(h.Sessions, AgentHostSession{ID: e.ID, Agent: e.Agent, Label: e.Label, Dir: e.Dir, State: e.State})
		}
		out = append(out, h)
	}
	return out
}
```

Fix every other `PublishSessions(` / `snapshotRegistry(` caller (`grep -rn "PublishSessions(\|snapshotRegistry(" internal`) — tests pass `""`.

`internal/tui/run.go`: move `m = m.startAgentHost()` + its `defer` ABOVE `m.steerDir = steerDirFor(…)` / `m = m.initSteerInbox()` (the presence written there must carry the URL), and change the publish line to `go domain.PublishSessions(pubCtx, domain.SessionRegistryDir(), publishedWorktree, m.agentURL())`.

`internal/tui/steer.go` (both `steer.Presence{` literals) and `internal/tui/steer_kept.go` (its literal): add `MCP: m.agentURL(),` (in `steer_kept.go`, if the literal is built where no `Model` is in scope, thread the URL in as a parameter from the caller that has `m`).

- [ ] **Step 4: Run tests**

Run: `go test ./internal/domain ./internal/sessionreg ./internal/steer ./internal/tui ./internal/web ./internal/archtest`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/sessionreg internal/steer internal/domain internal/tui
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(agents): the session registry and the TUI presence name the agent channel; LiveAgentHosts"
```

---

### Task 4: `gg agent …` CLI twins

**Files:**
- Create: `internal/cli/agent.go`, `internal/cli/agent_test.go`
- Modify: `internal/cli/cli.go` (`case "agent"`, `commands["agent"]`)
- Create: `e2e/scenarios/agent-outside-gg.toml`
- Modify: `e2e/env_test.go` (unset the channel env in TestMain)

**Interfaces:**
- Consumes: Task 1 `agentlink`; Task 3 `domain.LiveAgentHosts`; plan A domain types `AgentEntry`, `AgentStartResult`.
- Produces: `var agentGetenv = os.Getenv` (test seam); `func cmdAgent(rest []string, stdin io.Reader, stdout, stderr io.Writer) int`.

- [ ] **Step 1: Write the failing tests** (`agent_test.go`, serial)

```go
package cli

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/mcp"
)

// agentEnvFor serves a real agent channel with a fake starter and one
// manual agent session; agentGetenv returns its URL + token.
func agentEnvFor(t *testing.T, starter mcp.Starter) (dir, full string) {
	t.Helper()
	restore := domain.UseSessionManager(agentsession.NewManager())
	dir = newRepoDir(t)
	ts := httptest.NewServer(mcp.NewAgentHost().Handler(starter))
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, tok, err := domain.Open(dir).StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, ts.URL+"/mcp", domain.SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	env := map[string]string{"GG_MCP_URL": ts.URL + "/mcp", "GG_SESSION_TOKEN": tok}
	prev := agentGetenv
	agentGetenv = func(k string) string { return env[k] }
	t.Cleanup(func() {
		agentGetenv = prev
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		domain.Sessions().KillAll(ctx)
		restore()
	})
	return dir, domain.FullSessionID(s.Info().ID)
}

func runAgentCLI(t *testing.T, dir, stdin string, args ...string) (int, string, string) {
	t.Helper()
	return runCLIStdin(t, dir, stdin, append([]string{"agent"}, args...)...)
}

func TestAgentOutsideGG(t *testing.T) {
	prev := agentGetenv
	agentGetenv = func(string) string { return "" }
	t.Cleanup(func() { agentGetenv = prev })
	dir := newRepoDir(t)
	code, _, errOut := runAgentCLI(t, dir, "", "task")
	if code != 2 || !strings.Contains(errOut, "inside a gg console") {
		t.Fatalf("task outside gg = %d %q", code, errOut)
	}
	code, out, _ := runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, "no gg TUI is running") {
		t.Fatalf("list outside gg with no TUI = %d %q", code, out)
	}
}

func TestAgentStartReadsPromptFromStdin(t *testing.T) {
	var got domain.AgentStartRequest
	gotCh := make(chan domain.AgentStartRequest, 1)
	dir, _ := agentEnvFor(t, func(_ context.Context, r domain.AgentStartRequest) (domain.AgentStartResult, error) {
		gotCh <- r
		return domain.AgentStartResult{ID: "p/s9", Worktree: "/w", Tool: r.Tool, Warning: "claim stayed"}, nil
	})
	code, out, errOut := runAgentCLI(t, dir, "fix issue 7\nsecond line\n", "start", "--worktree", "job", "--tool", "Claude", "--note", "n", "--prompt-file", "-")
	if code != 0 || strings.TrimSpace(out) != "p/s9" || !strings.Contains(errOut, "warning: claim stayed") {
		t.Fatalf("start = %d %q %q", code, out, errOut)
	}
	got = <-gotCh
	if got.Prompt != "fix issue 7\nsecond line\n" || got.Worktree != "job" || got.Tool != "Claude" || got.Note != "n" {
		t.Fatalf("request %+v", got)
	}
}

func TestAgentListAndRefusal(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, out, _ := runAgentCLI(t, dir, "", "list")
	if code != 0 || !strings.Contains(out, full) || !strings.Contains(out, "running") {
		t.Fatalf("list = %d %q", code, out)
	}
	code, out, _ = runAgentCLI(t, dir, "", "list", "--json")
	if code != 0 || !strings.Contains(out, `"id":`) {
		t.Fatalf("list --json = %d %q", code, out)
	}
	code, _, errOut := runAgentCLI(t, dir, "", "task")
	if code != 2 || !strings.Contains(errOut, "not started by an agent") {
		t.Fatalf("a refusal = %d %q", code, errOut)
	}
	code, _, errOut = runAgentCLI(t, dir, "", "kill", full)
	if code != 2 || !strings.Contains(errOut, "not an agent you started") {
		t.Fatalf("kill of a non-descendant = %d %q", code, errOut)
	}
}

func TestAgentScreenPrintsText(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, _, errOut := runAgentCLI(t, dir, "", "screen", full)
	if code != 0 {
		t.Fatalf("screen = %d %q", code, errOut)
	}
}

func TestAgentSendNeedsTextOrKeys(t *testing.T) {
	dir, full := agentEnvFor(t, nil)
	code, _, errOut := runAgentCLI(t, dir, "", "send", full)
	if code != 2 || !strings.Contains(errOut, "usage: gg agent send") {
		t.Fatalf("send with nothing = %d %q", code, errOut)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/cli -run 'TestAgent'`
Expected: FAIL — `undefined: agentGetenv`.

- [ ] **Step 3: Implement `agent.go`**

```go
package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/agentlink"
	"github.com/homeend/gigagit/internal/domain"
)

// agentGetenv reads the console's environment (a test seam).
var agentGetenv = os.Getenv

const outsideGG = "run this inside a gg console (GG_MCP_URL and GG_SESSION_TOKEN are unset)"

// keyList collects a repeatable --key flag.
type keyList []string

func (k *keyList) String() string     { return strings.Join(*k, ",") }
func (k *keyList) Set(v string) error { *k = append(*k, v); return nil }

func cmdAgent(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: gg agent <start|list|screen|send|kill|task> [args]")
		return 2
	}
	verb, rest := args[0], args[1:]
	c, inside := agentlink.FromEnv(agentGetenv)
	if verb == "list" && !inside {
		return agentListOutside(rest, stdout, stderr)
	}
	if !inside {
		fmt.Fprintln(stderr, "agent "+verb+": "+outsideGG)
		return 2
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	switch verb {
	case "start":
		return agentStart(ctx, c, rest, stdin, stdout, stderr)
	case "list":
		return agentList(ctx, c, rest, stdout, stderr)
	case "screen":
		return agentOneID(ctx, c, "screen", rest, stdout, stderr)
	case "send":
		return agentSend(ctx, c, rest, stdout, stderr)
	case "kill":
		return agentKill(ctx, c, rest, stdout, stderr)
	case "task":
		return agentTask(ctx, c, rest, stdout, stderr)
	}
	fmt.Fprintf(stderr, "agent: unknown verb %q\n", verb)
	return 2
}

// call runs tool and maps the outcome to an exit code: a refusal prints the
// tool's text and is 2, a channel failure is 1.
func call(ctx context.Context, c *agentlink.Client, verb, tool string, in any, out any, stderr io.Writer) int {
	res, err := c.Call(ctx, tool, in)
	if err != nil {
		fmt.Fprintln(stderr, "agent "+verb+":", err)
		return 1
	}
	if res.IsError {
		fmt.Fprintln(stderr, "agent "+verb+": "+agentlink.ResultText(res))
		return 2
	}
	if out != nil {
		if err := agentlink.Decode(res, out); err != nil {
			fmt.Fprintln(stderr, "agent "+verb+":", err)
			return 1
		}
	}
	return 0
}

func agentStart(ctx context.Context, c *agentlink.Client, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent start", flag.ContinueOnError)
	fs.SetOutput(stderr)
	wt := fs.String("worktree", "", "worktree path, directory name or branch")
	tool := fs.String("tool", "", "session command name, as in [agents] spawn")
	prompt := fs.String("prompt", "", "the worker's task")
	file := fs.String("prompt-file", "", "read the task from this file (- = stdin)")
	note := fs.String("note", "", "note on the worktree claim (e.g. the issue URL)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	text := *prompt
	if *file != "" {
		var data []byte
		var err error
		if *file == "-" {
			data, err = io.ReadAll(io.LimitReader(stdin, domain.MaxBriefBytes+1))
		} else {
			data, err = os.ReadFile(*file)
		}
		if err != nil {
			fmt.Fprintln(stderr, "agent start:", err)
			return 1
		}
		text = string(data)
	}
	if *wt == "" || *tool == "" || text == "" {
		fmt.Fprintln(stderr, "usage: gg agent start --worktree <path|name> --tool <name> (--prompt <text> | --prompt-file <file|->) [--note <text>]")
		return 2
	}
	var res domain.AgentStartResult
	in := map[string]any{"worktree": *wt, "tool": *tool, "prompt": text, "note": *note}
	if code := call(ctx, c, "start", "agent_start", in, &res, stderr); code != 0 {
		return code
	}
	fmt.Fprintln(stdout, res.ID)
	if res.Warning != "" {
		fmt.Fprintln(stderr, "warning: "+res.Warning)
	}
	return 0
}

func agentList(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "the full rows as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	var out struct {
		Agents []domain.AgentEntry `json:"agents"`
	}
	if code := call(ctx, c, "list", "agent_list", map[string]any{}, &out, stderr); code != 0 {
		return code
	}
	if *asJSON {
		data, _ := json.Marshal(out.Agents)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	for _, a := range out.Agents {
		line := fmt.Sprintf("%s  %s  %s  %s", a.ID, a.State, a.Tool, a.Worktree)
		if a.Parent != "" {
			line += "  parent " + a.Parent
		}
		if a.Mine {
			line += "  mine"
		}
		fmt.Fprintln(stdout, line)
	}
	return 0
}

// agentListOutside prints the live TUIs from their registry files: no
// channel call (there is no token outside gg).
func agentListOutside(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent list", flag.ContinueOnError)
	fs.SetOutput(stderr)
	asJSON := fs.Bool("json", false, "the live TUIs as JSON")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	hosts := domain.LiveAgentHosts()
	if *asJSON {
		data, _ := json.Marshal(hosts)
		fmt.Fprintln(stdout, string(data))
		return 0
	}
	if len(hosts) == 0 {
		fmt.Fprintln(stdout, "no gg TUI is running")
		return 0
	}
	for _, h := range hosts {
		fmt.Fprintf(stdout, "gg TUI pid %d in %s\n", h.PID, h.Worktree)
		for _, s := range h.Sessions {
			name := s.Agent
			if name == "" {
				name = s.Label
			}
			fmt.Fprintf(stdout, "  %s  %s  %s  %s\n", s.ID, s.State, name, s.Dir)
		}
	}
	return 0
}

func agentOneID(ctx context.Context, c *agentlink.Client, verb string, args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: gg agent "+verb+" <id>")
		return 2
	}
	var out struct {
		Text string `json:"text"`
	}
	if code := call(ctx, c, verb, "agent_"+verb, map[string]any{"id": args[0]}, &out, stderr); code != 0 {
		return code
	}
	fmt.Fprintln(stdout, out.Text)
	return 0
}

func agentSend(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent send", flag.ContinueOnError)
	fs.SetOutput(stderr)
	noEnter := fs.Bool("no-enter", false, "do not press Enter after the text")
	var keys keyList
	fs.Var(&keys, "key", "a key to press after the text (repeatable): enter, esc, tab, up, ctrl+c, 1, space…")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() < 1 || (fs.NArg() == 1 && len(keys) == 0) {
		fmt.Fprintln(stderr, "usage: gg agent send <id> [<text>…] [--no-enter] [--key <name>]…")
		return 2
	}
	id, text := fs.Arg(0), strings.Join(fs.Args()[1:], " ")
	in := map[string]any{"id": id, "text": text, "enter": !*noEnter && text != "", "keys": []string(keys)}
	return call(ctx, c, "send", "agent_send", in, nil, stderr)
}

func agentKill(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agent kill", flag.ContinueOnError)
	fs.SetOutput(stderr)
	remove := fs.Bool("remove", false, "also forget the session once it exited")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: gg agent kill <id> [--remove]")
		return 2
	}
	return call(ctx, c, "kill", "agent_kill", map[string]any{"id": fs.Arg(0), "remove": *remove}, nil, stderr)
}

func agentTask(ctx context.Context, c *agentlink.Client, args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: gg agent task")
		return 2
	}
	var out struct {
		Brief string `json:"brief"`
	}
	if code := call(ctx, c, "task", "agent_task", map[string]any{}, &out, stderr); code != 0 {
		return code
	}
	fmt.Fprint(stdout, out.Brief)
	if !strings.HasSuffix(out.Brief, "\n") {
		fmt.Fprintln(stdout)
	}
	return 0
}
```

`cli.go`: add `case "agent": return cmdAgent(rest, stdin, stdout, stderr)` next to `case "session":` and `"agent": true,` to `commands`.

`e2e/env_test.go` TestMain: add `os.Unsetenv("GG_MCP_URL"); os.Unsetenv("GG_SESSION_TOKEN")` beside its other environment pins (a suite run from inside a gg console must not reach that console's TUI). Do the same in `internal/cli/main_test.go`.

`e2e/scenarios/agent-outside-gg.toml`:

```toml
name = "cli: gg agent outside a gg console"

[input]
steps = [
  { write = "a.txt", content = "A\n" },
  { commit = "base" },
]

[[run]]
cmd             = ["agent", "task"]
exit            = 2
stderr_contains = ["inside a gg console"]

[expect]
branch = "main"
clean  = true

[expect.files]
"a.txt" = "A\n"

[[expect.log]]
subjects = ["base"]
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/cli -run 'TestAgent' && go test ./e2e -run 'TestScenarios/agent' && go test ./internal/archtest`
Expected: PASS. (`internal/cli` importing `internal/mcp` happens only in the test file; archtest checks non-test imports.)

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/cli e2e
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(cli): gg agent start|list|screen|send|kill|task — twins of the agent tools; list outside gg reads the registry"
```

---

### Task 5: `gg init --mcp` registers gg with Claude Code

**Files:**
- Create: `internal/agentinit/mcpreg.go`, `internal/agentinit/mcpreg_test.go`
- Modify: `internal/cli/init.go` (flag + call)
- Test: `internal/cli/init_test.go` (or the existing init test file — `grep -ln cmdInit internal/cli/*_test.go`)

**Interfaces:**
- Produces: `func RegisterClaudeMCP(lookPath func(string) (string, error), run func(name string, args ...string) ([]byte, error), ggBin string) (string, error)` returning `"registered"` or `"already registered"`; CLI seams `var initLookPath = exec.LookPath`, `var initRun = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).CombinedOutput() }`, `var initExecutable = os.Executable`.

- [ ] **Step 1: Write the failing tests** (`mcpreg_test.go`)

```go
package agentinit

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

func TestRegisterClaudeMCPNoClaude(t *testing.T) {
	ran := false
	_, err := RegisterClaudeMCP(func(string) (string, error) { return "", errors.New("not found") },
		func(string, ...string) ([]byte, error) { ran = true; return nil, nil }, "/bin/gg")
	if err == nil || !strings.Contains(err.Error(), "claude mcp add -s user gg -- /bin/gg mcp") || ran {
		t.Fatalf("err = %v ran = %v", err, ran)
	}
}

func TestRegisterClaudeMCPAddsOnce(t *testing.T) {
	var calls [][]string
	registered := false
	run := func(name string, args ...string) ([]byte, error) {
		calls = append(calls, append([]string{name}, args...))
		if args[1] == "get" && !registered {
			return []byte("No MCP server found"), errors.New("exit 1")
		}
		if args[1] == "add" {
			registered = true
		}
		return nil, nil
	}
	look := func(string) (string, error) { return "/usr/bin/claude", nil }
	st, err := RegisterClaudeMCP(look, run, "/opt/gg")
	if err != nil || st != "registered" {
		t.Fatalf("first = %q %v", st, err)
	}
	want := []string{"/usr/bin/claude", "mcp", "add", "-s", "user", "gg", "--", "/opt/gg", "mcp"}
	if !slices.Equal(calls[1], want) {
		t.Fatalf("add argv = %v", calls[1])
	}
	st, err = RegisterClaudeMCP(look, run, "/opt/gg")
	if err != nil || st != "already registered" || len(calls) != 3 {
		t.Fatalf("second = %q %v calls %v", st, err, calls)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/agentinit -run RegisterClaudeMCP`
Expected: FAIL — `undefined: RegisterClaudeMCP`.

- [ ] **Step 3: Implement**

`mcpreg.go`:

```go
package agentinit

import (
	"fmt"
	"strings"
)

// RegisterClaudeMCP registers ggBin's MCP server with Claude Code at user
// scope through Claude's own CLI (`claude mcp add`), so an agent running in a
// gg console gets gg's agent tools. An existing "gg" entry is left alone.
func RegisterClaudeMCP(lookPath func(string) (string, error), run func(name string, args ...string) ([]byte, error), ggBin string) (string, error) {
	manual := "claude mcp add -s user gg -- " + ggBin + " mcp"
	claude, err := lookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude is not on PATH — register by hand: %s", manual)
	}
	if _, err := run(claude, "mcp", "get", "gg"); err == nil {
		return "already registered", nil
	}
	if out, err := run(claude, "mcp", "add", "-s", "user", "gg", "--", ggBin, "mcp"); err != nil {
		return "", fmt.Errorf("claude mcp add failed (%v): %s — register by hand: %s", err, strings.TrimSpace(string(out)), manual)
	}
	return "registered", nil
}
```

(If the Task 0 spike found `claude mcp get` exits 0 for a missing name, check its output for `No MCP server` instead, as ledgered there.)

`init.go`: add `mcpReg := fs.Bool("mcp", false, "register gg's MCP server with Claude Code (claude mcp add -s user gg -- <this gg> mcp), so agents in gg consoles get the agent tools")`; right after `fs.Parse`:

```go
	if *mcpReg {
		bin, err := initExecutable()
		if err != nil {
			fmt.Fprintln(stderr, "init --mcp:", err)
			return 1
		}
		st, err := agentinit.RegisterClaudeMCP(initLookPath, initRun, bin)
		if err != nil {
			fmt.Fprintln(stderr, "init --mcp:", err)
			return 1
		}
		fmt.Fprintln(stdout, "Claude Code: gg MCP server "+st)
		return 0
	}
```

plus the three seams as package vars (imports `os`, `os/exec`). A CLI test (in the init test file):

```go
func TestInitMCPFlag(t *testing.T) {
	prevL, prevR, prevE := initLookPath, initRun, initExecutable
	t.Cleanup(func() { initLookPath, initRun, initExecutable = prevL, prevR, prevE })
	initLookPath = func(string) (string, error) { return "/c", nil }
	initExecutable = func() (string, error) { return "/gg", nil }
	initRun = func(string, ...string) ([]byte, error) { return nil, nil } // get succeeds = present
	code, out, _ := runCLI(t, newRepoDir(t), "init", "--mcp")
	if code != 0 || !strings.Contains(out, "already registered") {
		t.Fatalf("init --mcp = %d %q", code, out)
	}
}
```

- [ ] **Step 4: Run tests**

Run: `go test ./internal/agentinit ./internal/cli -run 'RegisterClaudeMCP|InitMCP|Init'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add internal/agentinit internal/cli
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "feat(init): gg init --mcp registers gg's MCP server with Claude Code (user scope, via claude mcp add)"
```

---

### Task 6: docs

**Files:**
- Modify: `CHANGELOG.md`, `README.md` (`### Agents and worktrees`), `internal/agentskill/using-gg.md` (new `### Starting another agent` after `### Picking a worktree for another agent`), `internal/agentskill/agentskill.go` (Version 114), `.claude/skills/using-gg/SKILL.md` (regenerated), `docs/CLAUDE-details.md` (new section after "Worktree inventory, claims and guards"), `CLAUDE.md` (map rows)

- [ ] **Step 1: CHANGELOG** — under the unreleased heading add an "Agents start agents (orchestration stage 2)" entry: the TUI-hosted agent channel (MCP over loopback, per-session token, `GG_MCP_URL`/`GG_SESSION_TOKEN`/`GG_PARENT_SESSION`), the six tools, `[agents] spawn` + `max_spawned` (global only) and the one-time approval, `<prompt>` in session commands (accept the template update), claim handover + revert to the parent, `gg mcp` forwarding, `gg agent …`, `gg init --mcp`.

- [ ] **Step 2: README** — in `### Agents and worktrees` add a subsection "Agents starting agents": the config block

```toml
[agents]
spawn = ["Claude (yolo)"]   # global config only
max_spawned = 4
```

the steps (approve the command once from Start agent; accept the session-command update so it has `<prompt>`; run `gg init --mcp` once), what the overseer calls (`agent_start` / `gg agent start --worktree … --tool … --prompt-file brief.md`), what the user sees (sub-row, status line, no focus change), and the limits (no nesting, send/kill only your own workers).

- [ ] **Step 3: using-gg skill** — add `### Starting another agent` with: the tools and their arguments, the CLI twins with one example each, the worker's first act (`agent_task`), the claim behaviour, every refusal message an agent may meet and what to do about it (the texts from `domain/agentverbs.go` `SpawnAgent`). Bump `agentskill.Version` to 114.

- [ ] **Step 4: regenerate the dogfood skill** — write a throwaway test, run it, delete it:

```bash
cd /work/gigagit/.claude/worktrees/agent-spawn
cat > internal/agentskill/zz_regen_test.go <<'EOF'
package agentskill

import (
	"os"
	"testing"
)

func TestZZRegen(t *testing.T) {
	if os.Getenv("GG_REGEN_SKILL") == "" {
		t.Skip()
	}
	if err := os.WriteFile("../../.claude/skills/using-gg/SKILL.md", []byte(UsingGG.SkillFile()), 0o644); err != nil {
		t.Fatal(err)
	}
}
EOF
GG_REGEN_SKILL=1 go test ./internal/agentskill -run TestZZRegen && rm internal/agentskill/zz_regen_test.go
head -5 .claude/skills/using-gg/SKILL.md
```

Expected: the file's marker reads `v114`.

- [ ] **Step 5: CLAUDE-details + CLAUDE.md** — CLAUDE-details: a section "Agent spawn (agent orchestration stage 2)" covering the channel (host in TUI, bearer per request, token lifecycle, ambient authority), `SpawnAgent` order and refusals, the slot counter, claim handover/revert/young-claim grace/`wtclaim.Replace`, the caller-repo service, discovery fields, the forwarder, `gg agent`, `gg init --mcp`, rulings R1–R6 and the final-review rulings. CLAUDE.md map: `mcp` row gains "; hosts the TUI's agent channel (`AgentHost`: loopback streamable HTTP, bearer per request, six `agent_*` tools) and forwards them from `gg mcp` inside a gg console"; add a row `agentlink` — "The one client of a TUI's agent channel (bearer header, lazy connect, one reconnect) behind the `gg mcp` forwarder and `gg agent`. DAG leaf (stdlib + go-sdk)."

- [ ] **Step 6: Verify and commit**

Run: `go test ./internal/agentskill ./internal/agentinit && ./test.sh unit > /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/unit-b.log 2>&1; tail -3 /tmp/claude-1000/-work-gigagit/63e7bf78-bedf-425b-857e-5933228d7cb7/scratchpad/unit-b.log`
Expected: PASS / the unit stage ends green.

```bash
git -C /work/gigagit/.claude/worktrees/agent-spawn add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md internal/agentskill .claude/skills/using-gg/SKILL.md
git -C /work/gigagit/.claude/worktrees/agent-spawn commit -m "docs: agents start agents — changelog, readme, details, skill v114, package map"
```

---

### Task 7: gate

- [ ] **Step 1:** `./test.sh race > …/race-b.log 2>&1; grep -c "all green" …/race-b.log` → `1`.
- [ ] **Step 2:** Live smoke with the CLI twins: repeat plan A's tui-capture smoke (scratch repo, isolated XDG, Overseer/Worker commands), with the Overseer script replaced by `gg agent start --worktree job --tool Worker --prompt "hello brief"; gg agent list; sleep 600` (the `gg` on PATH inside the console = this branch's `bin/gg` — prepend its dir to PATH in the wrapper). Expected on the Overseer's screen: the worker id, then a list showing it `running … mine`. Ledger the result.
- [ ] **Step 3:** Final whole-branch review (plans A+B) by a fresh read-only reviewer; one fix pass; then ask the user before merging.
