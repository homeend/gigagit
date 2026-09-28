# AI Reviews as Notes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule:** NEVER use subagents here — this plan is executed inline by the session that wrote it (superpowers:executing-plans).

**Goal:** Every commit/range/branch AI review is written to, and read from, the per-repo note store as a path-less `review` note on its commit (plus the branch name for a branch review); the `reviews/`, `task-results/` and `.result` copies of reviews disappear.

**Architecture:** `internal/notes` learns three store rules for path-less notes (not capped, `ErrCorrupt` sentinel, `Quarantine`). `internal/domain` gets one typed write model (`SaveReview`, randomized-retry, corrupt-file quarantine) and a read model (`Review`, `ReviewsForCommit`, `ReviewsForBranch`, `Reviews`, `ReviewHeads` in `NoteCounts`). The task scheduler stores a review's result through a `TaskSpec.Store` hook instead of the history's `.result`; web and CLI call the same writer. The TUI reads reviews through a new `srcNote` viewer source, a virtual `@notes/` directory in a commit's file list (which the stack inherits), Branches-tab sub-rows, and View all notes.

**Tech Stack:** Go 1.26, Bubble Tea, go-toml/v2, real `git` in `t.TempDir()` tests.

**Spec:** `docs/superpowers/specs/2026-09-27-review-notes-design.md`

## Global Constraints

- A review note = `Address{State: StateCommitted, Commit: <full sha>, Path: "", Branch: <name or "">}` + `Tags: ["review"]` + `Source: agent`. `Address.Branch` alone NEVER marks a review (TUI working-tree line notes set it).
- Review notes: no stale sweep, no age limit, no entry cap.
- No fallback to files for commit/range/branch reviews: no `.result`, no `task-results/` copy, no `reviews/` report. Working-changes reviews (`ReviewWorking`) keep today's `.result` + viewer copy and save no note.
- Save retry: 15 s budget, random sleep 500 ms–2 s between attempts, only on `filelock.ErrHeld`; a corrupt `notes.toml` is moved to `notes.toml.corrupt-<unix>` and the review saved into a fresh store (warning returned).
- Deleting a branch in gg (`DeleteBranch`, `DeleteRemoteBranch`) deletes its review notes; `RenameBranch` renames `Address.Branch` on them. Line notes are never touched.
- Branches tab: review sub-rows ALWAYS shown (no fold); →/←/enter on a branch row keep today's meaning.
- Every user-visible TUI string through `i18n.T` with the key in all four bundles (ja/ko/zh/ru); engine/CLI prose stays English.
- `internal/tui`, `internal/cli`, `internal/web` never import `internal/git` or `internal/notes`.
- TUI and web test suites run with `domain.NotesDisabled = true`; a test that needs reviews calls `svc.UseNotesDir(t.TempDir())`.
- New tests call `t.Parallel()` unless they set a package global (`notes.Now`, `reviewSleep`, `NotesStatePath`).
- Commit messages end with the session's `Co-Authored-By` / `Claude-Session` trailers.

## Review Focus

1. **A branch review launched on a sha, not a branch name** (the Commits `.` menu "Review branch…" passes a tip that may be a sha): `ReviewTarget.Branch` must stay empty and the note is a plain commit review — covered in Task 3 (`TestBranchReviewTargetShaHasNoBranch`).
2. **Deleting a branch whose name also appears on a working-tree line note** (TUI `noteAddr` sets `Branch`): the line note must survive — Task 8 (`TestDeleteBranchKeepsLineNotes`).
3. **An interactive review that reports twice** (the agent rewrites `$GG_MESSAGE_FILE`): one note, updated in place, same id — Task 4 (`TestInteractiveReviewUpdatesOneNote`).
4. **A Branches-tab action on a review sub-row** (d, R, space, enter): refused like a Worktrees session row, never acting on the wrong branch — Task 10 (`TestBranchActionOnReviewRowIsRefused`).
5. **A missing reviewed commit** (gc'd after a rebase): the review is kept (sweep skips path-less), shown under the missing commit in View all notes and openable in the viewer — Tasks 2 and 11 (`TestSweepKeepsPathlessNoteOnMissingCommit`, `TestAllNotesOpensReviewOfMissingCommit`).

---

## File structure

| File | Change |
|---|---|
| `internal/filelock/filelock.go` | `ErrHeld` sentinel, wrapped in the deadline error |
| `internal/notes/file_store.go` | `ErrCorrupt` sentinel; cap skips path-less roots; `Quarantine()` |
| `internal/model/note.go` | `Note.Scope` field; `IsReviewNote()` |
| `internal/domain/notes_sweep.go` | age + resolve skip path-less notes |
| `internal/domain/review.go` | `ReviewTarget.Commit/Branch`; `ReviewReport*` save a note, `ReviewResult.NoteID` replaces `Path`; `writeReviewReport`/`SaveReviewReport` deleted |
| `internal/domain/review_notes.go` (new) | write + read models, retry, quarantine, kind, branch follow-ups |
| `internal/domain/notes.go` | `NoteCounts.Reviews []ReviewHead` |
| `internal/domain/notes_overview.go` | `NoteCommitNotes.Reviews` |
| `internal/domain/task_kinds.go`, `tasks.go` | `TaskSpec.Store`, `TaskInfo.NoteID/SaveErr`, `RetrySave` |
| `internal/taskhist/taskhist.go` | `Record.NoteID` |
| `internal/domain/service.go` | branch follow-ups after a successful op |
| `internal/cli/review.go` | `note: <id>` |
| `internal/web/review.go`, `static/review.js` | `noteId` instead of `path` |
| `internal/tui/open_file.go`, `open_files.go`, `file_viewer.go`, `sessions_popup.go`, `task_result_view.go` | `srcNote` |
| `internal/tui/review.go`, `task_tab.go` | open reviews by note id; Retry save |
| `internal/tui/files_view.go`, `diff_view.go`, `file_preview.go` | `@notes/` virtual entries |
| `internal/tui/view.go`, `viewstate.go`, `branch_reviews.go` (new), `action_menu.go` | Branches sub-rows, marker, "Show review" |
| `internal/tui/all_notes_popup.go` | reviews under commits |
| `internal/i18n/*.toml` | new keys ×4 |
| `e2e/scenarios/review-note-branch.toml` (new) | branch review → note → delete branch |
| docs | CHANGELOG, README, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md` (+ `Version`) |

---

### Task 1: Store primitives — `ErrHeld`, `ErrCorrupt`, uncapped path-less notes, `Quarantine`, `Note.Scope`

**Files:**
- Modify: `internal/filelock/filelock.go:22-95`
- Modify: `internal/notes/file_store.go:53-70` (read), `:244-278` (capOldestFirst), new `Quarantine`
- Modify: `internal/model/note.go:69-87`
- Test: `internal/filelock/filelock_test.go`, `internal/notes/file_store_test.go`, `internal/model/note_test.go`

**Interfaces:**
- Produces: `filelock.ErrHeld error`; `notes.ErrCorrupt error`; `func (fs *FileStore) Quarantine() (string, error)` (moved-to path; `""` when there was no file); `model.Note.Scope string` (`toml:"scope,omitempty"`); `func (n Note) IsCommitLevel() bool` (commit set, path empty); `func (n Note) IsReviewNote() bool` (commit-level and tagged `review`); `const model.ReviewTag = "review"`.

- [ ] **Step 1: Write the failing tests**

`internal/filelock/filelock_test.go` — append:
```go
func TestAcquireHeldLockWrapsErrHeld(t *testing.T) {
	t.Parallel()
	p := filepath.Join(t.TempDir(), "x.lock")
	release, err := Acquire(p)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	_, err = Acquire(p) // waits Wait, then gives up
	if !errors.Is(err, ErrHeld) {
		t.Fatalf("err = %v, want errors.Is(err, ErrHeld)", err)
	}
}
```

`internal/notes/file_store_test.go` — append:
```go
func TestReadCorruptWrapsErrCorrupt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.toml"), []byte("notes = [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewFileStore(dir).Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestCapNeverDropsCommitLevelNotes(t *testing.T) {
	t.Parallel()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	review := model.Note{ID: "r1", Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("a", 40)},
		Tags: []string{model.ReviewTag}, Created: old}
	line := func(id string, h int) model.Note {
		return model.Note{ID: id, Address: model.FileAddress{State: model.StateCommitted, Commit: strings.Repeat("b", 40), Path: "f.go"},
			Created: old.Add(time.Duration(h) * time.Hour)}
	}
	got := capOldestFirst([]model.Note{review, line("l1", 1), line("l2", 2), line("l3", 3)}, 2)
	ids := []string{}
	for _, n := range got {
		ids = append(ids, n.ID)
	}
	if !slices.Contains(ids, "r1") {
		t.Fatalf("cap dropped the review note: kept %v", ids)
	}
	if len(ids) != 3 || slices.Contains(ids, "l1") {
		t.Fatalf("kept %v, want r1 + the two newest line notes", ids)
	}
}

func TestQuarantineMovesTheFileAside(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "notes.toml"), []byte("garbage [[["), 0o644); err != nil {
		t.Fatal(err)
	}
	fs := NewFileStore(dir)
	moved, err := fs.Quarantine()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(filepath.Base(moved), "notes.toml.corrupt-") {
		t.Fatalf("moved to %q", moved)
	}
	if b, _ := os.ReadFile(moved); string(b) != "garbage [[[" {
		t.Fatalf("moved file content = %q", b)
	}
	if ns, err := fs.Load(); err != nil || len(ns) != 0 {
		t.Fatalf("after quarantine Load = %v, %v; want empty, nil", ns, err)
	}
}
```

`internal/model/note_test.go` — append:
```go
func TestIsReviewNote(t *testing.T) {
	t.Parallel()
	c := FileAddress{State: StateCommitted, Commit: strings.Repeat("a", 40)}
	cases := []struct {
		n    Note
		want bool
	}{
		{Note{Address: c, Tags: []string{ReviewTag}}, true},
		{Note{Address: c}, false}, // commit-level, not a review
		{Note{Address: FileAddress{State: StateCommitted, Commit: c.Commit, Path: "x"}, Tags: []string{ReviewTag}}, false},
		{Note{Address: FileAddress{State: StateUnstaged, Branch: "main", Path: "x"}}, false},
	}
	for i, tc := range cases {
		if got := tc.n.IsReviewNote(); got != tc.want {
			t.Errorf("case %d: IsReviewNote = %v, want %v", i, got, tc.want)
		}
	}
}
```
(Add missing imports: `errors`, `slices`, `strings`, `time`, `path/filepath`, `os` as each file needs.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/filelock ./internal/notes ./internal/model -run 'ErrHeld|ErrCorrupt|CommitLevel|Quarantine|IsReviewNote' 2>&1 | tail -20`
Expected: build failures — `undefined: ErrHeld`, `undefined: ErrCorrupt`, `fs.Quarantine undefined`, `undefined: ReviewTag`.

