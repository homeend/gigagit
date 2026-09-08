package domain

import (
	"context"
	"strconv"
	"time"

	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
)

// notesSweepTimeout bounds the background housekeeping pass: it reads file
// content per annotated target, and a huge repo on a slow mount must never
// leave a goroutine reading forever behind a quit.
const notesSweepTimeout = 30 * time.Second

// The built-in [notes] budget, mirroring config.Defaults(). A Service whose
// frontend never calls SetNotesPolicy (the CLI and MCP one-shots) still gets
// these — an UNSET (zero) value means "the default", while an explicit
// negative means forever / uncapped, exactly like versions.max_age_days.
const (
	notesDefaultMaxAgeDays = 30
	notesDefaultMaxEntries = 2000
)

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
		st.SetPolicy(notes.Policy{MaxEntries: notesEffective(maxEntries, notesDefaultMaxEntries)})
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
			if _, err := s.sweepNotes(ctx); err != nil {
				observ.NoteFailure("notes sweep", err)
			}
		}()
	})
}

// waitNotesSweepForTest / notesSweepRunsForTest exist for the once-semantics
// test (the goroutine is fire-and-forget in production).
func (s *Service) waitNotesSweepForTest()     { s.notesSweepWG.Wait() }
func (s *Service) notesSweepRunsForTest() int { return int(s.notesSweepRuns.Load()) }

// sweepNotes is the housekeeping pass: keep a note only when it has not
// expired AND still resolves as active. Side text is read once per (address,
// side) pair, so a file with twenty notes costs one read per side.
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
	maxAge := notesEffective(s.notesMaxAgeDays, notesDefaultMaxAgeDays)
	s.mu.Unlock()
	var cutoff time.Time
	if maxAge > 0 {
		cutoff = notes.Now().UTC().AddDate(0, 0, -maxAge)
	}

	cache := map[string][]string{}
	sideOf := func(n model.Note) []string {
		// State is part of the key: a staged and an unstaged note on the same
		// path read DIFFERENT old sides (HEAD vs the index). So is Worktree:
		// the same path in two worktrees of one repo is two different files.
		key := string(n.Side) + "\x00" + strconv.Itoa(int(n.Address.State)) + "\x00" +
			n.Address.Commit + "\x00" + n.Address.Worktree + "\x00" +
			n.Address.Path + "\x00" + n.Address.ShelfID
		if lines, ok := cache[key]; ok {
			return lines
		}
		lines, lerr := s.noteSideLines(ctx, n.Address, n.Side)
		if lerr != nil {
			lines = nil // unreadable target = gone = orphaned
		}
		cache[key] = lines
		return lines
	}

	keep := func(n model.Note) bool {
		if !cutoff.IsZero() && n.Created.Before(cutoff) {
			return false
		}
		if n.IsReply() {
			return true // a reply lives or dies with its root (dropOrphanReplies)
		}
		status, _ := resolveOne(n, sideOf(n))
		return status == model.NoteActive
	}
	return st.Sweep(keep)
}
