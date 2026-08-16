package web

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/fuzzy"
)

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("GET /api/files", s.handleFiles)
	})
}

// Finding things in a repo the browser cannot hold.
//
// The page's `/` filter narrows the commits ALREADY LOADED. On the repos gg is
// built for — 600k commits, 20GB of head — that is a rounding error: what you
// are looking for is almost never in the first pages. This file adds the two
// server-side halves of the answer, and commits.go wires them to the feed:
//
//   - a real feed filter (path / author / message / since / until) that git
//     itself applies during the walk, so the narrowed list is drawn from the
//     WHOLE history at the cost of one page;
//   - the tracked-path list behind the fuzzy file finder, ranked here rather
//     than in the browser.
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
// struct does not carry: the scope its live feed was last walked under, and the
// tracked-path list behind the finder. It is kept beside the Server rather than
// inside it so the feature lives in its own files.
//
// Neither field is user state — losing one costs a re-walk or a re-read, never
// a wrong answer — so nothing has to survive anything. Entries are one per
// Server (a process serves one repository) and a few words each.
type searchState struct {
	appliedScope string   // scopeSig of the scope the live feed walks
	filesHead    string   // the HEAD the path list below was read at
	files        []string // tracked paths, as of filesHead
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

// --- the fuzzy file finder --------------------------------------------------

// fileFinderLimit caps how many ranked paths one response carries. The finder
// is a picker, not a file tree: past a screenful the ranking is what matters,
// not the tail.
const fileFinderLimit = 50

// handleFiles answers the fuzzy file finder: the repo's tracked paths ranked
// against q.
//
// The RANKING happens here, not in the browser. A monorepo has hundreds of
// thousands of tracked paths, and shipping them all to the page so it can
// filter them is exactly the O(everything) mistake this task exists to undo.
// Each keystroke costs one ranked page.
//
// The path list itself is read once per HEAD — `git ls-files` is the expensive
// part — and re-read when HEAD moves. A file added since the last commit is
// therefore not offered until it is committed, which is what "tracked at this
// commit" means; the finder opens a file's HISTORY, and a file with no commits
// has none.
func (s *Server) handleFiles(w http.ResponseWriter, r *http.Request) {
	limit := fileFinderLimit
	if n, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && n > 0 {
		limit = min(n, 200)
	}
	paths, err := s.trackedFiles(readCtx(r))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	matches := fuzzy.Rank(r.URL.Query().Get("q"), paths, limit)
	files := make([]string, len(matches))
	for i, m := range matches {
		files[i] = m.S
	}
	writeJSON(w, map[string]any{
		"files":   files,
		"total":   len(paths),
		"limited": len(files) >= limit,
	})
}

// trackedFiles returns the repo's tracked paths, cached per HEAD.
func (s *Server) trackedFiles(ctx context.Context) ([]string, error) {
	svc := s.service()
	head, _, err := svc.ResolveRev(ctx, "HEAD")
	if err != nil {
		return nil, err
	}
	st := s.searchState()
	searchMu.Lock()
	if st.filesHead == head && st.files != nil {
		cached := st.files
		searchMu.Unlock()
		return cached, nil
	}
	searchMu.Unlock()

	paths, err := svc.LsFiles(ctx)
	if err != nil {
		return nil, err
	}
	searchMu.Lock()
	st.filesHead, st.files = head, paths
	searchMu.Unlock()
	return paths, nil
}
