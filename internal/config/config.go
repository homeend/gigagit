// Package config loads gigagit's global and per-repo TOML configuration and
// manages machine-local per-repo state (the <seq> counters). The committed
// config is read-only at runtime, with one narrow exception: the global file's
// [debug] log_operations key, which the , Settings menu toggle persists via
// SetGlobalDebugLogOperations (a non-destructive line edit).
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

	CommitInitialCount   int `toml:"commit_initial_count"`    // commits walked on first paint; <=0 = unset (default 300)
	CommitBatchSize      int `toml:"commit_batch_size"`       // commits per later page (scroll / ctrl+l); <=0 = unset (default 300)
	CommitSearchMaxPages int `toml:"commit_search_max_pages"` // eager /-search page cap before re-prompting; <=0 = unset (default 5)

	ShowEOLOnlyChanges bool `toml:"show_eol_only_changes"` // surface files whose only unstaged change is line endings (CRLF↔LF); false (default) hides them as noise

	// DisableSlowOpConfirm turns OFF the yes/no confirmation shown before slow
	// working-tree operations (switch, checkout, pull, merge, rebase,
	// fast-forward, reset). Inverted polarity: default false ⇒ confirmation ON;
	// only a true in a higher layer overlays (matching the zero-is-unset rule).
	DisableSlowOpConfirm bool `toml:"disable_slow_op_confirm"`
}

// DebugConfig configures diagnostic logging. TOML keys are snake_case.
type DebugConfig struct {
	// LogOperations mirrors every operation span and git invocation (redacted)
	// to the operation log file, so a hung or slow op leaves a trace. Default
	// false (off). Inverted polarity: only a true in a higher layer overlays.
	LogOperations bool `toml:"log_operations"`
}

// RefreshConfig configures background auto-refresh. All off by default.
// Enabled is the master gate; each interval is seconds (0 = that source never
// auto-refreshes). TOML keys snake_case under [refresh].
type RefreshConfig struct {
	Enabled    bool `toml:"enabled"` // master switch; default false (whole feature off)
	Status     int  `toml:"status"`  // seconds between background status reads; 0 = off
	Branches   int  `toml:"branches"`
	Remotes    int  `toml:"remotes"`
	Worktrees  int  `toml:"worktrees"`
	Tags       int  `toml:"tags"`
	Reflog     int  `toml:"reflog"`
	Feed       int  `toml:"feed"`
	Fetch      int  `toml:"fetch"`       // seconds between background `git fetch`; 0 = off
	RemoteTags int  `toml:"remote_tags"` // seconds between background remote-tag (ls-remote) lookups; 0 = off

	// MinSeconds is the floor on any auto-refresh interval: no source polls more
	// often than this, even when a source reads very cheaply. 0 = unset → default 10.
	MinSeconds int `toml:"min_seconds"`
}

// Config is the merged gigagit configuration.
type Config struct {
	Worktree WorktreeConfig `toml:"worktree"`
	UI       UIConfig       `toml:"ui"`
	Debug    DebugConfig    `toml:"debug"`
	Refresh  RefreshConfig  `toml:"refresh"`
}

// Defaults returns the built-in configuration used when no files set a field.
func Defaults() Config {
	return Config{
		Worktree: WorktreeConfig{
			PathTemplate:          "../<repo>.worktrees/<branch>",
			DefaultBranchTemplate: "b/from-<parent-branch>-<random-alpha:4>",
		},
		UI: UIConfig{WheelStep: 3, HScrollStep: 8, CommitGraphLanes: 8, CommitGraphMinLanes: 2, CommitGraphStep: 4,
			CommitInitialCount: 300, CommitBatchSize: 300, CommitSearchMaxPages: 5},
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
			overlayDebug(&cfg.Debug, layer.Debug)
			overlayRefresh(&cfg.Refresh, layer.Refresh)
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
	if src.CommitInitialCount > 0 {
		dst.CommitInitialCount = src.CommitInitialCount
	}
	if src.CommitBatchSize > 0 {
		dst.CommitBatchSize = src.CommitBatchSize
	}
	if src.CommitSearchMaxPages > 0 {
		dst.CommitSearchMaxPages = src.CommitSearchMaxPages
	}
	// Inverted polarity: the default (false) is the active feature (hide), so
	// only a true in a higher layer overlays — matching the zero-is-unset rule.
	if src.ShowEOLOnlyChanges {
		dst.ShowEOLOnlyChanges = true
	}
	if src.DisableSlowOpConfirm {
		dst.DisableSlowOpConfirm = true
	}
}

// overlayDebug copies each set field of src onto dst. Inverted polarity: the
// default (false) is "off", so only a true in a higher layer overlays —
// matching the zero-is-unset rule used elsewhere.
func overlayDebug(dst *DebugConfig, src DebugConfig) {
	if src.LogOperations {
		dst.LogOperations = true
	}
}

// overlayRefresh copies each set field of src onto dst. Intervals use the
// zero-is-unset rule (0 = unset). Enabled uses inverted polarity: default false
// is "off", so only a true in a higher layer overlays (a higher layer cannot
// reset a lower layer's true back to false — matching LogOperations).
func overlayRefresh(dst *RefreshConfig, src RefreshConfig) {
	if src.Enabled {
		dst.Enabled = true
	}
	if src.Status > 0 {
		dst.Status = src.Status
	}
	if src.Branches > 0 {
		dst.Branches = src.Branches
	}
	if src.Remotes > 0 {
		dst.Remotes = src.Remotes
	}
	if src.Worktrees > 0 {
		dst.Worktrees = src.Worktrees
	}
	if src.Tags > 0 {
		dst.Tags = src.Tags
	}
	if src.Reflog > 0 {
		dst.Reflog = src.Reflog
	}
	if src.Feed > 0 {
		dst.Feed = src.Feed
	}
	if src.Fetch > 0 {
		dst.Fetch = src.Fetch
	}
	if src.RemoteTags > 0 {
		dst.RemoteTags = src.RemoteTags
	}
	if src.MinSeconds > 0 {
		dst.MinSeconds = src.MinSeconds
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