- [ ] **Step 3: Implement**

`internal/filelock/filelock.go`: add after the const block
```go
// ErrHeld is wrapped by Acquire's give-up error: another holder kept the
// lock past Wait. Callers with a longer budget of their own retry on it.
var ErrHeld = errors.New("filelock: lock is held")
```
and change the deadline return to
```go
return nil, fmt.Errorf("%w: %s; try again (last: %v)", ErrHeld, path, last)
```

`internal/notes/file_store.go`: add
```go
// ErrCorrupt is wrapped by a read of a notes.toml that is not valid TOML.
var ErrCorrupt = errors.New("notes: store is corrupt")
```
(import `errors`, `strconv`), change read's unmarshal error to
```go
return nil, fmt.Errorf("%w: %s: %v", ErrCorrupt, fs.path(), err)
```
In `capOldestFirst`, count and drop only roots that are not commit-level:
```go
func capOldestFirst(ns []model.Note, max int) []model.Note {
	if max <= 0 {
		return ns
	}
	// Commit-level notes (AI reviews) are exempt: they are not counted and
	// never dropped (spec §2 "Store rules for path-less notes").
	exempt := map[string]bool{}
	for _, n := range ns {
		if !n.IsReply() && n.IsCommitLevel() {
			exempt[n.ID] = true
		}
	}
	size := 0
	for _, n := range ns {
		if !exempt[n.ID] && !exempt[n.ParentID] {
			size++
		}
	}
	if size <= max {
		return ns
	}
	roots := make([]model.Note, 0, len(ns))
	for _, n := range ns {
		if !n.IsReply() && !exempt[n.ID] {
			roots = append(roots, n)
		}
	}
	// … the existing oldest-first loop over roots, decrementing size, unchanged …
}
```
Add:
```go
// Quarantine moves a notes.toml aside to notes.toml.corrupt-<unix> under the
// store's locks and reports where it went ("" when there was no file). The
// next write starts a fresh store; nothing is deleted.
func (fs *FileStore) Quarantine() (string, error) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	unlock, err := fs.lock()
	if err != nil {
		return "", err
	}
	defer unlock()
	if _, err := os.Stat(fs.path()); os.IsNotExist(err) {
		return "", nil
	}
	dst := fs.path() + ".corrupt-" + strconv.FormatInt(Now().Unix(), 10)
	if err := os.Rename(fs.path(), dst); err != nil {
		return "", err
	}
	return dst, nil
}
```

`internal/model/note.go`: add field after `Confidence`:
```go
	// Scope is the reviewed range (hex "a..b") of a review note; "" otherwise.
	Scope string `toml:"scope,omitempty"`
```
and:
```go
// ReviewTag marks a commit-level note that holds an AI review.
const ReviewTag = "review"

// IsCommitLevel reports a note about a whole commit: a commit and no path.
func (n Note) IsCommitLevel() bool {
	return n.Address.State == StateCommitted && n.Address.Commit != "" && n.Address.Path == ""
}

// IsReviewNote reports a commit-level note tagged ReviewTag — the ONLY test
// for "this is a review"; Address.Branch alone proves nothing.
func (n Note) IsReviewNote() bool { return n.IsCommitLevel() && slices.Contains(n.Tags, ReviewTag) }
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/filelock ./internal/notes ./internal/model 2>&1 | tail -5`
Expected: `ok` ×3.

- [ ] **Step 5: Commit**
```bash
git add internal/filelock internal/notes internal/model
git commit -m "feat(notes): commit-level notes skip the cap; ErrHeld/ErrCorrupt; Quarantine; Note.Scope"
```

---

### Task 2: The sweep keeps commit-level notes

**Files:**
- Modify: `internal/domain/notes_sweep.go:151-170` (phase-1 loop)
- Test: `internal/domain/notes_sweep_test.go`

**Interfaces:**
- Consumes: `model.Note.IsCommitLevel()` (Task 1).

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/notes_sweep_test.go` (serial: sets `notes.Now`):
```go
func TestSweepKeepsPathlessNoteOnMissingCommit(t *testing.T) {
	dir, svc := newRealRepo(t)
	_ = dir
	store := notes.NewFileStore(t.TempDir())
	svc.SetNotesStore(store)
	gone := strings.Repeat("d", 40) // never existed: a gc'd commit
	old := time.Now().UTC().AddDate(-5, 0, 0)
	if err := store.Put(model.Note{ID: "rev1", Source: model.NoteSourceAgent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: gone},
		Tags:    []string{model.ReviewTag}, Summary: "Review", Created: old, Updated: old}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.sweepNotes(context.Background()); err != nil {
		t.Fatal(err)
	}
	all, _ := store.Load()
	if len(all) != 1 || all[0].ID != "rev1" {
		t.Fatalf("sweep dropped the review note: %v", all)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain -run TestSweepKeepsPathlessNoteOnMissingCommit`
Expected: FAIL `sweep dropped the review note: []` (age limit drops it first).

- [ ] **Step 3: Implement**

In `sweepNotes` phase 1, first line of the loop body:
```go
		// A commit-level note (an AI review) has no line to re-anchor and never
		// expires (spec §2): a missing commit shows it as missing, it is not
		// deleted behind the user's back.
		if n.IsCommitLevel() {
			continue
		}
```
(Replies to a commit-level root: `IsCommitLevel` is false for them only if their Address differs; `NoteReply` copies the root's address, so they are skipped too.)

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/domain -run 'Sweep' 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**
```bash
git add internal/domain/notes_sweep.go internal/domain/notes_sweep_test.go
git commit -m "feat(domain): the note sweep never drops a commit-level note"
```

---

### Task 3: Review write + read models

**Files:**
- Create: `internal/domain/review_notes.go`, `internal/domain/review_notes_test.go`
- Modify: `internal/domain/review.go` (`ReviewTarget` fields; `BranchReviewTarget`; the commit/range constructors fill `Commit`)
- Modify: `internal/domain/notes.go` (`NoteCounts.Reviews`)

**Interfaces:**
- Consumes: Task 1 (`ErrHeld`, `ErrCorrupt`, `Quarantine`, `IsReviewNote`, `Scope`).
- Produces:
```go
type ReviewKind int // ReviewOnCommit | ReviewOnBranch | ReviewWasTip
type SaveReview struct{ Target ReviewTarget; Agent, Text, NoteID string }
type Review struct{ ID string; Kind ReviewKind; Commit, Branch, Scope, Agent, Summary, Text string; Created, Updated time.Time }
type ReviewHead struct{ ID, Commit, Branch, Agent, Summary string; Created time.Time }
var ErrNoReviewCommit, ErrReviewNotFound error
func (s *Service) SaveReview(ctx context.Context, cmd SaveReview) (id string, warn string, err error)
func (s *Service) Review(ctx context.Context, id string) (Review, error)
func (s *Service) ReviewsForCommit(ctx context.Context, sha string) ([]Review, error)
func (s *Service) ReviewsForBranch(ctx context.Context, name string) ([]Review, error)
func (s *Service) Reviews(ctx context.Context) ([]Review, error)
func ReviewKindOf(branch, commit, branchTip string) ReviewKind
// NoteCounts gains: Reviews []ReviewHead (every review note, newest first)
// ReviewTarget gains: Commit string, Branch string
```
Naming note: the existing `ReviewKind` type in `review.go` names TARGET kinds (`ReviewBranch/ReviewRange/ReviewWorking`). The new one is `ReviewNoteKind` with values `ReviewOnCommit/ReviewOnBranch/ReviewWasTip` — use that name everywhere below (`Review.Kind ReviewNoteKind`, `ReviewKindOf(...) ReviewNoteKind`).

- [ ] **Step 1: Write the failing tests** (`internal/domain/review_notes_test.go`)

```go
package domain

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

func reviewRepo(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "f.txt", "x\n", "feature commit")
	tip := revParse(t, dir, "feature")
	return dir, svc, tip
}

func revParse(t *testing.T, dir, rev string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "rev-parse", rev).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func TestBranchReviewTargetFillsCommitAndBranch(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	tg, err := svc.BranchReviewTarget(context.Background(), "feature")
	if err != nil {
		t.Fatal(err)
	}
	if tg.Commit != tip || tg.Branch != "feature" {
		t.Fatalf("Commit=%q Branch=%q, want %q feature", tg.Commit, tg.Branch, tip)
	}
}

func TestBranchReviewTargetShaHasNoBranch(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	tg, err := svc.BranchReviewTarget(context.Background(), tip)
	if err != nil {
		t.Fatal(err)
	}
	if tg.Branch != "" || tg.Commit != tip {
		t.Fatalf("Branch=%q Commit=%q, want empty branch on a sha", tg.Branch, tg.Commit)
	}
}

func TestSaveReviewBranchNoteAndKinds(t *testing.T) {
	t.Parallel()
	dir, svc, tip := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, warn, err := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "Claude Code", Text: "# Looks good\nbody"})
	if err != nil || warn != "" {
		t.Fatalf("SaveReview: %v %q", err, warn)
	}
	r, err := svc.Review(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != ReviewOnBranch || r.Commit != tip || r.Branch != "feature" || r.Text != "# Looks good\nbody" || r.Scope != tg.Range {
		t.Fatalf("review = %+v", r)
	}
	if got, _ := svc.ReviewsForBranch(ctx, "feature"); len(got) != 1 {
		t.Fatalf("ReviewsForBranch = %d, want 1", len(got))
	}
	// The branch moves on: the review stays on its commit as "was the tip".
	commitFile(t, dir, "g.txt", "y\n", "second")
	svc.InvalidateNoteCounts()
	r, _ = svc.Review(ctx, id)
	if r.Kind != ReviewWasTip {
		t.Fatalf("after the branch moved: kind %v, want ReviewWasTip", r.Kind)
	}
	if got, _ := svc.ReviewsForBranch(ctx, "feature"); len(got) != 0 {
		t.Fatalf("ReviewsForBranch after move = %d, want 0 (current tip only)", len(got))
	}
	if got, _ := svc.ReviewsForCommit(ctx, tip); len(got) != 1 {
		t.Fatalf("ReviewsForCommit = %d, want 1", len(got))
	}
}

