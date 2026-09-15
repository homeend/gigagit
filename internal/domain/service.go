// Package domain is the frontend-facing layer of gg: commands (engine
// operations) run through Execute under a per-repo reservation, and — in
// later stages — queries (snapshot, commit feed) run here too. Frontends
// call domain; nothing above the engine acquires gates or assembles OpDeps
// by hand.
package domain

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/homeend/gigagit/internal/bookmark"
	"github.com/homeend/gigagit/internal/cache"
	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
	"github.com/homeend/gigagit/internal/prefix"
	"github.com/homeend/gigagit/internal/preflight"
	"github.com/homeend/gigagit/internal/preview"
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

	notes      notes.Store   // lazily resolved; nil disables notes
	notesOff   bool          // hard "no store" (the disabled-path test)
	preview    preview.Store // lazily resolved; nil disables previews
	noteCounts *NoteCounts   // cached badge counts; nil = cold, invalidated by every mutation
	// previewCounts caches PreviewNoteCounts per (tip, base) pair. It follows
	// BOTH clocks: a new tip is a new key, and every note mutation drops the
	// whole map through invalidateNoteCounts (ruling 7) — counts read the
	// notes store, which the summary cache knows nothing about.
	previewCounts map[string]previewCountEntry
	// notesGen rises on every count invalidation. NoteCounts computes OUTSIDE
	// the lock, so it stores its result only when the generation it started
	// from is still current — a mutation landing mid-compute would otherwise
	// have its invalidation overwritten by the stale result.
	notesGen uint64

	// notesMaxAgeDays / notesMaxEntries carry [notes] into the store and the
	// sweep. Set by SetNotesPolicy before StartNotesSweep; 0 = the built-in
	// defaults (30 / 2000), <=0 after an explicit set = forever / uncapped.
	notesMaxAgeDays int
	notesMaxEntries int
	notesSweepOnce  sync.Once
	// notesSweepWG / notesSweepRuns exist for the once-semantics test; the
	// production sweep is fire-and-forget (see StartNotesSweep).
	notesSweepWG   sync.WaitGroup
	notesSweepRuns atomic.Int32

	profileGlobal profile.Store // lazily resolved; nil disables profiles
	profileRepo   profile.Store // lazily resolved; nil disables profiles

	prefixGlobal prefix.Store // lazily resolved; nil disables prefixes
	prefixRepo   prefix.Store // lazily resolved; nil disables prefixes

	// preflightMu guards the resolved verdicts. reRoot builds a FRESH Service,
	// so a cached resolution can never outlive the repo it describes.
	preflightMu   sync.Mutex
	preflightDone bool
	preflightOut  []preflight.Verdict
	// preflightMarks is the store-format marker map preflightOut was resolved
	// from. Every cached Preflight call re-reads the markers and compares:
	// another gg process sharing this repo can migrate a store underneath a
	// long-lived Service (a `gg mcp` server never re-roots), and the cache
	// must not outlive the state it describes.
	preflightMarks map[string]int

	// gitDirMu guards gitDirPath — this worktree's git dir, resolved once on
	// first use (a repo's git dir never moves during a session; reRoot builds
	// a fresh Service). "" = not yet resolved; a failed resolution retries on
	// the next call. Backs the stat-level paused-op probe in conflictState.
	gitDirMu   sync.Mutex
	gitDirPath string

	// showEOLOnly, when false (the default), hides files whose only unstaged
	// change is line endings (CRLF↔LF) from Status/Snapshot. atomic because the
	// TUI re-applies it from config inside loadCmd on every reload, which races
	// op-triggered status refreshes reading it on another goroutine.
	showEOLOnly atomic.Bool

	// syntaxOff, when false (the zero value, i.e. the default), keeps diff
	// syntax colouring ON. Named so the zero value means "on": the Service is
	// constructed before config loads, and the CLI/web frontends may never
	// call SetSyntaxHighlighting at all, so they must still get colours.
	syntaxOff atomic.Bool

	// versionsPolicy stores the engine.VersionsPolicy injected into every
	// Execute. nil (never set) resolves to the default: enabled, 90 days.
	versionsPolicy atomic.Value

	// tagsMu guards the fingerprint-validated Tags cache. The full tags read
	// sorts by creatordate, which makes git read every tag object — seconds on
	// a big pack — so tagsCached revalidates with the cheap TagsFingerprint
	// probe instead and re-reads only when the tag ref set actually changed.
	// tagsOK (not a nil check) marks "cached", so a tagless repo caches too.
	tagsMu  sync.Mutex
	tagsFP  string
	tagsVal []model.Tag
	tagsOK  bool
}

// SetShowEOLOnlyChanges controls whether a file whose ONLY unstaged change is
// line endings (CRLF↔LF) is surfaced as modified. The default (false) drops
// such files from Status/Snapshot as noise; the TUI sets it from [ui]
// show_eol_only_changes, the CLI keeps it true (raw `git status`).
func (s *Service) SetShowEOLOnlyChanges(show bool) *Service {
	s.showEOLOnly.Store(show)
	return s
}

