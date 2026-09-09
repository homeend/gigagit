package domain

import (
	"context"
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/config"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
)

// notesSweepTimeout bounds the background housekeeping pass: it reads file
// content per annotated target, and a huge repo on a slow mount must never
// leave a goroutine reading forever behind a quit.
const notesSweepTimeout = 30 * time.Second

// notesDefaults is the built-in [notes] budget, taken from the config defaults
// so the two can never drift. A Service whose frontend never calls
// SetNotesPolicy (the CLI and MCP one-shots) still gets these — an UNSET
// (zero) value means "the default", while an explicit negative means forever /
// uncapped, exactly like versions.max_age_days.
var notesDefaults = config.Defaults().Notes

// notesEffective maps an unset (zero) policy value onto its built-in default;
// any other value — including a negative "forever / uncapped" — passes through.
func notesEffective(v, def int) int {
	if v == 0 {
		return def
	}
	return v
}

// SetNotesPolicy pushes [notes] onto the Service: the sweep's expiry window
// and the store's write-time entry cap. Call it before StartNotesSweep, from
// the same place a frontend applies SetVersionsPolicy.
func (s *Service) SetNotesPolicy(maxAgeDays, maxEntries int) {
	s.mu.Lock()
	s.notesMaxAgeDays, s.notesMaxEntries = maxAgeDays, maxEntries
	st := s.notes
	s.mu.Unlock()
	if st != nil {
		st.SetPolicy(notes.Policy{MaxEntries: notesEffective(maxEntries, notesDefaults.MaxEntries)})
	}
}

// StartNotesSweep runs housekeeping ONCE per Service, in the background: it
// drops notes past [notes] max_age_days and notes whose anchor no longer
// resolves (stale or orphaned), then rewrites the file once. It never blocks a
// read and never surfaces an error — failures go to the session error ring.
// A re-root builds a fresh Service for a different repo, which sweeps its own
// store.
func (s *Service) StartNotesSweep() {
	s.notesSweepOnce.Do(func() {
		s.notesSweepWG.Add(1)
		go func() {
			defer s.notesSweepWG.Done()
			s.notesSweepRuns.Add(1)
			ctx, cancel := context.WithTimeout(context.Background(), notesSweepTimeout)
			defer cancel()
			// Notes being disabled (no state dir, or the test seam) is a
			// legitimate configuration, not a failure worth a session error.
			if _, err := s.sweepNotes(ctx); err != nil && !errors.Is(err, ErrNotesDisabled) {
				observ.NoteFailure("notes sweep", err)
			}
		}()
	})
}

// waitNotesSweepForTest / notesSweepRunsForTest exist for the once-semantics
// test (the goroutine is fire-and-forget in production).
func (s *Service) waitNotesSweepForTest()     { s.notesSweepWG.Wait() }
func (s *Service) notesSweepRunsForTest() int { return int(s.notesSweepRuns.Load()) }

// noteSide is one cached side-text read: the lines the anchor is matched
// against, plus whether the read succeeded at all. A readable side with nil
// lines is legitimately ABSENT (the note is orphaned); an unreadable one says
// nothing about the note, so the sweep keeps it.
type noteSide struct {
	lines    []string
	readable bool
}

