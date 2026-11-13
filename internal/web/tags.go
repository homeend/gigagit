package web

import (
	"net/http"
	"slices"
	"strconv"
	"strings"

	"github.com/homeend/gigagit/internal/model"
)

// defaultTagRows is the sidebar's first page: big repos carry hundreds of tags
// (linux: 937). The section's "… show more" row asks for a bigger ?limit (the
// reflog pager); maxTagRows bounds what a client can ask for at all.
const (
	defaultTagRows = 100
	maxTagRows     = 20000
)

type tagRow struct {
	Name      string `json:"name"`
	Target    string `json:"target"`
	Annotated bool   `json:"annotated"`
	Subject   string `json:"subject,omitempty"`
	// Remote is the tag's presence on the default remote per the last
	// listing (remotetags.go) — absent from the wire while no listing exists.
	Remote *bool `json:"remote,omitempty"`
}

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	svc := s.service()
	ts, err := svc.Tags(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	// Sort before the cap (see handleRemotes). There is no per-tag date — the
	// TUI's tagList.Date returns 0 for the same reason — so a date mode leaves
	// git's own newest-first order alone.
	ts = sortedRows(ts, allowedSortMode(r.URL.Query().Get("sort")),
		func(tg model.Tag) string { return tg.Name },
		func(model.Tag) int64 { return 0 })
	// ?q= is the section's search box: a case-insensitive substring of the
	// tag's name, before the cap, so a tag past the first page is found.
	if q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q"))); q != "" {
		ts = slices.DeleteFunc(slices.Clone(ts), func(tg model.Tag) bool {
			return !strings.Contains(strings.ToLower(tg.Name), q)
		})
	}
	limit := defaultTagRows
	if v, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && v > 0 {
		limit = min(v, maxTagRows)
	}
	truncated := false
	if len(ts) > limit {
		ts = ts[:limit]
		truncated = true
	}
	// The remote verdict rides on the last listing; with none yet, this
	// request starts the one background listing and answers without one —
	// the hub's "tags" event re-fetches once the remote has spoken.
	remote, known := s.remoteTagNames(svc)
	if !known {
		s.kickRemoteTags(svc, false)
	}
	rows := make([]tagRow, 0, len(ts))
	for _, tg := range ts {
		row := tagRow{Name: tg.Name, Target: tg.Target, Annotated: tg.Annotated, Subject: tg.Subject}
		if known {
			on := remote[tg.Name]
			row.Remote = &on
		}
		rows = append(rows, row)
	}
	writeJSON(w, map[string]any{"tags": rows, "truncated": truncated, "remote_known": known})
}
