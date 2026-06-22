package domain

import (
	"context"
	"strconv"
	"strings"
	"sync"

	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repogate"
)

// LogScope re-exports git.LogScope for the commit feed's scope (empty Branches =
// all local branches).
type LogScope = git.LogScope

// Snapshot is the git-read half of a TUI load: everything loadCmd needs from
// git, fetched in parallel under one Read reservation. config.Load and
// repos.Touch are deliberately NOT here — they are not git reads and stay in
// the frontend. Commits are owned by CommitFeed, not Snapshot.
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	RemoteBranches  []model.RemoteBranch
	Worktrees       []model.Worktree
	Tags            []model.Tag
	CurrentWorktree string // git toplevel; "" if TopLevel failed
	GitCommonDir    string // "" if it failed
	HeadTimes       map[string]int64
	Conflict        ConflictState // source of any in-progress conflict (zero if none)
}

// query runs fn under a Read reservation on s's gate, coalescing concurrent
// calls with the same key. The reservation is outermost-via-singleflight: the
// leader acquires it and runs fn; followers share the result without
// acquiring.
func query[T any](ctx context.Context, s *Service, key string, fn func(context.Context) (T, error)) (T, error) {
	v, err := s.flight.Do(key, func() (any, error) {
		res, e := s.gateFor(ctx).Acquire(ctx, repogate.Read, "read "+key)
		if e != nil {
			return nil, e
		}
		defer res.Release()
		return fn(ctx)
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return v.(T), nil
}

// Snapshot fetches the six startup reads. Status/Branches/Worktrees are
// fatal (the first failure is returned); CommitTimes/TopLevel/GitCommonDir are
// best-effort (failures leave zero values). Commits are owned by CommitFeed.
func (s *Service) Snapshot(ctx context.Context) (Snapshot, error) {
	return query(ctx, s, "snapshot", s.loadSnapshot)
}

func (s *Service) loadSnapshot(ctx context.Context) (Snapshot, error) {
	var (
		snap     Snapshot
		mu       sync.Mutex
		firstErr error
		wg       sync.WaitGroup
	)
	fatal := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}
	run := func(f func()) { wg.Add(1); go func() { defer wg.Done(); f() }() }

	run(func() {
		st, err := s.repo.Status(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Status = st
		mu.Unlock()
	})
	run(func() {
		bs, err := s.repo.Branches(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Branches = bs
		mu.Unlock()
	})
	run(func() {
		// RemoteBranches is best-effort: a repo with no remotes (or a failing
		// for-each-ref) must not block startup.
		if rbs, err := s.repo.RemoteBranches(ctx); err == nil {
			mu.Lock()
			snap.RemoteBranches = rbs
			mu.Unlock()
		}
	})
	run(func() {
		// Tags is best-effort: a repo with no tags (or a failing for-each-ref)
		// must not block startup.
		if tags, err := s.repo.Tags(ctx); err == nil {
			mu.Lock()
			snap.Tags = tags
			mu.Unlock()
		}
	})
	run(func() {
		// Worktrees is fatal; CommitTimes (best-effort) depends on its result,
		// so it runs here after Worktrees returns.
		wts, err := s.repo.Worktrees(ctx)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Worktrees = wts
		mu.Unlock()
		shas := make([]string, 0, len(wts))
		for _, w := range wts {
			if w.Head != "" {
				shas = append(shas, w.Head)
			}
		}
		if times, terr := s.repo.CommitTimes(ctx, shas); terr == nil {
			mu.Lock()
			snap.HeadTimes = times
			mu.Unlock()
		}
	})
	run(func() {
		if top, err := s.repo.TopLevel(ctx); err == nil {
			mu.Lock()
			snap.CurrentWorktree = top
			mu.Unlock()
		}
	})
	run(func() {
		if gcd, err := s.repo.GitCommonDir(ctx); err == nil {
			mu.Lock()
			snap.GitCommonDir = gcd
			mu.Unlock()
		}
	})

	wg.Wait()
	if firstErr != nil {
		return Snapshot{}, firstErr
	}
	// Attribute any conflict to the merge/rebase in progress. Serial (after the
	// parallel reads) because it needs the resolved Status, and cheap because it
	// short-circuits unless Status actually has unmerged files.
	snap.Conflict = s.conflictState(ctx, snap.Status)
	return snap, nil
}

// logPage is the gated, singleflighted commit-page read the CommitFeed uses.
// The singleflight key includes the feed generation, scope, and skip. The gen is
// load-bearing: without it, a reload (C) that reuses a scope still in flight from
// a just-cancelled load (A) would coalesce onto A and inherit A's context.Canceled
// — blanking the panel. A distinct gen per load makes that impossible while still
// coalescing genuine concurrent reads of the same page within one generation.
func (s *Service) logPage(ctx context.Context, limit, skip int, scope LogScope, gen int, dateOrder bool) ([]model.Commit, error) {
	key := "commits:" + scopeKey(scope) + ":" + strconv.Itoa(gen) + ":" + strconv.Itoa(limit) + ":" + strconv.Itoa(skip) + ":" + strconv.FormatBool(dateOrder)
	return query(ctx, s, key, func(ctx context.Context) ([]model.Commit, error) {
		return s.repo.LogScoped(ctx, limit, skip, scope, dateOrder)
	})
}

// scopeKey is the stable cache/singleflight discriminator for a scope.
func scopeKey(scope LogScope) string {
	if len(scope.Branches) == 0 {
		return "all"
	}
	return strings.Join(scope.Branches, ",")
}

// Status is a single gated read for the CLI status command.
func (s *Service) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	return query(ctx, s, "status", s.repo.Status)
}

// Branches is a single gated read for the local branch list. The TUI uses it
// for a targeted refresh after a ref-only op (e.g. create-worktree) that does
// not warrant a full Snapshot (status walk + commit feed) on a huge repo.
func (s *Service) Branches(ctx context.Context) ([]model.Branch, error) {
	return query(ctx, s, "branches", s.repo.Branches)
}

// Worktrees is a single gated read for the CLI worktree commands.
func (s *Service) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	return query(ctx, s, "worktrees", s.repo.Worktrees)
}

// RemoteBranches lists remote-tracking branches (refs/remotes).
func (s *Service) RemoteBranches(ctx context.Context) ([]model.RemoteBranch, error) {
	return query(ctx, s, "remote-branches", s.repo.RemoteBranches)
}

// Tags is a single gated read for the CLI tag commands and the TUI Tags tab.
func (s *Service) Tags(ctx context.Context) ([]model.Tag, error) {
	return query(ctx, s, "tags", s.repo.Tags)
}

// ShowFile returns the raw blob of path at rev (git show rev:path), under a
// Read reservation, coalesced per (rev, path).
func (s *Service) ShowFile(ctx context.Context, rev, path string) ([]byte, error) {
	return query(ctx, s, "showfile:"+rev+":"+path, func(ctx context.Context) ([]byte, error) {
		return s.repo.ShowFile(ctx, rev, path)
	})
}

// CommitFiles returns the files changed by commit hash, under a Read
// reservation, coalesced per hash.
func (s *Service) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	return query(ctx, s, "commit-files:"+hash, func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.CommitFiles(ctx, hash)
	})
}

