package domain

import (
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/template"
)

// TaskMode is how an AI task runs.
type TaskMode string

const (
	TaskHeadless    TaskMode = "headless"    // queued, captured, no console
	TaskInteractive TaskMode = "interactive" // an agent session; each $GG_MESSAGE_FILE write is a result
)

// TaskModeOf says how tc runs as an AI task: capture → headless;
// interactive, or a whole-operation terminal command (today's conflict
// agent rows) → interactive. A per-file terminal command is a mergetool and
// a session command is not a task: ok = false.
func TaskModeOf(tc config.ToolCommand) (TaskMode, bool) {
	switch tc.Mode {
	case string(exttool.ModeCapture):
		return TaskHeadless, true
	case string(exttool.ModeInteractive):
		return TaskInteractive, true
	case string(exttool.ModeTerminal):
		if !tc.PerFile {
			return TaskInteractive, true
		}
	}
	return "", false
}

// AgentName is the display name of tc's agent: the catalogue label of the
// program it runs ("Claude Code"), else the command's own name.
func AgentName(tc config.ToolCommand) string {
	if id := agentIDFor(tc); id != "" {
		for _, tl := range exttool.Builtins() {
			if tl.ID == id {
				return tl.Label
			}
		}
	}
	return tc.Name
}

// TaskChoice is one agent the launch dialog offers for a kind, with its
// commands per mode (config order; more than one = variants, e.g. yolo).
type TaskChoice struct {
	AgentID     string // exttool tool id; "" for a custom command
	Agent       string // display name
	Headless    []config.ToolCommand
	Interactive []config.ToolCommand
}

// TaskChoices groups the valid kind commands offered in frontend by agent,
// in first-appearance order. A custom command (no catalogue program) is its
// own agent, keyed by name.
func TaskChoices(cfg config.Config, kind exttool.Category, frontend string) []TaskChoice {
	var out []TaskChoice
	index := map[string]int{}
	for _, tc := range cfg.Tools.Command {
		if tc.Category != string(kind) || !config.ToolVisibleIn(tc, frontend) {
			continue
		}
		if config.ValidateToolCommand(tc) != nil || template.ValidateCommandTokens(tc.Command, tc.PerFile) != nil {
			continue
		}
		mode, ok := TaskModeOf(tc)
		if !ok {
			continue
		}
		id := agentIDFor(tc)
		key := "id:" + id
		if id == "" {
			key = "name:" + tc.Name
		}
		i, seen := index[key]
		if !seen {
			i = len(out)
			index[key] = i
			out = append(out, TaskChoice{AgentID: id, Agent: AgentName(tc)})
		}
		if mode == TaskHeadless {
			out[i].Headless = append(out[i].Headless, tc)
		} else {
			out[i].Interactive = append(out[i].Interactive, tc)
		}
	}
	return out
}

// EnsureInteractiveCommands is the first-run append for interactive task
// commands (the EnsureSessionCommands shape): for each of commit_message
// and review with NO interactive-capable row in cfg, append the detected
// agents' safe (never OptIn) interactive rows to the global config.
// Conflict kinds already have interactive rows (their terminal agents).
func EnsureInteractiveCommands(cfg config.Config, globalPath string, detect func() []exttool.Detection) ([]string, error) {
	need := map[exttool.Category]bool{exttool.CatCommitMessage: true, exttool.CatReview: true}
	for _, tc := range cfg.Tools.Command {
		if mode, ok := TaskModeOf(tc); ok && mode == TaskInteractive {
			delete(need, exttool.Category(tc.Category))
		}
	}
	if len(need) == 0 {
		return nil, nil
	}
	var blocks []config.ToolCommand
	var names []string
	for _, det := range detect() {
		for _, ct := range det.Tool.Commands {
			if !need[ct.Category] || ct.Mode != exttool.ModeInteractive || ct.OptIn {
				continue
			}
			blocks = append(blocks, config.ToolCommand{
				Category: string(ct.Category), Name: ct.Name, Mode: string(ct.Mode),
				Frontends: ct.Frontends, Command: exttool.GenerateCommand(ct, det.Bin),
			})
			names = append(names, string(ct.Category)+"/"+ct.Name)
		}
	}
	if len(blocks) == 0 {
		return nil, nil
	}
	if err := config.AppendToolCommands(globalPath, blocks); err != nil {
		return nil, err
	}
	return names, nil
}
