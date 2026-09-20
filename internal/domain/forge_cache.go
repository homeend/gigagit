package domain

import (
	"context"
	"time"

	"github.com/homeend/gigagit/internal/forge"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// The pull-request cache. Opening a PR used to cost two forge round trips
// before git even started (the PR's head sha, the base repository) — every
// time, for the same answers. Now:
//
//   - the listing seeds the cache, so a PR the user can SEE opens with no
//     forge call at all, and FetchPRHead's head-sha check turns an unchanged
//     PR into a purely local open;
//   - PRRevalidate is the background half: it always asks the forge, and
//     reports whether the local head fell behind, so a frontend serves the
//     stale diff at once and updates it when (rarely) it moved;
//   - an entry nobody used for prCacheIdle is evicted. Eviction is lazy — it
//     runs on the next cache access, there is no timer goroutine.
//
// The cache is per Service (per session, per repository) and in memory; the
// fetched refs themselves are the durable record and are never evicted.

// prCacheIdle is how long an unused pull request stays cached.
const prCacheIdle = 5 * time.Minute

type forgePREntry struct {
	pr   model.PullRequest
	full bool      // read on its own (carries the body), not just listed
	used time.Time // last read or write; eviction counts from here
}

type forgeBaseRepo struct{ slug, url string }

func (s *Service) forgeClock() time.Time {
	if s.forgeNow != nil {
		return s.forgeNow()
	}
	return time.Now()
}

// putPRLocked stores pr. A listed row never overwrites a full read's body.
// Callers hold forgeMu.
func (s *Service) putPRLocked(pr model.PullRequest, full bool) {
	if s.forgePRCache == nil {
		s.forgePRCache = map[int]forgePREntry{}
	}
	if old, ok := s.forgePRCache[pr.Number]; ok && old.full && !full {
		pr.Body, full = old.pr.Body, true
	}
	s.forgePRCache[pr.Number] = forgePREntry{pr: pr, full: full, used: s.forgeClock()}
}

// takePRLocked returns the cached PR n, evicting every idle entry first and
// marking n used. Callers hold forgeMu.
func (s *Service) takePRLocked(n int) (forgePREntry, bool) {
	now := s.forgeClock()
	for k, e := range s.forgePRCache {
		if now.Sub(e.used) > prCacheIdle {
			delete(s.forgePRCache, k)
		}
	}
	e, ok := s.forgePRCache[n]
	if ok {
		e.used = now
		s.forgePRCache[n] = e
	}
	return e, ok
}

// PRDetailsCached is what a details view can show BEFORE the forge answers:
// PR n with its body and its comments, as the last reads left them. ok is
// false until both were read once (a listed row has no body; half a view is
// not a cached view). It never calls the forge, and counts as a use — a PR
// whose details are being looked at is not idle.
func (s *Service) PRDetailsCached(n int) (model.PullRequest, PRComments, bool) {
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	e, ok := s.takePRLocked(n)
	c, had := s.forgeComments[n]
	if !ok || !e.full || !had {
		return model.PullRequest{}, PRComments{}, false
	}
	return e.pr, c.c, true
}

// cachedPR is PR n from the cache, else from the forge (and then cached).
func (s *Service) cachedPR(ctx context.Context, p forge.Provider, n int) (model.PullRequest, error) {
	s.forgeMu.Lock()
	e, ok := s.takePRLocked(n)
	s.forgeMu.Unlock()
	if ok {
		return e.pr, nil
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return model.PullRequest{}, err
	}
	s.forgeMu.Lock()
	s.putPRLocked(pr, true)
	s.forgeMu.Unlock()
	return pr, nil
}

// baseRepo is the provider's base repository, asked once per session.
func (s *Service) baseRepo(ctx context.Context, p forge.Provider) (slug, url string, err error) {
	s.forgeMu.Lock()
	b := s.forgeBase
	s.forgeMu.Unlock()
	if b != nil {
		return b.slug, b.url, nil
	}
	slug, url, err = p.BaseRepo(ctx)
	if err != nil {
		return "", "", err
	}
	s.forgeMu.Lock()
	s.forgeBase = &forgeBaseRepo{slug: slug, url: url}
	s.forgeMu.Unlock()
	return slug, url, nil
}

// PRRevalidation is what the forge says about a PR a frontend is already
// showing from the cache.
type PRRevalidation struct {
	PR model.PullRequest
	// Moved: the local refs/gg/pr/<n> is missing or is not the forge's head —
	// a fetch would change what the diff shows.
	Moved bool
}

// PRRevalidate asks the forge for PR n (never the cache), refreshes the cache
// and reports whether the local head is behind. It is the one forge call a
// cached open still makes — after the user already has the diff on screen.
func (s *Service) PRRevalidate(ctx context.Context, n int) (PRRevalidation, error) {
	p, err := s.provider(ctx)
	if err != nil {
		return PRRevalidation{}, err
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return PRRevalidation{}, err
	}
	s.forgeMu.Lock()
	s.putPRLocked(pr, true)
	if !pr.IsOpen() {
		if s.forgeTerminal == nil {
			s.forgeTerminal = map[int]model.PullRequest{}
		}
		s.forgeTerminal[n] = pr
	}
	s.forgeMu.Unlock()
	local, rerr := s.repo.ResolveCommit(ctx, git.PRRef(n))
	return PRRevalidation{PR: pr, Moved: rerr != nil || (pr.HeadSHA != "" && local != pr.HeadSHA)}, nil
}
