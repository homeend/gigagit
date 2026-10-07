package domain

import (
	"context"
	"time"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/prcache"

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
//   - entries survive a restart (prcachestore.go: the PR, its comments, the
//     listing and the base repository on disk) and expire by READ time —
//     [forge] cache_hours, default 8 — not by idleness: an expired entry is
//     read from the forge again, never drawn.
//
// The memory maps are per Service; the fetched refs are git's and are never
// evicted.

type forgePREntry struct {
	pr     model.PullRequest
	full   bool      // read on its own (carries the body), not just listed
	readAt time.Time // when the forge answered; expiry counts from here
}

type forgeBaseRepo struct{ slug, url string }

func (s *Service) forgeClock() time.Time {
	if s.forgeNow != nil {
		return s.forgeNow()
	}
	return clock.Now()
}

// putPRLocked stores pr as read now. A listed row never overwrites what a
// full read learned (the body, the forge id, the viewer fields). Callers
// hold forgeMu.
func (s *Service) putPRLocked(pr model.PullRequest, full bool) {
	s.putPRAtLocked(pr, full, s.forgeClock())
}

func (s *Service) putPRAtLocked(pr model.PullRequest, full bool, readAt time.Time) {
	if s.forgePRCache == nil {
		s.forgePRCache = map[int]forgePREntry{}
	}
	if old, ok := s.forgePRCache[pr.Number]; ok && old.full && !full {
		pr.Body, full = old.pr.Body, true
		if pr.NodeID == "" {
			pr.NodeID = old.pr.NodeID
		}
		pr.ViewerDidAuthor, pr.ViewerPendingReview = old.pr.ViewerDidAuthor, old.pr.ViewerPendingReview
	}
	s.forgePRCache[pr.Number] = forgePREntry{pr: pr, full: full, readAt: readAt}
}

// takePRLocked returns PR n when its read is still fresh; a stale entry is
// dropped. Callers hold forgeMu.
func (s *Service) takePRLocked(n int) (forgePREntry, bool) {
	e, ok := s.forgePRCache[n]
	if !ok {
		return forgePREntry{}, false
	}
	maxAge, _ := s.prPolicyLocked()
	if !prcache.Fresh(e.readAt, s.forgeClock(), maxAge) {
		delete(s.forgePRCache, n)
		return forgePREntry{}, false
	}
	return e, true
}

// PRDetailsCached is what a details view can show BEFORE the forge answers:
// PR n with its body and its comments, as the last reads left them (this
// session or an earlier one, while fresh). ok is false until both were read
// once (a listed row has no body; half a view is not a cached view). It never
// calls the forge.
func (s *Service) PRDetailsCached(n int) (model.PullRequest, PRComments, bool) {
	s.loadDiskPR(context.Background(), n)
	c, cok := s.PRCommentsCached(n)
	s.forgeMu.Lock()
	defer s.forgeMu.Unlock()
	e, ok := s.takePRLocked(n)
	if !ok || !e.full || !cok {
		return model.PullRequest{}, PRComments{}, false
	}
	return e.pr, c, true
}

// loadDiskPR seeds the memory cache from PR n's fresh disk entry when memory
// has nothing fresh. It reports whether memory now holds PR n.
func (s *Service) loadDiskPR(ctx context.Context, n int) bool {
	s.forgeMu.Lock()
	_, ok := s.takePRLocked(n)
	s.forgeMu.Unlock()
	if ok {
		return true
	}
	d, ok := s.diskEntry(ctx, n)
	if !ok {
		return false
	}
	s.forgeMu.Lock()
	s.putPRAtLocked(d.PR, d.Full, d.ReadAt)
	s.forgeMu.Unlock()
	return true
}

// cachedPR is PR n from the cache — memory, then its disk entry, then the
// cached listing — else from the forge (and then cached). Every use stamps
// the entry's last-open time (the disk cache keeps the most recently opened
// PRs).
func (s *Service) cachedPR(ctx context.Context, p forge.Provider, n int) (model.PullRequest, error) {
	if !s.loadDiskPR(ctx, n) {
		if l, ok := s.cachedListing(ctx); ok {
			for _, row := range l.PRs {
				if row.Number == n {
					s.forgeMu.Lock()
					s.putPRAtLocked(row, false, l.ReadAt)
					s.forgeMu.Unlock()
					break
				}
			}
		}
	}
	s.forgeMu.Lock()
	e, ok := s.takePRLocked(n)
	s.forgeMu.Unlock()
	if ok {
		s.persistEntry(ctx, e)
		return e.pr, nil
	}
	pr, err := p.PR(ctx, n)
	if err != nil {
		return model.PullRequest{}, err
	}
	s.rememberPR(ctx, pr, true)
	return pr, nil
}

// rememberPR stores a forge answer in memory and on disk.
func (s *Service) rememberPR(ctx context.Context, pr model.PullRequest, full bool) {
	s.forgeMu.Lock()
	s.putPRLocked(pr, full)
	e := s.forgePRCache[pr.Number]
	s.forgeMu.Unlock()
	s.persistEntry(ctx, e)
}

// persistEntry writes a memory entry to disk as just opened: the PR and its
// read time (never older than what the disk already holds), and the open
// stamp the disk cache's bound keys on.
func (s *Service) persistEntry(ctx context.Context, e forgePREntry) {
	now := s.forgeClock()
	s.persistPR(ctx, e.pr.Number, func(x *prcache.Entry) {
		if !e.readAt.Before(x.ReadAt) {
			x.PR, x.Full, x.ReadAt = e.pr, e.full, e.readAt
		}
		x.OpenedAt = now
	})
}

// baseRepo is the provider's base repository: memory, then the disk cache
// while fresh, else asked (and cached on disk).
func (s *Service) baseRepo(ctx context.Context, p forge.Provider) (slug, url string, err error) {
	s.forgeMu.Lock()
	b := s.forgeBase
	s.forgeMu.Unlock()
	if b != nil {
		return b.slug, b.url, nil
	}
	st := s.prCacheStore(ctx)
	if st != nil {
		if r, ok := st.LoadRepo(); ok && r.Slug != "" && s.fresh(r.ReadAt) {
			s.forgeMu.Lock()
			s.forgeBase = &forgeBaseRepo{slug: r.Slug, url: r.URL}
			s.forgeMu.Unlock()
			return r.Slug, r.URL, nil
		}
	}
	slug, url, err = p.BaseRepo(ctx)
	if err != nil {
		return "", "", err
	}
	s.forgeMu.Lock()
	s.forgeBase = &forgeBaseRepo{slug: slug, url: url}
	s.forgeMu.Unlock()
	if st != nil {
		_ = st.SaveRepo(prcache.Repo{Slug: slug, URL: url, ReadAt: s.forgeClock()})
	}
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
	s.rememberPR(ctx, pr, true)
	s.forgeMu.Lock()
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
