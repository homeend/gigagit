package tui

import (
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/timespan"
)

// blameRecent is the blame view's "lines changed within the last…" highlight
// (d opens the span dialog, D turns it off). It lives on the Model — not on
// the blameView — so the span and the on/off state survive closing and
// reopening blame for the session. Not persisted across runs (ruling in the
// design spec: session-scoped).
type blameRecent struct {
	on   bool          // highlight is showing
	span time.Duration // the parsed span the highlight compares against
	last string        // the text last submitted; prefills the next dialog
}

// blameRecentDefaultSpan is what the dialog offers before the user has ever
// typed a span.
const blameRecentDefaultSpan = "7d"

// blameRecentSeed is the dialog's prefill: the last span the user submitted,
// or the default when there is none yet.
func blameRecentSeed(r blameRecent) string {
	if r.last == "" {
		return blameRecentDefaultSpan
	}
	return r.last
}

// lineRecent reports whether a blame line falls inside the recent span: its
// commit's author time is within span of now (boundary inclusive), or the
// line is uncommitted (hash "" — the newest change of all). now is the wall
// clock at render time, not the blamed revision's date, so blaming an old
// commit may highlight nothing; that is documented, not a bug. A zero Time
// (the epoch) is simply very old.
func lineRecent(ln model.BlameLine, now time.Time, span time.Duration) bool {
	if ln.Hash == "" {
		return true
	}
	return now.Sub(time.Unix(ln.Time, 0)) <= span
}

// blameRecentBadge is the header badge shown while the highlight is on:
// "≤" + the canonical span ("≤7d", "≤1d3h5m").
func blameRecentBadge(span time.Duration) string {
	return "≤" + timespan.Format(span)
}
