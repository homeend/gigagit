package web

import (
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/domain"
)

// Permanent overviews in the page (spec 2026-10-12-permanent-overviews §10):
// one overview's document, link and delete; its remarks and diff ride the
// file-content and diff lanes as overview=<id>. An id only ever matches a
// stored note's id — it never reaches a git argv.
func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/overview/{id}", s.handleOverviewDoc)
		mux.HandleFunc("GET /api/overview/{id}/link", s.handleOverviewLink)
	})
}

// overviewID is the request's id, answered 400 when it is not one.
func overviewID(w http.ResponseWriter, id string) (string, bool) {
	if !reviewIDRE.MatchString(id) {
		writeErr(w, http.StatusBadRequest, errors.New("want an overview id"))
		return "", false
	}
	return id, true
}

// overviewErr answers a domain error: 404 for a gone overview.
func overviewErr(w http.ResponseWriter, err error) {
	if errors.Is(err, domain.ErrOverviewNotFound) || errors.Is(err, domain.ErrNoOverview) {
		writeErr(w, http.StatusNotFound, err)
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func (s *Server) handleOverviewDoc(w http.ResponseWriter, r *http.Request) {
	id, ok := overviewID(w, r.PathValue("id"))
	if !ok {
		return
	}
	svc, ctx := s.service(), readCtx(r)
	o, err := svc.Overview(ctx, id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	od, err := svc.OverviewDocAt(ctx, id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	anchors := make([]overviewAnchor, 0, len(od.Anchors))
	for _, a := range od.Anchors {
		anchors = append(anchors, overviewAnchor{Dest: a.Dest, Path: a.Path, Start: a.Start, End: a.End, Note: a.Note,
			Label: a.Label, Missing: !a.OK, Plain: !a.OK})
	}
	pr, _ := domain.PRScopeNumber(o.Scope)
	writeJSON(w, map[string]any{
		"id": o.ID, "title": o.Title, "commit": o.Commit, "branch": o.Branch, "pr": pr,
		"agent": o.Agent, "created": wireTime(o.Created), "blocks": od.Doc.Blocks, "anchors": anchors, "text": od.Text,
	})
}

func (s *Server) handleOverviewLink(w http.ResponseWriter, r *http.Request) {
	id, ok := overviewID(w, r.PathValue("id"))
	if !ok {
		return
	}
	link, err := s.service().OverviewLink(readCtx(r), id)
	if err != nil {
		overviewErr(w, err)
		return
	}
	writeJSON(w, map[string]string{"link": link})
}

// handleOverviewDiff serves /api/diff?overview=<id>&path=<p>: the file's
// change in the overview's commit (a PR overview's: merge base..head) with
// its remarks — the review diff lane's shape.
func (s *Server) handleOverviewDiff(w http.ResponseWriter, r *http.Request, id string) {
	if _, ok := overviewID(w, id); !ok {
		return
	}
	path := r.URL.Query().Get("path")
	if !isGitArgSafe(path) {
		writeErr(w, http.StatusBadRequest, errors.New("a valid path is required"))
		return
	}
	rd, err := s.service().OverviewFileDiff(r.Context(), id, path)
	if err != nil {
		overviewErr(w, err)
		return
	}
	p := diffPayload(rd.Diff, nil)
	p["changed"], p["status"], p["old_path"] = rd.Changed, rd.Status, rd.OldPath
	p["left_label"], p["right_label"] = rd.Left, rd.Right
	p["remarks"] = overviewRemarkRows(rd.Notes)
	writeJSON(w, p)
}
