package domain

import (
	"context"
	"os"
	"sync"

	"github.com/homeend/gigagit/internal/model"
)

// Page sizes (commits). Initial paint stays cheap; later pages are larger
// since the user has signaled interest by scrolling.
const (
	commitInitialPage = 50
	commitPageSize    = 200
	commitNearEnd     = 10 // load more when the selection is within this of the end
)

// cachedScope is a feed accumulation remembered for a scope key, so toggling back
// to that scope (e.g. clearing a filter) restores instantly with no re-walk.
type cachedScope struct {
	commits   []model.Commit
	hashes    map[string]bool
	skip      int
	exhausted bool
}

// commitScopeCacheCap bounds the cache by ENTRY COUNT (remembered scopes), not
// bytes — one large base accumulation dominates memory.
const commitScopeCacheCap = 4

// CommitFeed is the single source of truth for the Commits panel: an
// incrementally loaded, newest-first view of HEAD history. Goroutine-safe;
// Snapshot returns a copy so a frontend can render while a page loads.
type CommitFeed struct {
	svc *Service

	mu          sync.Mutex
	scope       LogScope // refspec for the walk; empty = all local branches
	commits     []model.Commit
	hashes      map[string]bool // dedupe set, mirrors commits
	skip        int             // next --skip offset (advances by raw page length)
	exhausted   bool
	gen         int                    // bumped by LoadInitial; tags pages so stale ones drop
	inFlight    bool                   // at most one page request outstanding
	cancel      context.CancelFunc     // cancels the in-flight load's ctx on supersede
	pager       CommitPager            // page-fetch strategy; default plainPager
	initialPage int                    // configured first-paint size; <=0 → commitInitialPage
	pageSize    int                    // configured later-page size; <=0 → commitPageSize
	cache       map[string]cachedScope // scopeKey → remembered accumulation
	cacheOrder  []string               // LRU order, oldest first
}

// CommitFeed returns a fresh feed for this Service's repo.
func (s *Service) CommitFeed() *CommitFeed {
	// GG_COMMIT_PAGER picks the page strategy. Only "date-order" is special — it
	// opts into a global topological sort for guaranteed-perfect graph lanes (slow
	// on a large repo). Every other value (incl. unset and "plain") uses the
	// default plainPager: git's lazy newest-first order — instant on huge repos.
	mode := os.Getenv("GG_COMMIT_PAGER") // legacy opt-in; "" → plain
	return &CommitFeed{svc: s, hashes: map[string]bool{}, cache: map[string]cachedScope{}, pager: pagerForMode(s, mode)}
}

// PagerName reports the active page strategy ("plain" | "date-order").
func (f *CommitFeed) PagerName() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pager.Name()
}

// SetSortMode swaps the page-fetch strategy for the given commit-sort mode
// ("plain"|"date-order"). Apply before the next LoadInitial. The GG_COMMIT_PAGER
// env var, when set, overrides the argument (legacy/testing escape hatch), so a
// power user's env pin always wins over config.
func (f *CommitFeed) SetSortMode(mode string) {
	if env := os.Getenv("GG_COMMIT_PAGER"); env != "" {
		mode = env
	}
	f.mu.Lock()
	f.pager = pagerForMode(f.svc, mode)
	f.mu.Unlock()
}

// SetScope sets the refspec for subsequent loads. Callers then LoadInitial to
// re-walk; the gen bump drops any stale in-flight page.
func (f *CommitFeed) SetScope(scope LogScope) {
	f.mu.Lock()
	f.scope = scope
	f.mu.Unlock()
}

// SetPageSizes sets the first-paint and later-page commit counts (0 or negative
// keeps the built-in fallback). Apply before the next LoadInitial.
func (f *CommitFeed) SetPageSizes(initial, batch int) {
	f.mu.Lock()
	f.initialPage = initial
	f.pageSize = batch
	f.mu.Unlock()
}

// effInitial / effPage resolve the configured size or the constant fallback.
// Callers hold f.mu.
func (f *CommitFeed) effInitial() int {
	if f.initialPage > 0 {
		return f.initialPage
	}
	return commitInitialPage
}

func (f *CommitFeed) effPage() int {
	if f.pageSize > 0 {
		return f.pageSize
	}
	return commitPageSize
}

