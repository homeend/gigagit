package mcp

import (
	"context"
	"errors"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/domain"
)

type agentStartIn struct {
	Worktree string `json:"worktree" jsonschema:"worktree path, directory name or branch"`
	Tool     string `json:"tool" jsonschema:"session command name, as in [agents] spawn"`
	Prompt   string `json:"prompt" jsonschema:"the worker's task (its brief), up to 256 KiB"`
	Note     string `json:"note,omitempty" jsonschema:"note on the worktree claim, e.g. the issue URL"`
}
type agentIDIn struct {
	ID string `json:"id" jsonschema:"a session id as agent_list prints it"`
}
type agentSendIn struct {
	ID    string   `json:"id"`
	Text  string   `json:"text,omitempty" jsonschema:"text to paste"`
	Enter *bool    `json:"enter,omitempty" jsonschema:"press Enter after the text (default: true when text is given)"`
	Keys  []string `json:"keys,omitempty" jsonschema:"keys after the text: enter, esc, tab, up, down, ctrl+c, 1, space…"`
}
type agentKillIn struct {
	ID     string `json:"id"`
	Remove bool   `json:"remove,omitempty" jsonschema:"also forget the session once it exited"`
}
type agentListOut struct {
	Agents []domain.AgentEntry `json:"agents"`
}
type agentScreenOut struct {
	ID    string `json:"id"`
	State string `json:"state"`
	Text  string `json:"text"`
}
type agentTaskOut struct {
	Brief    string `json:"brief"`
	Parent   string `json:"parent"`
	Worktree string `json:"worktree"`
}
type empty struct{}

// RegisterAgentTools adds the six agent tools; caller names the
// authenticated session of a request, starter runs agent_start.
func RegisterAgentTools(srv *sdk.Server, caller func(*sdk.CallToolRequest) (string, error), starter Starter) {
	sdk.AddTool(srv, toolAgentStart(),
		func(ctx context.Context, req *sdk.CallToolRequest, in agentStartIn) (*sdk.CallToolResult, domain.AgentStartResult, error) {
			who, err := caller(req)
			if err != nil {
				return nil, domain.AgentStartResult{}, err
			}
			if starter == nil {
				return nil, domain.AgentStartResult{}, errors.New("this gg cannot start agents")
			}
			res, err := starter(ctx, domain.AgentStartRequest{Caller: who, Worktree: in.Worktree, Tool: in.Tool, Prompt: in.Prompt, Note: in.Note})
			return nil, res, err
		})
	sdk.AddTool(srv, toolAgentList(),
		func(_ context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, agentListOut, error) {
			who, err := caller(req)
			if err != nil {
				return nil, agentListOut{}, err
			}
			return nil, agentListOut{Agents: domain.AgentList(who)}, nil
		})
	sdk.AddTool(srv, toolAgentScreen(),
		func(_ context.Context, req *sdk.CallToolRequest, in agentIDIn) (*sdk.CallToolResult, agentScreenOut, error) {
			if _, err := caller(req); err != nil {
				return nil, agentScreenOut{}, err
			}
			st, text, err := domain.AgentScreen(in.ID)
			return nil, agentScreenOut{ID: in.ID, State: st, Text: text}, err
		})
	sdk.AddTool(srv, toolAgentSend(),
		func(_ context.Context, req *sdk.CallToolRequest, in agentSendIn) (*sdk.CallToolResult, empty, error) {
			who, err := caller(req)
			if err != nil {
				return nil, empty{}, err
			}
			enter := in.Text != "" // Enter by default only after text: a keys-only send presses no stray Enter
			if in.Enter != nil {
				enter = *in.Enter
			}
			return nil, empty{}, domain.AgentSend(who, in.ID, in.Text, enter, in.Keys)
		})
	sdk.AddTool(srv, toolAgentKill(),
		func(_ context.Context, req *sdk.CallToolRequest, in agentKillIn) (*sdk.CallToolResult, empty, error) {
			who, err := caller(req)
			if err != nil {
				return nil, empty{}, err
			}
			return nil, empty{}, domain.AgentKill(who, in.ID, in.Remove)
		})
	sdk.AddTool(srv, toolAgentTask(),
		func(_ context.Context, req *sdk.CallToolRequest, _ empty) (*sdk.CallToolResult, agentTaskOut, error) {
			who, err := caller(req)
			if err != nil {
				return nil, agentTaskOut{}, err
			}
			rec, err := domain.AgentTask(who)
			return nil, agentTaskOut{Brief: rec.Brief, Parent: rec.Parent, Worktree: rec.Worktree}, err
		})
}

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
	return &sdk.Tool{Name: "agent_send", Description: "Type into an agent you started: paste text, then Enter (default when there is text), then keys."}
}
func toolAgentKill() *sdk.Tool {
	return &sdk.Tool{Name: "agent_kill", Description: "End an agent you started."}
}
func toolAgentTask() *sdk.Tool {
	return &sdk.Tool{Name: "agent_task", Description: "Your own task, when an agent started you.", Annotations: readOnlyAnnotations()}
}
