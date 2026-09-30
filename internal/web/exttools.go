package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// The external-tools VIEW: an inventory of the configured [[tools.command]]
// blocks (every category, every frontend — including rows this web frontend
// itself would filter out at run time) plus which catalog tools are detected
// on this machine. Adding/editing stays in the TUI Settings wizard; the one
// write here is answering a tool-template update offer (take new / keep
// mine), resolved by an offer id against a fresh status read — the wire never
// names a path or a command.

type extToolCmdRow struct {
	Category  string   `json:"category"`
	Name      string   `json:"name"`
	Mode      string   `json:"mode"`
	PerFile   bool     `json:"per_file"`
	WhenOp    string   `json:"when_op"`
	Frontends []string `json:"frontends"`
	Command   string   `json:"command"`
	// Valid reports the same structural checks the run-time lanes apply
	// before offering a command; Problem carries the first failure's text so
	// a misconfigured block is diagnosable from the browser.
	Valid   bool   `json:"valid"`
	Problem string `json:"problem"`
	// Approved: this repo has already approved this exact command text
	// (promptstate CommandHash — the store the TUI and the web lanes share).
	Approved bool `json:"approved"`
}

type extToolTemplateRow struct {
	Category   string `json:"category"`
	Name       string `json:"name"`
	OptIn      bool   `json:"opt_in"`
	Configured bool   `json:"configured"`
}

type extToolDetectedRow struct {
	ID        string               `json:"id"`
	Label     string               `json:"label"`
	Bin       string               `json:"bin"`
	Templates []extToolTemplateRow `json:"templates"`
}

// detections runs the catalog probe (or the test seam).
func (s *Server) detections() []exttool.Detection {
	if s.detectTools != nil {
		return s.detectTools()
	}
	home, _ := os.UserHomeDir()
	return exttool.Detect(exec.LookPath, os.Stat, home)
}

func (s *Server) handleExtTools(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	cfg, err := s.effectiveConfig(r.Context(), svc)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	var approved map[string]bool
	if store := s.promptStore(); store != nil {
		approved = store.ApprovedToolCommands(s.toolRepoKey(r.Context(), svc))
	}
	cmds := make([]extToolCmdRow, 0, len(cfg.Tools.Command))
	have := map[string]bool{}
	for _, tc := range cfg.Tools.Command {
		have[tc.Key()] = true
		row := extToolCmdRow{
			Category: tc.Category, Name: tc.Name, Mode: tc.Mode, PerFile: tc.PerFile,
			WhenOp: tc.WhenOp, Frontends: tc.Frontends, Command: tc.Command,
			Valid:    true,
			Approved: approved[promptstate.CommandHash(tc.Command)],
		}
		if verr := config.ValidateToolCommand(tc); verr != nil {
			row.Valid, row.Problem = false, verr.Error()
		} else if terr := template.ValidateCommandTokens(tc.Command, tc.PerFile); terr != nil {
			row.Valid, row.Problem = false, terr.Error()
		}
		cmds = append(cmds, row)
	}
	dets := make([]extToolDetectedRow, 0, 4)
	for _, det := range s.detections() {
		row := extToolDetectedRow{ID: det.Tool.ID, Label: det.Tool.Label, Bin: det.Bin}
		for _, ct := range domain.InstallTemplates(r.Context(), det, true) {
			row.Templates = append(row.Templates, extToolTemplateRow{
				Category: string(ct.Category), Name: ct.Name, OptIn: ct.OptIn,
				Configured: have[string(ct.Category)+"\x00"+ct.Name],
			})
		}
		dets = append(dets, row)
	}
	writeJSON(w, map[string]any{
		"commands":           cmds,
		"detected":           dets,
		"global_config_path": config.DefaultGlobalPath(),
		"template_offers":    s.templateOffers(r.Context()),
	})
}

