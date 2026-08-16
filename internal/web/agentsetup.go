package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/homeend/gigagit/internal/agentinit"
	"github.com/homeend/gigagit/internal/agentskill"
)

// Agent-skill setup: `gg init`, in the browser.
//
// gg ships an embedded "using-gg" skill that teaches an AI agent this CLI.
// internal/agentinit knows which agents exist, where each one reads its
// skills from, and whether the copy already there is current. The TUI's
// Settings popup and `gg init` are two faces of that package; this is a third.
//
// Installing writes a file OUTSIDE the repository — into the project's
// .claude/, or into the user's home. So the wire may only ever name an agent
// ID, which is resolved against the detections this server just computed: the
// target path is never taken from the request, and an id that is not currently
// detected is a 404 rather than a write to wherever the caller asked.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/agents", s.handleAgents)
		mux.HandleFunc("POST /api/agents/install", writeGuard(s.handleAgentInstall))
	})
}

// agentRow is one detected agent on the wire. Checked is agentinit's own
// default (refresh what is already installed; a first install is opt-in), so
// the browser and `gg init` preselect the same set.
type agentRow struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Target  string `json:"target"`
	Status  string `json:"status"` // new | outdated | up to date
	Checked bool   `json:"checked"`
	Custom  bool   `json:"custom"` // added by `gg init --to`, not a builtin
}

// agentDetections lists what this machine has: the builtin registry filtered
// by what is actually present, plus any custom targets `gg init --to`
// recorded. homeDir comes from the environment on every call rather than
// being captured at boot, which is also what makes the tests hermetic (they
// point HOME at a temp dir).
func (s *Server) agentDetections(projDir string) []agentinit.Detection {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "" // no home: home-scoped agents are skipped entirely
	}
	dets := agentinit.Detect(projDir, home)
	if p := s.agentTargetsPath(); p != "" {
		if customs, err := agentinit.LoadCustomTargets(p); err == nil {
			dets = append(dets, agentinit.CustomDetections(customs)...)
		}
	}
	return dets
}

// agentTargetsPath is `gg init --to`'s registry, beside the MRU state file —
// the same path internal/cli derives, so a target added from the CLI shows up
// here.
func (s *Server) agentTargetsPath() string {
	sp := s.reposStatePath()
	if sp == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sp), "agent-targets.toml")
}

// isCustom reports whether d came from the custom-targets file rather than the
// builtin registry. CustomDetections builds entries whose ID is not in
// Builtins, which is the only distinction that survives into a Detection.
func isCustom(d agentinit.Detection) bool {
	for _, a := range agentinit.Builtins() {
		if a.ID == d.Agent.ID {
			return false
		}
	}
	return true
}

// agentWireID is the id the browser names an agent by. Builtin IDs are already
// unique; every custom target, however, arrives as agentinit.Detection with the
// literal ID "custom", so a second `gg init --to` target would be unreachable
// behind the first. The target path disambiguates them.
//
// The path in that string is COMPARED, never used: an install resolves the id
// against this same server-computed list and writes to the detection's own
// Target, so a request cannot aim the write anywhere by dressing a path up as
// an id.
func agentWireID(d agentinit.Detection) string {
	if isCustom(d) {
		return "custom:" + d.Target
	}
	return d.Agent.ID
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	top, err := s.service().TopLevel(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	dets := s.agentDetections(top)
	rows := make([]agentRow, 0, len(dets))
	for _, d := range dets {
		rows = append(rows, agentRow{
			ID:      agentWireID(d),
			Label:   d.Agent.Label,
			Target:  d.Target,
			Status:  d.Status.String(),
			Checked: d.Status.Checked(),
			Custom:  isCustom(d),
		})
	}
	writeJSON(w, map[string]any{"version": agentskill.Version, "project": top, "agents": rows})
}

type agentInstallRequest struct {
	ID string `json:"id"`
}

// handleAgentInstall writes the embedded skill into ONE detected agent's
// target and reports the target's new status. Synchronous: this is a single
// file write outside git, not a repository operation, so it neither takes the
// repo gate nor goes through the op transport (`gg init` does the same work
// the same way).
func (s *Server) handleAgentInstall(w http.ResponseWriter, r *http.Request) {
	var req agentInstallRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	top, err := s.service().TopLevel(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var chosen agentinit.Detection
	found := false
	for _, d := range s.agentDetections(top) {
		if agentWireID(d) == req.ID {
			chosen, found = d, true
			break
		}
	}
	if !found {
		// Not "unknown id": a detected agent can disappear between the listing
		// and the click (a directory removed), and that is worth saying plainly.
		writeErr(w, http.StatusNotFound, errors.New("no detected agent with id "+req.ID))
		return
	}
	was := chosen.Status.String()
	if err := agentinit.Install(chosen); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Re-read rather than assume "up to date": the install either landed or it
	// did not, and the file itself is the answer.
	after := ""
	for _, d := range s.agentDetections(top) {
		if agentWireID(d) == req.ID {
			after = d.Status.String()
			break
		}
	}
	writeJSON(w, map[string]any{
		"id":     agentWireID(chosen),
		"label":  chosen.Agent.Label,
		"target": chosen.Target,
		"was":    was,
		"status": after,
	})
}
