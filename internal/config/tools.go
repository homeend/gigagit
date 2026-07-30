package config

import (
	"fmt"
	"os"
	"strings"
)

// ToolCommand is one external-tool command ([[tools.command]] block): a menu
// label plus the shell command to run for a task category. Blocks are written
// by the Settings "External tools" wizard or by hand; only config content
// ever executes (catalog templates are generation-time input).
type ToolCommand struct {
	Category string `toml:"category"` // conflict | commit_message | review | conflict_complete
	Name     string `toml:"name"`     // menu label; unique per category
	Mode     string `toml:"mode"`     // terminal | capture (capture: stage 2+)
	PerFile  bool   `toml:"per_file"` // conflict only: run once per conflicted file
	WhenOp   string `toml:"when_op"`  // "" = any paused op; else merge|rebase|cherry-pick|revert
	Command  string `toml:"command"`  // shell command with <token> placeholders
}

// ToolsConfig is the [tools] section.
type ToolsConfig struct {
	Command []ToolCommand `toml:"command"`
}

// Key identifies a command for the overlay collision rule.
func (tc ToolCommand) Key() string { return tc.Category + "\x00" + tc.Name }

// overlayTools implements the tools-list overlay: CONCATENATE global + repo,
// repo winning a (category,name) collision in place. This is a deliberate
// exception to the field-level zero-is-unset rule — lists merge, they do not
// replace — documented in the settingDocs comment.
func overlayTools(dst *ToolsConfig, src ToolsConfig) {
	for _, tc := range src.Command {
		replaced := false
		for i, have := range dst.Command {
			if have.Key() == tc.Key() {
				dst.Command[i] = tc
				replaced = true
				break
			}
		}
		if !replaced {
			dst.Command = append(dst.Command, tc)
		}
	}
}

// ValidateToolCommand checks a block's structural fields. Token validation
// (template.ValidateCommandTokens) is the frontend's job — config stays free
// of the template dependency. An invalid block is made inert by the caller,
// never a startup error.
func ValidateToolCommand(tc ToolCommand) error {
	switch tc.Category {
	case "conflict", "commit_message", "review", "conflict_complete":
	default:
		return fmt.Errorf("tools: unknown category %q (want conflict|commit_message|review|conflict_complete)", tc.Category)
	}
	if strings.TrimSpace(tc.Name) == "" {
		return fmt.Errorf("tools: a command needs a name")
	}
	switch tc.Mode {
	case "terminal", "capture":
	default:
		return fmt.Errorf("tools: %s: unknown mode %q (want terminal|capture)", tc.Name, tc.Mode)
	}
	if strings.TrimSpace(tc.Command) == "" {
		return fmt.Errorf("tools: %s: empty command", tc.Name)
	}
	if tc.PerFile && tc.Category != "conflict" {
		return fmt.Errorf("tools: %s: per_file is only valid for category = \"conflict\"", tc.Name)
	}
	switch tc.WhenOp {
	case "", "merge", "rebase", "cherry-pick", "revert":
	default:
		return fmt.Errorf("tools: %s: unknown when_op %q", tc.Name, tc.WhenOp)
	}
	return nil
}

// AppendToolCommands appends [[tools.command]] blocks to the config file at
// path (creating it if missing), never touching existing content — the wizard
// must not overwrite a user-edited command. Command bodies are written as
// multi-line ”' literals; a body containing ”' is refused (TOML literal
// strings cannot escape their delimiter).
func AppendToolCommands(path string, cmds []ToolCommand) error {
	if path == "" {
		return fmt.Errorf("config: no config path; refusing to write")
	}
	for _, tc := range cmds {
		if strings.Contains(tc.Command, "'''") {
			return fmt.Errorf("config: %s: command must not contain ''' (TOML literal delimiter)", tc.Name)
		}
	}
	raw, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var b strings.Builder
	b.Write(raw)
	for _, tc := range cmds {
		if b.Len() > 0 && !strings.HasSuffix(b.String(), "\n\n") {
			if strings.HasSuffix(b.String(), "\n") {
				b.WriteString("\n")
			} else {
				b.WriteString("\n\n")
			}
		}
		fmt.Fprintf(&b, "[[tools.command]]\n")
		fmt.Fprintf(&b, "category = %q\n", tc.Category)
		fmt.Fprintf(&b, "name = %q\n", tc.Name)
		fmt.Fprintf(&b, "mode = %q\n", tc.Mode)
		fmt.Fprintf(&b, "per_file = %t\n", tc.PerFile)
		fmt.Fprintf(&b, "when_op = %q\n", tc.WhenOp)
		b.WriteString("command = '''\n")
		b.WriteString(strings.TrimRight(tc.Command, "\n"))
		b.WriteString("\n'''\n")
	}
	return atomicWriteFile(path, []byte(b.String()))
}