func TestSaveReviewRefusesWorkingChanges(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	_, _, err := svc.SaveReview(context.Background(), SaveReview{Target: WorkingReviewTarget(), Text: "x"})
	if !errors.Is(err, ErrNoReviewCommit) {
		t.Fatalf("err = %v, want ErrNoReviewCommit", err)
	}
}

func TestSaveReviewUpdatesInPlace(t *testing.T) {
	t.Parallel()
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "one"})
	id2, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "two", NoteID: id})
	if err != nil || id2 != id {
		t.Fatalf("update: id %q→%q err %v", id, id2, err)
	}
	all, _ := svc.Reviews(ctx)
	if len(all) != 1 || all[0].Text != "two" {
		t.Fatalf("reviews = %+v, want one with text two", all)
	}
}

// Serial: swaps the retry seams.
func TestSaveReviewRetriesAHeldLock(t *testing.T) {
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	var sleeps []time.Duration
	restore := setReviewRetrySeams(func(d time.Duration) { sleeps = append(sleeps, d) }, 0)
	defer restore()
	fails := 2
	reviewPutHook = func() error {
		if fails > 0 {
			fails--
			return filelock.ErrHeld
		}
		return nil
	}
	defer func() { reviewPutHook = nil }()
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatalf("SaveReview: %v", err)
	}
	if len(sleeps) != 2 {
		t.Fatalf("slept %d times, want 2", len(sleeps))
	}
	for _, d := range sleeps {
		if d < 500*time.Millisecond || d > 2*time.Second {
			t.Fatalf("sleep %v outside 500ms–2s", d)
		}
	}
}

func TestSaveReviewGivesUpAfterTheBudget(t *testing.T) {
	_, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	var total time.Duration
	restore := setReviewRetrySeams(func(d time.Duration) { total += d }, 0)
	defer restore()
	reviewClock = func() time.Duration { return total } // elapsed = slept
	reviewPutHook = func() error { return filelock.ErrHeld }
	defer func() { reviewPutHook = nil }()
	_, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"})
	if !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("err = %v, want ErrHeld", err)
	}
	if total > reviewRetryBudget {
		t.Fatalf("slept %v, over the %v budget", total, reviewRetryBudget)
	}
}

func TestSaveReviewQuarantinesACorruptStore(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	notesDir := t.TempDir()
	svc.UseNotesDir(notesDir)
	runGitIn(t, dir, "checkout", "-b", "feature")
	commitFile(t, dir, "f.txt", "x\n", "c")
	if err := os.WriteFile(filepath.Join(notesDir, "notes.toml"), []byte("[[[ broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	tg, _ := svc.BranchReviewTarget(context.Background(), "feature")
	id, warn, err := svc.SaveReview(context.Background(), SaveReview{Target: tg, Text: "x"})
	if err != nil || id == "" {
		t.Fatalf("SaveReview: %q %v", id, err)
	}
	if !strings.Contains(warn, "notes.toml.corrupt-") {
		t.Fatalf("warn = %q, want the quarantine path", warn)
	}
	m, _ := filepath.Glob(filepath.Join(notesDir, "notes.toml.corrupt-*"))
	if len(m) != 1 {
		t.Fatalf("quarantined files = %v", m)
	}
}

func TestNoteCountsListsReviewHeads(t *testing.T) {
	t.Parallel()
	_, svc, tip := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Agent: "A", Text: "x"})
	c, err := svc.NoteCounts(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Reviews) != 1 || c.Reviews[0].ID != id || c.Reviews[0].Branch != "feature" || c.Reviews[0].Commit != tip {
		t.Fatalf("Reviews = %+v", c.Reviews)
	}
	if c.ByCommit[tip] != 1 {
		t.Fatalf("ByCommit[tip] = %d, want 1 (the ◆ badge counts reviews)", c.ByCommit[tip])
	}
}
```
(Add `"os/exec"` to the imports.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'Review(Target|Notes?)|SaveReview|NoteCountsListsReviewHeads' 2>&1 | tail -20`
Expected: build failure — `tg.Commit undefined`, `undefined: SaveReview`, `undefined: setReviewRetrySeams`, …

- [ ] **Step 3: Implement**

`internal/domain/review.go` — `ReviewTarget` gains:
```go
	// Commit is the full sha a review NOTE anchors to: the range's last
	// commit (a branch's tip). Branch is set only when the target was named
	// by a branch ref. Both are data, never spliced into a command.
	Commit string
	Branch string
```
In `BranchReviewTarget`, after `tipSHA` is resolved, compute the branch and fill both fields on every return:
```go
	branch := ""
	if tip != tipSHA && !isFullSHA(tip) {
		for _, ref := range []string{"refs/heads/" + tip, "refs/remotes/" + tip} {
			if _, found, _ := s.ResolveRev(ctx, ref); found {
				branch = tip
				break
			}
		}
	}
```
and `ReviewTarget{…, Commit: tipSHA, Branch: branch}` in all three returns. Find the commit/range constructors (`grep -n "ReviewTarget{Kind: ReviewRange" internal/`) — each must set `Commit` to the full sha of the range's right side: add a helper
```go
// FillReviewCommit resolves t.Commit from the right side of t.Range when a
// constructor left it empty (a typed range, a commit's sha^..sha).
func (s *Service) FillReviewCommit(ctx context.Context, t ReviewTarget) ReviewTarget {
	if t.Commit != "" || t.Kind == ReviewWorking || strings.TrimSpace(t.Range) == "" {
		return t
	}
	right := t.Range
	if _, r, ok := strings.Cut(t.Range, ".."); ok {
		right = strings.TrimPrefix(r, ".")
	}
	if sha, found, err := s.ResolveRev(ctx, right); err == nil && found {
		t.Commit = strings.TrimSpace(sha)
	}
	return t
}
```
`SaveReview` calls it first, so constructors need no change.

`internal/domain/notes.go`: `NoteCounts` gains
```go
	Reviews []ReviewHead // every review note, newest first (Branches tab, @notes)
```
and in the counting loop, before the `StateCommitted` branch:
```go
		if n.IsReviewNote() {
			c.Reviews = append(c.Reviews, ReviewHead{ID: n.ID, Commit: n.Address.Commit, Branch: n.Address.Branch,
				Agent: n.Author, Summary: n.Summary, Created: n.Created})
		}
```
and after the loop `sort.SliceStable(c.Reviews, func(a, b int) bool { return c.Reviews[a].Created.After(c.Reviews[b].Created) })`.

