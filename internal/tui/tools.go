package tui

import (
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/template"
)

// toolCommands returns the runnable external-tool commands for a category:
// structurally valid, token-valid, in either execution mode — terminal
// (suspend the TUI, hand over the real terminal) or capture (run headless in
// the background while the TUI shows a "running" box). An invalid block is
// INERT — skipped with one session failure note per block (never a startup
// error), so a config typo degrades a menu instead of breaking the app.
func (m Model) toolCommands(category string) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range m.cfg.Tools.Command {
		// Validate before filtering by category: an invalid block (including
		// an unrecognized category) is noted the first time ANY toolCommands
		// call walks over it, not just when its own category is queried.
		if err := m.toolUsable(tc); err != nil {
			m.noteToolOnce(tc.Key(), err)
			continue
		}
		if tc.Category != category {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// toolUsable is the full usability check for one block.
func (m Model) toolUsable(tc config.ToolCommand) error {
	if err := config.ValidateToolCommand(tc); err != nil {
		return err
	}
	return template.ValidateCommandTokens(tc.Command, tc.PerFile)
}

// noteToolOnce records one failure note per block per session (m.toolNoted is
// a map field, so it persists across the value-receiver copies).
func (m Model) noteToolOnce(key string, err error) {
	if m.toolNoted == nil || m.toolNoted[key] {
		return
	}
	m.toolNoted[key] = true
	observ.NoteFailure("tools", err)
}

// conflictToolChoices filters conflict commands for the paused op and the
// focused file: when_op must match (empty = any), and a per_file command
// needs a focused both-sides conflict to act on. Pure, for tests.
func conflictToolChoices(cmds []config.ToolCommand, op string, focused *model.FileStatus) []config.ToolCommand {
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		if tc.PerFile && (focused == nil || focused.ConflictClass() != model.ConflictBothSides) {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// completeToolChoices filters conflict_complete commands: they exist to
// COMPLETE a paused sequencer op, so with no paused op (op == "") there is
// nothing to offer; when_op narrows further when set. Pure, for tests.
func completeToolChoices(cmds []config.ToolCommand, op string) []config.ToolCommand {
	if op == "" {
		return nil
	}
	var out []config.ToolCommand
	for _, tc := range cmds {
		if tc.WhenOp != "" && tc.WhenOp != op {
			continue
		}
		out = append(out, tc)
	}
	return out
}

// pendingToolRun is a resolved, ready-to-execute tool command (built by the
// pick/fill flow; executed after approval in tool_run.go).
type pendingToolRun struct {
	tc       config.ToolCommand
	resolved string   // command with all tokens substituted
	env      []string // extra GG_* environment entries
	cleanup  []string // temp files to remove after the run (quartet)
	file     string   // per-file: repo-relative conflicted path
	merged   string   // per-file: absolute worktree path of the file
}
