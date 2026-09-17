package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/homeend/gigagit/internal/model"
)

// The copied-link history. Unlike every other list this file serves, the web
// is a PRODUCER of these rows as well as a reader: links.js builds link text
// client-side (linkFor/repoSegment), so there is no server-side moment at
// which a copied link exists — the copy handler has to post the string it
// just wrote to the clipboard.
//
// And the history must be SERVED rather than kept in the browser: `gg web`
// binds a random port every run, and localStorage is per-origin, so anything
// stored there vanishes on the next start.
//
// maxLinkHistBody bounds the POST body. A link is a path plus a target plus
// an optional hint, and a Desc is capped at 60 runes at the producer, so 4 KiB
// is generous; the other JSON handlers use the same 4-8 KiB range.
const maxLinkHistBody = 4 << 10

type linkHistRow struct {
	Link    string `json:"link"`
	Desc    string `json:"desc,omitempty"`
	Created string `json:"created,omitempty"`
}

type linkHistPayload struct {
	// Links is never null: the client iterates it directly.
	Links []linkHistRow `json:"links"`
}

// handleLinkHist serves GET /api/linkhist — this repo's copied-link ring,
// newest first. An unresolvable state dir disables the history, and
// LinkHistory returns nil for that exactly as it does for "nothing copied
// yet"; both are an empty list here, never an error, matching RecordLink's
// best-effort posture on the write side.
func (s *Server) handleLinkHist(w http.ResponseWriter, r *http.Request) {
	hist := s.service().LinkHistory(readCtx(r))
	rows := make([]linkHistRow, 0, len(hist))
	for _, e := range hist {
		rows = append(rows, linkHistRow{Link: e.Link, Desc: e.Desc, Created: e.Created})
	}
	writeJSON(w, linkHistPayload{Links: rows})
}

// handleLinkHistAdd serves POST /api/linkhist — the client reporting a link
// it has just copied to the clipboard.
//
// The body is wire input, so the link is validated with model.ParseLink here
// rather than trusted: a malformed link would otherwise sit in the ring and
// fail later, at `gg open` time, far from the cause. A parse failure is 400
// (the caller sent something wrong), never 500.
func (s *Server) handleLinkHistAdd(w http.ResponseWriter, r *http.Request) {
	var in linkHistRow
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLinkHistBody)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if in.Link == "" {
		writeErr(w, http.StatusBadRequest, errors.New("link is required"))
		return
	}
	if _, err := model.ParseLink(in.Link); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	// Best-effort, like the CLI producers: RecordLink swallows a disabled
	// store and a write error, because a history that cannot be written must
	// never make the copy the user asked for look like it failed.
	s.service().RecordLink(readCtx(r), in.Link, in.Desc)
	writeJSON(w, linkHistPayload{Links: []linkHistRow{{Link: in.Link, Desc: in.Desc}}})
}