`internal/domain/review_notes.go`:
```go
package domain

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"time"

	"github.com/homeend/gigagit/internal/filelock"
	"github.com/homeend/gigagit/internal/model"
	"github.com/homeend/gigagit/internal/notes"
)

// AI reviews live in the note store as commit-level notes tagged "review"
// (spec 2026-09-27-review-notes-design.md). This file is the ONE writer and
// the read model every frontend uses; nothing outside domain knows a review
// is a note.

type ReviewNoteKind int

const (
	ReviewOnCommit ReviewNoteKind = iota // a commit or range review
	ReviewOnBranch                       // a branch review whose commit is still the branch tip
	ReviewWasTip                         // a branch review whose branch moved on or is gone
)

var (
	ErrNoReviewCommit = errors.New("review: working changes have no commit to attach a review to")
	ErrReviewNotFound = errors.New("review not found")
)

type SaveReview struct {
	Target ReviewTarget
	Agent  string // tool name → Note.Author
	Text   string
	NoteID string // non-empty: update this review in place
}

type Review struct {
	ID               string
	Kind             ReviewNoteKind
	Commit, Branch   string
	Scope            string
	Agent, Summary   string
	Text             string
	Created, Updated time.Time
}

type ReviewHead struct {
	ID, Commit, Branch, Agent, Summary string
	Created                            time.Time
}

// Retry seams: a held lock is retried within reviewRetryBudget, sleeping a
// random 500 ms–2 s between attempts so two gg processes drift apart.
const reviewRetryBudget = 15 * time.Second

var (
	reviewSleep   = time.Sleep
	reviewJitter  = func() time.Duration { return 500*time.Millisecond + rand.N(1500*time.Millisecond+1) }
	reviewClock   func() time.Duration // elapsed since the save began; nil = real clock
	reviewPutHook func() error         // tests: fail an attempt before the store is touched
)

// setReviewRetrySeams swaps the sleep (and, when fixed > 0, pins the jitter)
// for a test and returns the restore func. Tests using it run serially.
func setReviewRetrySeams(sleep func(time.Duration), fixed time.Duration) func() {
	s, j, c := reviewSleep, reviewJitter, reviewClock
	reviewSleep = sleep
	if fixed > 0 {
		reviewJitter = func() time.Duration { return fixed }
	}
	return func() { reviewSleep, reviewJitter, reviewClock = s, j, c }
}

// SaveReview writes (or, with NoteID, rewrites) a review note. warn is set
// when a corrupt store had to be moved aside first.
func (s *Service) SaveReview(ctx context.Context, cmd SaveReview) (string, string, error) {
	t := s.FillReviewCommit(ctx, cmd.Target)
	if t.Kind == ReviewWorking || t.Commit == "" {
		return "", "", ErrNoReviewCommit
	}
	st := s.notesStore(ctx)
	if st == nil {
		return "", "", ErrNotesDisabled
	}
	start := time.Now()
	elapsed := func() time.Duration {
		if reviewClock != nil {
			return reviewClock()
		}
		return time.Since(start)
	}
	warn := ""
	for {
		id, err := s.putReview(st, t, cmd)
		if err == nil {
			s.invalidateNoteCounts()
			return id, warn, nil
		}
		switch {
		case errors.Is(err, notes.ErrCorrupt) && warn == "":
			q, ok := st.(interface{ Quarantine() (string, error) })
			if !ok {
				return "", "", err
			}
			moved, qerr := q.Quarantine()
			if qerr != nil {
				return "", "", errors.Join(err, qerr)
			}
			warn = fmt.Sprintf("the note store was unreadable and was moved to %s", moved)
			continue
		case errors.Is(err, filelock.ErrHeld):
			d := reviewJitter()
			if elapsed()+d > reviewRetryBudget {
				return "", "", err
			}
			if ctx.Err() != nil {
				return "", "", ctx.Err()
			}
			reviewSleep(d)
			continue
		}
		return "", "", err
	}
}

// putReview is one attempt: read, build the note, Put.
func (s *Service) putReview(st notes.Store, t ReviewTarget, cmd SaveReview) (string, error) {
	if reviewPutHook != nil {
		if err := reviewPutHook(); err != nil {
			return "", err
		}
	}
	all, err := st.Load()
	if err != nil {
		return "", err
	}
	now := notes.Now().UTC()
	n := model.Note{ID: cmd.NoteID, Source: model.NoteSourceAgent, Author: cmd.Agent,
		Address: model.FileAddress{State: model.StateCommitted, Commit: t.Commit, Branch: t.Branch},
		Tags:    []string{model.ReviewTag}, Scope: t.Range,
		Summary: reviewSummary(t), Rationale: cmd.Text, Created: now, Updated: now}
	if n.ID == "" {
		n.ID = notes.NewID(all)
	} else {
		for _, old := range all {
			if old.ID == n.ID {
				n.Created = old.Created
			}
		}
	}
	return n.ID, st.Put(n)
}

// reviewSummary is "Review: <label> (<a7>..<b7>)".
func reviewSummary(t ReviewTarget) string {
	label := strings.TrimSpace(t.Branch)
	if label == "" {
		label = t.DisplayLabel()
	}
	if r := longHex.ReplaceAllStringFunc(t.Range, sha7); r != "" && r != label {
		return "Review: " + label + " (" + r + ")"
	}
	return "Review: " + label
}

// ReviewKindOf computes a review's kind from its stored branch and commit and
// the branch's current tip ("" = the branch is gone).
func ReviewKindOf(branch, commit, branchTip string) ReviewNoteKind {
	switch {
	case branch == "":
		return ReviewOnCommit
	case branchTip == commit:
		return ReviewOnBranch
	}
	return ReviewWasTip
}

func (s *Service) reviewOf(ctx context.Context, n model.Note, tips map[string]string) Review {
	tip, ok := tips[n.Address.Branch]
	if !ok && n.Address.Branch != "" {
		tip = s.branchTip(ctx, n.Address.Branch)
		tips[n.Address.Branch] = tip
	}
	return Review{ID: n.ID, Kind: ReviewKindOf(n.Address.Branch, n.Address.Commit, tip),
		Commit: n.Address.Commit, Branch: n.Address.Branch, Scope: n.Scope, Agent: n.Author,
		Summary: n.Summary, Text: n.Rationale, Created: n.Created, Updated: n.Updated}
}

// branchTip is a local or remote-tracking branch's full sha; "" when gone.
func (s *Service) branchTip(ctx context.Context, name string) string {
	for _, ref := range []string{"refs/heads/" + name, "refs/remotes/" + name} {
		if sha, found, err := s.ResolveRev(ctx, ref); err == nil && found {
			return strings.TrimSpace(sha)
		}
	}
	return ""
}

func (s *Service) reviewNotes(ctx context.Context) ([]model.Note, error) {
	st := s.notesStore(ctx)
	if st == nil {
		return nil, ErrNotesDisabled
	}
	all, err := st.Load()
	if err != nil {
		return nil, err
	}
	var out []model.Note
	for _, n := range all {
		if !n.IsReply() && n.IsReviewNote() {
			out = append(out, n)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Created.After(out[b].Created) })
	return out, nil
}

func (s *Service) Reviews(ctx context.Context) ([]Review, error) {
	ns, err := s.reviewNotes(ctx)
	tips := map[string]string{}
	out := make([]Review, 0, len(ns))
	for _, n := range ns {
		out = append(out, s.reviewOf(ctx, n, tips))
	}
	return out, err
}

func (s *Service) Review(ctx context.Context, id string) (Review, error) {
	ns, err := s.reviewNotes(ctx)
	if err != nil {
		return Review{}, err
	}
	for _, n := range ns {
		if n.ID == id {
			return s.reviewOf(ctx, n, map[string]string{}), nil
		}
	}
	return Review{}, ErrReviewNotFound
}

func (s *Service) ReviewsForCommit(ctx context.Context, sha string) ([]Review, error) {
	all, err := s.Reviews(ctx)
	var out []Review
	for _, r := range all {
		if r.Commit == sha {
			out = append(out, r)
		}
	}
	return out, err
}

// ReviewsForBranch lists the reviews of the branch's CURRENT tip (ruling 3).
func (s *Service) ReviewsForBranch(ctx context.Context, name string) ([]Review, error) {
	all, err := s.Reviews(ctx)
	var out []Review
	for _, r := range all {
		if r.Branch == name && r.Kind == ReviewOnBranch {
			out = append(out, r)
		}
	}
	return out, err
}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain -run 'Review|NoteCounts' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**
```bash
git add internal/domain
git commit -m "feat(domain): review write/read models over the note store"
```

---

### Task 4: The task scheduler stores reviews as notes

**Files:**
- Modify: `internal/domain/task_kinds.go:22-45` (`TaskSpec.Store`), `:137-152` (`ReviewTask`)
- Modify: `internal/domain/tasks.go:44-62` (`TaskInfo.NoteID`, `SaveErr`), `setResult` callers (`:240`, `:533`), `finish` (`:264-276`), `recordOf` (`:278`), new `RetrySave`
- Modify: `internal/taskhist/taskhist.go:23-38` (`Record.NoteID`)
- Test: `internal/domain/tasks_test.go` (or the existing task test file — `ls internal/domain/task*_test.go`)

**Interfaces:**
- Consumes: `SaveReview` (Task 3).
- Produces: `TaskSpec.Store func(ctx context.Context, noteID, text string) (string, string, error)`; `TaskInfo.NoteID string`, `TaskInfo.SaveErr string`; `taskhist.Record.NoteID string` (`toml:"note_id,omitempty"`); `func (m *TaskManager) RetrySave(id TaskID) error`.

- [ ] **Step 1: Write the failing tests**

Find the existing headless-task test helper first: `grep -n "func Test.*Headless\|fakeCapture\|TaskSpec{" internal/domain/tasks_test.go | head`. Using that helper's shape (a `TaskSpec` whose `Op` is a fake `engine.CaptureTask` returning a fixed `Captured`), add:
```go
func TestHeadlessReviewIsStoredNotRecorded(t *testing.T) {
	m := newTestTaskManager(t) // the file's existing constructor with a temp taskhist store
	var saved []string
	spec := fakeHeadlessSpec(t, "the review") // existing helper shape: Kind review, Captured "the review"
	spec.Store = func(_ context.Context, id, text string) (string, string, error) {
		saved = append(saved, id+"|"+text)
		return "n1", "", nil
	}
	info := runToEnd(t, m, spec)
	if info.State != TaskDone || info.NoteID != "n1" {
		t.Fatalf("state %v note %q", info.State, info.NoteID)
	}
	if len(saved) != 1 || saved[0] != "|the review" {
		t.Fatalf("Store calls = %v", saved)
	}
	rec := m.History()[0]
	if rec.NoteID != "n1" || rec.ResultFile != "" {
		t.Fatalf("record NoteID %q ResultFile %q; want n1 and no .result", rec.NoteID, rec.ResultFile)
	}
}

func TestInteractiveReviewUpdatesOneNote(t *testing.T) {
	m := newTestTaskManager(t)
	var ids []string
	task := &task{spec: TaskSpec{Kind: exttool.CatReview, Mode: TaskInteractive,
		Store: func(_ context.Context, id, _ string) (string, string, error) {
			ids = append(ids, id)
			return "n1", "", nil
		}}}
	m.setResult(task, "first")
	m.setResult(task, "second")
	if len(ids) != 2 || ids[0] != "" || ids[1] != "n1" {
		t.Fatalf("Store ids = %v, want [\"\" n1] (create, then update in place)", ids)
	}
}

func TestStoreFailureFailsTheTaskAndRetrySaveRecovers(t *testing.T) {
	m := newTestTaskManager(t)
	fail := true
	spec := fakeHeadlessSpec(t, "the review")
	spec.Store = func(context.Context, string, string) (string, string, error) {
		if fail {
			return "", "", errors.New("disk full")
		}
		return "n9", "", nil
	}
	info := runToEnd(t, m, spec)
	if info.State != TaskFailed || !strings.Contains(info.SaveErr, "disk full") || info.NoteID != "" {
		t.Fatalf("state %v SaveErr %q note %q", info.State, info.SaveErr, info.NoteID)
	}
	if rec := m.History()[0]; rec.ResultFile != "" {
		t.Fatalf("a failed save wrote a .result: %q", rec.ResultFile)
	}
	fail = false
	if err := m.RetrySave(info.ID); err != nil {
		t.Fatal(err)
	}
	got, _ := m.Get(info.ID)
	if got.NoteID != "n9" || got.State != TaskDone || got.SaveErr != "" {
		t.Fatalf("after retry: %+v", got)
	}
}
```
If `newTestTaskManager` / `fakeHeadlessSpec` / `runToEnd` do not exist under those names, write them in the test file from the existing tests' setup (Ruling-log the names used).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'ReviewIsStored|InteractiveReviewUpdates|StoreFailure' 2>&1 | tail -10`
Expected: build failure — `spec.Store undefined`, `info.NoteID undefined`, `m.RetrySave undefined`.

- [ ] **Step 3: Implement**

`TaskSpec` gains:
```go
	// Store, when set, persists each result (a review → a note) instead of
	// the history's .result file. noteID is "" on the first call and the id
	// it returned afterwards, so later results update the same note.
	Store func(ctx context.Context, noteID, text string) (id, warn string, err error)
```
`TaskInfo` gains `NoteID string` and `SaveErr string`. `taskhist.Record` gains `NoteID string \`toml:"note_id,omitempty"\``; `recordOf` copies `NoteID: info.NoteID`.

`setResult` becomes:
```go
func (m *TaskManager) setResult(t *task, result string) {
	m.mu.Lock()
	t.info.Result = result
	t.info.Results++
	if t.spec.Mode == TaskInteractive && t.info.State == TaskRunning {
		t.info.State = TaskResultReady
	}
	store, noteID := t.spec.Store, t.info.NoteID
	m.mu.Unlock()
	if store != nil {
		m.storeResult(t, store, noteID, result)
	}
	m.signal()
}

// storeResult runs the Store hook outside the lock (it may retry for 15 s).
func (m *TaskManager) storeResult(t *task, store func(context.Context, string, string) (string, string, error), noteID, text string) {
	id, _, err := store(context.Background(), noteID, text)
	m.mu.Lock()
	if err != nil {
		t.info.SaveErr = "review not saved: " + err.Error()
	} else {
		t.info.NoteID, t.info.SaveErr = id, ""
	}
	m.mu.Unlock()
}
```
In `finish`:
```go
	result := info.Result
	if t.spec.Store != nil {
		result = "" // stored as a note (or not at all): never a .result file
		if info.SaveErr != "" && end.state == TaskDone {
			info.State, info.Err = TaskFailed, info.SaveErr
		}
	}
	m.record(recordOf(info), result, end.tail)
```
(`info` is read under `m.mu` at the top of `finish`, so it sees `NoteID`/`SaveErr` set by `storeResult`, which ran before `runHeadless` returned.)

