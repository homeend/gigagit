package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/model"
)

// textTemplateRow is one text template on the wire. UserLabels are the
// body's <user:…> labels in order (the client prompts for these before a
// render); Automatic the other tokens it uses, as written.
type textTemplateRow struct {
	ID         string   `json:"id"`
	Title      string   `json:"title"`
	Body       string   `json:"body"`
	Scope      string   `json:"scope"` // "global" | "repo"
	UserLabels []string `json:"user_labels"`
	Automatic  []string `json:"automatic"`
}

func textTemplateRowOf(t model.TextTemplate) textTemplateRow {
	labels, auto := domain.TextTemplateTokens(t.Body)
	if labels == nil {
		labels = []string{}
	}
	if auto == nil {
		auto = []string{}
	}
	return textTemplateRow{ID: t.ID, Title: t.Title, Body: t.Body, Scope: t.Scope.String(), UserLabels: labels, Automatic: auto}
}

// handleTextTemplates lists the text templates, global rows then repo rows.
func (s *Server) handleTextTemplates(w http.ResponseWriter, r *http.Request) {
	ts, err := s.service().TextTemplates(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	rows := make([]textTemplateRow, 0, len(ts))
	for _, t := range ts {
		rows = append(rows, textTemplateRowOf(t))
	}
	writeJSON(w, map[string]any{"templates": rows})
}

// textTemplateReq is the body every text-template write shares.
type textTemplateReq struct {
	ID     string            `json:"id"`
	Scope  string            `json:"scope"`
	Title  string            `json:"title"`
	Body   string            `json:"body"`
	Inputs map[string]string `json:"inputs"`
}

// decodeTextTemplateReq reads the body and resolves its scope (400 on either).
func decodeTextTemplateReq(w http.ResponseWriter, r *http.Request) (textTemplateReq, model.ProfileScope, bool) {
	var req textTemplateReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return req, 0, false
	}
	scope, ok := parseProfileScope(req.Scope)
	if !ok {
		writeErr(w, http.StatusBadRequest, errors.New("scope must be global or repo"))
		return req, 0, false
	}
	return req, scope, true
}

// writeTextTemplateErr maps a store error: an unknown id is 404, a title
// already taken in the scope 409.
func writeTextTemplateErr(w http.ResponseWriter, err error) {
	switch {
	case domain.IsTextTemplateNotFound(err):
		writeErr(w, http.StatusNotFound, errors.New("unknown text template"))
	case domain.IsTextTemplateDuplicate(err):
		writeErr(w, http.StatusConflict, errors.New("a text template with this title already exists in that scope"))
	default:
		writeErr(w, http.StatusInternalServerError, err)
	}
}

// handleTextTemplateAdd stores a new template. Title and text are validated
// up front so a refusal is a clean 400 rather than a store error.
func (s *Server) handleTextTemplateAdd(w http.ResponseWriter, r *http.Request) {
	req, scope, ok := decodeTextTemplateReq(w, r)
	if !ok {
		return
	}
	if err := domain.ValidateTextTemplate(req.Title, req.Body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	added, err := s.service().AddTextTemplate(r.Context(), model.TextTemplate{Title: req.Title, Body: req.Body, Scope: scope})
	if err != nil {
		writeTextTemplateErr(w, err)
		return
	}
	writeJSON(w, textTemplateRowOf(added))
}

// handleTextTemplateUpdate replaces the title and text of the template
// (id, scope) names; the id follows the title, so the reply carries the row.
func (s *Server) handleTextTemplateUpdate(w http.ResponseWriter, r *http.Request) {
	req, scope, ok := decodeTextTemplateReq(w, r)
	if !ok {
		return
	}
	if err := domain.ValidateTextTemplate(req.Title, req.Body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	saved, err := s.service().UpdateTextTemplate(r.Context(), scope, req.ID, model.TextTemplate{Title: req.Title, Body: req.Body})
	if err != nil {
		writeTextTemplateErr(w, err)
		return
	}
	writeJSON(w, textTemplateRowOf(saved))
}

func (s *Server) handleTextTemplateRemove(w http.ResponseWriter, r *http.Request) {
	req, scope, ok := decodeTextTemplateReq(w, r)
	if !ok {
		return
	}
	if err := s.service().RemoveTextTemplate(r.Context(), scope, req.ID); err != nil {
		writeTextTemplateErr(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleTextTemplateRender previews a stored template: tokens resolve against
// the live repo, seq counters are PEEKED (the page consumes them through
// /take once the text is copied), and the wire never carries a raw template
// — only the exact id of one that exists in a fresh read.
func (s *Server) handleTextTemplateRender(w http.ResponseWriter, r *http.Request) {
	req, scope, ok := decodeTextTemplateReq(w, r)
	if !ok {
		return
	}
	svc := s.service()
	t, err := svc.FindTextTemplate(r.Context(), req.ID, &scope)
	if err != nil || t.ID != req.ID { // an id prefix is a CLI convenience, not a wire value
		writeErr(w, http.StatusNotFound, errors.New("unknown text template"))
		return
	}
	labels, _ := domain.TextTemplateTokens(t.Body)
	for _, l := range labels {
		if _, ok := req.Inputs[l]; !ok {
			writeErr(w, http.StatusBadRequest, fmt.Errorf("missing variable %s", l))
			return
		}
	}
	text, seqNames, err := svc.RenderTextTemplate(r.Context(), t.Body, req.Inputs)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	if seqNames == nil {
		seqNames = []string{}
	}
	writeJSON(w, map[string]any{"text": text, "seq_names": seqNames})
}

// seqNameRe bounds a counter name arriving on the wire.
var seqNameRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// handleTextTemplateTake consumes the counters of a rendered text the page
// has just copied (the TUI's bump on y).
func (s *Server) handleTextTemplateTake(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SeqNames []string `json:"seq_names"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	for _, n := range req.SeqNames {
		if !seqNameRe.MatchString(n) {
			writeErr(w, http.StatusBadRequest, errors.New("bad counter name"))
			return
		}
	}
	if len(req.SeqNames) > 0 {
		if err := s.service().BumpPrefixSeqs(r.Context(), req.SeqNames); err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}
