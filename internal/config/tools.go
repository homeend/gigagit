package config

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
)

// ToolCommand is one external-tool command ([[tools.command]] block): a menu
// label plus the shell command to run for a task category. Blocks are written
// by the Settings "External tools" wizard or by hand; only config content
// ever executes (catalog templates are generation-time input).
type ToolCommand struct {
	Category  string   `toml:"category"`  // conflict | commit_message | review | conflict_complete | session
	Name      string   `toml:"name"`      // menu label; unique per category
	Mode      string   `toml:"mode"`      // terminal | capture | interactive | session (session: category = "session" only)
	PerFile   bool     `toml:"per_file"`  // conflict only: run once per conflicted file
	WhenOp    string   `toml:"when_op"`   // "" = any paused op; else merge|rebase|cherry-pick|revert
	Frontends []string `toml:"frontends"` // limits which frontends offer this command: any of "tui", "web", "cli". Empty = everywhere.
	Command   string   `toml:"command"`   // shell command with <token> placeholders

	// The template stamp (written only on blocks gg generated from the
	// catalog): the family's template version, the variant's agent range,
	// and ToolFingerprint of the block as written — a later mismatch means
	// the user edited it. Older gg binaries ignore these keys.
	TemplateVersion int    `toml:"template_version"`
	AgentRange      string `toml:"agent_range"`
	Fingerprint     string `toml:"fingerprint"`
}

// ToolsConfig is the [tools] section.
type ToolsConfig struct {
	Command []ToolCommand `toml:"command"`
}

// Key identifies a command for the overlay collision rule.
func (tc ToolCommand) Key() string { return tc.Category + "\x00" + tc.Name }

// Stamped reports a block gg generated from the catalog.
func (tc ToolCommand) Stamped() bool { return tc.TemplateVersion > 0 }

// Edited reports a block whose content no longer matches its stamp; an
// unstamped block always counts as edited.
func (tc ToolCommand) Edited() bool { return !tc.Stamped() || ToolFingerprint(tc) != tc.Fingerprint }

// ToolFingerprint hashes a block's MEANING, not its text: mode, per_file,
// when_op, sorted frontends and the command with line ends normalised,
// every run of spaces/tabs collapsed, lines trimmed and blank lines dropped.
func ToolFingerprint(tc ToolCommand) string {
	fr := append([]string(nil), tc.Frontends...)
	sort.Strings(fr)
	var lines []string
	for _, ln := range strings.Split(strings.ReplaceAll(tc.Command, "\r\n", "\n"), "\n") {
		if ln = strings.Join(strings.Fields(ln), " "); ln != "" {
			lines = append(lines, ln)
		}
	}
	h := sha256.New()
	for _, part := range []string{tc.Mode, strconv.FormatBool(tc.PerFile), tc.WhenOp, strings.Join(fr, ","), strings.Join(lines, "\n")} {
		h.Write([]byte(part))
		h.Write([]byte{0})
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

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
	case "conflict", "commit_message", "review", "conflict_complete", "session":
	default:
		return fmt.Errorf("tools: unknown category %q (want conflict|commit_message|review|conflict_complete|session)", tc.Category)
	}
	if strings.TrimSpace(tc.Name) == "" {
		return fmt.Errorf("tools: a command needs a name")
	}
	switch tc.Mode {
	case "terminal", "capture", "session", "interactive":
	default:
		return fmt.Errorf("tools: %s: unknown mode %q (want terminal|capture|interactive|session)", tc.Name, tc.Mode)
	}
	if (tc.Category == "session") != (tc.Mode == "session") {
		return fmt.Errorf("tools: %s: category \"session\" requires mode = \"session\" (and that mode is only for sessions)", tc.Name)
	}
	if strings.TrimSpace(tc.Command) == "" {
		return fmt.Errorf("tools: %s: empty command", tc.Name)
	}
	if tc.PerFile && tc.Category != "conflict" {
		return fmt.Errorf("tools: %s: per_file is only valid for category = \"conflict\"", tc.Name)
	}
	if tc.PerFile && tc.Mode == "interactive" {
		return fmt.Errorf("tools: %s: per_file commands are mergetools (mode = \"terminal\"); interactive is whole-operation only", tc.Name)
	}
	switch tc.WhenOp {
	case "", "merge", "rebase", "cherry-pick", "revert":
	default:
		return fmt.Errorf("tools: %s: unknown when_op %q", tc.Name, tc.WhenOp)
	}
	for _, f := range tc.Frontends {
		switch f {
		case "tui", "web", "cli":
		default:
			return fmt.Errorf("tools: %s: unknown frontend %q (want tui|web|cli)", tc.Name, f)
		}
	}
	return nil
}

