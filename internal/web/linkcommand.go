package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/link-command", s.handleLinkCommand)
	})
}

// GET /api/link-command resolves a link pasted into the page's # prompt the
// way the TUI's # prompt does (linknav): a place in THIS checkout comes back
// as the steer command the page lands with steerNavigate; a link to another
// checkout comes back as that checkout, for the page to name.
func (s *Server) handleLinkCommand(w http.ResponseWriter, r *http.Request) {
	text := strings.TrimSpace(r.URL.Query().Get("link"))
	if text == "" {
		writeErr(w, http.StatusBadRequest, errors.New("link required"))
		return
	}
	svc := s.service()
	ctx := readCtx(r)
	res, err := linknav.Resolve(ctx, s.reposStatePath(), svc, text)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	top, err := svc.TopLevel(ctx)
	if err != nil || !domain.SamePath(top, res.Checkout) {
		writeJSON(w, map[string]string{"checkout": res.Checkout})
		return
	}
	if linknav.RepoOnly(res) {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("that link names this repository, not a place in it"))
		return
	}
	c, err := linknav.Command(ctx, svc, res)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	wire, err := toSteerWire(c)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	s.freezePair(ctx, &wire)
	writeJSON(w, map[string]any{"steer": wire})
}
