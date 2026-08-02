package web

import "net/http"

type branchRow struct {
	Name     string `json:"name"`
	Upstream string `json:"upstream,omitempty"`
	Ahead    int    `json:"ahead"`
	Behind   int    `json:"behind"`
	IsHead   bool   `json:"is_head"`
	Hash     string `json:"hash"`
	Time     int64  `json:"time"`
}

func (s *Server) handleBranches(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	bs, err := svc.Branches(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]branchRow, 0, len(bs))
	for _, b := range bs {
		rows = append(rows, branchRow{
			Name: b.Name, Upstream: b.Upstream, Ahead: b.Ahead, Behind: b.Behind,
			IsHead: b.IsHead, Hash: b.Hash, Time: b.UnixTime,
		})
	}
	writeJSON(w, map[string]any{"branches": rows})
}