`RetrySave`:
```go
// RetrySave re-runs a failed review save from the result still in memory.
func (m *TaskManager) RetrySave(id TaskID) error {
	m.mu.Lock()
	var t *task
	for _, x := range m.tasks {
		if x.info.ID == id {
			t = x
		}
	}
	if t == nil || t.spec.Store == nil || t.info.Result == "" {
		m.mu.Unlock()
		return errors.New("nothing to save")
	}
	store, noteID, text := t.spec.Store, t.info.NoteID, t.info.Result
	m.mu.Unlock()
	m.storeResult(t, store, noteID, text)
	m.mu.Lock()
	info := t.info
	if info.SaveErr == "" && info.State == TaskFailed {
		t.info.State, t.info.Err = TaskDone, ""
		info = t.info
	}
	m.mu.Unlock()
	if info.SaveErr != "" {
		return errors.New(info.SaveErr)
	}
	m.record(recordOf(info), "", info.Tail) // rewrite the record with its NoteID
	m.signal()
	return nil
}
```
(Check `m.tasks` element type with `grep -n "tasks " internal/domain/tasks.go | head -3`; adjust the loop if it is a map.)

`ReviewTask`: after `spec.Parse = parseReport`:
```go
	if target.Kind != ReviewWorking {
		agent := spec.Agent
		spec.Store = func(ctx context.Context, noteID, text string) (string, string, error) {
			return s.SaveReview(ctx, SaveReview{Target: target, Agent: agent, Text: text, NoteID: noteID})
		}
	}
```

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain ./internal/taskhist 2>&1 | tail -5`
Expected: `ok` ×2.

- [ ] **Step 5: Commit**
```bash
git add internal/domain internal/taskhist
git commit -m "feat(tasks): a review task stores its result as a note, never a .result"
```

---

### Task 5: TUI opens reviews from notes (`srcNote` viewer, task tab, Retry save)

**Files:**
- Modify: `internal/tui/open_file.go:16-21` (`srcNote`), `open_files.go:206-219, 244-255, 288`, `file_viewer.go:138-151`, `sessions_popup.go:155-166`, `open_files_watch.go:48`
- Modify: `internal/tui/task_result_view.go` (new `openReviewNote`)
- Modify: `internal/tui/review.go:188-201` (`applyReviewResult`)
- Modify: `internal/tui/task_tab.go:241-290` (`openTask`), `:340-365` (hints, `hasResult`), key handling for `s`
- Modify: `internal/i18n/{ja,ko,zh,ru}.toml`
- Test: `internal/tui/task_tab_test.go`, `internal/tui/review_note_view_test.go` (new)

**Interfaces:**
- Consumes: `svc.Review(ctx, id)` (Task 3), `TaskInfo.NoteID/SaveErr`, `Record.NoteID`, `Tasks().RetrySave` (Task 4).
- Produces: `func (m Model) openReviewNote(id, title string) (Model, tea.Cmd)`; `srcNote` source kind (`src.rev` = note id, `path` = `"review-" + id + ".md"`).

- [ ] **Step 1: Write the failing tests** (`internal/tui/review_note_view_test.go`)

Model the setup on `TestTaskTabEnterShowsResultAndCopy` in `task_tab_test.go` (read it first). Test 1: a Model whose `svc` has `UseNotesDir(t.TempDir())`, on a real repo with a branch `feature`; save a review with `svc.SaveReview`; call `m.openReviewNote(id, "Review: feature")`, run the returned Cmd, feed the message back through `Update`, and assert `ansi.Strip(m.View())` contains the review text line and the title. Test 2: `openReviewNote("missing", …)` shows `review deleted`. Test 3 (task tab): a finished history record with `NoteID` set → enter in the AI-tasks tab shows the note's text; a record with `NoteID` whose note is gone → `review deleted`. Test 4: a live task with `SaveErr` shows the hint `[s] retry save` in the tasks tab footer.

```go
func TestOpenReviewNoteShowsTheText(t *testing.T) {
	t.Parallel()
	m, svc, id := reviewNoteModel(t, "# Verdict\nship it") // helper: real repo + notes dir + SaveReview
	_ = svc
	m, cmd := m.openReviewNote(id, "Review: feature")
	m = drainCmds(t, m, cmd) // the package's existing cmd-drain helper (grep "func drain" internal/tui/*_test.go)
	out := ansi.Strip(m.View())
	if !strings.Contains(out, "ship it") || !strings.Contains(out, "Review: feature") {
		t.Fatalf("viewer:\n%s", out)
	}
}

func TestOpenReviewNoteDeleted(t *testing.T) {
	t.Parallel()
	m, _, _ := reviewNoteModel(t, "x")
	m, cmd := m.openReviewNote("nope", "Review")
	m = drainCmds(t, m, cmd)
	if out := ansi.Strip(m.View()); !strings.Contains(out, "review deleted") {
		t.Fatalf("viewer:\n%s", out)
	}
}
```

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'ReviewNote' 2>&1 | tail -10`
Expected: build failure — `m.openReviewNote undefined`.

- [ ] **Step 3: Implement**

`open_file.go`: add `srcNote // an AI review stored as a note; rev = note id, path = "review-<id>.md"` to the enum.

`open_files.go` `docLoader`:
```go
	case srcNote:
		return func(ctx context.Context) ([]byte, error) {
			r, err := svc.Review(ctx, src.rev)
			if errors.Is(err, domain.ErrReviewNotFound) {
				return []byte(i18n.T("review deleted") + "\n"), nil
			}
			if err != nil {
				return nil, err
			}
			return []byte(r.Text), nil
		}
```
`openFilesProto`: `case srcNote: f.Source, f.Rev = "review", d.src.rev`. `onDisk` unchanged (srcNote is not on disk). `file_viewer.go` `title()`: treat `srcNote` like `srcExternal` (`case srcExternal, srcNote:`). `sessions_popup.go` `openFileRowText`: `if d.src.kind == srcExternal || d.src.kind == srcNote` and label `i18n.T("AI review")` for srcNote. `open_files_watch.go` `docAbs`: srcNote falls to the `return ""` branch (no change needed — verify).

`task_result_view.go`:
```go
// openReviewNote opens a review note in the viewer (y copies it all).
func (m Model) openReviewNote(id, title string) (Model, tea.Cmd) {
	src := fileSource{kind: srcNote, rev: id}
	path := "review-" + id + ".md"
	d := m.openFiles.find(m.currentWorktree, docKey(src, path))
	if d == nil {
		d = newOpenFile(src, path)
	} else {
		m = m.detachDoc(d)
	}
	d.title, d.result = title, true
	d.p.extraHint = i18n.T("[y] copy")
	m = m.pushLayer(&fileViewer{d})
	m = m.registerDoc(d)
	return m, m.loadDoc(d)
}
```

`review.go` `applyReviewResult`:
```go
func (m Model) applyReviewResult(info domain.TaskInfo) (Model, tea.Cmd) {
	label := strings.TrimPrefix(info.Key, "review — ")
	if info.SaveErr != "" {
		return m.stickyNotice(i18n.T("%s: %s — ctrl+\\ to retry", info.Key, info.SaveErr))
	}
	if !m.canShowResult(info) {
		return m.stickyNotice(i18n.T("%s ready — ctrl+\\", info.Key))
	}
	if info.NoteID == "" { // a working-changes review: not a note (ruling 1)
		return m.openResultViewer(info.ID, ".md", reviewTitle(label), info.Result, nil)
	}
	return m.openReviewNote(info.NoteID, reviewTitle(label))
}
```
(drop the `time` import if unused.)

