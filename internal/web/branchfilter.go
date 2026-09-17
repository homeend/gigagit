package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/branchfilter"
	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/promptstate"
)

// Branch filters on the web: the server evaluates (one matcher, Go RE2) and
// the client only renders. The active slot is the same per-repo record the
// TUI keeps in prompts.toml, so alt+2 in one is what the other shows next.

type filterWire struct {
	Slot   int    `json:"slot"`
	Name   string `json:"name"`
	Mode   string `json:"mode"`
	Hidden int    `json:"hidden"`
}

type slotWire struct {
	Slot int `json:"slot"`
	// Name is the RAW name clause and Label is what to show (Name, or the
	// "slot N" fallback). Keeping them apart is what lets the settings form
	// round-trip an unnamed slot: prefilling its name input with the label
	// would bake "slot 3" into the file on the next save.
	Name        string `json:"name"`
	Label       string `json:"label"`
	Mode        string `json:"mode"`
	Summary     string `json:"summary"`
	Usable      bool   `json:"usable"`
	Error       string `json:"error,omitempty"`
	Scope       string `json:"scope,omitempty"` // "global" | "repo" | "both" | "" (unset)
	OlderThan   string `json:"older_than"`
	YoungerThan string `json:"younger_than"`
	Prefix      string `json:"prefix"`
	Suffix      string `json:"suffix"`
	Contains    string `json:"contains"`
	Regex       string `json:"regex"`
}

// branchFilterRepoKey is the promptstate scope: the git common dir (the key
// the TUI writes the same record under).
//
// It reads the process-wide cache first (notifications.go), because this runs
// on /api/branches and /api/remotes — every live refresh, filter or not — and
// `git rev-parse --git-common-dir` is a subprocess that domain's singleflight
// coalesces but never caches. The cache slot is keyed by the service pointer,
// so a re-root cannot serve the previous repo's key. This is a plain GET
// handler, never an operation, so resolving the key here is safe (see the
// deadlock note above rememberRepoKey).
func (s *Server) branchFilterRepoKey(ctx context.Context, svc *domain.Service) string {
	if key := cachedRepoKey(svc); key != "" {
		return key
	}
	key, err := svc.GitCommonDir(ctx)
	if err != nil {
		return ""
	}
	rememberRepoKey(svc, key)
	return key
}

// activeBranchFilter resolves list's remembered slot to a usable Compiled;
// nil when none is set, or the stored slot no longer exists / is inert.
// Callers resolve this FIRST and fetch the exemption inputs (worktrees, the
// branch list) only when it is non-nil: /api/branches and /api/remotes are
// hit by every live refresh and must not grow a git call for a repo that has
// no filter on.
func (s *Server) activeBranchFilter(ctx context.Context, svc *domain.Service, list string) *branchfilter.Compiled {
	store := s.webUIStore()
	if store == nil {
		return nil
	}
	// An unresolvable repo is no repo: reading under the empty key would
	// serve whatever a bad WRITE once left in the store's "" record.
	key := s.branchFilterRepoKey(ctx, svc)
	if key == "" {
		return nil
	}
	slot := store.BranchFilterSlot(key, list)
	if slot < 1 || slot > branchfilter.MaxSlots {
		return nil
	}
	all, _, err := svc.BranchFilters(ctx)
	if err != nil || !all[slot-1].Usable() {
		return nil
	}
	c := all[slot-1]
	return &c
}

// wireFor renders the active slot for a list payload; nil when none is on.
func wireFor(c *branchfilter.Compiled, hidden int) *filterWire {
	if c == nil {
		return nil
	}
	return &filterWire{Slot: c.Slot.Slot, Name: c.Label(), Mode: string(c.Mode), Hidden: hidden}
}

// applyFilter runs the active slot over rows; nil verdicts when none.
func applyFilter(c *branchfilter.Compiled, rows []branchfilter.Row, exempt []bool) ([]branchfilter.Verdict, int) {
	if c == nil {
		return nil, 0
	}
	return branchfilter.Apply(*c, rows, exempt, time.Now())
}

type branchFilterSetRequest struct {
	List string `json:"list"`
	Slot *int   `json:"slot"`
}

// handleBranchFilterSet activates a slot for one list (0 clears it). An
// unusable slot is refused rather than stored, so the chip can never show a
// rule that filters nothing.
func (s *Server) handleBranchFilterSet(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	var req branchFilterSetRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("bad request body"))
		return
	}
	if req.List != promptstate.BranchFilterListBranches && req.List != promptstate.BranchFilterListRemotes {
		writeErr(w, http.StatusBadRequest, errors.New("list must be branches or remotes"))
		return
	}
	if req.Slot == nil || *req.Slot < 0 || *req.Slot > branchfilter.MaxSlots {
		writeErr(w, http.StatusBadRequest, errors.New("slot must be 0.."+strconv.Itoa(branchfilter.MaxSlots)))
		return
	}
	var active *branchfilter.Compiled
	if *req.Slot > 0 {
		all, _, err := svc.BranchFilters(ctx)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		c := all[*req.Slot-1]
		if !c.Usable() {
			writeErr(w, http.StatusConflict, errors.New("slot "+strconv.Itoa(*req.Slot)+": "+c.Summary()))
			return
		}
		active = &c
	}
	store := s.webUIStore()
	if store == nil {
		writeErr(w, http.StatusInternalServerError, errors.New("no state dir to remember the filter in"))
		return
	}
	// The promptstate record is keyed by the git common dir, which is also
	// what the TUI writes under. With no key there is nothing to key it to:
	// writing anyway would persist a `[branch_filter.""]` record no reader
	// ever looks up, so the chip would come back off and the user would
	// never learn why. Say so instead of pretending it stuck.
	key := s.branchFilterRepoKey(ctx, svc)
	if key == "" {
		writeErr(w, http.StatusServiceUnavailable, errors.New("repo not resolved — not remembered"))
		return
	}
	if err := store.SetBranchFilterSlot(key, req.List, *req.Slot); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]any{"filter": wireFor(active, 0)})
}

// slotWires renders the five slots for the chip menu and the settings view,
// with provenance (which file defines each) so "remove" can peel the
// effective layer exactly as the TUI's d does.
func slotWires(all [branchfilter.MaxSlots]branchfilter.Compiled, globalPath, repoPath string) []slotWire {
	slots := make([]slotWire, 0, branchfilter.MaxSlots)
	for _, c := range all {
		sw := slotWire{Slot: c.Slot.Slot, Name: c.Slot.Name, Label: c.Label(), Mode: string(c.Mode), Summary: c.Summary(), Usable: c.Usable(),
			OlderThan: c.OlderThan, YoungerThan: c.YoungerThan, Prefix: c.Prefix, Suffix: c.Suffix, Contains: c.Contains, Regex: c.Regex}
		if c.Err != nil {
			sw.Error = c.Err.Error()
		}
		switch inGlobal, inRepo := config.BranchFilterScopes(globalPath, repoPath, c.Slot.Slot); {
		case inGlobal && inRepo:
			sw.Scope = "both"
		case inRepo:
			sw.Scope = "repo"
		case inGlobal:
			sw.Scope = "global"
		}
		slots = append(slots, sw)
	}
	return slots
}

func (s *Server) handleBranchFilters(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ctx := readCtx(r)
	all, warnings, err := svc.BranchFilters(ctx)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	repoPath := s.activeRepoConfigPathOr(ctx, svc) // "" on error: provenance degrades to global-only
	if warnings == nil {
		warnings = []string{}
	}
	writeJSON(w, map[string]any{"slots": slotWires(all, config.DefaultGlobalPath(), repoPath), "warnings": warnings})
}
