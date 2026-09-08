package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
	"github.com/homeend/gigagit/internal/observ"
)

// NOTE: no t.Parallel() anywhere in this file — notes.Now is a package var.

func TestSweepDropsExpiredStaleAndOrphaned(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	ctx := context.Background()

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	// The index blob the ACTIVE note anchors on. The span name is "git show"
	// (repo.ShowFile); "git -C show" is ShowFileInDir, which this path never uses.
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\nbeta\n"})

	keep, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{2, 2},
		Summary:     "live",
		ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	stale, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{2, 2},
		Summary:     "text is gone",
		ContextHash: model.NoteContextHash([]string{"vanished"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	// An EXPIRED note: created 40 days ago, max_age_days = 30.
	notes.Now = func() time.Time { return base.AddDate(0, 0, -40) }
	old, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{2, 2},
		Summary:     "ancient",
		ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return base }

	dropped, err := svc.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("sweepNotes: %v", err)
	}
	if dropped != 2 {
		t.Fatalf("dropped = %d, want 2 (stale + expired)", dropped)
	}
	left, _ := svc.notesStore(ctx).Load()
	if len(left) != 1 || left[0].ID != keep.ID {
		t.Fatalf("sweep left %+v (stale %s, expired %s)", left, stale.ID, old.ID)
	}
}

// sweepRepo is a real repo with one tracked, committed file the sweep can read
// through ShowFile/WorktreeFile.
func sweepRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("alpha\nbeta\ngamma\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("init", "-b", "main")
	run("add", "a.go")
	run("commit", "-m", "init")
	return dir
}

// The housekeeping contract against REAL git: an expired note, a stale note
// (its anchor text is gone) and an orphaned note (its side does not exist at
// all) are all dropped, while an active note survives untouched.
func TestSweepOnARealRepoDropsExpiredStaleAndOrphaned(t *testing.T) {
	dir := sweepRepo(t)
	svc := New(&git.Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(50))})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	ctx := context.Background()

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	wt := func(path string) model.FileAddress {
		return model.FileAddress{State: model.StateUnstaged, Worktree: dir, Path: path}
	}
	add := func(n model.Note) model.Note {
		t.Helper()
		got, err := svc.NoteAdd(ctx, n)
		if err != nil {
			t.Fatalf("NoteAdd(%s): %v", n.Summary, err)
		}
		return got
	}

	active := add(model.Note{
		Address: wt("a.go"), Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "live", ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	stale := add(model.Note{
		Address: wt("a.go"), Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "anchor text is gone", ContextHash: model.NoteContextHash([]string{"vanished"}),
	})
	// StateUntracked has NO old side at all — the note can never re-anchor.
	orphan := add(model.Note{
		Address: model.FileAddress{State: model.StateUntracked, Worktree: dir, Path: "a.go"},
		Side:    model.NoteSideOld, Range: [2]int{1, 1},
		Summary: "side does not exist", ContextHash: model.NoteContextHash([]string{"alpha"}),
	})
	notes.Now = func() time.Time { return base.AddDate(0, 0, -40) }
	expired := add(model.Note{
		Address: wt("a.go"), Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "ancient but anchored", ContextHash: model.NoteContextHash([]string{"alpha"}),
	})
	notes.Now = func() time.Time { return base }

	dropped, err := svc.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("sweepNotes: %v", err)
	}
	if dropped != 3 {
		t.Fatalf("dropped = %d, want 3 (stale %s + orphan %s + expired %s)",
			dropped, stale.ID, orphan.ID, expired.ID)
	}
	left, err := svc.notesStore(ctx).Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(left) != 1 || left[0].ID != active.ID {
		t.Fatalf("sweep left %+v, want only the active note %s", left, active.ID)
	}
}

func TestSweepKeepsEverythingWhenAgeIsNonPositive(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(-1, 2000) // keep forever
	ctx := context.Background()
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\n"})

	real := notes.Now
	notes.Now = func() time.Time { return time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC) }
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{1, 1},
		Summary:     "ancient but kept",
		ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC) }
	defer func() { notes.Now = real }()

	if dropped, err := svc.sweepNotes(ctx); err != nil || dropped != 0 {
		t.Fatalf("max_age_days <= 0 must keep everything: dropped %d err %v", dropped, err)
	}
}

// An UNSET (zero) policy value means the built-in default, not "forever":
// config's zero-is-unset overlay reserves -1 for "keep forever".
func TestSweepZeroPolicyUsesTheBuiltInDefaults(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(0, 0)
	ctx := context.Background()
	f.SetResponse("git show", gitexec.Result{Stdout: "alpha\n"})

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base.AddDate(0, 0, -40) }
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{1, 1},
		Summary:     "ancient",
		ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	if dropped, err := svc.sweepNotes(ctx); err != nil || dropped != 1 {
		t.Fatalf("an unset max_age_days must fall back to 30 days: dropped %d err %v", dropped, err)
	}
}

func TestStartNotesSweepRunsOnce(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	svc.StartNotesSweep()
	svc.StartNotesSweep()
	svc.waitNotesSweepForTest() // blocks until the goroutine(s) finish
	if n := svc.notesSweepRunsForTest(); n != 1 {
		t.Fatalf("sweep ran %d times, want 1", n)
	}
}