// FeedState is an immutable view handed to the frontend.
type FeedState struct {
	Commits   []model.Commit
	Exhausted bool
	Gen       int
}

// Gen returns the current generation (for a frontend's stale-page check).
func (f *CommitFeed) Gen() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gen
}

// snapshotLocked builds a FeedState copy; caller holds f.mu.
func (f *CommitFeed) snapshotLocked() FeedState {
	cp := make([]model.Commit, len(f.commits))
	copy(cp, f.commits)
	return FeedState{Commits: cp, Exhausted: f.exhausted, Gen: f.gen}
}

// Snapshot returns a copy of the current state for rendering.
func (f *CommitFeed) Snapshot() FeedState {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshotLocked()
}

// NeedsMore reports whether selection index sel is close enough to the end to
// warrant a page and the feed can serve one. Filter-suppression is the
// caller's concern.
func (f *CommitFeed) NeedsMore(sel int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.exhausted || f.inFlight {
		return false
	}
	return sel >= len(f.commits)-commitNearEnd
}

// CanLoadMore reports whether a LoadMore would do work (not exhausted, not
// already in flight) — independent of cursor position. Drives the ctrl+l key.
func (f *CommitFeed) CanLoadMore() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return !f.exhausted && !f.inFlight
}

// stashCurrentLocked saves the current scope's accumulation under its key.
// Caller holds f.mu. A no-op when nothing is loaded.
func (f *CommitFeed) stashCurrentLocked() {
	if len(f.commits) == 0 {
		return
	}
	key := scopeKey(f.scope)
	if _, ok := f.cache[key]; !ok {
		f.cacheOrder = append(f.cacheOrder, key)
	}
	hs := make(map[string]bool, len(f.hashes))
	for h := range f.hashes {
		hs[h] = true
	}
	cp := make([]model.Commit, len(f.commits))
	copy(cp, f.commits)
	f.cache[key] = cachedScope{commits: cp, hashes: hs, skip: f.skip, exhausted: f.exhausted}
	for len(f.cacheOrder) > commitScopeCacheCap {
		delete(f.cache, f.cacheOrder[0])
		f.cacheOrder = f.cacheOrder[1:]
	}
}

// clearCacheLocked drops every remembered scope (hard-refresh invalidation).
// Caller holds f.mu.
func (f *CommitFeed) clearCacheLocked() {
	f.cache = map[string]cachedScope{}
	f.cacheOrder = nil
}

// LoadInitial is the HARD REFRESH: it re-walks the current scope from page 0 and
// invalidates the entire scope cache, so post-operation reloads never restore a
// stale accumulation for some other scope. (Scope TOGGLES use ApplyScope.)
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	f.clearCacheLocked()
	f.mu.Unlock()
	return f.loadInitialWalk(ctx)
}

// Refresh is the RECONCILING refresh: it re-walks page 0 of the current scope —
// one git call, exactly what LoadInitial costs — and merges it into the existing
// accumulation instead of clearing it, so a periodic/background refresh no longer
// throws away every page the user paged in (a deep ctrl+f search, a long scroll).
// New commits prepend; a vanished tip (amend/reset) is trimmed. When the fresh
// page can't be reconciled (a rewrite, or more new commits than one page holds)
// it degrades to exactly LoadInitial's hard reset, so history is never wrong,
// only occasionally re-walked. Like LoadInitial it invalidates the scope cache.
//
// Frontends use this for automatic refreshes; LoadInitial stays the explicit
// "start clean" path (manual reload, sort/page-size change).
func (f *CommitFeed) Refresh(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	f.clearCacheLocked()
	if f.cancel != nil {
		f.cancel()
	}
	cctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	// The gen bump is load-bearing beyond staleness: a prepend shifts every --skip
	// offset, so an in-flight LoadMore page is misaligned and must drop.
	f.gen++
	gen0 := f.gen
	scope := f.scope
	initial := f.effInitial()
	loaded := f.commits // entries are never mutated in place, so the header is enough
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.pager.Page(cctx, initial, 0, gen0, scope)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if f.gen == gen0 {
		f.cancel = nil
	}
	if f.gen != gen0 { // superseded mid-walk; drop this page
		st := f.snapshotLocked()
		st.Gen = gen0
		return st, nil
	}
	if err != nil {
		return f.snapshotLocked(), err
	}
	if merged, skipDelta, ok := reconcilePage(loaded, page); ok {
		f.commits = merged
		f.hashes = make(map[string]bool, len(merged))
		for _, c := range merged {
			f.hashes[c.Hash] = true
		}
		// exhausted stays put: the kept tail still ends where it ended, and only
		// the head of history can grow.
		f.skip += skipDelta
		if f.skip < 0 {
			f.skip = 0 // undershooting only re-reads commits the dedupe drops
		}
		return f.snapshotLocked(), nil
	}
	f.applyPageZeroLocked(page, initial)
	return f.snapshotLocked(), nil
}

