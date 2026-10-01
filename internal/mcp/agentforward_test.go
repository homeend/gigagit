package mcp

import (
	"context"
	"strings"
	"testing"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
)

// stdioWith connects an in-memory client to a gg mcp server built with env.
func stdioWith(t *testing.T, dir string, env map[string]string) *sdk.ClientSession {
	t.Helper()
	prev := agentEnv
	agentEnv = func(k string) string { return env[k] }
	t.Cleanup(func() { agentEnv = prev })
	svc := domainOpenForTest(t, dir)
	server := New(svc)
	t.Cleanup(func() { // after hostEnv's own cleanup was registered: LIFO runs this first
		if server.agent != nil {
			server.agent.Close()
		}
	})
	srv := server.sdkServer()
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
	cs := stdioWith(t, t.TempDir(), map[string]string{"GG_MCP_URL": "http://127.0.0.1:1/mcp"}) // no token
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

// domainOpenForTest opens dir (a repo or not) the way Serve does.
func domainOpenForTest(t *testing.T, dir string) *domain.Service {
	t.Helper()
	svc := domain.Open(dir)
	svc.UseNotesDir(t.TempDir())
	return svc
}