// TreeFiles returns every file in commit hash's tree (the full checked-out file
// set at that commit), under a Read reservation, coalesced per hash.
func (s *Service) TreeFiles(ctx context.Context, hash string) ([]model.CommitFile, error) {
	return query(ctx, s, "tree-files:"+hash, func(ctx context.Context) ([]model.CommitFile, error) {
		return s.repo.TreeFiles(ctx, hash)
	})
}

// CompareFiles returns the files that differ between two endpoints (left =
// older, right = newer), under a Read reservation. The singleflight key
// includes both endpoints; live endpoints (working tree / index) change
// underfoot, so callers that need strict freshness should not rely on a long
// coalesce window.
func (s *Service) CompareFiles(ctx context.Context, left, right model.Endpoint) ([]model.CommitFile, error) {
	return query(ctx, s, "compare-files:"+left.CacheTag()+":"+right.CacheTag(), func(ctx context.Context) ([]model.CommitFile, error) {
		files, err := s.repo.DiffTreeFiles(ctx, left, right)
		if err != nil {
			return nil, err
		}
		// `git diff` omits untracked files, so a comparison whose newer side is the
		// working tree would miss brand-new files. Add them as added ("A") entries
		// (an untracked file is new relative to both a commit and the index).
		if right.Kind == model.EndpointWorkTree {
			untracked, err := s.repo.UntrackedFiles(ctx)
			if err != nil {
				return nil, err
			}
			seen := make(map[string]bool, len(files))
			for _, f := range files {
				seen[f.Path] = true
			}
			for _, p := range untracked {
				if !seen[p] {
					files = append(files, model.CommitFile{Status: "A", Path: p})
				}
			}
		}
		return files, nil
	})
}

