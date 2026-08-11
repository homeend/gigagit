package web

import (
	"net/http"
	"os"
	"os/exec"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/exttool"
	"github.com/homeend/gigagit/internal/promptstate"
	"github.com/homeend/gigagit/internal/template"
)

// The external-tools VIEW: a read-only inventory of the configured
// [[tools.command]] blocks (every category, every frontend — including rows
// this web frontend itself would filter out at run time) plus which catalog
// tools are detected on this machine. Adding/editing stays in the TUI
// Settings wizard for now; this surface answers "what is configured, is it
// approved, and what could I add".

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
		for _, ct := range det.Tool.Commands {
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
	})
}
