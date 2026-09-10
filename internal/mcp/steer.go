package mcp

import "github.com/homeend/gigagit/internal/steer"

// notifyNotesChanged tells a live gg session for this worktree that the note
// store changed. Best-effort and silent — an MCP tool's result must never
// depend on whether a human happens to have a window open.
func (s *Server) notifyNotesChanged() {
	steer.NotifyReload(s.steerDir, "notes")
}
