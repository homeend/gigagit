package web

import (
	"net/http"
	"strconv"
)

// defaultReflogRows is the sidebar's first page (the tags cap precedent): the
// section opens on a screenful, and its "show more" row asks for a bigger
// window. maxReflogRows bounds what a client can ask for at all.
const (
	defaultReflogRows = 100
	maxReflogRows     = 5000
)

type reflogRow struct {
	Selector string `json:"selector"` // "HEAD@{0}"
	Hash     string `json:"hash"`     // full sha — checkout/reset target
	Short    string `json:"short"`
	Subject  string `json:"subject"`
	Rel      string `json:"rel,omitempty"` // "2 hours ago"
}

func (s *Server) handleReflog(w http.ResponseWriter, r *http.Request) {
	limit := defaultReflogRows
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, maxReflogRows)
	}
	svc := s.service()
	// One entry past the window: its presence is what "truncated" means, so
	// the client's "show more" row disappears exactly when nothing follows.
	es, err := svc.Reflog(readCtx(r), limit+1)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	truncated := false
	if len(es) > limit {
		es = es[:limit]
		truncated = true
	}
	rows := make([]reflogRow, 0, len(es))
	for _, e := range es {
		rows = append(rows, reflogRow{Selector: e.Selector, Hash: e.Hash, Short: e.ShortHash, Subject: e.Subject, Rel: e.Rel})
	}
	writeJSON(w, map[string]any{"entries": rows, "truncated": truncated})
}