`task_tab.go` `openTask`: at the top
```go
	if r.kind() == exttool.CatReview {
		noteID := ""
		if r.live != nil {
			noteID = r.live.NoteID
		} else if r.record != nil {
			noteID = r.record.NoteID
		}
		if noteID != "" {
			return m.openReviewNote(noteID, reviewTitle(strings.TrimPrefix(r.key(), "review — ")))
		}
	}
```
`hasResult`: `return r.record.ResultFile != "" || r.record.NoteID != ""` (and live: `r.live.Results > 0`). Footer hint: when `r.live != nil && r.live.SaveErr != ""` append `i18n.T("[s] retry save")`. Key `s` in the tab's update (next to `x`): 
```go
	case "s":
		if r, ok := p.selectedTaskRow(); ok && r.live != nil && r.live.SaveErr != "" {
			if err := domain.Tasks().RetrySave(r.id()); err != nil {
				m.statusMsg = i18n.T("review not saved: %s", err.Error())
			} else {
				m.statusMsg = i18n.T("review saved")
			}
			p.refresh(m)
			return m, nil
		}
```
(match the tab's actual selected-row accessor; `grep -n "case \"x\"" internal/tui/task_tab.go`.)

i18n: add to all four bundles the keys `review deleted`, `AI review`, `[s] retry save`, `review not saved: %s`, `review saved`, `%s: %s — ctrl+\\ to retry` (use the `adding-translations` skill for the file format).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui -run 'ReviewNote|TaskTab|I18n|Bundle' 2>&1 | tail -10`
Expected: `ok`.

- [ ] **Step 5: Commit**
```bash
git add internal/tui internal/i18n
git commit -m "feat(tui): reviews open from their note; tasks tab retry save"
```

---

### Task 6: Web review writes and reads a note

**Files:**
- Modify: `internal/domain/review.go:78-135` (`ReviewResult.NoteID` replaces `Path`; `ReviewReportNotes` saves through `SaveReview`)
- Modify: `internal/web/review.go:123-135`
- Modify: `internal/web/static/review.js:176-210, 285-295, 441-450`
- Test: `internal/domain/review_test.go`, `internal/web/review_test.go`

**Interfaces:**
- Consumes: `SaveReview` (Task 3).
- Produces: `ReviewResult{NoteID, Warn, Content, Range, Label}` (no `Path`); `ReviewReportNotes(ctx, target, cmd, env, notesFile)` — the `now time.Time` parameter is dropped (it only named the report file).

- [ ] **Step 1: Write the failing tests**

In `internal/domain/review_test.go`, rewrite `TestReviewReportPersistsAndReturns` to: run `ReviewReport` for a branch target with a fake tool command (the test's existing command), `svc.UseNotesDir(t.TempDir())`, and assert `res.NoteID != ""`, `svc.Review(ctx, res.NoteID).Text == res.Content`, and that `filepath.Join(os.Getenv("XDG_STATE_HOME"), "gg", "reviews")` does not exist. Add `TestReviewReportWorkingChangesSavesNoNote`: `WorkingReviewTarget()` → `res.NoteID == ""`, no error. In `internal/web/review_test.go` (find the review lane test: `grep -n "report" internal/web/review_test.go`), assert the op payload has `noteId` and no `path`.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain ./internal/web -run 'ReviewReport|Review' 2>&1 | tail -10`
Expected: build failure `res.NoteID undefined`.

- [ ] **Step 3: Implement**

`ReviewResult`:
```go
type ReviewResult struct {
	NoteID  string // "" for a working-changes review (not stored)
	Warn    string // the store had to be moved aside first
	Content string
	Range   string
	Label   string
}
```
`ReviewReport(ctx, target, cmd, env)` / `ReviewReportNotes(ctx, target, cmd, env, notesFile)`: replace the `writeReviewReport` block with
```go
	out := ReviewResult{Content: report, Range: target.Range, Label: label}
	if target.Kind != ReviewWorking {
		id, warn, serr := s.SaveReview(ctx, SaveReview{Target: target, Agent: agentFromCommand(resolvedCommand), Text: report})
		if serr != nil {
			return ReviewResult{}, fmt.Errorf("review not saved: %w", serr)
		}
		out.NoteID, out.Warn = id, warn
	}
	return out, nil
```
with `agentFromCommand` = the command's first word (`strings.Fields(cmd)[0]`, `""` if none). Update every caller's argument list (`grep -rn "ReviewReport" internal cmd`).

`web/review.go`:
```go
		res, rerr := svc.ReviewReport(ctx, target, resolved, []string{"GG_TASK=review"})
		if rerr != nil {
			return engine.Result{}, nil, rerr
		}
		summary := "review finished"
		if res.NoteID != "" {
			summary = "review saved as note " + res.NoteID
		}
		return engine.Result{Summary: summary},
			map[string]any{"report": res.Content, "noteId": res.NoteID, "label": res.Label, "warn": res.Warn},
			nil
```
`review.js`: rename `path: ev.path` → `noteId: ev.noteId`; `openReport(title, ev.noteId, ev.report)`; in `openReport(title, noteId, content)` set `$("report-path").textContent = noteId ? "note " + noteId : ""` and the same for `.title`.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain ./internal/web 2>&1 | tail -5`
Expected: `ok` ×2.

- [ ] **Step 5: Commit**
```bash
git add internal/domain internal/web
git commit -m "feat(web): a web review is saved as a note; the dialog shows its id"
```

---

### Task 7: CLI `gg review` prints the note id

**Files:**
- Modify: `internal/cli/review.go:120-135`
- Modify: `internal/agentskill/using-gg.md`, `internal/agentskill/agentskill.go:22` (`Version = 100`)
- Test: `internal/cli/review_test.go`

- [ ] **Step 1: Write the failing test**

In `internal/cli/review_test.go` find the test asserting `report:` on stderr (`grep -n "report:" internal/cli/*_test.go`); change it to expect `note: <id>` where the id matches `^[0-9a-f]{8}$`, and add a `--working` case expecting no `note:` line. Both with notes enabled for the service (`svc.UseNotesDir` via the test's existing service construction).

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/cli -run Review 2>&1 | tail -10`
Expected: FAIL — stderr still has `report:` (or a build error from Task 6's signature change).

- [ ] **Step 3: Implement**
```go
	res, err := svc.ReviewReportNotes(ctx, target, resolved, []string{"GG_TASK=review"}, notesPath)
	…
	if res.Warn != "" {
		fmt.Fprintln(stderr, "warning:", res.Warn)
	}
	if res.NoteID != "" {
		fmt.Fprintln(stderr, "note:", res.NoteID)
	}
```
`using-gg.md`: replace the `report: <path>` description of `gg review` with: "prints the review on stdout and `note: <id>` on stderr; the review is stored as a note on the reviewed commit (on the branch tip for a branch review) — `gg note list` shows it. A `--working` review is printed only." Bump `Version` to 100.

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./internal/cli ./internal/agentskill 2>&1 | tail -5`
Expected: `ok` ×2.

- [ ] **Step 5: Commit**
```bash
git add internal/cli internal/agentskill
git commit -m "feat(cli): gg review stores the review as a note and prints its id"
```

---

### Task 8: Branch delete / rename follow the reviews

**Files:**
- Modify: `internal/domain/service.go:383-397` (after `op.Run`)
- Modify: `internal/domain/review_notes.go` (`reviewsFollowBranchOp`)
- Test: `internal/domain/review_notes_test.go`

**Interfaces:**
- Produces: `func (s *Service) reviewsFollowBranchOp(ctx context.Context, op engine.Operation)`.

- [ ] **Step 1: Write the failing tests**
```go
func TestDeleteBranchDeletesItsReviews(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"}); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.DeleteBranch{Name: "feature"}, nil, engine.MapDecider{"delete-branch": "delete", "branch-unmerged": "force-delete"}); err != nil {
		t.Fatal(err)
	}
	if all, _ := svc.Reviews(ctx); len(all) != 0 {
		t.Fatalf("reviews after delete = %+v", all)
	}
}

func TestDeleteBranchKeepsLineNotes(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	st := svc.notesStore(ctx)
	line := model.Note{ID: "line1", Address: model.FileAddress{State: model.StateUnstaged, Worktree: dir, Branch: "feature", Path: "f.txt"},
		Summary: "keep me", Created: time.Now(), ContextHash: "h"}
	if err := st.Put(line); err != nil {
		t.Fatal(err)
	}
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.DeleteBranch{Name: "feature"}, nil, engine.MapDecider{"delete-branch": "delete", "branch-unmerged": "force-delete"}); err != nil {
		t.Fatal(err)
	}
	all, _ := st.Load()
	if len(all) != 1 || all[0].ID != "line1" {
		t.Fatalf("line note gone: %v", all)
	}
}

func TestRenameBranchRenamesItsReviews(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	tg, _ := svc.BranchReviewTarget(ctx, "feature")
	id, _, _ := svc.SaveReview(ctx, SaveReview{Target: tg, Text: "x"})
	runGitIn(t, dir, "checkout", "main")
	if _, err := svc.Execute(ctx, engine.RenameBranch{Old: "feature", New: "feat2"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	r, _ := svc.Review(ctx, id)
	if r.Branch != "feat2" || r.Kind != ReviewOnBranch {
		t.Fatalf("after rename: %+v", r)
	}
}
```
(Check `engine.MapDecider`'s real shape: `grep -n "type MapDecider" internal/engine/*.go`; adapt the literal.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain -run 'DeleteBranch|RenameBranch' 2>&1 | tail -10`
Expected: FAIL — reviews still present / branch not renamed.

- [ ] **Step 3: Implement**

`service.go` after `observ.NoteFailure(label, opErr)`:
```go
	if opErr == nil && out.Changed {
		s.reviewsFollowBranchOp(ctx, op)
	}
```
`review_notes.go`:
```go
// reviewsFollowBranchOp keeps review notes in step with a branch op that
// just succeeded: a deleted branch takes its reviews, a renamed one keeps
// them. Only review notes are touched. Best-effort: the op already happened.
func (s *Service) reviewsFollowBranchOp(ctx context.Context, op engine.Operation) {
	var drop, from, to string
	switch o := op.(type) {
	case engine.DeleteBranch:
		drop = o.Name
	case engine.DeleteRemoteBranch:
		drop = o.Remote + "/" + o.Branch
	case engine.RenameBranch:
		from, to = o.Old, o.New
	default:
		return
	}
	st := s.notesStore(ctx)
	if st == nil {
		return
	}
	all, err := st.Load()
	if err != nil {
		return
	}
	for _, n := range all {
		if !n.IsReviewNote() {
			continue
		}
		switch {
		case drop != "" && n.Address.Branch == drop:
			_ = st.Remove(n.ID)
		case from != "" && n.Address.Branch == from:
			n.Address.Branch = to
			_ = st.Put(n)
		}
	}
	s.invalidateNoteCounts()
}
```
(import `engine` in review_notes.go.)

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain 2>&1 | tail -3`
Expected: `ok`.

- [ ] **Step 5: Commit**
```bash
git add internal/domain
git commit -m "feat(domain): deleting or renaming a branch in gg carries its reviews"
```

---

### Task 9: A commit's file list shows its reviews as `@notes/` files

**Files:**
- Modify: `internal/tui/files_view.go:235-293` (`commitFileLines`, `commitFilesMsg`, `loadCommitFilesCmd`), `:875-903` (`treeFileLoad` unchanged — goes to loadCommitDiffCmd)
- Modify: `internal/tui/content_popup.go:24-47` (`contentLine.noteID`)
- Modify: `internal/tui/diff_view.go:818-850` (`loadCommitDiffCmd`)
- Modify: `internal/tui/file_preview.go:~130` (commit preview of an `@notes` line)
- Test: `internal/tui/review_files_test.go` (new)

**Interfaces:**
- Consumes: `svc.ReviewsForCommit` (Task 3), `srcNote` + `docLoader` (Task 5).
- Produces: `contentLine.noteID string`; `commitFileLines(files []model.CommitFile, reviews []domain.Review) []contentLine`; `const reviewsDir = "@notes"`.

- [ ] **Step 1: Write the failing tests**
```go
func TestCommitFileLinesPutReviewsFirst(t *testing.T) {
	t.Parallel()
	when := time.Date(2026, 9, 27, 10, 0, 0, 0, time.UTC)
	lines := commitFileLines(
		[]model.CommitFile{{Path: ".github/x.yml", Status: "M"}, {Path: "a.go", Status: "A"}},
		[]domain.Review{{ID: "ab12cd34", Created: when, Summary: "Review: feature"}})
	if lines[0].text != "@notes/" || !lines[0].heading {
		t.Fatalf("first line %+v, want the @notes/ heading", lines[0])
	}
	if lines[1].noteID != "ab12cd34" || lines[1].path != "@notes/review-2026-09-27-ab12cd34.md" || lines[1].status != "R" {
		t.Fatalf("review line %+v", lines[1])
	}
}
```
Plus a Model test: a commit reviewed via `SaveReview`, open its files view (the existing helper that opens the files view on a commit: `grep -n "func openFilesOn\|filesViewFor" internal/tui/*_test.go`), move to the review entry, press enter, and assert the diff pane shows the review text as added lines; then press `S` (stack) and assert the review text appears before the first real file's header.

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'CommitFileLinesPutReviewsFirst|ReviewInCommitFiles' 2>&1 | tail -10`
Expected: build failure — too many arguments to `commitFileLines`, `noteID` undefined.

- [ ] **Step 3: Implement**

`contentLine` gains `noteID string // an @notes/ review entry: the note it shows`.

`commitFilesMsg` gains `reviews []domain.Review`; `loadCommitFilesCmd` adds `reviews, _ := svc.ReviewsForCommit(context.Background(), c.Hash)` (a disabled store yields nil) and passes it; every `commitFileLines(msg.files)` caller becomes `commitFileLines(msg.files, msg.reviews)` (shelf: `commitFileLines(msg.files, nil)`).

`commitFileLines`:
```go
const reviewsDir = "@notes"

func commitFileLines(files []model.CommitFile, reviews []domain.Review) []contentLine {
	var out []contentLine
	if len(reviews) > 0 {
		// The commit's reviews come first, as a virtual directory: the stack
		// is built from this list, so they read above the first real file.
		out = append(out, contentLine{text: reviewsDir + "/", heading: true})
		for _, r := range reviews {
			name := "review-" + r.Created.Local().Format("2006-01-02") + "-" + r.ID + ".md"
			out = append(out, contentLine{text: "  R  " + name, path: reviewsDir + "/" + name, status: "R", noteID: r.ID})
		}
	}
	if len(files) == 0 {
		if len(out) == 0 {
			return []contentLine{{text: i18n.T("(no files)")}}
		}
		return out
	}
	// … the existing sort + dir grouping appends to out …
}
```

`loadCommitDiffCmd`: at the top, after `v` is built:
```go
	if line.noteID != "" {
		// A review is shown as an all-added file; it has no note address (no
		// line notes on a review) and is never cached — an interactive review
		// rewrites it in place.
		v.noteAddr = model.FileAddress{}
		id := line.noteID
		newSrc := func(ctx context.Context) ([]byte, error) {
			r, err := svc.Review(ctx, id)
			if err != nil {
				return nil, err
			}
			return []byte(r.Text), nil
		}
		return func() tea.Msg {
			out, err := differ.Diff(context.Background(), domain.Request{Key: "", Path: line.path, New: newSrc})
			if err != nil {
				v.err = err
				return diffMsg{tag: tag, view: v}
			}
			applyDiff(v, out, body)
			return diffMsg{tag: tag, view: v}
		}
	}
```
`file_preview.go` (commit preview, line ~134): when the files-view line has `noteID != ""`, open the preview with `fileSource{kind: srcNote, rev: line.noteID}` and path `line.path` instead of `srcCommit` — find the caller that passes the line (`grep -n "openPreviewSrc(fileSource{kind: srcCommit" internal/tui/file_preview.go`) and branch there.

Also make sure numstat-driven `+/-` counts ignore `@notes/` paths: in the stack, `counted` stays false and is filled from rows on load (existing behaviour) — verify with the Model test.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui -run 'CommitFile|Review|Stack' 2>&1 | tail -10`
Expected: `ok`.

- [ ] **Step 5: Commit**
```bash
git add internal/tui
git commit -m "feat(tui): a commit's reviews show as @notes/ files, stacked above its files"
```

---

### Task 10: Branches tab — ◆n marker, review sub-rows, "Show review"

**Files:**
- Create: `internal/tui/branch_reviews.go`
- Modify: `internal/tui/viewstate.go:224-233` (`branchList` → entry-based), `:686-714` (`backingIndex`), `listFor`
- Modify: `internal/tui/view.go:1076-1130` (`branchRows` over entries)
- Modify: `internal/tui/branch_filter.go` callers that index hidden/exempt by display entry (via `displayIndices`)
- Modify: `internal/tui/model.go:2555-2562` (enter on a review sub-row)
- Modify: `internal/tui/action_menu.go` (branch menu row "Show review")
- Modify: i18n bundles
- Test: `internal/tui/branch_reviews_test.go` (new)

**Interfaces:**
- Consumes: `m.noteCounts.Reviews []domain.ReviewHead` (Task 3; already loaded by the srcNotes source), `openReviewNote` (Task 5).
- Produces: `type brEntry struct{ b int; review string }`; `func (m Model) branchEntries() []brEntry`; `func (m Model) branchReviewHeads(b model.Branch) []domain.ReviewHead`; `func (m Model) selectedBranchReview() (domain.ReviewHead, bool)`.

- [ ] **Step 1: Write the failing tests**
```go
func TestBranchRowsShowReviewSubRows(t *testing.T) {
	t.Parallel()
	m := branchesModel(t, "main", "feature") // helper: m.branches with Hash set
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: "r1", Branch: "feature", Commit: m.branches[1].Hash,
		Agent: "Claude Code", Summary: "Review: feature", Created: time.Now().Add(-2 * time.Hour)}}
	rows, _ := m.panelView(panelBranches)
	joined := strings.Join(rows, "\n")
	if !strings.Contains(joined, "feature ◆1") || !strings.Contains(joined, "└ ◆") || !strings.Contains(joined, "Review: feature") {
		t.Fatalf("rows:\n%s", joined)
	}
}

func TestBranchReviewOfOldTipIsNotListed(t *testing.T) {
	t.Parallel()
	m := branchesModel(t, "main", "feature")
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: "r1", Branch: "feature", Commit: strings.Repeat("e", 40)}}
	rows, _ := m.panelView(panelBranches)
	if strings.Contains(strings.Join(rows, "\n"), "◆") {
		t.Fatalf("a review of an older tip was listed:\n%s", strings.Join(rows, "\n"))
	}
}

func TestBranchActionOnReviewRowIsRefused(t *testing.T) {
	t.Parallel()
	m := branchesModel(t, "main", "feature")
	m.noteCounts.Reviews = []domain.ReviewHead{{ID: "r1", Branch: "feature", Commit: m.branches[1].Hash}}
	m.focus = panelBranches
	m = selectDisplayRow(m, panelBranches, 2) // main, feature, └ review
	if _, ok := m.selectedBranch(); ok {
		t.Fatal("a review sub-row resolved to a branch")
	}
	if h, ok := m.selectedBranchReview(); !ok || h.ID != "r1" {
		t.Fatalf("selectedBranchReview = %+v %v", h, ok)
	}
}
```
(Find/define `branchesModel` and `selectDisplayRow` from the Worktrees session sub-row tests: `grep -n "func Test.*SessionSubRow" -A25 internal/tui/worktree_sessions_test.go`.)

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/tui -run 'BranchRowsShowReview|BranchReviewOfOldTip|BranchActionOnReviewRow' 2>&1 | tail -10`
Expected: FAIL — no `◆` in rows / `selectedBranchReview` undefined.

- [ ] **Step 3: Implement**

`branch_reviews.go`:
```go
package tui

// The Branches tab lists a branch's reviews (of its CURRENT tip) as sub-rows
// under it, always shown — the Worktrees session sub-row pattern: entries
// interleave, backingIndex refuses a sub-row, Name/Date are the parent's so
// sorting keeps them under it.

type brEntry struct {
	b      int    // index into m.branches
	review string // a review sub-row: the note id
}

func (e brEntry) sub() bool { return e.review != "" }

// branchReviewHeads is b's reviews on its current tip, newest first.
func (m Model) branchReviewHeads(b model.Branch) []domain.ReviewHead {
	var out []domain.ReviewHead
	for _, r := range m.noteCounts.Reviews {
		if r.Branch == b.Name && r.Commit == b.Hash {
			out = append(out, r)
		}
	}
	return out
}

func (m Model) branchEntries() []brEntry {
	out := make([]brEntry, 0, len(m.branches))
	for i, b := range m.branches {
		out = append(out, brEntry{b: i})
		for _, r := range m.branchReviewHeads(b) {
			out = append(out, brEntry{b: i, review: r.ID})
		}
	}
	return out
}

// branchReviewRowText is "  └ ◆ 2h · Claude Code · Review: feature".
func branchReviewRowText(r domain.ReviewHead, now time.Time) string {
	parts := []string{coarseAgo(now.Sub(r.Created))}
	if r.Agent != "" {
		parts = append(parts, sanitizeLine(r.Agent))
	}
	parts = append(parts, sanitizeLine(r.Summary))
	return "  └ ◆ " + strings.Join(parts, " · ")
}

func (m Model) reviewHead(id string) (domain.ReviewHead, bool) {
	for _, r := range m.noteCounts.Reviews {
		if r.ID == id {
			return r, true
		}
	}
	return domain.ReviewHead{}, false
}

func (m Model) selectedBranchReview() (domain.ReviewHead, bool) {
	idx := m.displayIndices(panelBranches)
	sel := m.sel[panelBranches]
	if sel < 0 || sel >= len(idx) {
		return domain.ReviewHead{}, false
	}
	ents := m.branchEntries()
	if idx[sel] >= len(ents) || !ents[idx[sel]].sub() {
		return domain.ReviewHead{}, false
	}
	return m.reviewHead(ents[idx[sel]].review)
}
```
`viewstate.go` `branchList`:
```go
type branchList struct {
	items []model.Branch
	ents  []brEntry
	rows  []string // one per entry
}

func (l branchList) Len() int          { return len(l.ents) }
func (l branchList) Row(i int) string  { return l.rows[i] }
func (l branchList) Name(i int) string { return l.items[l.ents[i].b].Name }
func (l branchList) Date(i int) int64  { return l.items[l.ents[i].b].UnixTime }
func (l branchList) Key(i int) string {
	k := l.items[l.ents[i].b].Name
	if r := l.ents[i].review; r != "" {
		k += "\x00" + r
	}
	return k
}
func (l branchList) Haystack(i int) string { // a sub-row matches what its branch matches
	if !l.ents[i].sub() {
		return l.rows[i]
	}
	p := i
	for p > 0 && l.ents[p].sub() {
		p--
	}
	return l.rows[p] + " " + l.rows[i]
}
```
`listFor`: `ents := m.branchEntries(); return branchList{items: m.branches, ents: ents, rows: m.branchRows(ents)}`.

`displayIndices`: the branch-filter check indexes `bfHidden` by the BRANCH, so for `panelBranches` map `i` through the entries:
```go
		hi := i
		if bl, ok := l.(branchList); ok {
			hi = bl.ents[i].b
		}
		if bfHidden != nil && hi < len(bfHidden) && bfHidden[hi] {
			continue
		}
```
`backingIndex`: add, next to the Worktrees case:
```go
	if p == panelBranches {
		ents := m.branchEntries()
		if u >= len(ents) || ents[u].sub() {
			return 0, false
		}
		return ents[u].b, true
	}
```
`branchRows(ents []brEntry)`: iterate entries; for a sub-row append `branchReviewRowText(head, time.Now())`; for a branch row keep today's text, add ` ◆<n>` after the name when `n := len(m.branchReviewHeads(b)) > 0`, and index `exempt` by `e.b` (not the entry index).

Any other `m.sel[panelBranches]`/`displayIndices(panelBranches)` user that indexes `m.branches` directly: `grep -n "panelBranches" internal/tui/*.go | grep -v _test` and route each through `backingIndex`/`selectedBranch` (ledger a ruling per site changed).

`model.go` enter (Branches): before `commitGotoTipRow`:
```go
				if h, ok := m.selectedBranchReview(); ok {
					return m.openReviewNote(h.ID, h.Summary)
				}
```
`action_menu.go`: in the branch menu's rows, add a row labelled `i18n.T("Show review")` when `len(m.branchReviewHeads(b)) > 0`, running `m.openReviewNote(heads[0].ID, heads[0].Summary)` (follow the shape of the neighbouring branch rows; add its id to the menu-label test's expected set if `menu_labels_test.go` enumerates them).

i18n: `Show review` ×4.

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/tui 2>&1 | tail -10`
Expected: `ok` (the whole package — the entry change touches filters, marks and sorting).

- [ ] **Step 5: Commit**
```bash
git add internal/tui internal/i18n
git commit -m "feat(tui): Branches tab lists a branch's reviews under it (◆n)"
```

---

### Task 11: View all notes… lists reviews under their commit

**Files:**
- Modify: `internal/domain/notes_overview.go` (`NoteCommitNotes.Reviews`, `Count`, skip review notes in buckets)
- Modify: `internal/tui/all_notes_popup.go` (`anReview` rows, enter)
- Test: `internal/domain/notes_overview_test.go`, `internal/tui/all_notes_popup_test.go`

**Interfaces:**
- Consumes: `Reviews` / `reviewOf` (Task 3), `openReviewNote` (Task 5).
- Produces: `NoteCommitNotes.Reviews []Review`; `anReview anRowKind` with `anRow.review *domain.Review`.

- [ ] **Step 1: Write the failing tests**

Domain: save a branch review, call `NotesOverview`; assert one commit entry with `len(Reviews) == 1`, `len(Files) == 0`, `Count() == 1`. Missing commit: `Put` a review note on `strings.Repeat("d",40)`; assert that commit is listed with `Missing: true` and one review.

TUI (`TestAllNotesOpensReviewOfMissingCommit`): build rows from an overview with a missing commit holding one review; assert a row with text `@notes/` and a review row whose WHO column shows the agent and WHERE shows `was tip feature` / `branch feature` / `commit`; select the review row, press enter, assert a `fileViewer` layer with `src.kind == srcNote` was pushed (no "That commit no longer exists." notice).

- [ ] **Step 2: Run to verify they fail**

Run: `go test ./internal/domain ./internal/tui -run 'NotesOverview|AllNotes' 2>&1 | tail -10`
Expected: FAIL — no reviews in the overview.

- [ ] **Step 3: Implement**

`notes_overview.go`: in the bucket loop, `if n.IsReviewNote() { continue }` BEFORE bucketing (replies to a review root: skip when their root is a review note). After buckets, add:
```go
	revs, _ := s.Reviews(ctx)
	for _, r := range revs {
		c := commits[r.Commit]
		if c == nil {
			c = s.overviewCommit(ctx, r.Commit)
			commits[r.Commit] = c
		}
		c.Reviews = append(c.Reviews, r)
	}
```
`Count()` adds `len(c.Reviews)` per commit. `NoteCommitNotes` gains `Reviews []Review`.

`all_notes_popup.go`: add `anReview` to the kind enum; `anRow` gains `review *domain.Review`. In `buildAllNotesRows`, inside the commits loop right after `sub(...)`:
```go
			if len(c.Reviews) > 0 {
				rows = append(rows, anRow{kind: anDir, depth: 2, text: "@notes/"})
				for i := range c.Reviews {
					r := &c.Reviews[i]
					rows = append(rows, anRow{kind: anReview, depth: 3, review: r,
						filter: strings.ToLower(r.Summary + "\x00" + r.Agent + "\x00" + r.Branch)})
				}
			}
```
Row text for `anReview` (in `anRowText`/`anRowFull`/`anBarText` and the decorator, alongside `anNote`): STATUS `i18n.T("review")`, WHO the agent, WHERE `i18n.T("branch %s", r.Branch)` for `ReviewOnBranch`, `i18n.T("was tip %s", r.Branch)` for `ReviewWasTip`, `i18n.T("commit")` otherwise, WHEN `coarseAgo`, NOTE the summary — reuse the fixed column widths (`anStatusW` …). Enter: `case anReview: return m.openReviewNote(r.review.ID, r.review.Summary)` (works for a missing commit — the text is in the note).

i18n: `review`, `branch %s`, `was tip %s`, `commit` ×4 (check `commit` isn't already a key: `grep -n '^"commit" =' internal/i18n/ja.toml`).

- [ ] **Step 4: Run to verify they pass**

Run: `go test ./internal/domain ./internal/tui 2>&1 | tail -5`
Expected: `ok` ×2.

- [ ] **Step 5: Commit**
```bash
git add internal/domain internal/tui internal/i18n
git commit -m "feat: View all notes lists a commit's reviews under @notes/"
```

---

### Task 12: Delete the old review file paths

**Files:**
- Modify: `internal/domain/review.go` (delete `SaveReviewReport`, `writeReviewReport`, and their helpers)
- Modify: `internal/domain/statebasedir_callers_test.go:22-34` (drop `"reviews"`), `statebasedir_test.go` (drop `"reviews"`)
- Modify: tests that exercised `SaveReviewReport`/report paths (`grep -rn "SaveReviewReport\|writeReviewReport\|reviews/\|\.Path\b.*review" internal --include=*_test.go`)
- Modify: `internal/tui/task_result_view.go` comment (reviews no longer use `SaveTaskResult`)

- [ ] **Step 1: Write the failing guard test**

`internal/domain/review_notes_test.go`:
```go
// TestNoReviewReportFilesAnywhere pins the removal: nothing in domain asks
// for the "reviews" state dir any more.
func TestNoReviewReportFilesAnywhere(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("review.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), `stateBaseDir("reviews")`) {
		t.Fatal(`review.go still writes <state>/gg/reviews`)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./internal/domain -run TestNoReviewReportFilesAnywhere`
Expected: FAIL `review.go still writes <state>/gg/reviews`.

- [ ] **Step 3: Delete**

Remove `SaveReviewReport` and `writeReviewReport` (and any helper only they use — `go vet ./...` / `staticcheck`-style unused check via `go build ./... && go test ./internal/domain` will flag). Remove `"reviews"` from both kind lists. Delete or rewrite tests that asserted the old report file (e.g. `review_test.go:339` `10-04-working-changes.md`): a working-changes review now asserts `res.NoteID == ""` and no file. Update the `task_result_view.go` top comment: "a review opens from its note (openReviewNote); other results are files written by domain.SaveTaskResult".

- [ ] **Step 4: Run the whole suite**

Run: `./test.sh unit > /tmp/claude-1000/-mnt-t-others-gigagit/940fe89f-a4bc-4f07-b30b-700e08f4b67e/scratchpad/unit.log 2>&1; tail -20 /tmp/claude-1000/-mnt-t-others-gigagit/940fe89f-a4bc-4f07-b30b-700e08f4b67e/scratchpad/unit.log`
Expected: every package `ok`, gofmt/vet clean.

- [ ] **Step 5: Commit**
```bash
git add -A internal
git commit -m "refactor(domain): drop the reviews/ report files — reviews live in notes only"
```

---

### Task 13: e2e scenario + docs

**Files:**
- Create: `e2e/scenarios/review-note-branch.toml`
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`
- Test: `./test.sh e2e`

- [ ] **Step 1: Write the scenario** (use the `writing-e2e-scenarios` skill for the exact TOML schema; read one existing scenario that runs `gg review` if any: `grep -ln "review" e2e/scenarios/*.toml`)

Scenario: repo with `main` + `feature` (one commit); a `[tools]` review command that is a shell `printf '# ok\n'` (capture mode); run `gg review --branch feature` → assert exit 0 and stderr matches `^note: [0-9a-f]{8}$`; run `gg note list` → assert one note on the feature tip; run `gg branch delete feature --force` (the CLI's real flag — check `gg branch delete --help`) → `gg note list` shows none.

- [ ] **Step 2: Run to verify**

Run: `./test.sh e2e > …/scratchpad/e2e.log 2>&1; tail -20 …/scratchpad/e2e.log`
Expected: the new scenario passes (if `gg note list` does not list path-less notes, extend `internal/cli/note.go`'s list to print them as `<sha7>  review  <summary>` — a CLI surface change, so also document it in `using-gg.md`; ledger the ruling).

- [ ] **Step 3: Docs**

CHANGELOG (top, Unreleased): "AI reviews are stored as notes on the reviewed commit (branch reviews on the branch tip): shown as `@notes/` files in the commit's file list and stack, under the branch in the Branches tab (◆n), in View all notes…, and from the AI-tasks tab. Deleting/renaming a branch in gg carries its reviews. `gg review` prints `note: <id>`. The `reviews/` report files and per-review `task-results/` / `.result` copies are gone." README: the review section + Branches tab mention. `docs/CLAUDE-details.md`: a "Review notes" subsection with the rulings (review = commit-level + tag; Branch alone is not a marker; cap/age/sweep exemptions; retry 15 s / 500 ms–2 s jitter; quarantine; branch follow-ups in `Execute`; `TaskSpec.Store`).

- [ ] **Step 4: Race gate**

Run: `./test.sh race > …/scratchpad/race.log 2>&1; tail -30 …/scratchpad/race.log`
Expected: all green.

- [ ] **Step 5: Commit**
```bash
git add e2e CHANGELOG.md README.md docs
git commit -m "docs+e2e: AI reviews as notes"
```
