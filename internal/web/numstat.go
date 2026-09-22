package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/model"
)

// /api/numstat answers a change set's per-file line counts in one request —
// the stacked diff's header badges (+a −d). It is asked ONLY while a stack is
// open, so the listing endpoints and the status poll never pay for a numstat.
// Three sources, the ones git can name by a tree pair:
//
//	?sha=<hex>              a commit against its first parent (CommitFiles' pairing)
//	?left=<hex>&right=<hex> a hash comparison (CompareFiles' pairing)
//	?wt=staged|unstaged     a working-tree section
//
// Entry, shelf and link sets have no git pair; the client fills their counts
// from each file's diff when it loads.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/numstat", s.handleNumstat)
	})
}

type numstatFile struct {
	Path    string `json:"path"`
	OldPath string `json:"old_path,omitempty"`
	Add     int    `json:"add"`
	Del     int    `json:"del"`
	Binary  bool   `json:"binary,omitempty"`
}

func (s *Server) handleNumstat(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	q := r.URL.Query()
	var (
		stats []model.DiffStat
		err   error
	)
	switch sha, left, right, wt := q.Get("sha"), q.Get("left"), q.Get("right"), q.Get("wt"); {
	case sha != "" && left == "" && right == "" && wt == "":
		if !isHexSha(sha) {
			writeErr(w, http.StatusBadRequest, errors.New("sha must be a hex commit id"))
			return
		}
		stats, err = svc.CommitStat(r.Context(), sha)
	case left != "" && right != "" && sha == "" && wt == "":
		// hex-only: the pair is the one /api/compare resolved, never a name
		if !isHexSha(left) || !isHexSha(right) {
			writeErr(w, http.StatusBadRequest, errors.New("left/right must be hex commit ids"))
			return
		}
		stats, err = svc.DiffStat(r.Context(), model.DiffSpec{Rev: left + ".." + right})
	case wt == "staged" || wt == "unstaged":
		if sha != "" || left != "" || right != "" {
			writeErr(w, http.StatusBadRequest, errors.New("wt excludes sha/left/right"))
			return
		}
		stats, err = svc.DiffStat(r.Context(), model.DiffSpec{Cached: wt == "staged"})
	default:
		writeErr(w, http.StatusBadRequest, errors.New("want sha, left+right, or wt=staged|unstaged"))
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make([]numstatFile, len(stats))
	for i, st := range stats {
		out[i] = numstatFile{Path: st.Path, OldPath: st.OldPath, Add: st.Added, Del: st.Deleted, Binary: st.Binary}
	}
	writeJSON(w, map[string]any{"files": out})
}
