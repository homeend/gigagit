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
	st, err := s.svc.Status(r.Context())
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
	writeJSON(w, map[string]any{
		"files": files,
		"counts": map[string]int{
			"staged": c.Staged, "unstaged": c.Unstaged,
			"untracked": c.Untracked, "conflicted": c.Conflicted,
		},
	})
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
