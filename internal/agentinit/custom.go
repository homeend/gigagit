package agentinit

import (
	"os"
	"path/filepath"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// CustomTarget is one remembered custom install location — the `gg init --to`
// fallback for agents the hardcoded registry doesn't know. Path is the FINAL
// resolved target file (not the raw user input), so refreshes are unambiguous.
type CustomTarget struct {
	Path string `toml:"path"` // absolute target file
	Mode string `toml:"mode"` // "skill" (whole SKILL.md) | "block" (managed block)
}

// customRegistry is the on-disk shape of agent-targets.toml.
type customRegistry struct {
	Target []CustomTarget `toml:"target"`
}

// ResolveCustom maps a raw --to path to its target file and mode: a directory
// (existing, or stated with a trailing separator) receives a Claude-style
// skill file at <dir>/using-gg/SKILL.md; anything else is treated as a shared
// instruction file and receives the marker-delimited managed block (created if
// missing, surrounding content preserved — the AGENTS.md treatment).
func ResolveCustom(raw string) CustomTarget {
	dirIntent := strings.HasSuffix(raw, "/") || strings.HasSuffix(raw, string(os.PathSeparator))
	if fi, err := os.Stat(raw); err == nil && fi.IsDir() {
		dirIntent = true
	}
	if dirIntent {
		return CustomTarget{Path: filepath.Join(raw, "using-gg", "SKILL.md"), Mode: "skill"}
	}
	return CustomTarget{Path: filepath.Clean(raw), Mode: "block"}
}

// LoadCustomTargets reads the registry; a missing file is an empty list.
func LoadCustomTargets(file string) ([]CustomTarget, error) {
	data, err := os.ReadFile(file)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var reg customRegistry
	if err := toml.Unmarshal(data, &reg); err != nil {
		return nil, err
	}
	return reg.Target, nil
}

// SaveCustomTargets writes the registry atomically (temp file + rename).
func SaveCustomTargets(file string, ts []CustomTarget) error {
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return err
	}
	data, err := toml.Marshal(customRegistry{Target: ts})
	if err != nil {
		return err
	}
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, file)
}

// AddCustomTarget records a target, deduplicating by Path (a re-add refreshes
// the stored mode).
func AddCustomTarget(file string, ct CustomTarget) error {
	ts, err := LoadCustomTargets(file)
	if err != nil {
		return err
	}
	for i, have := range ts {
		if have.Path == ct.Path {
			ts[i] = ct
			return SaveCustomTargets(file, ts)
		}
	}
	return SaveCustomTargets(file, append(ts, ct))
}

// CustomDetections synthesizes Detection rows for remembered custom targets,
// so they list, check, and refresh exactly like registry agents.
func CustomDetections(ts []CustomTarget) []Detection {
	var out []Detection
	for _, ct := range ts {
		mode := ModeBlock
		if ct.Mode == "skill" {
			mode = ModeSkillFile
		}
		out = append(out, Detection{
			Agent:  Agent{ID: "custom", Label: "Custom", Target: ct.Path, Mode: mode},
			Target: ct.Path,
			Status: status(ct.Path),
		})
	}
	return out
}
