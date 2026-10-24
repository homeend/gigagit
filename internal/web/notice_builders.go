package web

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
	pf "github.com/homeend/gigagit/internal/preflight"
	"github.com/homeend/gigagit/internal/promptstate"
)

// The TUI centre's notices the browser carries too (internal/tui/notify.go),
// with the TUI's ids, words and actions. Not carried: the clipboard and WSL
// interop notices (a browser copies through navigator.clipboard), drift
// (the drift panel says it in place) and interrupted sends (the PR section's
// bar).

// noticeFeatureDisabledPrefix + a preflight feature id. PERMANENT.
const noticeFeatureDisabledPrefix = "feature_disabled_"

// noticeToolTemplateUpdate counts the open tool-template update offers;
// noticeToolAgentUnsupportedPrefix + 12 hex of a block key names one block
// whose agent is outside every range. PERMANENT.
const (
	noticeToolTemplateUpdate         = "tool_template_update"
	noticeToolAgentUnsupportedPrefix = "tool_agent_unsupported_"
)

// opCommitGraphAuto is "Enable auto-refresh only": fetch.writeCommitGraph,
// no write now. PERMANENT wire name.
const opCommitGraphAuto = "commit-graph-auto"

func init() {
	RegisterOp(opCommitGraphAuto, func(*Server, *http.Request, opStartRequest) (engine.Operation, func(), int, error) {
		return engine.SetGitConfig{Key: "fetch.writeCommitGraph", Value: "true"}, nil, 0, nil
	})
}

// Ids "Never for this repo" may persist beyond the static allowlist: one per
// preflight feature, one per unsupported tool block.
var (
	featureNoticeID = regexp.MustCompile(`^` + noticeFeatureDisabledPrefix + `[a-z0-9-]{1,64}$`)
	toolNoticeID    = regexp.MustCompile(`^` + noticeToolAgentUnsupportedPrefix + `[0-9a-f]{12}$`)
)

// dismissable says whether POST /api/notifications/dismiss may persist id.
func dismissable(id string) bool {
	return dismissableNotices[id] || featureNoticeID.MatchString(id) || toolNoticeID.MatchString(id)
}

// staleLockNotice is the TUI's: a git lockfile is present. It does NOT claim
// the lock is stale — a git running right now holds one legitimately — so it
// lists each lock's age and lets the human decide.
func staleLockNotice(_ context.Context, _ *Server, _ *domain.Service, h model.RepoHealth, _ map[string]bool) []noticeWire {
	if len(h.StaleLocks) == 0 {
		return nil
	}
	title := fmt.Sprintf("%d git lock files are present — operations will fail", len(h.StaleLocks))
	remove := "Remove the lock files"
	if len(h.StaleLocks) == 1 {
		title, remove = "A git lock file is present — operations will fail", "Remove the lock file"
	}
	detail := []string{
		"git creates a lock file while it rewrites the file beside it and removes it on exit, so one left behind means a git process was killed before it could clean up.",
		`Until it is removed, git refuses to run with "Another git process seems to be running in this repository".`,
	}
	now := time.Now()
	paths := make([]string, 0, len(h.StaleLocks))
	for _, l := range h.StaleLocks {
		detail = append(detail, "  "+l.Name+"  ("+lockAgeLabel(now.Sub(l.ModTime))+")")
		paths = append(paths, l.Path)
	}
	detail = append(detail, "Only remove these if no other git is running right now — a live git holding one is doing real work, and deleting its lock would corrupt that write.")
	// The paths are the scan's; engine.RemoveGitLocks re-validates each
	// against the repository's git dirs before removing anything.
	actions := append([]noticeAction{{Op: "remove-locks", Label: remove, Paths: paths}}, notNowNever(noticeStaleLock)...)
	return []noticeWire{{ID: noticeStaleLock, Title: title, Detail: detail, Actions: actions}}
}

// lockAgeLabel is the TUI's: age is the only staleness signal there is.
func lockAgeLabel(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "less than a minute old"
	case d < time.Hour:
		return fmt.Sprintf("%dm old", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh old", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd old", int(d.Hours()/24))
	}
}

// commitGraphNotice is the TUI's: a big repo with no commit-graph and
// fetch.writeCommitGraph unset. The big-repo banner says the same thing; they
// share the id, so a Never in either silences both.
func commitGraphNotice(_ context.Context, s *Server, _ *domain.Service, h model.RepoHealth, _ map[string]bool) []noticeWire {
	if h.PackBytes < s.packFloor() || h.HasCommitGraph || h.WriteCommitGraphSet {
		return nil
	}
	return []noticeWire{{
		ID:    noticeCommitGraph,
		Title: "Commit browsing can be ~10× faster in this repo",
		Detail: []string{
			fmt.Sprintf("This repo is big (%.0f MB of packs) and has no commit-graph file, so ordered commit walks (the commit list's paging) re-walk history every time.", float64(h.PackBytes)/(1<<20)),
			"Writing one takes a moment and git keeps it fresh when fetch.writeCommitGraph is on.",
		},
		Actions: append([]noticeAction{
			{Op: "commit-graph", Label: "Write commit-graph now + keep it fresh (fetch.writeCommitGraph=true)"},
			{Op: opCommitGraphAuto, Label: "Enable auto-refresh only (graph appears on next fetch/gc)"},
		}, notNowNever(noticeCommitGraph)...),
	}}
}

