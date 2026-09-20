package domain

import (
	"context"
	"errors"
	"regexp"
	"slices"
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/model"
)

// PRQuery is the forge-neutral pull-request search (frontends never import
// forge). Text is opaque: it goes to the forge's own search unparsed.
type PRQuery = forge.PRQuery

// The states a search can ask for — distinct from model.PRState*, which name
// the state a pull request IS in.
const (
	PRStateFilterAll    = forge.StateAll
	PRStateFilterOpen   = forge.StateOpen
	PRStateFilterClosed = forge.StateClosed
	PRStateFilterMerged = forge.StateMerged

	PRSearchDefaultLimit = forge.DefaultSearchLimit
	PRSearchMaxLimit     = forge.MaxSearchLimit
)

// PRStateFilters lists the search states in the order a frontend cycles them.
func PRStateFilters() []string { return forge.PRStates() }

// NormalizePRQuery fills a query's defaults and rejects a bad state or limit.
func NormalizePRQuery(q PRQuery) (PRQuery, error) { return q.Normalize() }

// PRSearchResult is one answered search. Query is the normalized query.
type PRSearchResult struct {
	Query PRQuery
	PRs   []model.PullRequest
	More  bool // the forge had rows beyond Query.Limit
	At    time.Time
}

func (r PRSearchResult) clone() PRSearchResult {
	r.PRs = slices.Clone(r.PRs)
	return r
}

// prNumberText is a search text that names one pull request: 123 or #123.
var prNumberText = regexp.MustCompile(`^#?([0-9]+)$`)

// PRSearch asks the forge for the pull requests matching q — of any state,
// which is how a closed or merged PR gg never fetched is found. The answer is
// a TRANSIENT set: it never joins PullRequests' list (a found PR gets there
// the usual way, by being opened — its fetched ref makes it known). A text
// that is only a number looks that pull request up directly, whatever the
// state asked for. Every success becomes the session's PRSearchLast.
func (s *Service) PRSearch(ctx context.Context, q PRQuery) (PRSearchResult, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return PRSearchResult{}, err
	}
	if q, err = q.Normalize(); err != nil {
		return PRSearchResult{}, err
	}
	key := "forge-search:" + q.State + "\x00" + strconv.Itoa(q.Limit) + "\x00" + q.Text
	v, err := s.flight.Do(key, func() (any, error) { return s.prSearch(ctx, p, q) })
	if err != nil {
		return PRSearchResult{}, err
	}
	return v.(PRSearchResult).clone(), nil
}

func (s *Service) prSearch(ctx context.Context, p forge.Provider, q PRQuery) (PRSearchResult, error) {
	res := PRSearchResult{Query: q}
	if m := prNumberText.FindStringSubmatch(q.Text); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			// PullRequest caches the full read itself.
			pr, err := s.PullRequest(ctx, n)
			switch {
			case errors.Is(err, forge.ErrNotFound):
			case err != nil:
				return PRSearchResult{}, err
			default:
				res.PRs = []model.PullRequest{pr}
			}
			return s.rememberSearch(res), nil
		}
	}
	prs, more, err := p.Search(ctx, q)
	if err != nil {
		return PRSearchResult{}, err
	}
	slices.SortFunc(prs, prByUpdated)
	res.PRs, res.More = prs, more
	s.forgeMu.Lock()
	for _, pr := range prs {
		// The cache only: forgeSeen is what grows the normal list.
		s.putPRLocked(pr, false)
	}
	s.forgeMu.Unlock()
	return s.rememberSearch(res), nil
}

func (s *Service) rememberSearch(res PRSearchResult) PRSearchResult {
	res.At = s.forgeClock()
	s.forgeMu.Lock()
	keep := res.clone()
	s.forgeSearchLast = &keep
	s.forgeMu.Unlock()
	return res
}

// PRSearchLast is the session's last answered search; it never calls the
// forge. Nothing is kept across sessions.
func (s *Service) PRSearchLast() (PRSearchResult, bool) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	if s.forgeSearchLast == nil {
		return PRSearchResult{}, false
	}
	return s.forgeSearchLast.clone(), true
}
