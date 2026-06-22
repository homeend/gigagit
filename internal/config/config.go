// Package config loads gigagit's global and per-repo TOML configuration and
// manages machine-local per-repo state (the <seq> counters). The committed
// config is read-only at runtime; only the local state file is written.
package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/pelletier/go-toml/v2"
)

// WorktreeConfig configures worktree creation. TOML keys are snake_case.
type WorktreeConfig struct {
	PathTemplate          string   `toml:"path_template"`
	DefaultBranchTemplate string   `toml:"default_branch_template"`
	BranchTemplates       []string `toml:"branch_templates"`
}

// UIConfig configures TUI behavior. TOML keys are snake_case.
type UIConfig struct {
	WheelStep     int      `toml:"wheel_step"`     // rows per mouse-wheel tick; <=0 = unset
	HScrollStep   int      `toml:"hscroll_step"`   // diff scroll-mode pan columns per ←/→; <=0 = unset
	FooterActions []string `toml:"footer_actions"` // action ids shown in the footer; empty = all (default)
	MenuActions   []string `toml:"menu_actions"`   // action ids shown in the . menu; empty = all (default)

	SearchHistorySize int `toml:"search_history_size"` // entries kept per search-history ring; <=0 = unset (default 20), clamped to searchhist.MaxSize

	ReflogLimit int `toml:"reflog_limit"` // max HEAD reflog entries shown in the Reflog panel; <=0 = unset (default 200)

	CommitGraphLanes    int `toml:"commit_graph_lanes"`     // default graph window width in lanes; <=0 = unset
	CommitGraphMinLanes int `toml:"commit_graph_min_lanes"` // minimum window width (narrow floor); <=0 = unset
	CommitGraphStep     int `toml:"commit_graph_step"`      // widen/narrow increment in lanes; <=0 = unset
	CommitGraphPanStep  int `toml:"commit_graph_pan_step"`  // pan increment in lanes; <=0 = derived max(1, cols/2)
	CommitGraphMaxLanes int `toml:"commit_graph_max_lanes"` // plane cap in lanes; <=0 = unset; clamped to commitgraph.MaxLanes
}

// Config is the merged gigagit configuration.
type Config struct {
	Worktree WorktreeConfig `toml:"worktree"`
	UI       UIConfig       `toml:"ui"`
}

// Defaults returns the built-in configuration used when no files set a field.
func Defaults() Config {
	return Config{
		Worktree: WorktreeConfig{
			PathTemplate:          "../<repo>.worktrees/<branch>",
			DefaultBranchTemplate: "b/from-<parent-branch>-<random-alpha:4>",
		},
		UI: UIConfig{WheelStep: 3, HScrollStep: 8, CommitGraphLanes: 8, CommitGraphMinLanes: 2, CommitGraphStep: 4},
	}
}

// Load builds the effective Config: built-in defaults, overlaid by any field the
// global file sets, then overlaid by any field the repo file sets (repo wins).
// A missing file is skipped (not an error); a present-but-malformed file errors.
func Load(globalPath, repoPath string) (Config, error) {
	cfg := Defaults()

	for _, path := range []string{globalPath, repoPath} {
		layer, ok, err := decodeFile(path)
		if err != nil {
			return Config{}, err
		}
		if ok {
			overlayWorktree(&cfg.Worktree, layer.Worktree)
			overlayUI(&cfg.UI, layer.UI)
		}
	}
	return cfg, nil
}

// decodeFile reads and decodes one config file. ok is false (no error) when the
// file does not exist.
func decodeFile(path string) (Config, bool, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	var c Config
	if err := toml.Unmarshal(data, &c); err != nil {
		return Config{}, false, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	return c, true, nil
}

// overlayWorktree copies each non-empty field of src onto dst (field-level
// overlay: an unset field in src leaves dst untouched). A field left empty in
// src is treated as unset, so a higher layer cannot deliberately reset a lower
// layer's field to empty; this is intentional.
func overlayWorktree(dst *WorktreeConfig, src WorktreeConfig) {
	if src.PathTemplate != "" {
		dst.PathTemplate = src.PathTemplate
	}
	if src.DefaultBranchTemplate != "" {
		dst.DefaultBranchTemplate = src.DefaultBranchTemplate
	}
	if len(src.BranchTemplates) > 0 {
		dst.BranchTemplates = src.BranchTemplates
	}
}

// overlayUI copies each set field of src onto dst. WheelStep <= 0 is unset
// (same rule as the string fields: a higher layer cannot reset a lower
// layer's value to the zero value).
func overlayUI(dst *UIConfig, src UIConfig) {
	if src.WheelStep > 0 {
		dst.WheelStep = src.WheelStep
	}
	if src.HScrollStep > 0 {
		dst.HScrollStep = src.HScrollStep
	}
	if len(src.FooterActions) > 0 {
		dst.FooterActions = src.FooterActions
	}
	if len(src.MenuActions) > 0 {
		dst.MenuActions = src.MenuActions
	}
	if src.SearchHistorySize > 0 {
		dst.SearchHistorySize = src.SearchHistorySize
	}
	if src.ReflogLimit > 0 {
		dst.ReflogLimit = src.ReflogLimit
	}
	if src.CommitGraphLanes > 0 {
		dst.CommitGraphLanes = src.CommitGraphLanes
	}
	if src.CommitGraphMinLanes > 0 {
		dst.CommitGraphMinLanes = src.CommitGraphMinLanes
	}
	if src.CommitGraphStep > 0 {
		dst.CommitGraphStep = src.CommitGraphStep
	}
	if src.CommitGraphPanStep > 0 {
		dst.CommitGraphPanStep = src.CommitGraphPanStep
	}
	if src.CommitGraphMaxLanes > 0 {
		dst.CommitGraphMaxLanes = src.CommitGraphMaxLanes
	}
}

// DefaultGlobalPath returns the global config path, honoring $XDG_CONFIG_HOME
// and falling back to ~/.config/gg/config.toml.
func DefaultGlobalPath() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			home = ""
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "gg", "config.toml")
}
