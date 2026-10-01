package agentlink

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

var boomCalls atomic.Int32

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
	srv.AddTool(&sdk.Tool{Name: "boom", InputSchema: map[string]any{"type": "object"}}, func(context.Context, *sdk.CallToolRequest) (*sdk.CallToolResult, error) {
		boomCalls.Add(1)
		return nil, errors.New("protocol failure")
	})
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

func TestCallNeverRetriesAToolCall(t *testing.T) {
	ts := channel(t)
	c := New(ts.URL, "good")
	defer c.Close()
	boomCalls.Store(0)
	if _, err := c.Call(context.Background(), "boom", struct{}{}); err == nil {
		t.Fatal("a protocol failure must surface")
	}
	if n := boomCalls.Load(); n != 1 {
		t.Fatalf("the tool ran %d times: a retried agent_start would start two workers", n)
	}
}

// The channel answered: a JSON-RPC error is not "unreachable".
func TestProtocolErrorIsNotUnreachable(t *testing.T) {
	ts := channel(t)
	c := New(ts.URL, "good")
	defer c.Close()
	for _, tool := range []string{"boom", "nope"} {
		_, err := c.Call(context.Background(), tool, struct{}{})
		if err == nil || errors.Is(err, ErrUnreachable) || strings.Contains(err.Error(), "not reachable") {
			t.Errorf("%s: err = %v, want a protocol error, not unreachable", tool, err)
		}
	}
}
