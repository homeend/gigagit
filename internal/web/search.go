package web

import (
	"errors"
	"net/url"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
)

// Finding things in a repo the browser cannot hold.
//
// The page's `/` filter narrows the commits ALREADY LOADED. On the repos gg is
// built for — 600k commits, 20GB of head — that is a rounding error: what you
// are looking for is almost never in the first pages. This file adds the
// server-side half of the answer, and commits.go wires it to the feed: a real
// feed filter (path / author / message / since / until) that git itself
// applies during the walk, so the narrowed list is drawn from the WHOLE
// history at the cost of one page. (F's file list is worktreefiles.go.)
//
// Everything is O(page): no endpoint here walks history to answer.

// --- the commit-feed filter -------------------------------------------------

// feedFilter is the content narrowing of the commit feed — the fields
// LogScope already carries and the TUI's `\` popup already offers. Branch
// selection (solo) is NOT part of it: that selects refs, this filters history,
// and only the latter makes the lane graph meaningless.
type feedFilter struct {
	Paths  []string
	Author string
	Grep   string
	Since  string
	Until  string
}

func (f feedFilter) active() bool {
	return len(f.Paths) > 0 || f.Author != "" || f.Grep != "" || f.Since != "" || f.Until != ""
}

// parseFeedFilter reads the filter off /api/commits' query string.
//
// The filter travels as query parameters on EVERY commits request instead of
// being stored on the server. One feed serves every tab (see solo.go), so
// server-side filter state would show a second tab a narrowed list with no
// filter bar to clear it — the exact trap solo.go's chip exists to avoid.
// Sent per request, each tab's next poll re-applies its own scope, and a
// filter can never outlive the page that set it.
//
// A key this build does not know is ignored rather than rejected: the browser
// and the server are shipped together but not necessarily loaded together.
//
// Values are user text and reach git as SEPARATE argv entries (gitcmd builds
// `--author=<value>`; paths go after `--`), so nothing here needs escaping —
// but control characters are refused: NUL is the separator domain's scopeKey
// joins filter fields with, and two different filters must never collide onto
// one cache key.
func parseFeedFilter(q url.Values) (feedFilter, error) {
	f := feedFilter{
		Author: strings.TrimSpace(q.Get("author")),
		Grep:   strings.TrimSpace(q.Get("grep")),
		Since:  strings.TrimSpace(q.Get("since")),
		Until:  strings.TrimSpace(q.Get("until")),
	}
	for _, p := range q["path"] {
		if p = strings.TrimSpace(p); p != "" {
			f.Paths = append(f.Paths, p)
		}
	}
	for _, v := range append([]string{f.Author, f.Grep, f.Since, f.Until}, f.Paths...) {
		if strings.ContainsFunc(v, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
			return feedFilter{}, errors.New("a filter value cannot contain control characters")
		}
	}
	return f, nil
}

// scopeFor composes the stored solo selection and this request's filter into
// the scope the feed walks.
func scopeFor(solo string, f feedFilter) domain.LogScope {
	sc := soloScope(solo)
	sc.Paths = f.Paths
	sc.Author = f.Author
	sc.Grep = f.Grep
	sc.Since = f.Since
	sc.Until = f.Until
	return sc
}

// scopeSig is a stable signature of a scope, used to notice that the scope a
// request asks for differs from the one the live feed was walked under. It
// mirrors domain's own scopeKey (which is unexported); \x01 separates fields
// and \x00 list members, neither of which can appear in a value (parseFeedFilter
// refuses control characters, and git refuses them in a refname).
func scopeSig(sc domain.LogScope) string {
	return strings.Join(sc.Branches, "\x00") + "\x01" +
		strings.Join(sc.Paths, "\x00") + "\x01" +
		sc.Author + "\x01" + sc.Grep + "\x01" + sc.Since + "\x01" + sc.Until
}

// --- per-server bookkeeping -------------------------------------------------

// searchState is what this feature needs to remember per Server and the Server
// struct does not carry: the scope its live feed was last walked under. It is
// kept beside the Server rather than inside it so the feature lives in its own
// files.
//
// It is not user state — losing it costs a re-walk, never a wrong answer — so
// nothing has to survive anything. Entries are one per Server (a process
// serves one repository) and a few words each.
type searchState struct {
	appliedScope string // scopeSig of the scope the live feed walks
}

var (
	searchMu     sync.Mutex
	searchStates = map[*Server]*searchState{}
)

func (s *Server) searchState() *searchState {
	searchMu.Lock()
	defer searchMu.Unlock()
	st := searchStates[s]
	if st == nil {
		st = &searchState{}
		searchStates[s] = st
	}
	return st
}

// scopeApplied records the scope the feed now walks. Called wherever the feed
// is (re)built or re-scoped — a fresh feed with a stale signature would leave
// a filtered request looking already-applied and answer unfiltered rows.
func (s *Server) scopeApplied(sc domain.LogScope) {
	st := s.searchState()
	searchMu.Lock()
	st.appliedScope = scopeSig(sc)
	searchMu.Unlock()
}

// scopeNeedsApply reports whether sc differs from what the live feed walks.
func (s *Server) scopeNeedsApply(sc domain.LogScope) bool {
	st := s.searchState()
	searchMu.Lock()
	defer searchMu.Unlock()
	return st.appliedScope != scopeSig(sc)
}
