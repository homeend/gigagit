package domain

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/config"
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

	// The index blob the ACTIVE note anchors on. Worktree-state notes read
	// THEIR OWN checkout, so the span is "git -C show" (repo.ShowFileInDir);
	// plain "git show" is the commit-note path, which this test never takes.
	f.SetResponse("git -C show", gitexec.Result{Stdout: "alpha\nbeta\n"})

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
// through the worktree-scoped side reads (ShowFileInDir / worktreeFileIn).
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
	f.SetResponse("git -C show", gitexec.Result{Stdout: "alpha\n"})

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
	f.SetResponse("git -C show", gitexec.Result{Stdout: "alpha\n"})

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

// A pass that is CANCELLED (a quit, or the 30 s deadline behind a startup
// pull holding a TreeWrite) must abort before it touches the store: failing
// every read is not evidence that every note is orphaned.
func TestSweepAbortsWhenTheReadFailsAndTheContextIsDone(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	addr := model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"}
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "would look orphaned", ContextHash: model.NoteContextHash([]string{"beta"}),
	}); err != nil {
		t.Fatal(err)
	}
	// An expired note too: even a drop the sweep decided WITHOUT reading must
	// not reach the store once the pass is aborting.
	notes.Now = func() time.Time { return base.AddDate(0, 0, -40) }
	if _, err := svc.NoteAdd(context.Background(), model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "ancient", ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}
	notes.Now = func() time.Time { return base }

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.SetHandler("git -C show", func(context.Context, []string) (gitexec.Result, error) {
		cancel() // the pass is torn down mid-read
		return gitexec.Result{}, context.Canceled
	})

	dropped, err := svc.sweepNotes(ctx)
	if err == nil {
		t.Fatal("a cancelled pass must report the failure, not a clean sweep")
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0 — a cancelled pass must change nothing", dropped)
	}
	left, lerr := svc.notesStore(ctx).Load()
	if lerr != nil || len(left) != 2 {
		t.Fatalf("the store must be untouched: %d notes, err %v", len(left), lerr)
	}
}

// A read that merely FAILED (permissions, a disk error, a parked reservation)
// says nothing about the note: keep it. Only an ABSENT target orphans a note.
func TestSweepKeepsANoteWhoseReadMerelyFailed(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	svc.SetNotesPolicy(30, 2000)
	ctx := context.Background()

	real := notes.Now
	notes.Now = func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	defer func() { notes.Now = real }()

	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "unreadable, not gone", ContextHash: model.NoteContextHash([]string{"beta"}),
	}); err != nil {
		t.Fatal(err)
	}
	f.SetError("git -C show", errors.New("input/output error"))

	dropped, err := svc.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("an unreadable target must not fail the pass: %v", err)
	}
	if dropped != 0 {
		t.Fatalf("dropped = %d, want 0 — an I/O failure is not proof of an orphan", dropped)
	}
	if left, _ := svc.notesStore(ctx).Load(); len(left) != 1 {
		t.Fatalf("the note must survive an unreadable read, got %+v", left)
	}
}

// noteTargetGone is what licenses a deletion: it must recognise git's and the
// OS's "absent" wording and NOTHING else.
func TestNoteTargetGoneClassification(t *testing.T) {
	gone := []error{
		errors.New("git -C show failed (exit 128): fatal: path 'a.go' does not exist in 'HEAD'"),
		errors.New("git show failed (exit 128): fatal: invalid object name 'deadbeef'."),
		errors.New("git -C show failed (exit 128): fatal: cannot change to '/gone': No such file or directory"),
		fmt.Errorf("read a.go: %w", fs.ErrNotExist),
	}
	for _, err := range gone {
		if !noteTargetGone(err) {
			t.Errorf("must classify as ABSENT: %v", err)
		}
	}
	kept := []error{
		errors.New("input/output error"),
		errors.New("git -C show failed (exit 128): fatal: unable to read /x: permission denied"),
		fmt.Errorf("showfile cancelled: %w", context.Canceled),
		context.DeadlineExceeded,
	}
	for _, err := range kept {
		if noteTargetGone(err) {
			t.Errorf("must NOT be treated as ABSENT (would delete the note): %v", err)
		}
	}
}

// sweepHookStore runs a callback the first time Sweep is called — the seam for
// "another writer landed a note between the snapshot and the rewrite".
type sweepHookStore struct {
	notes.Store
	once   sync.Once
	before func()
}

func (s *sweepHookStore) Sweep(keep func(model.Note) bool) (int, error) {
	s.once.Do(s.before)
	return s.Store.Sweep(keep)
}

