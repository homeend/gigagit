// Package agentlink is the one client of a gg TUI's agent channel (the MCP
// server the TUI hosts on loopback): `gg mcp` forwards its agent tools
// through it and `gg agent …` calls it. It reads GG_MCP_URL and
// GG_SESSION_TOKEN, sends the token as a bearer header on every request,
// connects lazily, re-dials a session that stopped answering, and never
// repeats a tool call. DAG leaf: stdlib + go-sdk.
package agentlink

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrUnreachable wraps every failure to reach the channel.
var ErrUnreachable = errors.New("gg's agent channel is not reachable")

// ErrProtocol wraps a JSON-RPC error the channel answered with.
var ErrProtocol = errors.New("gg's agent channel rejected the call")

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

// session returns a live session: a cached one that answers a ping, else a
// fresh connect. Holding mu across Connect is fine: one forwarder, one
// client; a black-holed host blocks other callers only for ctx's lifetime.
func (c *Client) session(ctx context.Context) (*sdk.ClientSession, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cs != nil {
		if c.cs.Ping(ctx, nil) == nil {
			return c.cs, nil
		}
		_ = c.cs.Close() // gone (TUI restarted, network blip): reconnect once
		c.cs = nil
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

// Call runs tool with args EXACTLY once — never retried, since agent_start
// or agent_send repeated would duplicate a worker or a keystroke. A tool
// refusal comes back as a result with IsError set; an error is ErrProtocol
// (the channel answered with a JSON-RPC error) or ErrUnreachable.
func (c *Client) Call(ctx context.Context, tool string, args any) (*sdk.CallToolResult, error) {
	cs, err := c.session(ctx)
	if err != nil {
		return nil, err
	}
	res, err := cs.CallTool(ctx, &sdk.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		var wire *jsonrpc.Error
		if errors.As(err, &wire) {
			return nil, fmt.Errorf("%w: %s", ErrProtocol, wire.Message)
		}
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	return res, nil
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
