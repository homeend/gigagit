package forge

import (
	"fmt"
	"slices"
	"strings"
)

// The pull-request states a search can ask for. They are forge-neutral words:
// every provider maps them onto its own vocabulary.
const (
	StateAll    = "all"
	StateOpen   = "open"
	StateClosed = "closed"
	StateMerged = "merged"
)

const (
	// DefaultSearchLimit is how many rows a search asks for when it names none.
	DefaultSearchLimit = 50
	// MaxSearchLimit caps one search: a search is a single forge call.
	MaxSearchLimit = 200
)

// PRStates lists the search states in the order a frontend cycles them: the
// default first, the one the normal list already covers last.
func PRStates() []string { return []string{StateAll, StateClosed, StateMerged, StateOpen} }

// PRQuery is a forge-neutral pull-request search. Text is opaque to gg: it is
// handed to the forge's own search unparsed.
type PRQuery struct {
	State string
	Text  string
	Limit int
}

// Normalize fills the defaults (state all, DefaultSearchLimit) and trims the
// text; an unknown state or a limit outside 1…MaxSearchLimit is an error.
// Every caller runs it first — a provider never sees a bad query.
func (q PRQuery) Normalize() (PRQuery, error) {
	q.Text = strings.TrimSpace(q.Text)
	if q.State == "" {
		q.State = StateAll
	}
	if !slices.Contains(PRStates(), q.State) {
		return PRQuery{}, fmt.Errorf("unknown pull request state %q (want %s)", q.State, strings.Join(PRStates(), ", "))
	}
	if q.Limit == 0 {
		q.Limit = DefaultSearchLimit
	}
	if q.Limit < 1 || q.Limit > MaxSearchLimit {
		return PRQuery{}, fmt.Errorf("limit %d is out of range (1…%d)", q.Limit, MaxSearchLimit)
	}
	return q, nil
}
