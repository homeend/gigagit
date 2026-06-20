package domain

import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
)

// Page sizes (commits). Initial paint stays cheap; later pages are larger
// since the user has signaled interest by scrolling.
const (
	commitInitialPage = 50
	commitPageSize    = 200
	commitNearEnd     = 10 // load more when the selection is within this of the end
)

// CommitFeed is the single source of truth for the Commits panel: an
// incrementally loaded, newest-first view of HEAD history. Goroutine-safe;
// Snapshot returns a copy so a frontend can render while a page loads.
type CommitFeed struct {
	svc *Service

	mu        sync.Mutex
	scope     LogScope // refspec for the walk; empty = all local branches
	commits   []model.Commit
	hashes    map[string]bool // dedupe set, mirrors commits
	skip      int             // next --skip offset (advances by raw page length)
	exhausted bool
	gen       int                // bumped by LoadInitial; tags pages so stale ones drop
	inFlight  bool               // at most one page request outstanding
	cancel    context.CancelFunc // cancels the in-flight load's ctx on supersede
}

// CommitFeed returns a fresh feed for this Service's repo.
func (s *Service) CommitFeed() *CommitFeed {
	return &CommitFeed{svc: s, hashes: map[string]bool{}}
}

// SetScope sets the refspec for subsequent loads. Callers then LoadInitial to
// re-walk; the gen bump drops any stale in-flight page.
func (f *CommitFeed) SetScope(scope LogScope) {
	f.mu.Lock()
	f.scope = scope
	f.mu.Unlock()
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

// LoadInitial resets the feed (bumps gen, clears) and loads page 0. It is the
// reload primitive: callers re-fill a feed by calling LoadInitial again.
func (f *CommitFeed) LoadInitial(ctx context.Context) (FeedState, error) {
	f.mu.Lock()
	if f.cancel != nil {
		f.cancel() // stop a superseded in-flight walk, not just drop its result
	}
	cctx, cancel := context.WithCancel(ctx)
	f.cancel = cancel
	f.gen++
	gen0 := f.gen
	scope := f.scope
	f.commits = nil
	f.hashes = map[string]bool{}
	f.skip = 0
	f.exhausted = false
	f.inFlight = true
	f.mu.Unlock()

	page, err := f.svc.logPage(cctx, commitInitialPage, 0, scope, gen0)

	f.mu.Lock()
	defer f.mu.Unlock()
	f.inFlight = false
	if f.gen == gen0 {
		f.cancel = nil // our load finished current; nothing to cancel
	}
	if f.gen != gen0 { // a newer reload raced; drop this page, stamp OUR gen so the
		// caller's message is droppable (not masquerading as the current gen)
		st := f.snapshotLocked()
		st.Gen = gen0
		return st, nil
	}
	if err != nil {
		return f.snapshotLocked(), err
	}
	for _, c := range page {
		if !f.hashes[c.Hash] {
			f.commits = append(f.commits, c)
			f.hashes[c.Hash] = true
		}
	}
	f.skip = len(page)
	f.exhausted = len(page) < commitInitialPage
	return f.snapshotLocked(), nil
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
	f.mu.Unlock()

	page, err := f.svc.logPage(ctx, commitPageSize, skip, scope, gen0)

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
	f.exhausted = len(page) < commitPageSize
	return f.snapshotLocked(), true, nil
}