// SetSyntaxHighlighting turns diff syntax colouring on/off for every later
// Differ call ([ui] diff_syntax). Default on; the differ reads it per call so
// a settings change needs no rebuild. Cache entries are keyed by the flag.
func (s *Service) SetSyntaxHighlighting(on bool) *Service {
	s.syntaxOff.Store(!on)
	return s
}

// syntaxOn reports the current diff-syntax-highlighting setting; the zero
// value of syntaxOff means highlighting is ON.
func (s *Service) syntaxOn() bool { return !s.syntaxOff.Load() }

// SetVersionsPolicy overrides the branch-version snapshot policy injected
// into operations (from [versions] config). Unset = enabled, 90 days.
func (s *Service) SetVersionsPolicy(p engine.VersionsPolicy) *Service {
	s.versionsPolicy.Store(p)
	return s
}

// currentVersionsPolicy resolves the active branch-version snapshot policy:
// whatever was last set via SetVersionsPolicy, or the default (enabled, 90
// days) when never set.
func (s *Service) currentVersionsPolicy() engine.VersionsPolicy {
	if v := s.versionsPolicy.Load(); v != nil {
		return v.(engine.VersionsPolicy)
	}
	return engine.VersionsPolicy{Enabled: true, MaxAgeDays: 90}
}

// Open builds a Service rooted at workdir with the standard runner — the
// one place frontends construct the repo stack. It runs exactly one git
// command (rev-parse --show-toplevel, see openWith). The scriptable CLI uses
// this: a real terminal can service an ssh/credential prompt.
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
	workdir = resolveRoot(workdir, sshBatch, ring)
	er := gitexec.NewExecRunner("git", workdir, ring)
	if sshBatch {
		er = er.WithSSHBatchMode()
	}
	s := New(&git.Repo{Runner: gitexec.NewLimitRunner(er)})
	s.workdir = workdir
	return s
}

// resolveRoot re-roots a workdir that is a SUBDIRECTORY of a worktree onto
// that worktree's top level. Every gg surface speaks worktree-root-relative
// paths (git status --porcelain reports them that way regardless of cwd), so
// a git command run with the subdirectory as its cwd resolves the path gg
// just printed against the wrong base — "src/xxx.txt" staged from src/
// became "src/src/xxx.txt" and failed. Running every invocation at the top
// level makes the paths gg prints the paths gg accepts, and likewise fixes
// the cwd-scoped verbs (ls-files, grep, blame) and the external-tool Dir.
//
// It costs one rev-parse. Anything that is not a worktree subdirectory — a
// plain directory, a bare repo, a deleted cwd — keeps the given workdir, so
// the existing friendly startup errors fire unchanged.
func resolveRoot(workdir string, sshBatch bool, rec observ.Recorder) string {
	er := gitexec.NewExecRunner("git", workdir, rec)
	if sshBatch {
		er = er.WithSSHBatchMode()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	top, err := (&git.Repo{Runner: er}).TopLevel(ctx)
	if err != nil || top == "" {
		return workdir
	}
	return filepath.FromSlash(top)
}

// New wraps an existing repo (tests, callers with their own runner wiring).
func New(repo *git.Repo) *Service {
	return &Service{repo: repo, factory: cache.NewFactory(0, 0)}
}

// Root is the directory every git invocation of this Service runs in: the
// worktree top level for a Service built by Open/OpenTUI (see resolveRoot),
// the given workdir when that could not be resolved, and "" for a Service
// built by New. Frontends use it to turn a user's cwd-relative pathspec into
// the worktree-root-relative form gg speaks everywhere else; a "" Root means
// "unknown", and the caller must pass the pathspec through untouched.
func (s *Service) Root() string { return s.workdir }

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
		s.differ = NewDiffer(DifferOptions{Enhanced: true, Cached: true, Syntax: s.syntaxOn}, s.factory.Cache("diff"))
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
	// The branch-version WRITER is gated on preflight as well as config: a
	// user who chose Skip on a repairable versions store (or an older build
	// opening a store a newer gg wrote) must not have the next operation
	// write a version ref and stamp a marker over it. Computed locally — the
	// STORED policy stays config-only, so a transient preflight probe failure
	// never becomes a sticky "versions off".
	//
	// Resolved BEFORE the reservation is acquired, and it must stay that way:
	// Preflight shells out (a marker for-each-ref on every call, plus the
	// version/data probes when it re-resolves), and no git subprocess may
	// extend an exclusive hold on the repo gate. The && also short-circuits,
	// so a config-disabled policy probes nothing at all. Nothing inside
	// op.Run calls Preflight, so the gate never sees a probe.
	versions := s.currentVersionsPolicy()
	versions.Enabled = versions.Enabled && s.FeatureEnabled(ctx, FeatureVersions)
	versions.Format = VersionsFormat

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
		Versions: versions,
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
