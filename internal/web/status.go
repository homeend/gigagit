package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/model"
)

// statusFile is one row of the /api/status contract (spec §B).
type statusFile struct {
	Path     string `json:"path"`
	OrigPath string `json:"orig_path,omitempty"`
	Staged   string `json:"staged"`
	Unstaged string `json:"unstaged"`
	Kind     string `json:"kind"`
}

func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	s.writeStatus(w, r)
}

// writeStatus responds with the current working-tree status. Shared with the
// stage handler, whose success response is a fresh status read (one
// round-trip for the SPA).
func (s *Server) writeStatus(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	st, err := svc.Status(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	files := make([]statusFile, 0, len(st.Files))
	for _, f := range st.Files {
		files = append(files, statusFile{
			Path:     f.Path,
			OrigPath: f.OrigPath,
			Staged:   statusByte(f.Staged),
			Unstaged: statusByte(f.Unstaged),
			Kind:     fileKindString(f.Kind),
		})
	}
	c := st.Counts()
	resp := map[string]any{
		"files": files,
		"counts": map[string]int{
			"staged": c.Staged, "unstaged": c.Unstaged,
			"untracked": c.Untracked, "conflicted": c.Conflicted,
		},
	}
	// domain.Conflict derives the paused-op state from the status just read
	// (no second status round-trip); the clean steady state costs zero git
	// invocations.
	if cs := svc.Conflict(readCtx(r), st); cs.Op != "" {
		resp["conflict"] = conflictPayload{
			Op: cs.Op, Source: cs.Source, Target: cs.Target,
			Desc: cs.Describe(), Conflicted: c.Conflicted,
		}
	} else if c.Conflicted > 0 {
		// Unmerged paths with NO paused sequencer op: a conflicted stash
		// apply (or similar application). The client bar must still appear —
		// per-file resolution works exactly as in the paused case — but with
		// the standalone action set: no continue (nothing is paused), no
		// conflict_complete AI lane (those tools finish a paused op), and a
		// "discard conflicted changes" escape backed by abort-apply.
		resp["conflict"] = conflictPayload{
			Op: "apply", Desc: "a stash apply or similar left conflicts",
			Conflicted: c.Conflicted, Standalone: true,
		}
	}
	writeJSON(w, resp)
}

// statusByte renders a porcelain XY byte as a 1-char string; a zero byte
// (never populated) renders as '.', the porcelain "unmodified" marker.
func statusByte(b byte) string {
	if b == 0 {
		return "."
	}
	return string(b)
}

func fileKindString(k model.FileKind) string {
	switch k {
	case model.KindUntracked:
		return "untracked"
	case model.KindUnmerged:
		return "conflicted"
	}
	return "tracked"
}