// TopLevel returns the repo's working-tree root, under a Read reservation.
func (s *Service) TopLevel(ctx context.Context) (string, error) {
	return query(ctx, s, "toplevel", func(ctx context.Context) (string, error) {
		return s.repo.TopLevel(ctx)
	})
}

// CurrentBranch returns the checked-out branch name, under a Read reservation.
func (s *Service) CurrentBranch(ctx context.Context) (string, error) {
	return query(ctx, s, "current-branch", s.repo.CurrentBranch)
}

// LastCommitMessage returns HEAD's full commit message, under a Read reservation.
func (s *Service) LastCommitMessage(ctx context.Context) (string, error) {
	return query(ctx, s, "last-commit-message", s.repo.LastCommitMessage)
}

// FileLog returns up to limit commits touching path at rev, newest first,
// under a Read reservation, coalesced per (rev, path, limit).
func (s *Service) FileLog(ctx context.Context, rev, path string, limit int) ([]model.FileCommit, error) {
	return query(ctx, s, "filelog:"+rev+":"+path+":"+strconv.Itoa(limit), func(ctx context.Context) ([]model.FileCommit, error) {
		return s.repo.FileLog(ctx, rev, path, limit)
	})
}

// CommitMessage returns rev's full commit message, under a Read reservation.
// Backs the reword popup's pre-fill.
func (s *Service) CommitMessage(ctx context.Context, rev string) (string, error) {
	return query(ctx, s, "commit-message:"+rev, func(c context.Context) (string, error) {
		return s.repo.CommitMessage(c, rev)
	})
}

// CommitRange lists onto..branch oldest-first with full messages, under a Read
// reservation. Backs the interactive-rebase editor.
func (s *Service) CommitRange(ctx context.Context, onto, branch string) ([]model.RangeCommit, error) {
	return query(ctx, s, "commit-range:"+onto+".."+branch, func(c context.Context) ([]model.RangeCommit, error) {
		return s.repo.LogRangeMessages(c, onto, branch)
	})
}

// WorktreeFile reads the working-tree bytes of a path under a Read reservation.
// Backs the conflict hunk picker (marker text) and hunk staging (the new side).
func (s *Service) WorktreeFile(ctx context.Context, path string) ([]byte, error) {
	return query(ctx, s, "worktree-file:"+path, func(c context.Context) ([]byte, error) {
		return s.repo.ReadWorktreeFile(c, path)
	})
}

// cachedBlame wraps a blame result so it can report its heap weight to the
// byte-budgeted cache (a bare []model.BlameLine cannot implement Sized).
type cachedBlame struct{ lines []model.BlameLine }

func (b cachedBlame) Size() int {
	n := 0
	for _, l := range b.lines {
		n += len(l.Hash) + len(l.Author) + len(l.Summary) + len(l.Content) + 64
	}
	return n
}

// Blame returns per-line blame for path at rev under a Read reservation,
// coalesced per (rev, path). Blame at a committed rev is immutable by
// (rev, path), so it is memoized in the "blame" LRU (a hit skips both the
// reservation and the git run — git blame is expensive on large repos).
// Working-tree blame (rev == "") is never cached: the file changes under live
// edits, exactly as the Differ leaves working-tree diffs uncached.
func (s *Service) Blame(ctx context.Context, rev, path string) ([]model.BlameLine, error) {
	load := func() ([]model.BlameLine, error) {
		return query(ctx, s, "blame:"+rev+":"+path, func(ctx context.Context) ([]model.BlameLine, error) {
			return s.repo.Blame(ctx, rev, path)
		})
	}
	if rev == "" {
		return load()
	}
	v, err := s.factory.Cache("blame").GetOrLoad("blame:"+rev+":"+path, func() (any, error) {
		lines, e := load()
		if e != nil {
			return nil, e
		}
		return cachedBlame{lines}, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(cachedBlame).lines, nil
}

// GitCommonDir returns the git common dir path, under a Read reservation.
func (s *Service) GitCommonDir(ctx context.Context) (string, error) {
	return query(ctx, s, "gitcommondir", s.repo.GitCommonDir)
}
