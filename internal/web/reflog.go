package web

import "net/http"

// maxReflogRows caps the sidebar payload (the tags cap precedent): the domain
// read already stops at its own configured limit, but 200 rows is more than a
// sidebar section wants to carry.
const maxReflogRows = 100

type reflogRow struct {
	Selector string `json:"selector"` // "HEAD@{0}"
	Hash     string `json:"hash"`     // full sha — checkout/reset target
	Short    string `json:"short"`
	Subject  string `json:"subject"`
	Rel      string `json:"rel,omitempty"` // "2 hours ago"
}

func (s *Server) handleReflog(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	es, err := svc.Reflog(readCtx(r), 0) // 0 = the domain default limit
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	truncated := false
	if len(es) > maxReflogRows {
		es = es[:maxReflogRows]
		truncated = true
	}
	rows := make([]reflogRow, 0, len(es))
	for _, e := range es {
		rows = append(rows, reflogRow{Selector: e.Selector, Hash: e.Hash, Short: e.ShortHash, Subject: e.Subject, Rel: e.Rel})
	}
	writeJSON(w, map[string]any{"entries": rows, "truncated": truncated})
}
