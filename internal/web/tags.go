package web

import "net/http"

// maxTagRows caps the sidebar payload: big repos carry hundreds of tags
// (linux: 937) and the sidebar is not the place for all of them.
const maxTagRows = 100

type tagRow struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Annotated bool   `json:"annotated"`
	Subject   string `json:"subject,omitempty"`
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ts, err := svc.Tags(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	truncated := false
	if len(ts) > maxTagRows {
		ts = ts[:maxTagRows]
		truncated = true
	}
	rows := make([]tagRow, 0, len(ts))
	for _, tg := range ts {
		rows = append(rows, tagRow{Name: tg.Name, Target: tg.Target, Annotated: tg.Annotated, Subject: tg.Subject})
	}
	writeJSON(w, map[string]any{"tags": rows, "truncated": truncated})
}
