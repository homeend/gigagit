package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/repos"
)

// The web settings panel mirrors the TUI Settings popup's config-value rows.
// Two write destinations exist and routing one key to the wrong file would
// put a machine setting into a committed .gg.toml (or vice versa):
//   - per-repo (activeRepoConfigPath): show_graph, commit_sort, versions.*,
//     refresh intervals, the worktree post-create hook
//   - global (config.DefaultGlobalPath): op log, auto-refresh master switch,
//     remote-tags auto
// Settings the web frontend itself does not consume (the TUI's background
// refresh lane, its operation log) are still editable here — same machine,
// same config files — and the client captions them (TUI). The TUI display
// language is NOT here: i18n is a TUI-layer package the archtest DAG keeps
// out of web, and the web renders English regardless.

// refreshSources is the allowlisted [refresh] interval key set — the TUI's
// scheduledItems vocabulary (refreshTomlKey).
var refreshSources = []string{"status", "branches", "remotes", "worktrees", "tags", "reflog", "feed", "fetch", "remote_tags"}

type settingsPayload struct {
	ShowGraph          bool           `json:"show_graph"`   // effective: anything but explicit "off" is on
	CommitSort         string         `json:"commit_sort"`  // "date-order" | "plain"
	AutoRefresh        bool           `json:"auto_refresh"` // [refresh] enabled (TUI background lane)
	RemoteTagsAuto     bool           `json:"remote_tags_auto"`
	OpLog              bool           `json:"op_log"`
	OpLogPath          string         `json:"op_log_path,omitempty"`
	VersionsEnabled    bool           `json:"versions_enabled"`
	VersionsMaxAgeDays int            `json:"versions_max_age_days"`
	Refresh            map[string]int `json:"refresh"`
	Hook               string         `json:"hook"`
	RepoConfigPath     string         `json:"repo_config_path"`
	RepoConfigPrivate  bool           `json:"repo_config_private"` // machine-local file vs committed .gg.toml
	GlobalConfigPath   string         `json:"global_config_path"`
	CommitGraphKnown   bool           `json:"commit_graph_known"`
	CommitGraphPresent bool           `json:"commit_graph_present"`
	CommitGraphAuto    bool           `json:"commit_graph_auto"` // fetch.writeCommitGraph=true
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	active, err := s.activeRepoConfigPath(ctx, svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	cfg, err := config.Load(config.DefaultGlobalPath(), active)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	sort := cfg.UI.CommitSort
	if sort == "" {
		sort = "date-order"
	}
	p := settingsPayload{
		ShowGraph:          cfg.UI.ShowGraph != "off",
		CommitSort:         sort,
		AutoRefresh:        cfg.Refresh.Enabled,
		RemoteTagsAuto:     !cfg.Refresh.DisableRemoteTagsAuto,
		OpLog:              cfg.Debug.LogOperations,
		OpLogPath:          webOpLogPath(),
		VersionsEnabled:    !cfg.Versions.Disabled,
		VersionsMaxAgeDays: cfg.Versions.MaxAgeDays,
		Refresh: map[string]int{
			"status": cfg.Refresh.Status, "branches": cfg.Refresh.Branches,
			"remotes": cfg.Refresh.Remotes, "worktrees": cfg.Refresh.Worktrees,
			"tags": cfg.Refresh.Tags, "reflog": cfg.Refresh.Reflog,
			"feed": cfg.Refresh.Feed, "fetch": cfg.Refresh.Fetch,
			"remote_tags": cfg.Refresh.RemoteTags,
		},
		Hook:              cfg.Worktree.PostCreateHook,
		RepoConfigPath:    active,
		RepoConfigPrivate: filepath.Base(active) != ".gg.toml",
		GlobalConfigPath:  config.DefaultGlobalPath(),
	}
	// Best-effort: the commit-graph row degrades to "checking…" client-side
	// when health cannot be read; the rest of the panel must still render.
	if h, herr := svc.RepoHealth(ctx); herr == nil {
		p.CommitGraphKnown = true
		p.CommitGraphPresent = h.HasCommitGraph
		p.CommitGraphAuto = h.WriteCommitGraphSet && h.WriteCommitGraphValue == "true"
	}
	writeJSON(w, p)
}

// webOpLogPath mirrors the TUI's defaultOpLogPath: beside the repo registry
// in the gg state dir. Display-only here — the web has no oplog sink; the
// toggle arms the NEXT TUI session.
func webOpLogPath() string {
	sp := repos.DefaultStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "operations.log")
}

type settingsWriteRequest struct {
	ShowGraph          string         `json:"show_graph"`  // "on" | "off"
	CommitSort         string         `json:"commit_sort"` // "date-order" | "plain"
	AutoRefresh        *bool          `json:"auto_refresh"`
	RemoteTagsAuto     *bool          `json:"remote_tags_auto"`
	OpLog              *bool          `json:"op_log"`
	VersionsEnabled    *bool          `json:"versions_enabled"`
	VersionsMaxAgeDays *int           `json:"versions_max_age_days"` // -1 = keep forever, else > 0
	Refresh            map[string]int `json:"refresh"`               // source → seconds (0 = off)
	Hook               *string        `json:"hook"`
}

