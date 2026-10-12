package web

import (
	"errors"
	"net/http"
	"regexp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/overviewhtml"
)

// GET /api/overview-export?id=f7 | ?review=<id> | ?overview=<id>: the
// overview as one self-contained HTML file, sent as a download (spec
// 2026-10-12-overview-html-export) — an agent's overview from this page's
// store, a stored review's, or a permanent overview (gg overview save).
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/overview-export", s.handleOverviewExport)
	})
}

var reviewIDRE = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,80}$`)

func (s *Server) handleOverviewExport(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id, review, perm := q.Get("id"), q.Get("review"), q.Get("overview")
	given := 0
	for _, v := range []string{id, review, perm} {
		if v != "" {
			given++
		}
	}
	var src domain.OverviewSource
	switch {
	case given != 1:
		writeErr(w, http.StatusBadRequest, errors.New("want one of ?id=<overview id>, ?review=<review id>, ?overview=<id>"))
		return
	case id != "" && fileIDRE.MatchString(id):
		var err error
		if src, err = domain.AgentOverviewSource(s.docs, id, s.docsRoot(r.Context())); err != nil {
			writeErr(w, http.StatusNotFound, errors.New("no overview "+id))
			return
		}
	case review != "" && reviewIDRE.MatchString(review):
		src = domain.OverviewSource{ReviewID: review}
	case perm != "" && reviewIDRE.MatchString(perm):
		src = domain.OverviewSource{OverviewID: perm}
	default:
		writeErr(w, http.StatusBadRequest, errors.New("want one of ?id=<overview id>, ?review=<review id>, ?overview=<id>"))
		return
	}
	page, name, err := s.service().ExportOverviewHTML(readCtx(r), src)
	var tl *overviewhtml.TooLargeError
	switch {
	case errors.Is(err, domain.ErrNoOverview), errors.Is(err, domain.ErrReviewNotFound), errors.Is(err, domain.ErrOverviewNotFound):
		writeErr(w, http.StatusNotFound, err)
		return
	case errors.As(err, &tl):
		writeErr(w, http.StatusRequestEntityTooLarge, err)
		return
	case err != nil:
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="`+name+`"`) // a Slug: [a-z0-9-] only
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "no-store")
	_, _ = w.Write(page)
}