// ToolVisibleIn reports whether frontend ("tui"|"web"|"cli") offers tc.
// Empty Frontends means everywhere.
func ToolVisibleIn(tc ToolCommand, frontend string) bool {
	if len(tc.Frontends) == 0 {
		return true
	}
	for _, f := range tc.Frontends {
		if f == frontend {
			return true
		}
	}
	return false
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
		renderToolCommand(&b, tc)
	}
	return atomicWriteFile(path, []byte(b.String()))
}

// renderToolCommand writes one [[tools.command]] block (no leading blank
// line). A stamped block gets its three stamp keys, the fingerprint
// computed here from the block being written.
func renderToolCommand(b *strings.Builder, tc ToolCommand) {
	fmt.Fprintf(b, "[[tools.command]]\n")
	fmt.Fprintf(b, "category = %q\n", tc.Category)
	fmt.Fprintf(b, "name = %q\n", tc.Name)
	fmt.Fprintf(b, "mode = %q\n", tc.Mode)
	fmt.Fprintf(b, "per_file = %t\n", tc.PerFile)
	fmt.Fprintf(b, "when_op = %q\n", tc.WhenOp)
	if len(tc.Frontends) > 0 {
		quoted := make([]string, len(tc.Frontends))
		for i, f := range tc.Frontends {
			quoted[i] = fmt.Sprintf("%q", f)
		}
		fmt.Fprintf(b, "frontends = [%s]\n", strings.Join(quoted, ", "))
	}
	if tc.Stamped() {
		fmt.Fprintf(b, "template_version = %d\n", tc.TemplateVersion)
		fmt.Fprintf(b, "agent_range = %q\n", tc.AgentRange)
		fmt.Fprintf(b, "fingerprint = %q\n", ToolFingerprint(tc))
	}
	b.WriteString("command = '''\n")
	b.WriteString(strings.TrimRight(tc.Command, "\n"))
	b.WriteString("\n'''\n")
}

// RenderToolCommand is the block text the config writer lays out for tc,
// stamp keys included (the review popup shows exactly this).
func RenderToolCommand(tc ToolCommand) string {
	var b strings.Builder
	renderToolCommand(&b, tc)
	return b.String()
}

// ToolCommandsIn is the [[tools.command]] blocks of ONE config file, with no
// overlay; a missing file has none.
func ToolCommandsIn(path string) ([]ToolCommand, error) {
	c, _, err := decodeFile(path)
	return c.Tools.Command, err
}

// ReplaceToolCommandBodies rewrites, in place, the command body of each
// [[tools.command]] block whose body replace accepts, and returns how many it
// replaced. Only the multi-line literal bodies gg writes (AppendToolCommands) are
// seen; every other byte of the file is kept, and a file with no match is not
// written at all. replace receives the body without its trailing newline; a
// new body containing the literal's delimiter is refused.
func ReplaceToolCommandBodies(path string, replace func(body string) (string, bool)) (int, error) {
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	lines := strings.SplitAfter(string(raw), "\n")
	var out strings.Builder
	inTool, n := false, 0
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		out.WriteString(line)
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "[") {
			inTool = trimmed == "[[tools.command]]"
			continue
		}
		if !inTool || !isCommandLiteralOpen(trimmed) {
			continue
		}
		end := -1
		for j := i + 1; j < len(lines); j++ {
			if strings.TrimRight(lines[j], "\r\n") == "'''" {
				end = j
				break
			}
		}
		if end < 0 {
			continue // an unterminated literal: not ours to touch
		}
		body := strings.TrimRight(strings.Join(lines[i+1:end], ""), "\r\n")
		nl := "\n"
		if strings.HasSuffix(lines[end-1], "\r\n") || strings.HasSuffix(line, "\r\n") {
			nl = "\r\n"
		}
		if repl, ok := replace(body); ok {
			if strings.Contains(repl, "'''") {
				return 0, fmt.Errorf("config: a command must not contain ''' (TOML literal delimiter)")
			}
			out.WriteString(strings.ReplaceAll(strings.TrimRight(repl, "\n"), "\n", nl) + nl)
			n++
		} else {
			out.WriteString(strings.Join(lines[i+1:end], ""))
		}
		i = end - 1 // the closing ''' line is written by the loop
	}
	if n == 0 {
		return 0, nil
	}
	return n, atomicWriteFile(path, []byte(out.String()))
}

// isCommandLiteralOpen reports a command key line that opens a multi-line
// literal (the body starts on the next line).
func isCommandLiteralOpen(trimmed string) bool {
	k, v, ok := strings.Cut(trimmed, "=")
	return ok && strings.TrimSpace(k) == "command" && strings.TrimSpace(v) == "'''"
}