// The predicate handed to Sweep defaults to KEEP for ids the snapshot never
// saw, so a note written while the pass was resolving is not deleted for being
// unknown.
func TestSweepKeepsANoteAddedDuringThePass(t *testing.T) {
	f := gitexec.NewFakeRunner()
	svc := New(&git.Repo{Runner: f})
	fs := notes.NewFileStore(t.TempDir())
	svc.SetNotesStore(fs)
	svc.SetNotesPolicy(30, 2000)
	ctx := context.Background()

	real := notes.Now
	base := time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC)
	notes.Now = func() time.Time { return base }
	defer func() { notes.Now = real }()

	f.SetResponse("git -C show", gitexec.Result{Stdout: "alpha\nbeta\n"})
	addr := model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"}
	keep, err := svc.NoteAdd(ctx, model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "live", ContextHash: model.NoteContextHash([]string{"beta"}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.NoteAdd(ctx, model.Note{
		Address: addr, Side: model.NoteSideNew, Range: [2]int{2, 2},
		Summary: "stale", ContextHash: model.NoteContextHash([]string{"vanished"}),
	}); err != nil {
		t.Fatal(err)
	}

	// Deliberately un-anchorable: a predicate that RESOLVED inside Sweep (the
	// pre-fix shape) would call this stale and delete it. The pure id-set
	// predicate keeps it — the next pass, whose snapshot includes it, decides.
	late := model.Note{
		ID: "latecomr", Address: addr, Side: model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "written mid-pass", Source: model.NoteSourceUser,
		ContextHash: model.NoteContextHash([]string{"never in the file"}),
		Created:     base, Updated: base,
	}
	svc.SetNotesStore(&sweepHookStore{Store: fs, before: func() {
		if err := fs.Put(late); err != nil {
			t.Errorf("mid-pass Put: %v", err)
		}
	}})

	dropped, err := svc.sweepNotes(ctx)
	if err != nil {
		t.Fatalf("sweepNotes: %v", err)
	}
	if dropped != 1 {
		t.Fatalf("dropped = %d, want 1 (only the stale note)", dropped)
	}
	left, _ := fs.Load()
	ids := map[string]bool{}
	for _, n := range left {
		ids[n.ID] = true
	}
	if len(left) != 2 || !ids[keep.ID] || !ids[late.ID] {
		t.Fatalf("a note added during the pass must survive; left %+v", left)
	}
}

// The built-in budget is the CONFIG default, never a second hardcoded copy.
func TestNotesDefaultsTrackConfig(t *testing.T) {
	want := config.Defaults().Notes
	if notesDefaults.MaxAgeDays != want.MaxAgeDays || notesDefaults.MaxEntries != want.MaxEntries {
		t.Fatalf("notesDefaults = %+v, want config's %+v", notesDefaults, want)
	}
}

// NotesDisabled switches the whole surface off for a test binary that cannot
// import internal/notes; UseNotesDir opts one Service back in.
func TestNotesDisabledSeamAndUseNotesDir(t *testing.T) {
	NotesDisabled = true
	defer func() { NotesDisabled = false }()
	ctx := context.Background()

	off := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	if st := off.notesStore(ctx); st != nil {
		t.Fatal("NotesDisabled must yield no store")
	}
	if _, err := off.sweepNotes(ctx); !errors.Is(err, ErrNotesDisabled) {
		t.Fatalf("a disabled sweep must be a silent no-op, got %v", err)
	}

	on := New(&git.Repo{Runner: gitexec.NewFakeRunner()})
	on.UseNotesDir(t.TempDir())
	if _, err := on.NoteAdd(ctx, model.Note{
		Address: model.FileAddress{State: model.StateStaged, Worktree: "/wt", Path: "a.go"},
		Side:    model.NoteSideNew, Range: [2]int{1, 1},
		Summary: "opted in", ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatalf("UseNotesDir must override NotesDisabled: %v", err)
	}
}

// A stored path is DATA: it must not be able to address a file outside the
// checkout it names.
func TestWorktreeJoinRejectsAnEscapingPath(t *testing.T) {
	root := filepath.Join(t.TempDir(), "wt")
	if _, err := worktreeJoin(root, "../../etc/shadow"); err == nil {
		t.Fatal("worktreeJoin must reject a path that climbs out of the worktree")
	}
	got, err := worktreeJoin(root, "a/b.go")
	if err != nil || got != filepath.Join(root, "a", "b.go") {
		t.Fatalf("worktreeJoin(%q) = %q, %v", "a/b.go", got, err)
	}
}

// NoteCounts must not fail when the checkout cannot be resolved: commit badges
// need no worktree at all — only ByPath does.
func TestNoteCountsSurvivesAFailedTopLevel(t *testing.T) {
	f := gitexec.NewFakeRunner()
	f.SetError("git rev-parse (toplevel)", errors.New("fatal: not a git repository"))
	svc := New(&git.Repo{Runner: f})
	svc.SetNotesStore(notes.NewFileStore(t.TempDir()))
	ctx := context.Background()

	real := notes.Now
	notes.Now = func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	defer func() { notes.Now = real }()

	if _, err := svc.NoteAdd(ctx, model.Note{
		Address:     model.FileAddress{State: model.StateCommitted, Commit: "abc1234", Path: "a.go"},
		Side:        model.NoteSideNew,
		Range:       [2]int{1, 1},
		Summary:     "on a commit",
		ContextHash: model.NoteContextHash([]string{"alpha"}),
	}); err != nil {
		t.Fatal(err)
	}

	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatalf("NoteCounts must not fail without a worktree: %v", err)
	}
	if c.ByCommit["abc1234"] != 1 || c.ByCommitPath["abc1234:a.go"] != 1 {
		t.Fatalf("commit badges must still populate: %+v", c)
	}
	if len(c.ByPath) != 0 {
		t.Fatalf("ByPath must be empty without a resolvable checkout: %+v", c.ByPath)
	}
}