// featureDisabledNotices is one standing notice per preflight feature gg
// runs without here (the TUI's featureDisabledNotices).
func featureDisabledNotices(ctx context.Context, _ *Server, svc *domain.Service, _ model.RepoHealth, _ map[string]bool) []noticeWire {
	vs, err := svc.Preflight(ctx)
	if err != nil {
		return nil // best-effort, like every other health-derived notice
	}
	return featureDisabledFor(vs)
}

// featureDisabledFor is the pure half: a Silent feature never raises one (its
// absence is the normal case); Repairable gets the `gg migrate` line,
// Unsatisfiable the remedy.
func featureDisabledFor(vs []pf.Verdict) []noticeWire {
	var out []noticeWire
	for _, v := range vs {
		if v.State == pf.Satisfied || v.Feature.Silent {
			continue
		}
		detail := []string{fmt.Sprintf(v.Reason.Format, v.Reason.Args...)}
		switch v.State {
		case pf.Repairable:
			detail = append(detail, "Run `gg migrate` to repair this, or answer the migration prompt at the next launch.")
		case pf.Unsatisfiable:
			if v.Remedy.Format != "" {
				detail = append(detail, fmt.Sprintf(v.Remedy.Format, v.Remedy.Args...))
			}
		}
		id := noticeFeatureDisabledPrefix + v.Feature.ID
		out = append(out, noticeWire{ID: id, Title: v.Feature.ID + " is unavailable in this repository", Detail: detail, Actions: notNowNever(id)})
	}
	return out
}

// toolTemplateNotices is the TUI's: one notice counting the open update
// offers (declined ones never count; no Never — a new offer must surface
// again) and one per block whose agent is outside every range. Both open
// External tools; nothing is written from a notice.
func toolTemplateNotices(ctx context.Context, s *Server, _ *domain.Service, _ model.RepoHealth, _ map[string]bool) []noticeWire {
	var declined map[string]bool
	if store := s.promptStore(); store != nil {
		declined = store.DeclinedToolUpdates()
	}
	open := noticeAction{Label: "Open external tools", Open: "exttools"}
	offers := 0
	var out []noticeWire
	for _, st := range s.toolStatusesCached(ctx) {
		switch st.Kind {
		case domain.ToolUpdateAvailable:
			if !declined[promptstate.ToolUpdateID(st.OfferKey())] {
				offers++
			}
		case domain.ToolUnsupported:
			id := noticeToolAgentUnsupportedPrefix + promptstate.ToolUpdateID(st.Block.Key())[:12]
			out = append(out, noticeWire{
				ID: id, Title: toolReason(st),
				Detail:  []string{"The block keeps running as it is. Update the agent, or edit the block in External tools."},
				Actions: append([]noticeAction{open}, notNowNever(id)...),
			})
		}
	}
	if offers == 0 {
		return out
	}
	return append([]noticeWire{{
		ID:      noticeToolTemplateUpdate,
		Title:   fmt.Sprintf("External-tool template updates available: %d", offers),
		Detail:  []string{"Newer templates exist for commands gg once wrote into your config. Nothing changes until you review each one."},
		Actions: []noticeAction{open, {Label: labelNotNow, Session: true}},
	}}, out...)
}

// The tool-status read probes every detected agent (`--version`), too much
// for a centre that re-reads after every op. The centre reads the last answer
// — the TUI's m.toolStatuses — which every full read refreshes (the External
// tools view, an offer taken or kept); a first read starts one in the
// background and the next centre read sees it.
type toolStatusCache struct {
	mu      sync.Mutex
	gen     int // bumped by a re-root: a read for the old repo is dropped
	have    bool
	loading bool
	val     []domain.ToolTemplateStatus
}

func (s *Server) toolStatusesCached(ctx context.Context) []domain.ToolTemplateStatus {
	if s.toolStatuses != nil {
		return s.toolStatuses(ctx) // the test seam answers at once
	}
	c := &s.toolCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.have && !c.loading {
		c.loading = true
		gen := c.gen
		s.goBackground(func(ctx context.Context) {
			s.rememberToolStatuses(gen, s.service().ToolTemplateStatuses(ctx))
			if h := s.liveHubRef(); h != nil {
				h.fanOut(liveMsg{Changed: []string{}, Reason: "notices"}) // the tool notices show now (the TUI rebuilds on arrival)
			}
		})
	}
	return c.val
}

// rememberToolStatuses is a full read's write into the cache (gen: the cache
// generation the read started in).
func (s *Server) rememberToolStatuses(gen int, v []domain.ToolTemplateStatus) {
	c := &s.toolCache
	c.mu.Lock()
	defer c.mu.Unlock()
	if gen != c.gen {
		return
	}
	c.val, c.have, c.loading = v, true, false
}

// toolCacheGen is the generation a read starting now belongs to.
func (s *Server) toolCacheGen() int {
	s.toolCache.mu.Lock()
	defer s.toolCache.mu.Unlock()
	return s.toolCache.gen
}

// forgetToolStatuses drops the cache (a re-root: another repo's config).
func (s *Server) forgetToolStatuses() {
	c := &s.toolCache
	c.mu.Lock()
	c.gen++
	c.val, c.have, c.loading = nil, false, false
	c.mu.Unlock()
}
