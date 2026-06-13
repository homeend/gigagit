// Package domain is the frontend-facing layer of gg: commands (engine
// operations) run through Execute under a per-repo reservation, and — in
// later stages — queries (snapshot, commit feed) run here too. Frontends
// call domain; nothing above the engine acquires gates or assembles OpDeps
// by hand.
package domain

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gigagit/gg/internal/cache"
	"github.com/gigagit/gg/internal/engine"
	"github.com/gigagit/gg/internal/git"
	"github.com/gigagit/gg/internal/gitexec"
	"github.com/gigagit/gg/internal/observ"
	"github.com/gigagit/gg/internal/repogate"
)

// Service couples one repository with its process-wide gate and a singleflight
// group that coalesces concurrent identical queries.
type Service struct {
	repo    *git.Repo
	workdir string // fallback gate key when common-dir resolution fails

	mu      sync.Mutex
	gate    *repogate.Gate // resolved lazily on first Execute or query
	flight  flightGroup    // coalesces concurrent calls sharing a key
	factory cache.Factory  // vends the diff (and future) caches
	differ  Differ         // memoized production diff engine
}

// Open builds a Service rooted at workdir with the standard runner — the
// one place frontends construct the repo stack. It runs no git command.
func Open(workdir string) *Service {
	s := New(&git.Repo{Runner: gitexec.NewLimitRunner(gitexec.NewExecRunner("git", workdir, observ.NewRing(200)))})
	s.workdir = workdir
	return s
}

// New wraps an existing repo (tests, callers with their own runner wiring).
func New(repo *git.Repo) *Service {
	return &Service{repo: repo, factory: cache.NewFactory(0, 0)}
}

// Repo exposes the underlying repo for READ verbs. Transitional: stage 2
// moves frontend reads into domain queries; stage 4 removes this.
func (s *Service) Repo() *git.Repo { return s.repo }

// Differ returns this Service's diff engine: enhanced (intraline) and cached,
// over the Service's "diff" cache. Built once, lazily, under the Service lock.
func (s *Service) Differ() Differ {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.differ == nil {
		s.differ = NewDiffer(DifferOptions{Enhanced: true, Cached: true}, s.factory.Cache("diff"))
	}
	return s.differ
}

// gateFor resolves (once) the gate for this repo, keyed by the git common
// dir so all linked worktrees share one gate. A repo whose common dir
// cannot be resolved falls back to the workdir (or a per-Service key) —
// sound for everything except cross-worktree races in a broken repo, where
// the verb error surfaces anyway.
func (s *Service) gateFor(ctx context.Context) *repogate.Gate {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gate != nil {
		return s.gate
	}
	key := ""
	if cd, err := s.repo.GitCommonDir(ctx); err == nil {
		key = strings.TrimSpace(cd)
	}
	if key == "" {
		key = s.workdir
	}
	if key == "" {
		key = fmt.Sprintf("repo-%p", s.repo)
	}
	s.gate = repogate.For(key)
	return s.gate
}

// lockModer is implemented by operations that need less than the exclusive
// default (e.g. a background pull that only moves refs).
type lockModer interface{ LockMode() repogate.Mode }

// Execute runs one command under its gate reservation: acquire (honoring
// ctx while queued), run the op with fully assembled OpDeps, release, and
// emit the "op <Name>" span. Execute is synchronous; frontends keep their
// own goroutine and event-pumping structure around it.
func (s *Service) Execute(ctx context.Context, op engine.Operation,
	events chan<- engine.Event, dec engine.Decider) (engine.Result, error) {
	mode := repogate.TreeWrite
	if lm, ok := op.(lockModer); ok {
		mode = lm.LockMode()
	}
	label := "op " + engine.OpName(op)
	res, err := s.gateFor(ctx).Acquire(ctx, mode, label)
	if err != nil {
		return engine.Result{}, err
	}
	// A failed Escalate releases without re-acquiring, so the reservation
	// may already be gone by the time the op returns.
	defer func() {
		if !res.Released() {
			res.Release()
		}
	}()

	opStart := time.Now()
	out, opErr := op.Run(ctx, engine.OpDeps{
		Repo:     s.repo,
		Events:   events,
		Decider:  dec,
		Escalate: res.Escalate,
	})
	span := observ.Span{Name: label, Start: opStart, Duration: time.Since(opStart)}
	if opErr != nil {
		span.ExitCode = 1
		span.Err = opErr.Error()
	}
	observ.EmitSpan(span)
	return out, opErr
}