// handleSettingsSet validates every named field first, then writes — a bad
// member refuses the whole request before any file changes. Enum fields stay
// allowlisted (the uiconfig rule); the hook is the one deliberate exception
// to "free config text never crosses the wire": the script's EXECUTION is
// separately gated by the engine's approval decision that shows it before a
// worktree create runs it, so storing it is no more power than the TUI's
// hook editor grants.
func (s *Server) handleSettingsSet(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	var req settingsWriteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	// Validate everything up front.
	if req.ShowGraph != "" && req.ShowGraph != "on" && req.ShowGraph != "off" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid show_graph"))
		return
	}
	if req.CommitSort != "" && req.CommitSort != "date-order" && req.CommitSort != "plain" {
		writeErr(w, http.StatusBadRequest, errors.New("invalid commit_sort"))
		return
	}
	if req.VersionsMaxAgeDays != nil && (*req.VersionsMaxAgeDays == 0 || *req.VersionsMaxAgeDays < -1) {
		writeErr(w, http.StatusBadRequest, errors.New("retention must be a positive day count or -1"))
		return
	}
	for src, secs := range req.Refresh {
		if !validRefreshSource(src) {
			writeErr(w, http.StatusBadRequest, errors.New("unknown refresh source: "+src))
			return
		}
		if secs < 0 {
			writeErr(w, http.StatusBadRequest, errors.New("interval must be >= 0"))
			return
		}
	}
	if req.ShowGraph == "" && req.CommitSort == "" && req.AutoRefresh == nil && req.RemoteTagsAuto == nil &&
		req.OpLog == nil && req.VersionsEnabled == nil && req.VersionsMaxAgeDays == nil &&
		len(req.Refresh) == 0 && req.Hook == nil {
		writeErr(w, http.StatusBadRequest, errors.New("nothing to set"))
		return
	}

	// Per-repo destination.
	needRepo := req.ShowGraph != "" || req.CommitSort != "" || req.VersionsEnabled != nil ||
		req.VersionsMaxAgeDays != nil || len(req.Refresh) > 0 || req.Hook != nil
	repoPath := ""
	if needRepo {
		p, err := s.activeRepoConfigPath(r.Context(), svc)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		repoPath = p
	}
	fail := func(err error) {
		writeErr(w, http.StatusInternalServerError, err)
	}
	if req.ShowGraph != "" {
		if err := config.SetShowGraph(repoPath, req.ShowGraph); err != nil {
			fail(err)
			return
		}
	}
	if req.CommitSort != "" {
		if err := config.SetCommitSort(repoPath, req.CommitSort); err != nil {
			fail(err)
			return
		}
		s.resetFeed() // feedFor re-reads commit_sort at the next build
	}
	if req.VersionsEnabled != nil {
		if err := config.SetVersionsDisabled(repoPath, !*req.VersionsEnabled); err != nil {
			fail(err)
			return
		}
	}
	if req.VersionsMaxAgeDays != nil {
		if err := config.SetVersionsMaxAgeDays(repoPath, *req.VersionsMaxAgeDays); err != nil {
			fail(err)
			return
		}
	}
	if req.VersionsEnabled != nil || req.VersionsMaxAgeDays != nil {
		// Re-apply live so the NEXT op honors it (the TUI does the same);
		// without this the long-lived server keeps its boot-time policy.
		applyVersionsPolicy(r.Context(), svc, s.activeRepoConfigPathOr(r.Context(), svc))
	}
	for src, secs := range req.Refresh {
		if err := config.SetRefreshInterval(repoPath, src, secs); err != nil {
			fail(err)
			return
		}
	}
	if req.Hook != nil {
		if err := config.SetWorktreePostCreateHook(repoPath, *req.Hook); err != nil {
			fail(err)
			return
		}
	}

	// Global destination.
	gp := config.DefaultGlobalPath()
	if req.AutoRefresh != nil {
		if err := config.SetGlobalRefreshEnabled(gp, *req.AutoRefresh); err != nil {
			fail(err)
			return
		}
	}
	if req.RemoteTagsAuto != nil {
		if err := config.SetGlobalDisableRemoteTagsAuto(gp, !*req.RemoteTagsAuto); err != nil {
			fail(err)
			return
		}
	}
	if req.OpLog != nil {
		if err := config.SetGlobalDebugLogOperations(gp, *req.OpLog); err != nil {
			fail(err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func validRefreshSource(src string) bool {
	for _, s := range refreshSources {
		if s == src {
			return true
		}
	}
	return false
}

// activeRepoConfigPathOr is activeRepoConfigPath with a "" fallback for the
// best-effort policy re-apply (a resolve failure must not fail the write
// that already happened).
func (s *Server) activeRepoConfigPathOr(ctx context.Context, svc *domain.Service) string {
	p, err := s.activeRepoConfigPath(ctx, svc)
	if err != nil {
		return ""
	}
	return p
}

// applyVersionsPolicy loads the effective config and injects the branch-
// version snapshot policy into the service — the cli.Run pattern. Called at
// serve boot, after a re-root, and after a settings write, so the long-lived
// server honors [versions] like the one-shot frontends do. Best-effort: a
// load failure keeps the current policy.
func applyVersionsPolicy(ctx context.Context, svc *domain.Service, activeRepoPath string) {
	cfg, err := config.Load(config.DefaultGlobalPath(), activeRepoPath)
	if err != nil {
		return
	}
	svc.SetVersionsPolicy(engine.VersionsPolicy{Enabled: !cfg.Versions.Disabled, MaxAgeDays: cfg.Versions.MaxAgeDays})
}
