package web

import (
	"net/http"
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

type worktreeRow struct {
	Path     string `json:"path"`
	Branch   string `json:"branch"`
	Head     string `json:"head"`
	Detached bool   `json:"detached"`
	Bare     bool   `json:"bare"`
	// Time is the HEAD commit's committer time — what "sort worktrees by
	// date" means, exactly as in the TUI (its headTimes map). 0 when unknown
	// (a bare worktree, or the lookup failed): those rows sort last.
	Time int64 `json:"time"`
	// Reserved / Claim are the TUI's Worktrees mark column (⊘ / ⚑ <agent>):
	// a worktree kept away from agents, and the live claim of the agent
	// working in it.
	Reserved bool           `json:"reserved,omitempty"`
	Claim    *worktreeClaim `json:"claim,omitempty"`
}

// worktreeClaim is a live claim as the page draws it (the TUI's bar text):
// Since is RFC3339, formatted in the browser.
type worktreeClaim struct {
	Agent string `json:"agent"`
	Since string `json:"since"`
	Note  string `json:"note"`
}

func (s *Server) handleWorktrees(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ws, err := svc.Worktrees(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// One extra git invocation for every HEAD at once (the Snapshot query does
	// the same for the TUI). Best-effort: a failure costs the date sort its
	// keys, never the list.
	shas := make([]string, 0, len(ws))
	for _, wt := range ws {
		if wt.Head != "" {
			shas = append(shas, wt.Head)
		}
	}
	times := map[string]int64{}
	if len(shas) > 0 {
		if t, terr := svc.CommitTimes(readCtx(r), shas); terr == nil {
			times = t
		}
	}
	// The marks are the TUI's own read (worktreeMarksFor): stat-level, no
	// git. A config that does not load costs the reserve marks only.
	var reserved []string
	if ac, _, aerr := domain.AgentsConfigFrom(ws); aerr == nil {
		reserved = ac.Reserved
	}
	marks := svc.WorktreeMarks(ws, reserved)
	rows := make([]worktreeRow, 0, len(ws))
	for _, wt := range ws {
		row := worktreeRow{
			Path: wt.Path, Branch: wt.Branch, Head: wt.Head,
			Detached: wt.Detached, Bare: wt.Bare, Time: times[wt.Head],
		}
		if mk, ok := marks[wt.Path]; ok {
			row.Reserved = mk.Reserved
			if c := mk.Claim; c != nil {
				row.Claim = &worktreeClaim{Agent: c.Agent, Note: c.Note}
				if !c.Since.IsZero() {
					row.Claim.Since = c.Since.UTC().Format(time.RFC3339)
				}
			}
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"worktrees": rows})
}