// sweepNotes is the housekeeping pass: keep a note only when it has not
// expired AND still resolves as active.
//
// It runs in two phases on purpose. Phase 1 resolves every note against a
// Load() SNAPSHOT while holding NO lock — resolution shells out to git once
// per (address, side) pair, and doing that inside Sweep's predicate would hold
// the store's mutex and its cross-process lock across every one of those
// reads, parking a concurrent NoteAdd (in this process or another gg) for the
// whole pass. Phase 2 hands Sweep a pure id-set predicate that defaults to
// KEEP, so a note written between the snapshot and the rewrite survives.
//
// Deletion is deliberately timid: only a target git or the OS reports ABSENT
// makes a note orphaned. A read that merely FAILED (a parked reservation, a
// permission or disk error) keeps its note, and a cancelled or timed-out pass
// aborts before Sweep is called at all — otherwise a startup pull holding a
// TreeWrite, or the 30 s deadline, would fail every read and silently wipe the
// whole store.
func (s *Service) sweepNotes(ctx context.Context) (int, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return 0, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return 0, err
	}
	if len(all) == 0 {
		return 0, nil
	}
	s.mu.Lock()
	maxAge := notesEffective(s.notesMaxAgeDays, notesDefaults.MaxAgeDays)
	s.mu.Unlock()
	var cutoff time.Time
	if maxAge > 0 {
		cutoff = notes.Now().UTC().AddDate(0, 0, -maxAge)
	}

	// Phase 1 — resolve against the snapshot, no lock held.
	cache := map[string]noteSide{}
	drop := map[string]bool{}
	for _, n := range all {
		if !cutoff.IsZero() && n.Created.Before(cutoff) {
			drop[n.ID] = true
			continue
		}
		if n.IsReply() {
			continue // a reply lives or dies with its root (dropOrphanReplies)
		}
		side, err := s.noteSideCached(ctx, cache, n)
		if err != nil {
			return 0, err // cancelled or timed out: change nothing
		}
		if !side.readable {
			continue // the read says nothing about the note — keep it
		}
		if status, _ := resolveOne(n, side.lines); status != model.NoteActive {
			drop[n.ID] = true
		}
	}
	if len(drop) == 0 {
		// Nothing to do: never take the store's lock. This deliberately skips
		// the rewrite ENTIRELY, so a store that is over the entry cap but has
		// no expired or dangling note is left alone until the next write — the
		// cap is a write-time rule (global constraint), not a sweep-time one.
		return 0, nil
	}

	// Phase 2 — apply under the store's lock with a PURE predicate.
	dropped, err := st.Sweep(func(n model.Note) bool { return !drop[n.ID] })
	if dropped > 0 {
		// The badge counts are cached, and at startup the fan-out's NoteCounts
		// read races this goroutine and usually wins — so without this a ◆N
		// badge would outlive the notes it counts for the whole session, and
		// `r` would keep re-reading the same stale cache.
		s.invalidateNoteCounts()
	}
	return dropped, err
}

// noteSideCached reads the side text a note anchors on, once per (side, state,
// commit, path, shelf) pair — a file with twenty notes costs one read per
// side. A non-nil error means the pass was cancelled and must abort.
func (s *Service) noteSideCached(ctx context.Context, cache map[string]noteSide, n model.Note) (noteSide, error) {
	// State is part of the key: a staged and an unstaged note on the same
	// path read DIFFERENT old sides (HEAD vs the index). So is Worktree —
	// the store is shared by every worktree of the repo.
	key := string(n.Side) + "\x00" + strconv.Itoa(int(n.Address.State)) + "\x00" +
		n.Address.Worktree + "\x00" + n.Address.Commit + "\x00" +
		n.Address.Path + "\x00" + n.Address.ShelfID
	if side, ok := cache[key]; ok {
		return side, nil
	}
	lines, err := s.noteSideLines(ctx, n.Address, n.Side)
	side := noteSide{lines: lines, readable: true}
	if err != nil {
		if ctx.Err() != nil {
			return noteSide{}, err
		}
		// An absent target orphans the note; anything else keeps it.
		side = noteSide{readable: noteTargetGone(err)}
	}
	cache[key] = side
	return side, nil
}

// noteTargetGone reports whether a failed side read proves the target is
// ABSENT (its blob, rev, file or whole worktree is gone) rather than merely
// unreadable. Only this classification may delete a note, so it matches on
// git's own "not there" wording and the OS's not-exist errors, and defaults to
// false for everything it does not recognise.
func noteTargetGone(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) { // the direct working-copy read
		return true
	}
	// git errors reach here as text (gitexec wraps stderr, not a typed error).
	msg := strings.ToLower(err.Error())
	for _, gone := range []string{
		"does not exist",             // path 'x' does not exist in 'HEAD' / …nor in the index
		"exists on disk, but not in", // a path that is untracked at that rev
		"invalid object name",        // the rev itself is gone
		"unknown revision or path",   //  "
		"bad object",                 //  "
		"cannot change to",           // git -C into a worktree that was removed
	} {
		if strings.Contains(msg, gone) {
			return true
		}
	}
	return false
}
