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
	"sync/atomic"
	"time"

	"github.com/homeend/gigagit/internal/bookmark"
	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/prefix"
	"github.com/homeend/gigagit/internal/profile"
	"github.com/homeend/gigagit/internal/repogate"
	"github.com/homeend/gigagit/internal/searchhist"
	"github.com/homeend/gigagit/internal/shelf"
)

// Service couples one repository with its process-wide gate and a singleflight
// group that coalesces concurrent identical queries.
type Service struct {
	repo    *git.Repo
	workdir string // fallback gate key when common-dir resolution fails

	mu         sync.Mutex
	gate       *repogate.Gate   // resolved lazily on first Execute or query
	flight     flightGroup      // coalesces concurrent calls sharing a key
	factory    cache.Factory    // vends the diff (and future) caches
	differ     Differ           // memoized production diff engine
	shelf      shelf.Store      // lazily resolved; nil disables the shelf
	bookmark   bookmark.Store   // lazily resolved; nil disables bookmarks
	searchhist searchhist.Store // lazily resolved; nil disables search history

	profileGlobal profile.Store // lazily resolved; nil disables profiles
	profileRepo   profile.Store // lazily resolved; nil disables profiles

	prefixGlobal prefix.Store // lazily resolved; nil disables prefixes
	prefixRepo   prefix.Store // lazily resolved; nil disables prefixes

	// showEOLOnly, when false (the default), hides files whose only unstaged
	// change is line endings (CRLF↔LF) from Status/Snapshot. atomic because the
	// TUI re-applies it from config inside loadCmd on every reload, which races
	// op-triggered status refreshes reading it on another goroutine.
	showEOLOnly atomic.Bool
}

// SetShowEOLOnlyChanges controls whether a file whose ONLY unstaged change is
// line endings (CRLF↔LF) is surfaced as modified. The default (false) drops
// such files from Status/Snapshot as noise; the TUI sets it from [ui]
// show_eol_only_changes, the CLI keeps it true (raw `git status`).
func (s *Service) SetShowEOLOnlyChanges(show bool) *Service {
	s.showEOLOnly.Store(show)
	return s
}

// Open builds a Service rooted at workdir with the standard runner — the
// one place frontends construct the repo stack. It runs no git command. The
// scriptable CLI uses this: a real terminal can service an ssh/credential prompt.
func Open(workdir string) *Service {
	return openWith(workdir, false, observ.NewRing(200))
}

// OpenTUI is Open for the interactive TUI: its runner forces ssh BatchMode so an
// ssh host-key/passphrase prompt fails fast instead of hanging the raw-mode UI
// (mirroring the always-on GIT_TERMINAL_PROMPT=0 for HTTPS). Used by the repo
// switcher's reRoot; cmd/gg wires the initial session via OpenTUIWithRing.
func OpenTUI(workdir string) *Service {
	return openWith(workdir, true, observ.NewRing(200))
}

// OpenTUIWithRing is OpenTUI with a caller-supplied span ring: cmd/gg keeps the
// ring (and, via Repo, the repo) so its panic-dump defer can include the
// session's git spans. This keeps the runner stack built in exactly one place —
// any change to the wrapping here reaches both the initial session and reRoot.
func OpenTUIWithRing(workdir string, ring *observ.Ring) *Service {
	return openWith(workdir, true, ring)
}

func openWith(workdir string, sshBatch bool, ring *observ.Ring) *Service {
	er := gitexec.NewExecRunner("git", workdir, ring)
	if sshBatch {
		er = er.WithSSHBatchMode()
	}
	s := New(&git.Repo{Runner: gitexec.NewLimitRunner(er)})
	s.workdir = workdir
	return s
}

// New wraps an existing repo (tests, callers with their own runner wiring).
func New(repo *git.Repo) *Service {
	return &Service{repo: repo, factory: cache.NewFactory(0, 0)}
}

// Repo exposes the underlying repo to the composition root (cmd/gg's
// panic-dump defer) and tests. Not for frontends: reads go through domain
// queries, commands through Execute.
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
	// Emit a start marker BEFORE running, so the operation log captures an op that
	// hangs or runs slowly — the completion span below is only written once Run
	// returns, which never happens for a stuck op (the case this log exists for).
	// A started line with no matching completion is exactly the trace wanted.
	observ.EmitSpan(observ.Span{Name: label + " started", Start: opStart})
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
	observ.NoteFailure(label, opErr)
	return out, opErr
}
