// Package agentinit detects installed AI coding agents and installs the
// embedded gg skills (using-gg and reviewing-with-gg) into their instruction
// locations. The agent registry is hardcoded — supporting a new agent is a
// code change (one Builtins entry), never a runtime definition.
package agentinit

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/agentskill"
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

// TargetFor derives where skill sk lands for this agent from the registry's
// using-gg target, so the registry keeps ONE path per agent:
//
//	ModeSkillFile  .claude/skills/using-gg/SKILL.md → .claude/skills/<name>/SKILL.md
//	ModePlainFile  .cursor/rules/using-gg.mdc       → .cursor/rules/<name>.mdc
//	ModeBlock      the same shared file, a second marked block
//
// Derivation is filepath-based (Dir/Base/Ext/Join), never a textual replace of
// a path segment: on Windows the registry path's separators are not "/".
func (a Agent) TargetFor(sk agentskill.Skill, projDir, homeDir string) string {
	t := resolve(a.Target, projDir, homeDir)
	if t == "" {
		return ""
	}
	switch a.Mode {
	case ModeSkillFile:
		// <root>/<skill-name>/SKILL.md
		return filepath.Join(filepath.Dir(filepath.Dir(t)), sk.Name, filepath.Base(t))
	case ModePlainFile:
		return filepath.Join(filepath.Dir(t), sk.Name+filepath.Ext(t))
	default: // ModeBlock: one file, two blocks
		return t
	}
}

// Builtins is the hardcoded agent registry. Adding support for a new agent is
// exactly one entry here.
func Builtins() []Agent {
	return []Agent{
		{ID: "claude-project", Label: "Claude Code (project)", Detect: ".claude", Target: ".claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "claude-global", Label: "Claude Code (global)", Detect: "~/.claude", Target: "~/.claude/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "junie", Label: "Junie (project)", Detect: ".junie", Target: ".junie/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "junie-global", Label: "Junie (global)", Detect: "~/.junie", Target: "~/.junie/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		// Kimi Code discovers Agent Skills from .kimi-code/skills/<name>/SKILL.md
		// (project) and ~/.kimi-code/skills/<name>/SKILL.md (user) — the same
		// directory-form SKILL.md with name+description frontmatter as Claude,
		// so ModeSkillFile fits as-is. The user dir moves with KIMI_CODE_HOME;
		// this static registry only probes the default location.
		{ID: "kimi", Label: "Kimi Code (project)", Detect: ".kimi-code", Target: ".kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "kimi-global", Label: "Kimi Code (global)", Detect: "~/.kimi-code", Target: "~/.kimi-code/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
		{ID: "codex", Label: "Codex (global)", Detect: "~/.codex", Target: "~/.codex/AGENTS.md", Mode: ModeBlock},
		// Antigravity CLI (agy 1.1.4, verified 2026-07-20): skills are
		// skills/<name>/SKILL.md with name+description frontmatter under a
		// customization root; the global root is ~/.gemini/config/ (per the
		// bundled agy-customizations docs). Detect the agy-specific home,
		// not plain ~/.gemini (gemini-cli also creates that).
		{ID: "antigravity", Label: "Antigravity (global)", Detect: "~/.gemini/antigravity-cli", Target: "~/.gemini/config/skills/using-gg/SKILL.md", Mode: ModeSkillFile},
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

// Detection is one detected agent with its resolved targets and status. gg
// installs TWO skills per agent; Status is the worst of the two so the TUI
// Settings popup, the web page and `gg init --list` keep one row per agent.
type Detection struct {
	Agent        Agent
	Target       string // absolute: using-gg
	ReviewTarget string // absolute: reviewing-with-gg (== Target in block mode)
	Status       Status
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
		review := a.TargetFor(agentskill.ReviewingWithGG, projDir, homeDir)
		out = append(out, Detection{Agent: a, Target: target, ReviewTarget: review, Status: combinedStatus(target, review)})
	}
	return out
}

// skillStatus classifies one target file against one skill's embedded version.
func skillStatus(sk agentskill.Skill, target string) Status {
	data, err := os.ReadFile(target)
	if err != nil {
		return StatusNew
	}
	if !sk.HasMarker(data) {
		return StatusNew // file exists but has no marker for this skill
	}
	if sk.InstalledVersion(data) < sk.Version {
		return StatusOutdated
	}
	return StatusUpToDate
}

// combinedStatus folds the two skills into the single row the frontends show:
// using-gg missing = new (this agent has never been set up); using-gg present
// but the review skill missing or behind = outdated (a refresh adds it).
func combinedStatus(usingTarget, reviewTarget string) Status {
	u := skillStatus(agentskill.UsingGG, usingTarget)
	if u == StatusNew {
		return StatusNew
	}
	r := skillStatus(agentskill.ReviewingWithGG, reviewTarget)
	if u == StatusOutdated || r != StatusUpToDate {
		return StatusOutdated
	}
	return StatusUpToDate
}

// Install writes BOTH embedded skills into d's targets according to the agent's
// mode, creating parent directories as needed. Shared files keep all
// surrounding content — and each skill's own block — byte-for-byte. Idempotent.
func Install(d Detection) error {
	for _, sk := range agentskill.All() {
		target := d.Target
		if sk.Name == agentskill.ReviewingWithGG.Name && d.ReviewTarget != "" {
			target = d.ReviewTarget
		}
		if err := installSkill(sk, target, d.Agent.Mode); err != nil {
			return err
		}
	}
	return nil
}

func installSkill(sk agentskill.Skill, target string, mode Mode) error {
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return err
	}
	switch mode {
	case ModeSkillFile:
		return os.WriteFile(target, []byte(sk.SkillFile()), 0o644)
	case ModePlainFile:
		return os.WriteFile(target, []byte(sk.PlainFile()), 0o644)
	case ModeBlock:
		block := sk.Block()
		existing, err := os.ReadFile(target)
		if os.IsNotExist(err) {
			return os.WriteFile(target, []byte(block+"\n"), 0o644)
		}
		if err != nil {
			return err
		}
		// Only THIS skill's block is replaced — the sibling block in the same
		// file must survive untouched.
		if sk.BlockRe().Match(existing) {
			return os.WriteFile(target, sk.BlockRe().ReplaceAllLiteral(existing, []byte(block)), 0o644)
		}
		sep := "\n\n"
		if len(existing) == 0 || strings.HasSuffix(string(existing), "\n\n") {
			sep = ""
		} else if strings.HasSuffix(string(existing), "\n") {
			sep = "\n"
		}
		return os.WriteFile(target, []byte(string(existing)+sep+block+"\n"), 0o644)
	}
	return fmt.Errorf("agentinit: unknown mode %d", mode)
}
