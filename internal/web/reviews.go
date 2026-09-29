package web

import (
	"time"

	"github.com/homeend/gigagit/internal/domain"
)

// Stored AI reviews in the page: the TUI's review rows and review view
// (internal/tui/review_view.go). A review is one commit-level note; the page
// lists them from the note counts (a branch's sub-rows) and from an opened
// commit (its "Reviews" rows), and reads one whole for the review view.

// reviewHeadWire is one review as a list row needs it.
type reviewHeadWire struct {
	ID      string `json:"id"`
	Commit  string `json:"commit"`
	Branch  string `json:"branch,omitempty"`
	Agent   string `json:"agent"`
	Summary string `json:"summary"`
	Created string `json:"created"` // RFC3339 UTC, "" when unknown
}

func wireTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// reviewHeads is hs on the wire; never nil, so the field is always an array.
func reviewHeads(hs []domain.ReviewHead) []reviewHeadWire {
	out := make([]reviewHeadWire, 0, len(hs))
	for _, h := range hs {
		out = append(out, reviewHeadWire{ID: h.ID, Commit: h.Commit, Branch: h.Branch, Agent: h.Agent, Summary: h.Summary, Created: wireTime(h.Created)})
	}
	return out
}

// reviewHeadsOf is reviewHeads for full reviews.
func reviewHeadsOf(rs []domain.Review) []reviewHeadWire {
	out := make([]reviewHeadWire, 0, len(rs))
	for _, r := range rs {
		out = append(out, reviewHeadWire{ID: r.ID, Commit: r.Commit, Branch: r.Branch, Agent: r.Agent, Summary: r.Summary, Created: wireTime(r.Created)})
	}
	return out
}
