package tui

import (
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/timespan"
)

// blameRecent is the blame view's "highlight lines by commit age" state (d
// opens the filter dialog, D turns it off). It lives on the blameView, so a
// freshly opened blame always starts OFF (ruling in the design spec, revision
// 2); only the last submitted TEXT survives on the Model (blameRecentLast)
// to prefill the next dialog. Not persisted across runs.
type blameRecent struct {
	on bool            // highlight is showing
	f  timespan.Filter // the parsed age window the highlight compares against
}

// blameRecentDefaultText is what the dialog offers before the user has ever
// typed a filter.
const blameRecentDefaultText = "7d"

// blameRecentSeed is the dialog's prefill: the last text the user submitted,
// or the default when there is none yet.
func blameRecentSeed(last string) string {
	if last == "" {
		return blameRecentDefaultText
	}
	return last
}

// lineMatches reports whether a blame line falls inside the age filter: its
// age (now minus the commit's author time) satisfies f. An uncommitted line
// (hash "") has age 0 — the newest change of all — so it matches a "-" half
// and never a "+" half. now is the wall clock at render time, not the blamed
// revision's date, so blaming an old commit may highlight nothing under a
// "-" filter; that is documented, not a bug. A zero Time (the epoch) is
// simply very old.
func lineMatches(ln model.BlameLine, now time.Time, f timespan.Filter) bool {
	var age time.Duration
	if ln.Hash != "" {
		age = now.Sub(time.Unix(ln.Time, 0))
	}
	return f.Matches(age)
}