// applyPageZeroLocked replaces the accumulation with a freshly walked page 0.
// Caller holds f.mu. Shared by the initial walk and Refresh's fallback so the
// two produce identical state.
func (f *CommitFeed) applyPageZeroLocked(page []model.Commit, initial int) {
	f.commits = nil
	f.hashes = map[string]bool{}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip = len(page)
	f.exhausted = len(page) < initial
}

// loadInitialWalk resets the current scope's accumulation and walks page 0. It is
// the body shared by LoadInitial and ApplyScope's cache-miss path; it does NOT
// touch the cache.
func (f *CommitFeed) loadInitialWalk(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel()
	}
	cctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.gen++
	gen0 := f.gen
	scope := f.scope
	initial := f.effInitial()
	f.commits = nil
	f.hashes = map[string]bool{}
	f.skip = 0
	f.exhausted = false
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.pager.Page(cctx, initial, 0, gen0, scope)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if f.gen == gen0 {
		f.cancel = nil
	}
	if f.gen != gen0 {
		st := f.snapshotLocked()
		st.Gen = gen0
		return st, nil
	}
	if err != nil {
		return f.snapshotLocked(), err
	}
	f.applyPageZeroLocked(page, initial)
	return f.snapshotLocked(), nil
}

// ApplyScope switches the feed to scope. If that scope's accumulation is cached
// (a prior toggle with no hard refresh since), it is restored with NO git call;
// otherwise the feed re-walks page 0. The current scope's accumulation is stashed
// first so toggling back is instant. Scope toggles (filter / solo / show-all /
// upstreams) call this; LoadInitial remains the cache-clearing hard refresh.
func (f *CommitFeed) ApplyScope(ctx context.Context, scope LogScope) (FeedState, error) {
	f.mu.Lock()
	f.stashCurrentLocked()
	if cs, ok := f.cache[scopeKey(scope)]; ok {
		if f.cancel != nil {
			f.cancel() // a cached restore supersedes any in-flight walk
		}
		f.cancel = nil
		f.gen++
		f.scope = scope
		f.commits = append([]model.Commit(nil), cs.commits...)
		f.hashes = make(map[string]bool, len(cs.hashes))
		for h := range cs.hashes {
			f.hashes[h] = true
		}
		f.skip = cs.skip
		f.exhausted = cs.exhausted
		f.inFlight = false
		st := f.snapshotLocked()
		f.mu.Unlock()
		return st, nil
	}
	f.scope = scope
	f.mu.Unlock()
	return f.loadInitialWalk(ctx)
}

// LoadMore loads the next page when warranted. Returns (state, true) when a
// page was applied; (state, false) for a no-op (exhausted, in-flight, or a
// raced reset). Single-flight via inFlight; the Service query coalesces
// identical concurrent reads.
func (f *CommitFeed) LoadMore(ctx context.Context) (FeedState, bool, error) {
	f.mu.Lock()
	if f.exhausted || f.inFlight {
		st := f.snapshotLocked()
		f.mu.Unlock()
		return st, false, nil
	}
	f.inFlight = true
	gen0 := f.gen
	skip := f.skip
	scope := f.scope
	size := f.effPage()
	f.mu.Unlock()

	page, err := f.pager.Page(ctx, size, skip, gen0, scope)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if err != nil {
		return f.snapshotLocked(), false, err
	}
	if f.gen != gen0 { // a reload raced; drop the page
		return f.snapshotLocked(), false, nil
	}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip += len(page) // advance by raw page length to stay aligned with git's walk
	f.exhausted = len(page) < size
	return f.snapshotLocked(), true, nil
}
