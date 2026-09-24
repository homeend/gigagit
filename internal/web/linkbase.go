package web

import (
	"errors"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// The base picker's server half (spec W1). The page has no link parser, so it
// can neither classify a link nor rewrite one: this route answers the LOCATED
// kind and the suggestion and — given a base — the rewritten link text, made
// by model.Link.WithBase, the ONE rewrite. Both forms are reads.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/link-base", s.handleLinkBase)
	})
}

// linkBoundKindWire is the wire spelling; a kind missing here reads as "none"
// (no base row), the safe side.
func linkBoundKindWire(k model.LinkBoundKind) string {
	switch k {
	case model.LinkBoundRef:
		return "ref"
	case model.LinkBoundCommit:
		return "commit"
	}
	return "none"
}

func (s *Server) handleLinkBase(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("link"))
	if text == "" {
		writeErr(w, http.StatusBadRequest, errors.New("link required"))
		return
	}
	base := strings.TrimSpace(q.Get("base"))
	none := func(err error) {
		// No base row is the whole answer while the user is still typing (an
		// unparseable link, a sha that does not resolve yet): the comparison
		// itself reports what is wrong with a link. A REWRITE, though, was
		// asked for by name and must say why it cannot happen.
		if base != "" {
			writeErr(w, http.StatusUnprocessableEntity, err)
			return
		}
		writeJSON(w, map[string]string{"kind": "none"})
	}
	l, err := model.ParseLink(text)
	if err != nil {
		none(err)
		return
	}
	svc := s.service()
	// A parsed link is DESCRIBED too (desc, and origin when it carries a landing
	// hint): the dialog's field holds forty hex digits, and the history row the
	// user picked it from said "pair: Fix tests…". The words follow the link into
	// the field — and a typed or pasted link gets the same ones.
	described := func(m map[string]string) map[string]string {
		m["desc"] = svc.DescribeLink(r.Context(), l)
		// A view hint names where the link LANDS, not a saved surface it was
		// copied from.
		if l.Hint.Kind != "" && l.Hint.Kind != model.ContentHintKind {
			m["origin"] = "copied from a saved " + l.Hint.Kind
		}
		return m
	}
	// The kind comes from SuggestBase, which LOCATES the link: the pure
	// BoundKind cannot tell a local-form file link from a whole tree.
	sug, err := svc.SuggestBase(r.Context(), l, s.linkOpts(svc))
	if err != nil {
		if r.Context().Err() != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		if base == "" {
			writeJSON(w, described(map[string]string{"kind": "none"}))
			return
		}
		none(err)
		return
	}
	if base == "" {
		writeJSON(w, described(map[string]string{"kind": linkBoundKindWire(sug.Kind), "base": sug.Base, "why": sug.Why}))
		return
	}
	if sug.Kind == model.LinkBoundNone {
		writeErr(w, http.StatusUnprocessableEntity, errors.New("this link cannot take a base"))
		return
	}
	// Self is the commit's FULL sha, resolved here: the link may hold an
	// abbreviation the user typed, and the client never supplies one.
	out, ok := l.WithBase(base, sug.Self)
	if !ok {
		why := "a branch or tag other than the link's own"
		if sug.Kind == model.LinkBoundCommit {
			why = "a full commit id"
		}
		writeErr(w, http.StatusUnprocessableEntity, errors.New("that base cannot bound this link — it takes "+why))
		return
	}
	writeJSON(w, map[string]string{"link": out.String()})
}
