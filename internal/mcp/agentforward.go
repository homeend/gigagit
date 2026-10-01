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
