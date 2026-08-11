package web

import (
	"net/http"
	"path/filepath"
	"time"

	"github.com/homeend/gigagit/internal/observ"
)

// maxSessionErrorRows caps the payload (the reflog precedent) — the ring
// itself holds up to 500; a browser list wants the newest slice.
const maxSessionErrorRows = 100

type sessionErrorRow struct {
	Time   string `json:"time"` // RFC3339, UTC
	Source string `json:"source"`
	Detail string `json:"detail"`
}

// handleSessionErrors returns this server process's failure ring, newest
// first — the same genuine-failures feed the TUI's session-errors view
// reads (captured at the domain boundary; user aborts excluded).
func (s *Server) handleSessionErrors(w http.ResponseWriter, r *http.Request) {
	es := observ.SessionFailures()
	truncated := false
	if len(es) > maxSessionErrorRows {
		es = es[:maxSessionErrorRows]
		truncated = true
	}
	rows := make([]sessionErrorRow, 0, len(es))
	for _, e := range es {
		rows = append(rows, sessionErrorRow{Time: e.Time.UTC().Format(time.RFC3339), Source: e.Source, Detail: e.Detail})
	}
	logPath := ""
	if sp := s.reposStatePath(); sp != "" {
		logPath = filepath.Join(filepath.Dir(sp), "errors.log")
	}
	writeJSON(w, map[string]any{"errors": rows, "truncated": truncated, "log_path": logPath})
}