// extToolOfferRow is one tool-template status worth showing: an update
// offer or an unsupported agent.
type extToolOfferRow struct {
	Category string `json:"category"`
	Name     string `json:"name"`
	Status   string `json:"status"` // "update" | "unsupported"
	Reason   string `json:"reason"`
	NewText  string `json:"new_text"` // the block as it would be written ("update" only)
	Path     string `json:"path"`     // display only; never read back from the wire
	OfferID  string `json:"offer_id"`
	Declined bool   `json:"declined"` // answered "Keep mine" earlier
}

func (s *Server) toolStatusesFor(ctx context.Context) []domain.ToolTemplateStatus {
	if s.toolStatuses != nil {
		return s.toolStatuses(ctx)
	}
	return s.service().ToolTemplateStatuses(ctx)
}

func (s *Server) templateOffers(ctx context.Context) []extToolOfferRow {
	var declined map[string]bool
	if store := s.promptStore(); store != nil {
		declined = store.DeclinedToolUpdates()
	}
	out := []extToolOfferRow{}
	for _, st := range s.toolStatusesFor(ctx) {
		row := extToolOfferRow{Category: st.Block.Category, Name: st.Block.Name, Reason: toolReason(st), Path: st.Path}
		switch st.Kind {
		case domain.ToolUpdateAvailable:
			row.Status = "update"
			row.NewText = config.RenderToolCommand(st.New)
			row.OfferID = promptstate.ToolUpdateID(st.OfferKey())
			row.Declined = declined[row.OfferID]
		case domain.ToolUnsupported:
			row.Status = "unsupported"
		default:
			continue
		}
		out = append(out, row)
	}
	return out
}

// toolReason is the TUI's offer-reason line in English (the web is not
// localised).
func toolReason(st domain.ToolTemplateStatus) string {
	if st.Kind == domain.ToolUnsupported {
		return fmt.Sprintf("%s %s is outside every version range this template supports", st.ToolLabel, st.AgentVersion)
	}
	var parts []string
	switch {
	case st.FromVersion == 0:
		parts = append(parts, fmt.Sprintf("written before template versions — current template v%d", st.ToVersion))
	case st.FromVersion < st.ToVersion:
		parts = append(parts, fmt.Sprintf("template updated (v%d → v%d)", st.FromVersion, st.ToVersion))
	}
	if st.FromVersion > 0 && st.FromRange != st.ToRange && st.AgentVersion != "" {
		parts = append(parts, fmt.Sprintf("%s %s detected — this block was written for %s", st.ToolLabel, st.AgentVersion, st.FromRange))
	}
	if st.Edited && st.FromVersion > 0 {
		parts = append(parts, "you changed this block")
	}
	return strings.Join(parts, " · ")
}

func (s *Server) handleExtToolsUpdate(w http.ResponseWriter, r *http.Request) {
	st, ok := s.offerByID(w, r)
	if !ok {
		return
	}
	if err := domain.ApplyToolUpdate(st); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) handleExtToolsKeep(w http.ResponseWriter, r *http.Request) {
	st, ok := s.offerByID(w, r)
	if !ok {
		return
	}
	store := s.promptStore()
	if store == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("no state directory: cannot remember the answer"))
		return
	}
	if err := store.DeclineToolUpdate(st.OfferKey()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// offerByID resolves a wire offer id against a FRESH status read.
func (s *Server) offerByID(w http.ResponseWriter, r *http.Request) (domain.ToolTemplateStatus, bool) {
	var req struct {
		OfferID string `json:"offer_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return domain.ToolTemplateStatus{}, false
	}
	for _, st := range s.toolStatusesFor(r.Context()) {
		if st.Kind == domain.ToolUpdateAvailable && promptstate.ToolUpdateID(st.OfferKey()) == req.OfferID {
			return st, true
		}
	}
	writeErr(w, http.StatusNotFound, errors.New("no such update offer"))
	return domain.ToolTemplateStatus{}, false
}
