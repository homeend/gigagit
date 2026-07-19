package mcp

import (
	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// RepoInfo identifies which repository answered — every tool reply carries it
// so an agent juggling projects can sanity-check the target.
type RepoInfo struct {
	CommonDir string `json:"common_dir"`
	Worktree  string `json:"worktree"`
}

func boolPtr(b bool) *bool { return &b }

// readOnlyAnnotations marks a tool as not modifying anything; gg's world is
// the local repo, so OpenWorld is explicitly false on every tool.
func readOnlyAnnotations() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}
}

// mutatingAnnotations marks a tool that changes state (the repo, or a target
// directory for export) — MCP clients gate these behind their consent prompt.
func mutatingAnnotations() *sdk.ToolAnnotations {
	return &sdk.ToolAnnotations{
		ReadOnlyHint:    false,
		DestructiveHint: boolPtr(true),
		OpenWorldHint:   boolPtr(false),
	}
}
