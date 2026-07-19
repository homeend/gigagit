package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/homeend/gigagit/internal/config"
)

type uiStateOut struct {
	Repo RepoInfo `json:"repo"`
	// Session is a pointer so the SDK's inferred output schema allows a JSON
	// null (map[string]any alone infers as non-nullable "object"; jsonschema-go
	// only widens a type to allow null when it follows a Go pointer — see
	// jsonschema.forType's allowNull handling). nil marshals as null = no live
	// session.
	Session *map[string]any `json:"session"`
	Hint    string          `json:"hint,omitempty"`
}

func (s *Server) registerStateTool(srv *sdk.Server) {
	sdk.AddTool(srv, &sdk.Tool{
		Name: "gg_ui_state",
		Description: "Current gg TUI session snapshot for this repository: focused panel, " +
			"per-panel cursor values, marked commits/files, the open files/diff/compare view " +
			"and its selected file, the open bookmark/shelf switcher's highlighted entry, " +
			"active filters, conflict and running-operation state. session is null when no " +
			"gg TUI is running for this repo. The status field is display-only text — do not parse it.",
	}, func(ctx context.Context, _ *sdk.CallToolRequest, _ struct{}) (*sdk.CallToolResult, uiStateOut, error) {
		out := uiStateOut{Repo: s.repoInfo()}
		if err := s.repoCheck(); err != nil {
			return nil, out, err
		}
		data, err := os.ReadFile(config.SessionSnapshotPath(s.commonDir))
		if err != nil {
			out.Hint = "no gg TUI session snapshot for this repository"
			return nil, out, nil
		}
		var sess map[string]any
		if err := json.Unmarshal(data, &sess); err != nil {
			return nil, out, fmt.Errorf("session snapshot unreadable: %v", err)
		}
		if v, _ := sess["version"].(float64); v > 1 {
			return nil, out, fmt.Errorf("session snapshot version %d is newer than this gg — upgrade gg", int(v))
		}
		out.Session = &sess
		return nil, out, nil
	})
}
