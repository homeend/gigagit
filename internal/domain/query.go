package domain

import (
	"context"
	"sync"

	"github.com/gigagit/gg/internal/model"
	"github.com/gigagit/gg/internal/repogate"
)

// Snapshot is the git-read half of a TUI load: everything loadCmd needs from
// git, fetched in parallel under one Read reservation. config.Load and
// repos.Touch are deliberately NOT here — they are not git reads and stay in
// the frontend.
type Snapshot struct {
	Status          model.WorkingTreeStatus
	Branches        []model.Branch
	Commits         []model.Commit
	Worktrees       []model.Worktree
	CurrentWorktree string // git toplevel; "" if TopLevel failed
	GitCommonDir    string // "" if it failed
	HeadTimes       map[string]int64
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

// Snapshot fetches the seven startup reads. Status/Branches/Log/Worktrees are
// fatal (the first failure is returned); CommitTimes/TopLevel/GitCommonDir are
// best-effort (failures leave zero values, exactly as loadCmd behaved).
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
		cs, err := s.repo.Log(ctx, 50)
		if err != nil {
			fatal(err)
			return
		}
		mu.Lock()
		snap.Commits = cs
		mu.Unlock()
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
	return snap, nil
}

// Status is a single gated read for the CLI status command.
func (s *Service) Status(ctx context.Context) (model.WorkingTreeStatus, error) {
	return query(ctx, s, "status", s.repo.Status)
}

// Worktrees is a single gated read for the CLI worktree commands.
func (s *Service) Worktrees(ctx context.Context) ([]model.Worktree, error) {
	return query(ctx, s, "worktrees", s.repo.Worktrees)
}
