package mcp

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/auth"
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/buildinfo"
	"github.com/homeend/gigagit/internal/domain"
)

// Starter runs agent_start inside the hosting TUI (it owns the console
// size, the child env and the status line).
type Starter func(ctx context.Context, req domain.AgentStartRequest) (domain.AgentStartResult, error)

// AgentHost is the agent channel a TUI serves: MCP over streamable HTTP on
// loopback; every request authenticated by the calling session's token.
type AgentHost struct {
	srv *http.Server
	url string
}

func NewAgentHost() *AgentHost { return &AgentHost{} }

// agentSessionTimeout closes an MCP session the TUI has not heard from in
// this long: a gg mcp killed with its agent never sends its goodbye, and the
// client re-dials an expired session once (agentlink), so nothing is lost.
var agentSessionTimeout = time.Hour

const agentInstructions = "gg agent channel. These tools exist only for an agent running inside a gg console. " +
	"A worker agent's first act is agent_task (its brief). agent_start starts a worker in a worktree " +
	"(the gg [agents] spawn allow-list governs which session commands); agent_send/agent_kill reach only agents you started."

// Handler is the authenticated MCP endpoint (tests mount it on httptest).
func (h *AgentHost) Handler(starter Starter) http.Handler {
	srv := sdk.NewServer(&sdk.Implementation{Name: "gg-agents", Version: buildinfo.Version},
		&sdk.ServerOptions{Instructions: agentInstructions})
	RegisterAgentTools(srv, callerFromToken, starter)
	mcpH := sdk.NewStreamableHTTPHandler(func(*http.Request) *sdk.Server { return srv },
		&sdk.StreamableHTTPOptions{SessionTimeout: agentSessionTimeout})
	authed := auth.RequireBearerToken(verifyToken, nil)(mcpH)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Origin") != "" {
			http.Error(w, "browsers may not call the gg agent channel", http.StatusForbidden)
			return
		}
		authed.ServeHTTP(w, r)
	})
}

func verifyToken(_ context.Context, tok string, _ *http.Request) (*auth.TokenInfo, error) {
	id, ok := domain.VerifyAgentToken(tok)
	if !ok {
		return nil, auth.ErrInvalidToken
	}
	return &auth.TokenInfo{UserID: id, Expiration: time.Now().Add(24 * time.Hour)}, nil
}

func callerFromToken(req *sdk.CallToolRequest) (string, error) {
	if req.Extra == nil || req.Extra.TokenInfo == nil || req.Extra.TokenInfo.UserID == "" {
		return "", errors.New("not running inside a gg console (no session token)")
	}
	return req.Extra.TokenInfo.UserID, nil
}

// Start listens on a random loopback port and serves until Close.
func (h *AgentHost) Start(starter Starter) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	mux := http.NewServeMux()
	mux.Handle("/mcp", h.Handler(starter))
	h.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	h.url = "http://" + ln.Addr().String() + "/mcp"
	go func() { _ = h.srv.Serve(ln) }()
	return h.url, nil
}

func (h *AgentHost) URL() string { return h.url }

// Close drops every connection at once: a client's standalone SSE stream is
// never idle, so a graceful Shutdown would only wait out its timeout, and the
// agents it serves die with the TUI anyway.
func (h *AgentHost) Close() {
	if h.srv != nil {
		_ = h.srv.Close()
	}
}
