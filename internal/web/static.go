package web

import (
	"net/http"

	"github.com/homeend/gigagit/internal/web/static"
)

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	b, err := static.FS.ReadFile("index.html")
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(b)
}
