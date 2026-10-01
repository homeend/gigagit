package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/agentsession"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
)

type bearer struct{ tok string }

func (b bearer) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+b.tok)
	return http.DefaultTransport.RoundTrip(r)
}

func agentClient(t *testing.T, url, tok string) (*sdk.ClientSession, error) {
	t.Helper()
	tr := &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{tok}}, MaxRetries: -1, DisableStandaloneSSE: true}
	return sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), tr, nil)
}

// hostEnv: a real repo, a fresh session manager, one agent session with a
// token, and the host's handler behind httptest.
func hostEnv(t *testing.T, starter Starter) (url, tok, full string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	restore := domain.UseSessionManager(agentsession.NewManager())
	dir := t.TempDir()
	gitRun(t, dir, "init", "-b", "main")
	gitRun(t, dir, "commit", "--allow-empty", "-m", "seed")
	svc := domain.Open(dir)
	ts := httptest.NewServer(NewAgentHost().Handler(starter))
	tc := config.ToolCommand{Category: "session", Name: "Sh", Mode: "session", Command: "sh -c 'sleep 600'"}
	s, tok, err := svc.StartAgentSession(context.Background(), tc, dir, "", 80, 24, nil, ts.URL+"/mcp", domain.SpawnRecord{}, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ts.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		domain.Sessions().KillAll(ctx)
		restore()
	})
	return ts.URL + "/mcp", tok, domain.FullSessionID(s.Info().ID)
}

func TestAgentToolsOverHTTP(t *testing.T) {
	gotCh := make(chan domain.AgentStartRequest, 1) // the handler runs on the server's goroutine
	url, tok, full := hostEnv(t, func(_ context.Context, r domain.AgentStartRequest) (domain.AgentStartResult, error) {
		gotCh <- r
		return domain.AgentStartResult{ID: "p/s9", Worktree: "/w", Tool: r.Tool}, nil
	})
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_start",
		Arguments: map[string]any{"worktree": "/w", "tool": "Claude", "prompt": "do it", "note": "n"}})
	if err != nil || res.IsError {
		t.Fatalf("agent_start = %v %+v", err, res)
	}
	got := <-gotCh
	if got.Caller != full || got.Tool != "Claude" || got.Prompt != "do it" || got.Note != "n" {
		t.Fatalf("the starter must receive the AUTHENTICATED caller: %+v", got)
	}
	res, err = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}})
	if err != nil || res.IsError || !strings.Contains(resultText(res), full) {
		t.Fatalf("agent_list = %v %s", err, resultText(res))
	}
	res, _ = cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_task", Arguments: map[string]any{}})
	if !res.IsError || !strings.Contains(resultText(res), "not started by an agent") {
		t.Fatalf("agent_task for a manual session = %s", resultText(res))
	}
}

func TestMissingOrUnknownTokenIs401(t *testing.T) {
	url, _, _ := hostEnv(t, nil)
	for _, tok := range []string{"", strings.Repeat("a", 64)} {
		if _, err := agentClient(t, url, tok); err == nil {
			t.Fatalf("token %q connected", tok)
		}
	}
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no bearer = %v %v", resp.StatusCode, err)
	}
}

func TestKilledSessionTokenIs401(t *testing.T) {
	url, tok, full := hostEnv(t, nil)
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	s, _ := domain.Sessions().Get(domain.SessionID(full[strings.LastIndex(full, "/")+1:]))
	domain.Sessions().Kill(s.Info().ID)
	<-s.Done()
	if res, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}}); err == nil && !res.IsError {
		t.Fatal("a killed session's token still works")
	}
}

func TestOriginHeaderRefused(t *testing.T) {
	url, tok, _ := hostEnv(t, nil)
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Origin", "http://evil.example")
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a browser Origin = %v %v", resp.StatusCode, err)
	}
}

func TestIdleAgentSessionsExpire(t *testing.T) {
	prev := agentSessionTimeout
	agentSessionTimeout = 200 * time.Millisecond
	t.Cleanup(func() { agentSessionTimeout = prev })
	url, tok, _ := hostEnv(t, nil)
	cs, err := agentClient(t, url, tok)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	if _, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(600 * time.Millisecond)
	if _, err := cs.CallTool(context.Background(), &sdk.CallToolParams{Name: "agent_list", Arguments: map[string]any{}}); err == nil {
		t.Fatal("an idle session must be closed by the TUI (a gg mcp that was killed never says goodbye)")
	}
}

func TestCloseDoesNotWaitOnAnOpenStream(t *testing.T) {
	_, tok, _ := hostEnv(t, nil)
	h := NewAgentHost()
	url, err := h.Start(nil)
	if err != nil {
		t.Fatal(err)
	}
	tr := &sdk.StreamableClientTransport{Endpoint: url, HTTPClient: &http.Client{Transport: bearer{tok}}, MaxRetries: -1}
	cs, err := sdk.NewClient(&sdk.Implementation{Name: "t", Version: "0"}, nil).Connect(context.Background(), tr, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	time.Sleep(100 * time.Millisecond) // let the standalone SSE stream open
	start := time.Now()
	h.Close()
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Fatalf("Close took %v: an idle SSE stream must not hold the TUI's quit", d)
	}
}
