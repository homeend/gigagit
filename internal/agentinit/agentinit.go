// Package agentinit detects installed AI coding agents and installs the
// embedded using-gg skill into their instruction locations. The agent
// registry is hardcoded — supporting a new agent is a code change (one
// Builtins entry), never a runtime definition.
package agentinit

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/gigagit/gg/internal/agentskill"
)

// Mode is how the skill lands in a target file.
type Mode int

const (
	ModeSkillFile Mode = iota // whole gg-owned file with Claude frontmatter
	ModePlainFile             // whole gg-owned file, no frontmatter (Cursor)
	ModeBlock                 // marker-delimited block inside a shared file
)

// Agent is one registry entry. Detect/Target paths are relative to the
// project dir, or to the home dir when prefixed "~/".
type Agent struct {
	ID     string
	Label  string
	Detect string
	Target string
	Mode   Mode
}

// Builtins is the hardcoded agent registry. Adding support for a new agent is
// exactly one entry here.
func Builtins() []Agent {
	return []Agent{
		{ID: "claude-project", Label: "Claude Code (project)", Detect: ".claude", Target: ".claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "claude-global", Label: "Claude Code (global)", Detect: "~/.claude", Target: "~/.claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "junie", Label: "Junie (JetBrains)", Detect: ".junie", Target: ".junie/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "codex", Label: "Codex (global)", Detect: "~/.codex", Target: "~/.codex/AGENTS.md", Mode: ModeBlock},
		{ID: "opencode", Label: "OpenCode (global)", Detect: "~/.config/opencode", Target: "~/.config/opencode/AGENTS.md", Mode: ModeBlock},
		{ID: "agents-md", Label: "AGENTS.md (generic)", Detect: "AGENTS.md", Target: "AGENTS.md", Mode: ModeBlock},
		{ID: "cursor", Label: "Cursor (project)", Detect: ".cursor", Target: ".cursor/rules/using-gg.mdc", Mode: ModePlainFile},
		{ID: "gemini", Label: "Gemini CLI (project)", Detect: "GEMINI.md", Target: "GEMINI.md", Mode: ModeBlock},
		{ID: "copilot", Label: "GitHub Copilot (project)", Detect: ".github", Target: ".github/copilot-instructions.md", Mode: ModeBlock},
		{ID: "windsurf", Label: "Windsurf (project)", Detect: ".windsurfrules", Target: ".windsurfrules", Mode: ModeBlock},
	}
}

// Status of a target relative to the binary's embedded skill version.
type Status int

const (
	StatusNew Status = iota
	StatusOutdated
	StatusUpToDate
)

func (s Status) String() string {
	switch s {
	case StatusOutdated:
		return "outdated"
	case StatusUpToDate:
		return "up to date"
	}
	return "new"
}

// Checked is the default checkbox state: targets that already have the skill
// (any version) default to checked — applying refreshes them; first-time
// installs are explicit opt-in.
func (s Status) Checked() bool { return s != StatusNew }

// Detection is one detected agent with its resolved target and status.
type Detection struct {
	Agent  Agent
	Target string // absolute
	Status Status
}

// resolve maps a registry path to an absolute path; "" means "not resolvable
// in this run" (home-scoped path with no homeDir — the hermeticity rule).
func resolve(p, projDir, homeDir string) string {
	if strings.HasPrefix(p, "~/") {
		if homeDir == "" {
			return ""
		}
		return filepath.Join(homeDir, p[2:])
	}
	return filepath.Join(projDir, p)
}

// Detect returns the registry entries whose Detect path exists, with each
// target's install status. An empty homeDir skips home-scoped agents entirely
// (tests must never see the developer's real home).
func Detect(projDir, homeDir string) []Detection {
	var out []Detection
	for _, a := range Builtins() {
		probe := resolve(a.Detect, projDir, homeDir)
		if probe == "" {
			continue
		}
		if _, err := os.Stat(probe); err != nil {
			continue
		}
		target := resolve(a.Target, projDir, homeDir)
		out = append(out, Detection{Agent: a, Target: target, Status: status(target)})
	}
	return out
}

// status reads the target and classifies it against the embedded version.
func status(target string) Status {
	data, err := os.ReadFile(target)
	if err != nil {
		return StatusNew
	}
	if !agentskill.HasMarker(data) {
		return StatusNew // file exists but has no gg marker at all
	}
	v := agentskill.InstalledVersion(data)
	if v < agentskill.Version {
		return StatusOutdated
	}
	return StatusUpToDate
}

// blockRe matches a previously installed managed block, any version.
var blockRe = regexp.MustCompile(`(?s)<!-- gg:using-gg:v\d+:begin -->.*?<!-- gg:using-gg:end -->`)

// Install writes the embedded skill into d.Target according to the agent's
// mode, creating parent directories as needed. Shared files keep all
// surrounding content byte-for-byte. Idempotent.
func Install(d Detection) error {
	if err := os.MkdirAll(filepath.Dir(d.Target), 0o755); err != nil {
		return err
	}
	switch d.Agent.Mode {
	case ModeSkillFile:
		return os.WriteFile(d.Target, []byte(agentskill.SkillFile()), 0o644)
	case ModePlainFile:
		return os.WriteFile(d.Target, []byte(agentskill.PlainFile()), 0o644)
	case ModeBlock:
		block := agentskill.Block()
		existing, err := os.ReadFile(d.Target)
		if os.IsNotExist(err) {
			return os.WriteFile(d.Target, []byte(block+"\n"), 0o644)
		}
		if err != nil {
			return err
		}
		if blockRe.Match(existing) {
			return os.WriteFile(d.Target, blockRe.ReplaceAllLiteral(existing, []byte(block)), 0o644)
		}
		sep := "\n\n"
		if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
			sep = ""
		} else if strings.HasSuffix(string(existing), "\n") {
			sep = "\n"
		}
		return os.WriteFile(d.Target, []byte(string(existing)+sep+block+"\n"), 0o644)
	}
	return fmt.Errorf("agentinit: unknown mode %d", d.Agent.Mode)
}
