package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/linknav"
	"github.com/homeend/gigagit/internal/steer"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/link-switch", writeGuard(s.handleLinkSwitch))
	})
}

// POST /api/link-switch {link} is the # confirm's "switch there" (the TUI's
// switchToLink): a link into ANOTHER checkout is resolved again here — never
// a command from the wire — and its navigate is armed as the replay a
// steer-ask Switch arms, so the page lands it once its re-root (the page's
// own doReroot, called with the answered checkout) reaches that checkout.
// A repository link arms nothing: it only switches.
func (s *Server) handleLinkSwitch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Link string `json:"link"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&req); err != nil || strings.TrimSpace(req.Link) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("link required"))
		return
	}
	svc, ctx := s.service(), readCtx(r)
	res, err := linknav.Resolve(ctx, s.reposStatePath(), svc, strings.TrimSpace(req.Link))
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if top, err := svc.TopLevel(ctx); err == nil && domain.SamePath(top, res.Checkout) {
		writeErr(w, http.StatusConflict, errors.New("that link is in this checkout now — paste it again"))
		return
	}
	var replay *steer.Command
	if !linknav.RepoOnly(res) {
		// Lowered against the link's checkout, opened as /api/reroot opens it:
		// the hunk lowered here is the one the switched page shows.
		c, err := linknav.Command(ctx, s.open(res.Checkout), res)
		if err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		if _, err := toSteerWire(c); err != nil {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		replay = &c
	}
	s.armSwitchReplay(res.Checkout, replay)
	writeJSON(w, map[string]string{"checkout": res.Checkout})
}
