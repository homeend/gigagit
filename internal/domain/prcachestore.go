package domain

// The on-disk half of the pull-request cache (spec 2026-10-07 §2). The
// in-memory maps in forge_cache.go stay the hot layer; this file resolves
// the per-repo prcache.Store and moves entries between the two. Every write
// is best effort: a cache that cannot be written never fails the read it
// follows. Lock rule: none of these helpers may be called while holding
// forgeMu — each takes it itself.

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/prcache"
)

// Defaults when no frontend set a policy (CLI one-shots, tests).
const (
	defaultPRMaxAge   = 8 * time.Hour
	defaultPRPrefetch = 5
)

// SetPRCachePolicy applies [forge] cache_hours / prefetch.
func (s *Service) SetPRCachePolicy(maxAge time.Duration, prefetch int) {
	s.forgeMu.Lock()
	s.prMaxAge, s.prPrefetch, s.prPolicySet = maxAge, prefetch, true
	s.forgeMu.Unlock()
}

// SetPRCacheStore injects the store (tests).
func (s *Service) SetPRCacheStore(st *prcache.Store) {
	s.forgeMu.Lock()
	s.prStore, s.prStoreReady = st, true
	s.forgeMu.Unlock()
}

// prPolicyLocked is the effective policy. Callers hold forgeMu.
func (s *Service) prPolicyLocked() (time.Duration, int) {
	if !s.prPolicySet {
		return defaultPRMaxAge, defaultPRPrefetch
	}
	return s.prMaxAge, s.prPrefetch
}

// prCacheStore resolves (once) the per-repo store keyed by git common dir;
// nil when no state dir resolves (the cache is then memory-only).
func (s *Service) prCacheStore(ctx context.Context) *prcache.Store {
	s.forgeMu.Lock()
	if s.prStoreReady {
		st := s.prStore
		s.forgeMu.Unlock()
		return st
	}
	s.forgeMu.Unlock()
	var st *prcache.Store
	if base := stateBaseDir("prcache"); base != "" {
		if cd, err := s.GitCommonDir(ctx); err == nil {
			st = prcache.New(filepath.Join(base, repoKey(strings.TrimSpace(cd))), prcache.DefaultMax)
		}
	}
	s.forgeMu.Lock()
	if !s.prStoreReady {
		s.prStore, s.prStoreReady = st, true
	}
	st = s.prStore
	s.forgeMu.Unlock()
	return st
}

// fresh reports whether a read at t is still fresh under the policy.
func (s *Service) fresh(t time.Time) bool {
	s.forgeMu.Lock()
	maxAge, _ := s.prPolicyLocked()
	s.forgeMu.Unlock()
	return prcache.Fresh(t, s.forgeClock(), maxAge)
}

// diskEntry is PR n's disk entry when its forge read is still fresh.
func (s *Service) diskEntry(ctx context.Context, n int) (prcache.Entry, bool) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return prcache.Entry{}, false
	}
	e, ok := st.Load(n)
	if !ok || !s.fresh(e.ReadAt) {
		return prcache.Entry{}, false
	}
	return e, true
}

// persistPR merges what the caller learned into PR n's disk entry.
func (s *Service) persistPR(ctx context.Context, n int, edit func(e *prcache.Entry)) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return
	}
	e, ok := st.Load(n)
	if !ok {
		e = prcache.Entry{Number: n}
	}
	edit(&e)
	_ = st.Save(e)
}

// cachedListing is the fresh cached open-PR listing.
func (s *Service) cachedListing(ctx context.Context) (prcache.List, bool) {
	st := s.prCacheStore(ctx)
	if st == nil {
		return prcache.List{}, false
	}
	l, ok := st.LoadList()
	if !ok || !s.fresh(l.ReadAt) {
		return prcache.List{}, false
	}
	return l, true
}

// saveListing records a listing the forge just answered.
func (s *Service) saveListing(ctx context.Context, provider string, prs []model.PullRequest) {
	if st := s.prCacheStore(ctx); st != nil {
		_ = st.SaveList(prcache.List{ReadAt: s.forgeClock(), Provider: provider, PRs: prs})
	}
}

// PullRequestsCached is the fresh cached listing; it never calls the forge.
func (s *Service) PullRequestsCached() ([]model.PullRequest, bool) {
	l, ok := s.cachedListing(context.Background())
	if !ok {
		return nil, false
	}
	return l.PRs, true
}

// PRCacheReadAt is when PR n was last read from the forge.
func (s *Service) PRCacheReadAt(n int) (time.Time, bool) {
	s.forgeMu.Lock()
	e, ok := s.forgePRCache[n]
	s.forgeMu.Unlock()
	if ok {
		return e.readAt, true
	}
	if d, ok := s.diskEntry(context.Background(), n); ok {
		return d.ReadAt, true
	}
	return time.Time{}, false
}
