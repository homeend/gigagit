# Working-Changes Reviews as Notes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (native — CLAUDE.md forbids implementer subagents) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A review of uncommitted changes is stored as a note in this worktree's part file, carrying a (path, blob id) fingerprint per reviewed file. Each file's annotations show while that file still matches. Once no file matches, the review is outdated and is swept after `max_age_days`. Untracked files become part of the review input.

**Architecture:**
- **engine:** `ReviewChanges.Prepare` gains a `Working` mode. It fingerprints every reviewed path in-process (git blob id in the repo's object format) and synthesizes new-file patches for untracked files. The fingerprints ride `TaskInputs` → `Result` → the task's `Store` hook → `domain.SaveReview`, which writes a path-less live note `{State: Unstaged, Worktree: top}` tagged `review`.
- **domain:** matches the fingerprints against the worktree with a process-wide (path, mtime, size) cache. The read model (`Review`, `WorkingReviews`, `NotesOverview`, `NoteCounts`, the sweep, `NotesFor`/`NotesAt`, the review-view reads) branches on the new kind.
- **Frontends:** TUI and web show a Review row and ✎ markers, open the review as a HEAD↔worktree compare in review mode, and list working reviews in View all notes with an "outdated" label.

**Tech Stack:** Go 1.26, Bubble Tea TUI, embedded web SPA (vanilla JS), BurntSushi TOML note store, real `git` in tests.

**Spec:** `docs/superpowers/specs/2026-10-04-working-reviews-design.md` (user rulings in §3). Builds on spec 1, `docs/superpowers/specs/2026-10-04-notes-partitions-design.md`, which is merged.

## Global Constraints

- **One review note per review:**
  - `Address{State: model.StateUnstaged, Worktree: <filepath.Clean(top)>}`, no Path, tag `review`.
  - `Author` = tool name; `Summary` = "Review: working changes"; `Rationale` = the canonical review document (or prose).
  - `Files []model.NoteFile` with `NoteFile{Path, Blob string; Deleted bool}` (toml `files,omitempty`).
- **Fingerprints:**
  - Taken by gg in `ReviewChanges.Prepare`, never by the agent, for every path in the reviewed input.
  - Blob = git's blob id of the working-tree bytes (`"blob <n>\0" + bytes`), hashed in-process in the repo's object format (sha1, or sha256). It is never compared with an index blob id.
  - A deleted file is `Deleted: true` with no blob; it matches while the path stays absent.
- **Untracked input:**
  - `git ls-files --others --exclude-standard -z` lists the files. Each becomes a synthesized new-file patch, and a binary one becomes `Binary files /dev/null and b/<path> differ`.
  - They count toward `MaxDiffBytes`; past the cap the diff is truncated exactly as today.
- **Match:**
  - Per file: matches / changed / gone. The review is current while ≥ 1 file matches, outdated when none does.
  - Hashing is cached process-wide by (worktree, path, mtime, size).
- **Own worktree only:** the note lives in `worktrees/<key>.toml`. A sibling worktree never lists it.
- **Sweep:** drop when outdated AND `Created` is older than `[notes] max_age_days`. A current review is never swept.
- **CLI:** `gg review --working` stores the review and prints `note: <id>`. `--working --notes` → exit 2 with the message "a working review is stored; its notes show on the files".
- **Display:**
  - A Review row at the top of the TUI Files panel and the web working list, opening the review view.
  - ✎ on each file row a current review still matches.
  - Annotations are drawn only on matching files.
  - View all notes lists every working review and labels one with no matching file "outdated".
- **Layering:**
  - `internal/tui` and `internal/cli` never import `internal/git` (archtest); they use domain types only.
  - Engine/CLI prose stays English.
  - Every user-visible TUI string goes through `i18n.T` with a literal key present in all four bundles (ja/ko/zh/ru).
- **Process:**
  - Tests use a real `git` in `t.TempDir()` and call `t.Parallel()` unless they swap package seams.
  - TDD: watch each test fail first.
  - No implementer subagents; one read-only review subagent at the end.
- **Commits:**
  - Stage with `gg add <paths>` (never `git add -A` — a built `bin/gg` may sit in the worktree), then `git commit -F <msgfile>` (gg commit has no -F).
  - Messages end with:
    `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>` and
    `Claude-Session: https://claude.ai/code/session_01AMREn9YaHAb1mRQm9uPcor`.
- **Paths:** every command runs as `cd /work/gigagit/.claude/worktrees/working-reviews && …` (the shell cwd resets to the main checkout).

## Review Focus

1. **Racily-clean cache.** A reviewed file rewritten with the same size inside the filesystem's mtime granularity must not keep reporting "matches". Hashes of files modified in the last 2 s are not cached. Pinned in Task 5 (`TestWorkingBlobCacheNeverTrustsAFreshMtime`).
2. **Unstaged rename.** `mv a b` without staging shows as D a + untracked b. Both must be fingerprinted: a as `Deleted`, b with a blob. Pinned in Task 3 (`TestReviewChangesWorkingUnstagedRename`).
3. **Huge untracked file.** An untracked file larger than `MaxDiffBytes` is stream-hashed, never read whole into memory. The diff is reported truncated. Pinned in Task 3 (`TestReviewChangesWorkingUntrackedPastTheCap`).
4. **Directory entries from `ls-files --others`.** A nested untracked repo is listed as `sub/`. It must not crash the review, must not be fingerprinted and gets no patch. Pinned in Task 3 (`TestReviewChangesWorkingSkipsADirectoryEntry`).
5. **Files-panel gestures with the Review row present.** Marking, staging (space), stacked diff, `/` filter, mouse click and enter must never act on the pseudo row. The cursor must stay on the same file when the row appears or disappears. Pinned in Task 10.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `internal/model/note.go` | modify | `NoteFile`, `Note.Files`, `IsWorktreeLevel`, `IsWorkingReview`, `IsEntryLevel` |
| `internal/git/blob.go` | create | `WorktreeBlob`, `BlobOf`, `HashWorktreeFile`, `BlobFormatOf`, `ErrNotAFile` |
| `internal/git/repo.go` (or `compare.go`) | modify | `ObjectFormat` verb |
| `internal/engine/gitops.go` | modify | `ObjectFormat`, `UntrackedFiles` on `GitOps` |
| `internal/engine/capture_task.go`, `operation.go` | modify | `TaskInputs.ReviewFiles`, `Result.ReviewFiles` |
| `internal/engine/review_working.go` | create | untracked patches + fingerprints (`reviewWorkingInput`, `newFilePatch`) |
| `internal/engine/review_changes.go` | modify | `Working` flag, Prepare/Collect wiring, brief wording |
| `internal/domain/review_notes.go` | modify | working write path, `ReviewOnWorktree`, read model |
| `internal/domain/review.go` | modify | `ReviewReport` stores working reviews |
| `internal/domain/task_kinds.go`, `tasks.go` | modify | `Store` takes files; task keeps them for RetrySave |
| `internal/domain/working_review.go` | create | `WorkingFileState`, `WorkingReviewState`, blob cache, `WorkingReviews`, `workingReviewNotes` |
| `internal/domain/notes_sweep.go`, `notes.go`, `notes_overview.go` | modify | sweep branch, `NoteCounts.WorkingReviews`, `NotesOverview.WorkingReviews`, `resolveOne` entry-level, NotesFor/NotesAt injection |
| `internal/domain/review_doc.go` | modify | review-view reads for the new kind, `ReviewOtherNote.Changed` |
| `internal/cli/review.go`, `note.go` | modify | `--working` stores; `--working --notes` usage error; note-line label |
| `internal/tui/review_view.go`, `review.go`, `all_notes_popup.go` | modify | open working review, result opens by NoteID, all-notes rows |
| `internal/tui/working_review_row.go` | create | Files-panel Review pseudo row + ✎ |
| `internal/tui/viewstate.go` + callers | modify | the pseudo row's sentinel index |
| `internal/web/notes.go`, `reviews.go`, `allnotes.go`, `diff.go`, `static/*.js` | modify | web surfaces |
| `e2e/scenarios/s1xx_working_review.toml`, `tui_working_review.toml` | create | e2e + golden screen |
| `CHANGELOG.md`, `docs/CLAUDE-details.md`, `internal/agentskill/*.md`, `agentskill.go` | modify | docs |

---

### Task 1: The note shape

**Files:**
- Modify: `internal/model/note.go`
- Test: `internal/model/note_test.go`, `internal/notes/part_test.go`, `internal/notes/part_file_test.go`

**Interfaces:**
- Produces:
  - `type NoteFile struct { Path string; Blob string; Deleted bool }`
  - `Note.Files []NoteFile`
  - `func (n Note) IsWorktreeLevel() bool`
  - `func (n Note) IsWorkingReview() bool`
  - `IsEntryLevel()` now includes `IsWorktreeLevel()`

- [ ] **Step 1: Write the failing tests**

Append to `internal/model/note_test.go`:

```go
// The working-review predicates: a live, path-less note with a worktree.
func TestWorkingReviewPredicates(t *testing.T) {
	t.Parallel()
	sha := strings.Repeat("a", 40)
	review := []string{ReviewTag}
	cases := []struct {
		name               string
		n                  Note
		wtLevel, wr, entry bool
	}{
		{"working review", Note{Tags: review, Address: FileAddress{State: StateUnstaged, Worktree: "/r"}}, true, true, true},
		{"worktree-level untagged", Note{Address: FileAddress{State: StateUnstaged, Worktree: "/r"}}, true, false, true},
		{"staged state", Note{Tags: review, Address: FileAddress{State: StateStaged, Worktree: "/r"}}, true, true, true},
		{"no worktree", Note{Tags: review, Address: FileAddress{State: StateUnstaged}}, false, false, false},
		{"a file", Note{Tags: review, Address: FileAddress{State: StateUnstaged, Worktree: "/r", Path: "a.go"}}, false, false, false},
		{"commit review", Note{Tags: review, Address: FileAddress{State: StateCommitted, Commit: sha}}, false, false, true},
		{"shelf entry", Note{Address: FileAddress{State: StateShelf, ShelfID: "e1"}}, false, false, true},
	}
	for _, c := range cases {
		if got := c.n.IsWorktreeLevel(); got != c.wtLevel {
			t.Errorf("%s: IsWorktreeLevel = %v, want %v", c.name, got, c.wtLevel)
		}
		if got := c.n.IsWorkingReview(); got != c.wr {
			t.Errorf("%s: IsWorkingReview = %v, want %v", c.name, got, c.wr)
		}
		if got := c.n.IsEntryLevel(); got != c.entry {
			t.Errorf("%s: IsEntryLevel = %v, want %v", c.name, got, c.entry)
		}
		if c.n.IsWorkingReview() && c.n.IsReviewNote() {
			t.Errorf("%s: IsReviewNote must stay commit-only", c.name)
		}
	}
}
```

(Add `"strings"` to the test imports if missing.)

Add a row to `TestPartOfRoutesEveryKind` in `internal/notes/part_test.go`:

```go
		{"working review", model.FileAddress{State: model.StateUnstaged, Worktree: "/repo"}, "", wt},
```

Append to `internal/notes/part_file_test.go`. Model it on `TestCapNeverDropsCommitLevelNotes` at line 499; read it first and reuse its `line(...)` helper:

```go
// A working review is entry-level: the cap never counts or drops it.
func TestCapExemptsWorkingReviews(t *testing.T) {
	t.Parallel()
	old := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	review := model.Note{ID: "rv", Tags: []string{model.ReviewTag}, Created: old,
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: "/r"}}
	got := capOldestFirst([]model.Note{review, line("l1", 1), line("l2", 2), line("l3", 3)}, 2)
	ids := map[string]bool{}
	for _, n := range got {
		ids[n.ID] = true
	}
	if !ids["rv"] || len(got) != 3 {
		t.Fatalf("kept %v, want the review plus the two newest line notes", ids)
	}
}

// Files round-trips through the TOML part file.
func TestNoteFilesRoundTrip(t *testing.T) {
	t.Parallel()
	st := NewFileStore(t.TempDir())
	n := model.Note{ID: "rv1", Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: "/repo"},
		Files:   []model.NoteFile{{Path: "a b.go", Blob: strings.Repeat("c", 40)}, {Path: "gone.go", Deleted: true}},
		Created: time.Now().UTC(), Updated: time.Now().UTC()}
	if err := st.Put(n); err != nil {
		t.Fatal(err)
	}
	got, err := st.Load(WorktreePart("/repo"))
	if err != nil || len(got) != 1 {
		t.Fatalf("Load = %v, %v", got, err)
	}
	if !reflect.DeepEqual(got[0].Files, n.Files) {
		t.Fatalf("Files = %+v, want %+v", got[0].Files, n.Files)
	}
}
```

(Check that `line`'s Created dates make l1 the oldest. Add imports `reflect`, `strings`, `time` as needed.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/model/ ./internal/notes/ 2>&1 | tail -20`
Expected: compile errors — `IsWorktreeLevel`, `IsWorkingReview`, `Files` and `NoteFile` are undefined.

- [ ] **Step 3: Implement**

In `internal/model/note.go`, add to `Note` after `PreviewBase`:

```go
	// Files is a working-changes review's fingerprint: every file the review
	// read, with git's blob id of the bytes it read (spec 2026-10-04 working
	// reviews §4). Empty on every other note.
	Files []NoteFile `toml:"files,omitempty"`
```

Add the type and the predicates after `IsShelfLevel`:

```go
// NoteFile is one file a working-changes review read: its repo-relative
// path and git's blob id of the working-tree bytes it reviewed (hex, in the
// repo's object format). Deleted: the review saw the file absent.
type NoteFile struct {
	Path    string `toml:"path"`
	Blob    string `toml:"blob,omitempty"`
	Deleted bool   `toml:"deleted,omitempty"`
}

// IsWorktreeLevel reports a note about a whole worktree's uncommitted
// changes: a live address with a worktree and no path, commit or shelf entry.
func (n Note) IsWorktreeLevel() bool {
	a := n.Address
	return a.Path == "" && a.Commit == "" && a.ShelfID == "" && a.Worktree != "" &&
		(a.State == StateUnstaged || a.State == StateStaged || a.State == StateUntracked)
}

// IsWorkingReview reports a worktree-level note tagged ReviewTag: an AI
// review of uncommitted changes. IsReviewNote stays commit-only; the review
// read model checks both.
func (n Note) IsWorkingReview() bool { return n.IsWorktreeLevel() && NoteHasTag(n, ReviewTag) }
```

Change `IsEntryLevel`:

```go
// IsEntryLevel reports a note about a whole object — a commit, a shelf entry
// or a worktree's uncommitted changes. It has no line to re-anchor.
func (n Note) IsEntryLevel() bool { return n.IsCommitLevel() || n.IsShelfLevel() || n.IsWorktreeLevel() }
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/model/ ./internal/notes/ 2>&1 | tail -5`
Expected: `ok` for both packages.

- [ ] **Step 5: Commit**

```bash
cd /work/gigagit/.claude/worktrees/working-reviews && gg add internal/model/note.go internal/model/note_test.go internal/notes/part_test.go internal/notes/part_file_test.go && git commit -F <msgfile>
```
Message: `feat(model): a note can fingerprint the files a working review read`

---

### Task 2: Blob ids in-process + the git verbs

**Files:**
- Create: `internal/git/blob.go`, `internal/git/blob_test.go`
- Modify: `internal/git/compare.go` (next to `UntrackedFiles`), `internal/engine/gitops.go`

**Interfaces:**
- Produces:
  - `type WorktreeBlob struct { ID string; Lines int; Binary bool }`
  - `func BlobOf(format string, data []byte) WorktreeBlob`
  - `func HashWorktreeFile(format, path string) (WorktreeBlob, error)`, where `path` is absolute
  - `func BlobFormatOf(id string) string`, returning `"sha256"` for a 64-hex id and `"sha1"` otherwise
  - `var ErrNotAFile`
  - `func (r *Repo) ObjectFormat(ctx) (string, error)`
  - On `engine.GitOps`: `ObjectFormat(ctx) (string, error)` and `UntrackedFiles(ctx) ([]string, error)`

- [ ] **Step 1: Write the failing tests**

Create `internal/git/blob_test.go`:

```go
package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/gitexec"
	"github.com/homeend/gigagit/internal/observ"
)

// hashObject is git's own answer for path's blob id in dir's repo.
func hashObject(t *testing.T, dir, path string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "hash-object", path).Output()
	if err != nil {
		t.Fatalf("hash-object %s: %v", path, err)
	}
	return strings.TrimSpace(string(out))
}

func initRepo(t *testing.T, format string) string {
	t.Helper()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--object-format="+format, dir).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

// The in-process blob id is git's, in both object formats, for every kind
// of content a review meets.
func TestHashWorktreeFileMatchesGit(t *testing.T) {
	t.Parallel()
	files := map[string][]byte{
		"text.txt":   []byte("one\ntwo\n"),
		"noeol.txt":  []byte("one\ntwo"),
		"empty.txt":  {},
		"binary.dat": {0, 1, 2, 'x', 0},
		"crlf.txt":   []byte("a\r\nb\r\n"),
	}
	for _, format := range []string{"sha1", "sha256"} {
		dir := initRepo(t, format)
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
			got, err := HashWorktreeFile(format, p)
			if err != nil {
				t.Fatalf("%s %s: %v", format, name, err)
			}
			if want := hashObject(t, dir, name); got.ID != want {
				t.Errorf("%s %s: id %s, want %s", format, name, got.ID, want)
			}
			if b := BlobOf(format, data); b != got {
				t.Errorf("%s %s: BlobOf %+v != HashWorktreeFile %+v", format, name, b, got)
			}
			if BlobFormatOf(got.ID) != format {
				t.Errorf("%s %s: BlobFormatOf(%s) = %s", format, name, got.ID, BlobFormatOf(got.ID))
			}
		}
	}
}

func TestBlobOfLinesAndBinary(t *testing.T) {
	t.Parallel()
	cases := []struct {
		data   string
		lines  int
		binary bool
	}{
		{"", 0, false}, {"a\n", 1, false}, {"a\nb", 2, false}, {"a\x00b\n", 1, true},
	}
	for _, c := range cases {
		b := BlobOf("sha1", []byte(c.data))
		if b.Lines != c.lines || b.Binary != c.binary {
			t.Errorf("%q: lines %d binary %v, want %d %v", c.data, b.Lines, b.Binary, c.lines, c.binary)
		}
	}
}

// A symlink is hashed as git stores it: its target string.
func TestHashWorktreeFileSymlinkIsItsTarget(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := initRepo(t, "sha1")
	if err := os.Symlink("text.txt", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	got, err := HashWorktreeFile("sha1", filepath.Join(dir, "link"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != BlobOf("sha1", []byte("text.txt")).ID {
		t.Fatalf("symlink id %s is not the target string's blob", got.ID)
	}
}

func TestHashWorktreeFileRefusesADirectory(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := HashWorktreeFile("sha1", dir); err != ErrNotAFile {
		t.Fatalf("err = %v, want ErrNotAFile", err)
	}
	if _, err := HashWorktreeFile("sha1", filepath.Join(dir, "absent")); !os.IsNotExist(err) {
		t.Fatalf("absent file err = %v, want not-exist", err)
	}
}

func TestObjectFormat(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"sha1", "sha256"} {
		dir := initRepo(t, format)
		r := &Repo{Runner: gitexec.NewExecRunner("git", dir, observ.NewRing(10))}
		got, err := r.ObjectFormat(context.Background())
		if err != nil || got != format {
			t.Errorf("ObjectFormat = %q, %v; want %q", got, err, format)
		}
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/git/ -run 'HashWorktree|BlobOf|ObjectFormat' 2>&1 | tail -10`
Expected: compile errors — the functions are undefined.

- [ ] **Step 3: Implement**

Create `internal/git/blob.go`:

```go
package git

import (
	"bytes"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
)

// Working-file blob ids, computed in-process (spec 2026-10-04 working
// reviews §5): no `git hash-object` per file. The id is git's — "blob <n>\0"
// + the bytes, in the repo's object format — of the WORKING-TREE bytes, so
// it is never compared with an index blob (clean filters, autocrlf).

// ErrNotAFile is a path that is neither a regular file nor a symlink (a
// directory, e.g. a nested repository `ls-files --others` lists as "sub/").
var ErrNotAFile = errors.New("not a regular file")

// WorktreeBlob is a working file's blob id plus what a numstat line needs.
type WorktreeBlob struct {
	ID     string
	Lines  int  // newline-terminated lines, plus a final unterminated one
	Binary bool // git's heuristic: a NUL in the first 8000 bytes
}

// binarySniff is how far git looks for a NUL to call a file binary.
const binarySniff = 8000

func objectHash(format string) hash.Hash {
	if format == "sha256" {
		return sha256.New()
	}
	return sha1.New()
}

// BlobFormatOf names the object format a blob id was hashed in, by length.
func BlobFormatOf(id string) string {
	if len(id) == 64 {
		return "sha256"
	}
	return "sha1"
}

// blobStats counts lines and sniffs for binary over a stream.
type blobStats struct {
	seen   int64
	nl     int
	last   byte
	binary bool
}

func (s *blobStats) Write(p []byte) (int, error) {
	if s.seen < binarySniff {
		head := p
		if rest := binarySniff - s.seen; int64(len(head)) > rest {
			head = head[:rest]
		}
		if bytes.IndexByte(head, 0) >= 0 {
			s.binary = true
		}
	}
	s.nl += bytes.Count(p, []byte{'\n'})
	if len(p) > 0 {
		s.last = p[len(p)-1]
	}
	s.seen += int64(len(p))
	return len(p), nil
}

func (s *blobStats) lines() int {
	if s.seen > 0 && s.last != '\n' {
		return s.nl + 1
	}
	return s.nl
}

// BlobOf is data's blob id in format, with its line stats.
func BlobOf(format string, data []byte) WorktreeBlob {
	h := objectHash(format)
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	var st blobStats
	st.Write(data)
	return WorktreeBlob{ID: hex.EncodeToString(h.Sum(nil)), Lines: st.lines(), Binary: st.binary}
}

// HashWorktreeFile hashes the file at path (absolute) as git would store it:
// a symlink as its target string, a regular file as its bytes — streamed, so
// a huge file is never held whole. Anything else is ErrNotAFile; an absent
// path is the OS's not-exist error.
func HashWorktreeFile(format, path string) (WorktreeBlob, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return WorktreeBlob{}, err
	}
	switch {
	case fi.Mode()&os.ModeSymlink != 0:
		target, err := os.Readlink(path)
		if err != nil {
			return WorktreeBlob{}, err
		}
		return BlobOf(format, []byte(target)), nil
	case !fi.Mode().IsRegular():
		return WorktreeBlob{}, ErrNotAFile
	}
	f, err := os.Open(path)
	if err != nil {
		return WorktreeBlob{}, err
	}
	defer f.Close()
	h := objectHash(format)
	fmt.Fprintf(h, "blob %d\x00", fi.Size())
	var st blobStats
	n, err := io.Copy(io.MultiWriter(h, &st), f)
	if err != nil {
		return WorktreeBlob{}, err
	}
	if n != fi.Size() {
		return WorktreeBlob{}, fmt.Errorf("%s changed while it was read", path)
	}
	return WorktreeBlob{ID: hex.EncodeToString(h.Sum(nil)), Lines: st.lines(), Binary: st.binary}, nil
}
```

In `internal/git/compare.go`, after `UntrackedFiles`:

```go
// ObjectFormat is the repository's object hash: "sha1" or "sha256".
func (r *Repo) ObjectFormat(ctx context.Context) (string, error) {
	res, err := r.Runner.Run(ctx, "git rev-parse (object format)",
		gitcmd.New("rev-parse").Arg("--show-object-format").ToArgv())
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(res.Stdout), nil
}
```

In `internal/engine/gitops.go`, add inside `GitOps` (after `DiffNumstat`):

```go
	// UntrackedFiles / ObjectFormat feed a working-changes review: the
	// untracked files it reviews as new files, and the hash its fingerprints
	// use.
	UntrackedFiles(ctx context.Context) ([]string, error)
	ObjectFormat(ctx context.Context) (string, error)
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go build ./... && go test ./internal/git/ -run 'HashWorktree|BlobOf|ObjectFormat' 2>&1 | tail -5`
Expected: `ok`. If any `GitOps` test double fails to compile (`grep -rn "GitOps = " internal`), give it the two methods returning zero values.

- [ ] **Step 5: Commit**

Message: `feat(git): in-process blob ids for working files, and the object-format verb`

---

### Task 3: The working review's input — untracked files + fingerprints

**Files:**
- Create: `internal/engine/review_working.go`, `internal/engine/review_working_test.go`
- Modify: `internal/engine/review_changes.go`, `internal/engine/capture_task.go`, `internal/engine/operation.go`

**Interfaces:**
- Consumes (from Task 2): `git.HashWorktreeFile`, `git.BlobOf`, `git.ParseNumstat`, `GitOps.UntrackedFiles`, `GitOps.ObjectFormat`, `GitOps.TopLevel`.
- Produces:
  - `ReviewChanges.Working bool`
  - `TaskInputs.ReviewFiles []model.NoteFile`
  - `Result.ReviewFiles []model.NoteFile`, copied by `ReviewChanges.Collect` from its inputs

- [ ] **Step 1: Write the failing tests**

Create `internal/engine/review_working_test.go`:

```go
package engine

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// prepareWorking runs a working review's Prepare and returns the inputs and
// the diff and context files it wrote.
func prepareWorking(t *testing.T, dir string, repo *git.Repo) (TaskInputs, string, string) {
	t.Helper()
	in, err := ReviewChanges{Command: "true", Dir: dir, Diff: model.DiffSpec{Rev: "HEAD"},
		RangeLabel: "working changes", Working: true}.Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(in.Cleanup)
	env := map[string]string{}
	for _, kv := range in.Env {
		k, v, _ := strings.Cut(kv, "=")
		env[k] = v
	}
	return in, readFile(t, env["GG_REVIEW_DIFF"]), readFile(t, env["GG_CONTEXT_FILE"])
}

func fileOf(files []model.NoteFile, path string) (model.NoteFile, bool) {
	for _, f := range files {
		if f.Path == path {
			return f, true
		}
	}
	return model.NoteFile{}, false
}

func gitHash(t *testing.T, dir, path string) string {
	t.Helper()
	out, err := exec.Command("git", "-C", dir, "hash-object", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReviewChangesWorkingIncludesUntrackedFiles(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	write(t, dir, "a.txt", "one\nTWO\n")
	write(t, dir, "my new.txt", "x\ny\n")
	in, diff, ctxDoc := prepareWorking(t, dir, repo)
	for _, want := range []string{"+TWO", "diff --git a/my new.txt b/my new.txt", "new file mode 100644",
		"--- /dev/null", "+++ b/my new.txt", "@@ -0,0 +1,2 @@", "+x", "+y"} {
		if !strings.Contains(diff, want) {
			t.Errorf("diff lacks %q:\n%s", want, diff)
		}
	}
	if !strings.Contains(ctxDoc, "2\t0\tmy new.txt") {
		t.Errorf("context numstat lacks the untracked file:\n%s", ctxDoc)
	}
	for _, p := range []string{"a.txt", "my new.txt"} {
		f, ok := fileOf(in.ReviewFiles, p)
		if !ok || f.Blob != gitHash(t, dir, p) || f.Deleted {
			t.Errorf("%s fingerprint = %+v (found %v), want git's blob", p, f, ok)
		}
	}
}

func TestReviewChangesWorkingSkipsIgnoredFiles(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, ".gitignore", "*.log\n", "ignore")
	write(t, dir, "x.log", "noise\n")
	write(t, dir, "keep.txt", "k\n")
	in, diff, _ := prepareWorking(t, dir, repo)
	if strings.Contains(diff, "x.log") {
		t.Errorf("an ignored file reached the diff:\n%s", diff)
	}
	if _, ok := fileOf(in.ReviewFiles, "x.log"); ok {
		t.Error("an ignored file was fingerprinted")
	}
	if _, ok := fileOf(in.ReviewFiles, "keep.txt"); !ok {
		t.Error("the untracked file was not fingerprinted")
	}
}

// The fingerprint is of the bytes Prepare read: a file rewritten while the
// agent runs keeps the reviewed blob.
func TestReviewChangesWorkingFingerprintsArePrepareTime(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	write(t, dir, "a.txt", "reviewed\n")
	in, _, _ := prepareWorking(t, dir, repo)
	write(t, dir, "a.txt", "edited later\n")
	f, _ := fileOf(in.ReviewFiles, "a.txt")
	if f.Blob != git.BlobOf("sha1", []byte("reviewed\n")).ID {
		t.Fatalf("blob %s is not the reviewed bytes'", f.Blob)
	}
}

func TestReviewChangesWorkingRecordsADeletion(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "b.txt", "b\n", "c1")
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	in, _, _ := prepareWorking(t, dir, repo)
	if f, ok := fileOf(in.ReviewFiles, "b.txt"); !ok || !f.Deleted || f.Blob != "" {
		t.Fatalf("b.txt = %+v (found %v), want Deleted", f, ok)
	}
}

// Review Focus 2: an unstaged rename is a deletion plus an untracked file.
func TestReviewChangesWorkingUnstagedRename(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "old.txt", "same\n", "c1")
	if err := os.Rename(filepath.Join(dir, "old.txt"), filepath.Join(dir, "new.txt")); err != nil {
		t.Fatal(err)
	}
	in, _, _ := prepareWorking(t, dir, repo)
	if f, _ := fileOf(in.ReviewFiles, "old.txt"); !f.Deleted {
		t.Errorf("old.txt = %+v, want Deleted", f)
	}
	if f, _ := fileOf(in.ReviewFiles, "new.txt"); f.Blob == "" {
		t.Errorf("new.txt = %+v, want a blob", f)
	}
}

func TestReviewChangesWorkingBinaryUntracked(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	write(t, dir, "bin.dat", "a\x00b")
	_, diff, ctxDoc := prepareWorking(t, dir, repo)
	if !strings.Contains(diff, "Binary files /dev/null and b/bin.dat differ") {
		t.Errorf("diff:\n%s", diff)
	}
	if !strings.Contains(ctxDoc, "-\t-\tbin.dat") {
		t.Errorf("numstat:\n%s", ctxDoc)
	}
}

// Review Focus 3: past the cap the diff is truncated as today, and the big
// file is still fingerprinted.
func TestReviewChangesWorkingUntrackedPastTheCap(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	write(t, dir, "huge.txt", strings.Repeat("x\n", MaxDiffBytes))
	in, diff, ctxDoc := prepareWorking(t, dir, repo)
	if !strings.Contains(diff, "(diff truncated") || !strings.Contains(ctxDoc, "truncated") {
		t.Errorf("the diff must say it was truncated:\n%.200s", diff)
	}
	if f, ok := fileOf(in.ReviewFiles, "huge.txt"); !ok || f.Blob != gitHash(t, dir, "huge.txt") {
		t.Errorf("huge.txt fingerprint = %+v", f)
	}
}

// Review Focus 4: a nested repository is listed as "sub/" — not a file.
func TestReviewChangesWorkingSkipsADirectoryEntry(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	sub := filepath.Join(dir, "sub")
	if out, err := exec.Command("git", "init", "-q", sub).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	write(t, dir, "sub/inner.txt", "i\n")
	in, _, _ := prepareWorking(t, dir, repo)
	for _, f := range in.ReviewFiles {
		if strings.HasPrefix(f.Path, "sub") {
			t.Fatalf("a directory entry was fingerprinted: %+v", f)
		}
	}
}

// Only a working review reads untracked files and fingerprints.
func TestReviewChangesRangeHasNoFingerprints(t *testing.T) {
	t.Parallel()
	dir, repo := newRepo(t)
	stageAndCommit(t, dir, repo, "a.txt", "one\n", "c1")
	stageAndCommit(t, dir, repo, "a.txt", "two\n", "c2")
	write(t, dir, "u.txt", "u\n")
	in, err := ReviewChanges{Command: "true", Dir: dir, Diff: model.DiffSpec{Rev: "HEAD~1..HEAD"}}.
		Prepare(context.Background(), OpDeps{Repo: repo})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Cleanup()
	if in.ReviewFiles != nil {
		t.Fatalf("ReviewFiles = %+v, want none", in.ReviewFiles)
	}
}

func TestReviewChangesCollectCarriesTheFingerprints(t *testing.T) {
	t.Parallel()
	files := []model.NoteFile{{Path: "a", Blob: "b"}}
	res, _ := ReviewChanges{Working: true}.Collect(TaskInputs{ReviewFiles: files}, []byte("r"))
	if len(res.ReviewFiles) != 1 || res.ReviewFiles[0] != files[0] {
		t.Fatalf("ReviewFiles = %+v", res.ReviewFiles)
	}
}
```

Check `gittest.BasicRepo` first: if it commits a file, `newRepo` starts with one commit, which these tests rely on for `HEAD`. If it does not, add a `stageAndCommit` seed at the top of the tests that lack one.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/engine/ -run 'ReviewChanges' 2>&1 | tail -10`
Expected: compile errors — `Working` and `ReviewFiles` are undefined.

- [ ] **Step 3: Implement**

`internal/engine/capture_task.go`, in `TaskInputs`:

```go
	// ReviewFiles is a working-changes review's fingerprint of every file it
	// read (ReviewChanges.Working), taken when the input was built.
	ReviewFiles []model.NoteFile
```

`internal/engine/operation.go`, in `Result`:

```go
	// ReviewFiles carries a working review's fingerprints from Prepare to
	// whoever stores the review (domain.SaveReview).
	ReviewFiles []model.NoteFile
```

(Add the `model` import where needed.)

Create `internal/engine/review_working.go`:

```go
package engine

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// workingInput is what a working-changes review adds to `git diff HEAD`
// (spec 2026-10-04 working reviews §5–6): a fingerprint of every file the
// review reads, and the untracked files as new-file patches plus their
// numstat records (NUL-terminated, as `--numstat -z` prints them).
type workingInput struct {
	files   []model.NoteFile
	patch   string
	numstat string
	overCap bool // a file was left out of the patch because it alone breaks the cap
}

// reviewWorkingInput fingerprints the paths of the working diff (stats) and
// every untracked file, and synthesizes the untracked files' patches while
// the diff (diffBytes so far) stays under MaxDiffBytes. Paths are read from
// the worktree's top level, never op.Dir (which may be a subdirectory).
func reviewWorkingInput(ctx context.Context, deps OpDeps, stats []model.DiffStat, diffBytes int) (workingInput, error) {
	top, err := deps.Repo.TopLevel(ctx)
	if err != nil {
		return workingInput{}, err
	}
	top = strings.TrimSpace(top)
	format, ferr := deps.Repo.ObjectFormat(ctx)
	if ferr != nil || format == "" {
		format = "sha1"
	}
	var w workingInput
	seen := map[string]bool{}
	abs := func(p string) string { return filepath.Join(top, filepath.FromSlash(p)) }
	add := func(p string, b git.WorktreeBlob, err error) bool {
		f := model.NoteFile{Path: p}
		switch {
		case errors.Is(err, fs.ErrNotExist):
			f.Deleted = true
		case err != nil:
			return false // present but unreadable: it proves nothing, so it is not fingerprinted
		default:
			f.Blob = b.ID
		}
		w.files = append(w.files, f)
		return true
	}
	for _, st := range stats {
		for _, p := range []string{st.OldPath, st.Path} {
			if p == "" || seen[p] {
				continue
			}
			seen[p] = true
			b, herr := git.HashWorktreeFile(format, abs(p))
			add(p, b, herr)
		}
	}
	untracked, err := deps.Repo.UntrackedFiles(ctx)
	if err != nil {
		return workingInput{}, err
	}
	var patch, numstat strings.Builder
	size := diffBytes
	for _, p := range untracked {
		if seen[p] || strings.HasSuffix(p, "/") { // "sub/" = a nested repository, not a file
			continue
		}
		seen[p] = true
		full := abs(p)
		if fi, serr := os.Lstat(full); serr == nil && fi.Mode().IsRegular() && size <= MaxDiffBytes &&
			fi.Size() <= int64(MaxDiffBytes-size) {
			if data, rerr := os.ReadFile(full); rerr == nil {
				b := git.BlobOf(format, data)
				add(p, b, nil)
				text := newFilePatch(p, fi.Mode(), data, b.Binary)
				patch.WriteString(text)
				size += len(text)
				numstat.WriteString(numstatRecord(p, b))
				continue
			}
		}
		b, herr := git.HashWorktreeFile(format, full)
		if add(p, b, herr) && !errors.Is(herr, fs.ErrNotExist) {
			numstat.WriteString(numstatRecord(p, b))
			w.overCap = true
		}
	}
	w.patch, w.numstat = patch.String(), numstat.String()
	return w, nil
}

// numstatRecord is one `--numstat -z` record for an added file.
func numstatRecord(path string, b git.WorktreeBlob) string {
	if b.Binary {
		return "-\t-\t" + path + "\x00"
	}
	return fmt.Sprintf("%d\t0\t%s\x00", b.Lines, path)
}

// newFilePatch is git's patch for an untracked file as if it were added:
// every line added, a binary file only named, an empty one header-only.
func newFilePatch(path string, mode fs.FileMode, data []byte, binary bool) string {
	var b strings.Builder
	fileMode := "100644"
	if mode&0o111 != 0 {
		fileMode = "100755"
	}
	fmt.Fprintf(&b, "diff --git a/%s b/%s\nnew file mode %s\n", path, path, fileMode)
	switch {
	case len(data) == 0:
		return b.String()
	case binary:
		fmt.Fprintf(&b, "Binary files /dev/null and b/%s differ\n", path)
		return b.String()
	}
	text := string(data)
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	count := fmt.Sprintf("1,%d", len(lines))
	if len(lines) == 1 {
		count = "1" // git drops ",1"
	}
	fmt.Fprintf(&b, "--- /dev/null\n+++ b/%s\n@@ -0,0 +%s @@\n", path, count)
	for _, l := range lines {
		b.WriteString("+" + l + "\n")
	}
	if !strings.HasSuffix(text, "\n") {
		b.WriteString("\\ No newline at end of file\n")
	}
	return b.String()
}
```

`internal/engine/review_changes.go`:
- Add the field to `ReviewChanges`:
  ```go
  	// Working: a review of uncommitted changes. The input gains the
  	// untracked files as new-file patches, and Prepare fingerprints every
  	// reviewed file (TaskInputs.ReviewFiles).
  	Working bool
  ```
- Replace the head of `Prepare` up to the truncation logic:
  ```go
  	diff, err := deps.Repo.DiffPatch(ctx, op.Diff)
  	if err != nil {
  		return TaskInputs{}, err
  	}
  	stat, _ := deps.Repo.DiffNumstat(ctx, op.Diff)
  	var files []model.NoteFile
  	overCap := false
  	if op.Working {
  		w, werr := reviewWorkingInput(ctx, deps, git.ParseNumstat(stat), len(diff))
  		if werr != nil {
  			return TaskInputs{}, werr
  		}
  		files, overCap = w.files, w.overCap
  		diff += w.patch
  		stat += w.numstat
  	}
  	truncated := overCap || len(diff) > MaxDiffBytes
  ```
  (Add the `git` import.)
- Return `ReviewFiles: files` in the `TaskInputs` literal.
- `Collect`:
  ```go
  func (op ReviewChanges) Collect(in TaskInputs, stdout []byte) (Result, error) {
  	return Result{Captured: collectCaptured(in.MessageFile, stdout), ReviewFiles: in.ReviewFiles}.
  		WithSummary("reviewed %s", op.RangeLabel), nil
  }
  ```
- In `reviewSummary`, change the numstat heading when `op.Working`:
  ```go
  	heading := "\n\n## Files changed (git diff --numstat)\n"
  	if op.Working {
  		heading = "\n\n## Files changed (git diff --numstat; untracked files as added)\n"
  	}
  	b.WriteString(heading)
  ```
  This replaces the existing `b.WriteString("\n\n## Files changed (git diff --numstat)\n")`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/engine/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

Message: `feat(engine): a working review reads untracked files and fingerprints what it read`

---

### Task 4: Storing a working review

**Files:**
- Modify: `internal/domain/review_notes.go`, `internal/domain/review.go`, `internal/domain/task_kinds.go`, `internal/domain/tasks.go`
- Test: `internal/domain/review_notes_test.go`, `internal/domain/tasks_test.go`, `internal/domain/review_test.go` (or wherever `ReviewReport` is tested — `grep -ln "ReviewReport(" internal/domain/*_test.go`)

**Interfaces:**
- Consumes: `engine.Result.ReviewFiles`, `engine.TaskInputs.ReviewFiles`, `ReviewChanges.Working`, `model.Note.Files`.
- Produces:
  - `SaveReview.Files []model.NoteFile`
  - `ErrNoReviewWorktree`
  - `TaskSpec.Store func(ctx context.Context, noteID, text string, files []model.NoteFile) (id, warn string, err error)`
  - `ReviewReport` returns `NoteID` for a working review

- [ ] **Step 1: Write the failing tests**

In `internal/domain/review_notes_test.go`, replace `TestSaveReviewRefusesWorkingChanges` with:

```go
// A working-changes review is a note in this worktree's part file: path-less,
// live, tagged review, carrying the fingerprints it was given.
func TestSaveReviewStoresAWorkingReview(t *testing.T) {
	t.Parallel()
	dir, svc, _ := reviewRepo(t)
	ctx := context.Background()
	files := []model.NoteFile{{Path: "f.txt", Blob: strings.Repeat("a", 40)}, {Path: "gone.txt", Deleted: true}}
	id, warn, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Agent: "Claude Code", Text: "x", Files: files})
	if err != nil || warn != "" {
		t.Fatalf("SaveReview: %v %q", err, warn)
	}
	_ = dir
	top, _ := svc.TopLevel(ctx) // the spelling the store keys the part by
	st := svc.notesStore(ctx)
	got, err := st.Load(notes.WorktreePart(strings.TrimSpace(top)))
	if err != nil || len(got) != 1 {
		all, _ := st.LoadAll()
		t.Fatalf("worktree part = %+v (%v); store = %+v", got, err, all)
	}
	n := got[0]
	if n.ID != id || !n.IsWorkingReview() || n.Author != "Claude Code" || n.Summary != "Review: working changes" ||
		!reflect.DeepEqual(n.Files, files) || n.Scope != "" {
		t.Fatalf("note = %+v", n)
	}
	if cs, _ := st.Load(notes.PartCommits); len(cs) != 0 {
		t.Fatalf("a working review reached commits.toml: %+v", cs)
	}
}
```

(Imports: `reflect`, and `notes` = `github.com/homeend/gigagit/internal/notes`.)

Add `TestReviewReportStoresAWorkingReview` next to the existing `ReviewReport` tests (skip on Windows, as they do):

```go
func TestReviewReportStoresAWorkingReview(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	dir, svc, _ := reviewRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "new.txt"), []byte("n\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	res, err := svc.ReviewReport(ctx, WorkingReviewTarget(), "Echo",
		`printf '{"version":1,"summary":"ok","files":[]}' > "$GG_MESSAGE_FILE"`, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.NoteID == "" {
		t.Fatal("a working review must be stored")
	}
	r, err := svc.Review(ctx, res.NoteID)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, f := range r.Files {
		found = found || (f.Path == "new.txt" && f.Blob != "")
	}
	if !found {
		t.Fatalf("Files = %+v, want the untracked new.txt fingerprinted", r.Files)
	}
}
```

(`Review.Files` arrives in Task 5. Until then this test will not compile. Write it now, and expect it to compile only after Step 3 adds the `Files` field to `Review` here. To keep this task self-contained, add the field `Files []model.NoteFile` and `Worktree string` to `Review` in this task. `reviewOf` filling them, and `reviewNotes` reading the worktree part, are what make this test go green at the end of THIS task. Task 5 then adds the matching state.)

In `internal/domain/tasks_test.go`:
- Update `storedReviewSpec`'s parameter type and the two `Store` literals to the new 4-argument signature (`files []model.NoteFile` ignored).
- Add a test that the files the op returns reach `Store`, and that `RetrySave` sends them again. Give `blockOp` (find its definition with `grep -n "type blockOp" internal/domain/*_test.go`) a `files []model.NoteFile` field that its `Run`/`Collect` copies into `Result.ReviewFiles`:

```go
func TestStoreReceivesTheReviewedFilesAndRetrySaveResendsThem(t *testing.T) {
	t.Parallel()
	m, svc := newTestTasks(t)
	files := []model.NoteFile{{Path: "a.txt", Blob: "abc"}}
	var fail atomic.Bool
	fail.Store(true)
	var got [][]model.NoteFile
	rel := make(chan struct{})
	close(rel)
	spec := headlessSpec(svc, "review — files", blockOp{started: make(chan string, 1), release: rel, key: "review — files", out: "r", files: files})
	spec.Store = func(_ context.Context, _, _ string, fs []model.NoteFile) (string, string, error) {
		got = append(got, fs)
		if fail.Load() {
			return "", "", errors.New("disk full")
		}
		return "n1", "", nil
	}
	info := waitInfo(t, m, m.Submit(spec), "ended", func(i TaskInfo) bool { return !i.State.Live() })
	fail.Store(false)
	if err := m.RetrySave(info.ID); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !reflect.DeepEqual(got[0], files) || !reflect.DeepEqual(got[1], files) {
		t.Fatalf("Store saw %+v, want the op's files twice", got)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ -run 'SaveReview|ReviewReport|Store' 2>&1 | tail -15`
Expected: compile errors (`SaveReview.Files`, the `Store` arity, `Review.Files`).

- [ ] **Step 3: Implement**

`review_notes.go`:
- Add `ReviewOnWorktree` at the END of the `ReviewNoteKind` consts:
  ```go
  	ReviewOnWorktree                     // a review of a worktree's uncommitted changes (spec 2026-10-04 working reviews)
  ```
- Add the error: `ErrNoReviewWorktree = errors.New("review: no worktree to attach a working review to")`.
- Add to `SaveReview`: `Files []model.NoteFile // a working review's fingerprints (engine Prepare)`.
- Add to `Review`: `Worktree string // a working review's checkout ("" otherwise)` and `Files []model.NoteFile // a working review's fingerprints`.
- `SaveReview()`: replace the opening guard:
  ```go
  	t := s.FillReviewCommit(ctx, cmd.Target)
  	wt := ""
  	if t.Kind == ReviewWorking {
  		top, err := s.TopLevel(ctx)
  		if err != nil || strings.TrimSpace(top) == "" {
  			return "", "", ErrNoReviewWorktree
  		}
  		wt = filepath.Clean(strings.TrimSpace(top))
  	} else if t.Commit == "" {
  		return "", "", ErrNoReviewCommit
  	}
  ```
  Call `s.putReview(st, t, wt, cmd)`.
- `putReview(st notes.Store, t ReviewTarget, wt string, cmd SaveReview)`: build the address:
  ```go
  	addr := model.FileAddress{State: model.StateCommitted, Commit: t.Commit, Branch: t.Branch}
  	scope := t.Range
  	if t.Kind == ReviewWorking {
  		// One note per review in this worktree's part file (spec §4): the
  		// cleaned top level is what PartOf and the readers key it by.
  		addr, scope = model.FileAddress{State: model.StateUnstaged, Worktree: wt}, ""
  	}
  	n := model.Note{ID: cmd.NoteID, Source: model.NoteSourceAgent, Author: cmd.Agent,
  		Address: addr, Side: model.NoteSideNew, Tags: []string{model.ReviewTag}, Scope: scope,
  		Summary: reviewSummary(t), Rationale: cmd.Text, Files: cmd.Files, Created: now, Updated: now}
  ```
- `reviewOf`: when `n.IsWorkingReview()`, return a working Review:
  ```go
  	if n.IsWorkingReview() {
  		r := Review{ID: n.ID, Kind: ReviewOnWorktree, Worktree: n.Address.Worktree, Files: n.Files,
  			Agent: n.Author, Summary: n.Summary, Text: n.Rationale, Created: n.Created, Updated: n.Updated}
  		if doc, err := notebatch.ParseReview([]byte(n.Rationale)); err == nil {
  			r.Doc = &doc
  		}
  		return r
  	}
  ```
- `reviewNotes`: read commits plus this worktree's part, keeping the worktree filter:
  ```go
  	parts := []notes.Part{notes.PartCommits}
  	top, _ := s.TopLevel(ctx)
  	top = strings.TrimSpace(top)
  	if top != "" {
  		parts = append(parts, notes.WorktreePart(top))
  	}
  	all, err := loadParts(st, parts...)
  	if err != nil {
  		return nil, err
  	}
  	var out []model.Note
  	for _, n := range all {
  		if n.IsReply() {
  			continue
  		}
  		if n.IsReviewNote() || (n.IsWorkingReview() && sameWorktreePath(n.Address.Worktree, top)) {
  			out = append(out, n)
  		}
  	}
  ```

`review.go` (`ReviewReport`):
- Set `Working: target.Kind == ReviewWorking` on the op.
- Delete the `if target.Kind == ReviewWorking { return out, nil }` early return.
- Pass `Files: res.ReviewFiles` into `SaveReview`.
- Update the doc comments of `ReviewReport` and `ReviewResult.NoteID`: a working review is stored in this worktree's notes.

`task_kinds.go`:
- Change `TaskSpec.Store` to `Store func(ctx context.Context, noteID, text string, files []model.NoteFile) (id, warn string, err error)`. Document `files` as the reviewed files' fingerprints from Prepare (nil for a non-working review).
- In `ReviewTask`, set `Working: target.Kind == ReviewWorking` on the op and always attach Store:
  ```go
  	agent := spec.Agent
  	spec.Store = func(ctx context.Context, noteID, text string, files []model.NoteFile) (string, string, error) {
  		return s.SaveReview(ctx, SaveReview{Target: target, Agent: agent, Text: text, NoteID: noteID, Files: files})
  	}
  ```

`tasks.go`:
- Add `files []model.NoteFile // the fingerprints the latest result was stored with (RetrySave re-sends them)` to `type task struct`.
- `setResult(t *task, result string, files []model.NoteFile)`: under the lock set `t.files = files`, read `store, noteID`, and call `m.storeResult(t, store, noteID, result, files)`.
- `storeResult(..., text string, files []model.NoteFile)` passes `files` to `store`.
- `RetrySave`: read `files := t.files` under the lock and pass it on.
- `runHeadless`: `m.setResult(t, result, res.ReviewFiles)`.
- `runInteractive`: `m.setResult(t, out, res.ReviewFiles)`, where `res` is the `Collect(in, nil)` result.
- Fix `TestInteractiveReviewUpdatesOneNote`'s direct `m.setResult` calls by adding a `nil` argument.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go build ./... && go test ./internal/domain/ 2>&1 | tail -10`
Expected: `ok`. A compile error elsewhere (`grep -rn "spec.Store\|\.Store = func" internal`) gets the new arity.

- [ ] **Step 5: Commit**

Message: `feat(domain): a working-changes review is stored as a note in its worktree's part`

---

### Task 5: Matching — file states, the blob cache, `WorkingReviews`

**Files:**
- Create: `internal/domain/working_review.go`, `internal/domain/working_review_test.go`

**Interfaces:**
- Consumes: `git.HashWorktreeFile`, `git.BlobFormatOf`, `Review.Files`/`Worktree` (Task 4).
- Produces:
  - `type WorkingFileState int`, with consts `WorkingFileMatches`, `WorkingFileChanged`, `WorkingFileGone`
  - `type WorkingReviewMatch struct { States map[string]WorkingFileState; Current bool }`
  - `func WorkingReviewState(worktree string, files []model.NoteFile) WorkingReviewMatch`
  - `type WorkingReview struct { Review; WorkingReviewMatch }`
  - `func (s *Service) WorkingReviews(ctx) ([]WorkingReview, error)` — this worktree's reviews, newest first, current and outdated alike

- [ ] **Step 1: Write the failing tests**

Create `internal/domain/working_review_test.go`:

```go
package domain

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

func blob(s string) string { return git.BlobOf("sha1", []byte(s)).ID }

func writeAt(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The spec §9 walk: edit, stage-unchanged, delete, recreate.
func TestWorkingReviewStateFollowsTheFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeAt(t, dir, "a.txt", "A\n")
	writeAt(t, dir, "c.txt", "C\n")
	files := []model.NoteFile{{Path: "a.txt", Blob: blob("A\n")}, {Path: "c.txt", Blob: blob("C\n")}, {Path: "b.txt", Deleted: true}}
	m := WorkingReviewState(dir, files)
	if !m.Current || m.States["a.txt"] != WorkingFileMatches || m.States["b.txt"] != WorkingFileMatches {
		t.Fatalf("fresh: %+v", m)
	}
	writeAt(t, dir, "a.txt", "A edited\n")
	m = WorkingReviewState(dir, files)
	if m.States["a.txt"] != WorkingFileChanged || !m.Current {
		t.Fatalf("after editing a: %+v", m)
	}
	os.Remove(filepath.Join(dir, "c.txt"))
	writeAt(t, dir, "b.txt", "back\n")
	m = WorkingReviewState(dir, files)
	if m.States["c.txt"] != WorkingFileGone || m.States["b.txt"] != WorkingFileChanged || m.Current {
		t.Fatalf("after delete/recreate: %+v, want outdated", m)
	}
}

// Staging an unchanged file keeps the review: the working bytes are the same.
func TestWorkingReviewSurvivesStaging(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	writeAt(t, dir, "a.txt", "reviewed\n")
	runGitIn(t, dir, "add", "a.txt")
	top, _ := svc.TopLevel(context.Background())
	if m := WorkingReviewState(top, []model.NoteFile{{Path: "a.txt", Blob: blob("reviewed\n")}}); !m.Current {
		t.Fatalf("staged unchanged: %+v", m)
	}
}

func TestWorkingReviewStateWorktreeGone(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "removed")
	m := WorkingReviewState(gone, []model.NoteFile{{Path: "x", Deleted: true}})
	if m.Current || m.States["x"] != WorkingFileGone {
		t.Fatalf("a removed worktree: %+v, want every file gone", m)
	}
}

// A sha256 repository's blob ids match too: the algorithm comes from the id.
func TestWorkingReviewStateSha256(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if out, err := exec.Command("git", "init", "-q", "--object-format=sha256", dir).CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	writeAt(t, dir, "a.txt", "x\n")
	out, err := exec.Command("git", "-C", dir, "hash-object", "a.txt").Output()
	if err != nil {
		t.Fatal(err)
	}
	if m := WorkingReviewState(dir, []model.NoteFile{{Path: "a.txt", Blob: strings.TrimSpace(string(out))}}); !m.Current {
		t.Fatalf("sha256 blob did not match: %+v", m)
	}
}

func TestWorkingBlobCacheRehashesOnlyChangedFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeAt(t, dir, "a.txt", "one\n")
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	c := newBlobCache()
	for i := 0; i < 3; i++ {
		if _, err := c.id("sha1", p); err != nil {
			t.Fatal(err)
		}
	}
	if c.hashes != 1 {
		t.Fatalf("hashed %d times for an unchanged file, want 1", c.hashes)
	}
	writeAt(t, dir, "a.txt", "two, longer\n")
	os.Chtimes(p, old.Add(time.Minute), old.Add(time.Minute))
	id, _ := c.id("sha1", p)
	if c.hashes != 2 || id != blob("two, longer\n") {
		t.Fatalf("after a change: hashes %d id %s", c.hashes, id)
	}
}

// Review Focus 1: a same-size rewrite within the mtime tick is never served stale.
func TestWorkingBlobCacheNeverTrustsAFreshMtime(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "a.txt")
	writeAt(t, dir, "a.txt", "AAAA\n")
	c := newBlobCache()
	if id, _ := c.id("sha1", p); id != blob("AAAA\n") {
		t.Fatal("first hash")
	}
	fi, _ := os.Stat(p)
	writeAt(t, dir, "a.txt", "BBBB\n")
	os.Chtimes(p, fi.ModTime(), fi.ModTime()) // same size, same mtime
	if id, _ := c.id("sha1", p); id != blob("BBBB\n") {
		t.Fatalf("a fresh same-size rewrite was served from the cache")
	}
}

// Own worktree only: a sibling worktree lists none of this one's reviews.
func TestWorkingReviewsOnlyInTheirWorktree(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	notesDir := t.TempDir()
	svc.UseNotesDir(notesDir)
	ctx := context.Background()
	if _, _, err := svc.SaveReview(ctx, SaveReview{Target: WorkingReviewTarget(), Text: "x",
		Files: []model.NoteFile{{Path: "gone", Deleted: true}}}); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "wt2")
	runGitIn(t, dir, "worktree", "add", "-b", "side", other)
	svc2 := Open(other)
	svc2.UseNotesDir(notesDir)
	if got, _ := svc2.WorkingReviews(ctx); len(got) != 0 {
		t.Fatalf("the sibling worktree sees %d reviews", len(got))
	}
	got, err := svc.WorkingReviews(ctx)
	if err != nil || len(got) != 1 || !got[0].Current || got[0].Kind != ReviewOnWorktree {
		t.Fatalf("own worktree: %+v %v", got, err)
	}
}
```

(Check the helper names in the domain test files: `newRealRepo`, `runGitIn`, `UseNotesDir`, and `Open` vs `domain.Open`. If no `Open(dir)` constructor exists in package tests, use the one `newRealRepo` uses.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ -run 'WorkingReview|WorkingBlob' 2>&1 | tail -10`
Expected: compile errors — the types are undefined.

- [ ] **Step 3: Implement**

Create `internal/domain/working_review.go`:

```go
package domain

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/homeend/gigagit/internal/git"
	"github.com/homeend/gigagit/internal/model"
)

// A working-changes review (spec 2026-10-04 working reviews) identifies its
// files by (path, blob id). This file matches them against the worktree now.

// WorkingFileState is one reviewed file against the worktree now.
type WorkingFileState int

const (
	WorkingFileMatches WorkingFileState = iota // the reviewed bytes (or a reviewed deletion, still absent)
	WorkingFileChanged                         // edited since, or recreated after a reviewed deletion
	WorkingFileGone                            // the reviewed file is no longer there
)

// WorkingReviewMatch is a working review's files against the worktree:
// each file's state, and Current while at least one still matches (§7).
type WorkingReviewMatch struct {
	States  map[string]WorkingFileState
	Current bool
}

// WorkingReview is a stored working review, matched now.
type WorkingReview struct {
	Review
	WorkingReviewMatch
}

// WorkingReviewState matches files against worktree. A worktree that is gone
// leaves every file gone — the review is outdated (a reviewed deletion must
// not keep "matching" a directory that no longer exists).
func WorkingReviewState(worktree string, files []model.NoteFile) WorkingReviewMatch {
	m := WorkingReviewMatch{States: make(map[string]WorkingFileState, len(files))}
	fi, err := os.Stat(worktree)
	if worktree == "" || err != nil || !fi.IsDir() {
		for _, f := range files {
			m.States[f.Path] = WorkingFileGone
		}
		return m
	}
	for _, f := range files {
		st := matchWorkingFile(filepath.Join(worktree, filepath.FromSlash(f.Path)), f)
		m.States[f.Path] = st
		m.Current = m.Current || st == WorkingFileMatches
	}
	return m
}

func matchWorkingFile(abs string, f model.NoteFile) WorkingFileState {
	if f.Deleted {
		if _, err := os.Lstat(abs); errors.Is(err, fs.ErrNotExist) {
			return WorkingFileMatches
		}
		return WorkingFileChanged
	}
	id, err := workingBlobs.id(git.BlobFormatOf(f.Blob), abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return WorkingFileGone
	case err != nil || id != f.Blob:
		return WorkingFileChanged
	}
	return WorkingFileMatches
}

// blobCacheMax bounds the cache; past it the cache starts over.
const blobCacheMax = 20000

// blobFreshness: a file modified this recently is hashed every time — a
// same-size rewrite inside the filesystem's mtime granularity would otherwise
// be served from the cache (git's "racily clean" problem).
const blobFreshness = 2 * time.Second

// workingBlobs caches working-file blob ids process-wide by absolute path,
// trusted while the file's mtime and size are unchanged (spec §7): a status
// refresh re-hashes only files that changed on disk.
var workingBlobs = newBlobCache()

type blobEntry struct {
	format string
	mtime  time.Time
	size   int64
	id     string
}

type blobCache struct {
	mu     sync.Mutex
	m      map[string]blobEntry
	hashes int // how many files were actually hashed (tests)
}

func newBlobCache() *blobCache { return &blobCache{m: map[string]blobEntry{}} }

func (c *blobCache) id(format, abs string) (string, error) {
	fi, err := os.Lstat(abs)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	e, ok := c.m[abs]
	c.mu.Unlock()
	if ok && e.format == format && e.size == fi.Size() && e.mtime.Equal(fi.ModTime()) {
		return e.id, nil
	}
	b, err := git.HashWorktreeFile(format, abs)
	if err != nil {
		return "", err
	}
	c.mu.Lock()
	c.hashes++
	if time.Since(fi.ModTime()) >= blobFreshness {
		if len(c.m) >= blobCacheMax {
			c.m = map[string]blobEntry{}
		}
		c.m[abs] = blobEntry{format: format, mtime: fi.ModTime(), size: fi.Size(), id: b.ID}
	}
	c.mu.Unlock()
	return b.ID, nil
}

// WorkingReviews is this worktree's working reviews, newest first, each
// matched now — current and outdated alike (View all notes lists both).
func (s *Service) WorkingReviews(ctx context.Context) ([]WorkingReview, error) {
	ns, err := s.reviewNotes(ctx)
	if err != nil {
		return nil, err
	}
	var out []WorkingReview
	for _, n := range ns {
		if !n.IsWorkingReview() {
			continue
		}
		r := s.reviewOf(ctx, n, map[string]string{})
		out = append(out, WorkingReview{Review: r, WorkingReviewMatch: WorkingReviewState(r.Worktree, r.Files)})
	}
	return out, nil
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ -run 'WorkingReview|WorkingBlob|SaveReview|ReviewReport' 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

Message: `feat(domain): match a working review's files against the worktree`

---

### Task 6: Sweep, counts, overview, note listing

**Files:**
- Modify: `internal/domain/notes_sweep.go`, `internal/domain/notes.go`, `internal/domain/notes_overview.go`
- Test: `internal/domain/working_review_test.go` (append)

**Interfaces:**
- Consumes: `WorkingReviewState`, `WorkingReviews`.
- Produces:
  - `NoteCounts.WorkingReviews []ReviewHead`
  - `NotesOverview.WorkingReviews []WorkingReview`, kept by `ShownOn`
  - `resolveOne` resolves every entry-level note as active

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/working_review_test.go`. Read `notes_sweep_test.go` first for how other sweep tests set `max_age_days` and age a note (`SetNotesPolicy(1, 0)` plus a `Created` in the past put directly through the store):

```go
// putWorkingReview stores a working review of dir's a.txt (blob of reviewed)
// created at created, directly through the store.
func putWorkingReview(t *testing.T, svc *Service, top, reviewed string, created time.Time) string {
	t.Helper()
	st := svc.notesStore(context.Background())
	n := model.Note{ID: "rv" + reviewed[:1], Source: model.NoteSourceAgent, Tags: []string{model.ReviewTag},
		Address: model.FileAddress{State: model.StateUnstaged, Worktree: top},
		Summary: "Review: working changes", Files: []model.NoteFile{{Path: "a.txt", Blob: blob(reviewed)}},
		Created: created, Updated: created}
	if err := st.Put(n); err != nil {
		t.Fatal(err)
	}
	return n.ID
}

func TestSweepWorkingReviews(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		onDisk   string // a.txt now
		age      time.Duration
		wantKept bool
	}{
		{"current and old: kept", "A\n", 400 * 24 * time.Hour, true},
		{"outdated and young: kept", "edited\n", time.Hour, true},
		{"outdated and old: dropped", "edited\n", 400 * 24 * time.Hour, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dir, svc := newRealRepo(t)
			svc.UseNotesDir(t.TempDir())
			svc.SetNotesPolicy(30, 0)
			top, _ := svc.TopLevel(context.Background())
			writeAt(t, dir, "a.txt", c.onDisk)
			id := putWorkingReview(t, svc, top, "A\n", time.Now().UTC().Add(-c.age))
			if _, err := svc.sweepNotes(context.Background()); err != nil {
				t.Fatal(err)
			}
			all, _ := svc.notesStore(context.Background()).LoadAll()
			kept := false
			for _, n := range all {
				kept = kept || n.ID == id
			}
			if kept != c.wantKept {
				t.Fatalf("kept = %v, want %v", kept, c.wantKept)
			}
		})
	}
}

func TestNoteCountsListWorkingReviewsApart(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	top, _ := svc.TopLevel(context.Background())
	writeAt(t, dir, "a.txt", "A\n")
	putWorkingReview(t, svc, top, "A\n", time.Now().UTC())
	c, err := svc.NoteCounts(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(c.WorkingReviews) != 1 || len(c.Reviews) != 0 || len(c.ByPath) != 0 {
		t.Fatalf("counts: working %d reviews %d byPath %v", len(c.WorkingReviews), len(c.Reviews), c.ByPath)
	}
}

func TestNotesOverviewListsWorkingReviewsWithTheirState(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	top, _ := svc.TopLevel(context.Background())
	writeAt(t, dir, "a.txt", "A\n")
	putWorkingReview(t, svc, top, "A\n", time.Now().UTC())          // current
	putWorkingReview(t, svc, top, "B\n", time.Now().UTC().Add(-time.Minute)) // outdated
	ov, err := svc.NotesOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ov.WorkingReviews) != 2 || !ov.WorkingReviews[0].Current || ov.WorkingReviews[1].Current {
		t.Fatalf("working reviews = %+v", ov.WorkingReviews)
	}
	if len(ov.Unstaged) != 0 || len(ov.Commits) != 0 || ov.Count() != 2 {
		t.Fatalf("leaked: unstaged %d commits %d count %d", len(ov.Unstaged), len(ov.Commits), ov.Count())
	}
	if got := ov.ShownOn([]string{"main"}); len(got.WorkingReviews) != 2 {
		t.Fatalf("ShownOn dropped the working reviews")
	}
}

// `gg note list` reaches a working review through NoteAddresses → NotesAt.
func TestNotesAtShowsAWorkingReviewItself(t *testing.T) {
	t.Parallel()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	ctx := context.Background()
	top, _ := svc.TopLevel(ctx)
	writeAt(t, dir, "a.txt", "A\n")
	id := putWorkingReview(t, svc, top, "A\n", time.Now().UTC())
	addrs, _ := svc.NoteAddresses(ctx)
	found := false
	for _, a := range addrs {
		rs, err := svc.NotesAt(ctx, a)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			found = found || (r.Note.ID == id && r.Status == model.NoteActive)
		}
	}
	if !found {
		t.Fatal("the working review is not listed")
	}
}
```

(Check `ShownOn`'s name and signature in `notes_overview.go` around line 280.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ -run 'SweepWorkingReviews|NoteCountsListWorking|NotesOverviewListsWorking|NotesAtShowsAWorking' 2>&1 | tail -15`
Expected: compile errors (the missing fields), then failures.

- [ ] **Step 3: Implement**

`notes_sweep.go`, in the phase-1 loop, BEFORE `if n.IsCommitLevel()`:

```go
		// A working review (and its replies, which copy its address) has no
		// line to re-anchor. It is dropped once no reviewed file matches
		// (outdated) AND it is past max_age_days; a current one is never
		// swept. Matched against the note's own worktree — the sweep reads
		// every worktree's part.
		if n.IsWorktreeLevel() {
			if !n.IsReply() && n.IsWorkingReview() && !cutoff.IsZero() && n.Created.Before(cutoff) &&
				!WorkingReviewState(n.Address.Worktree, n.Files).Current {
				drop[n.ID] = true
			}
			continue
		}
```

`notes.go`:
- Add the field to `NoteCounts`, after `Reviews`:
  ```go
  	// WorkingReviews is this worktree's reviews of uncommitted changes,
  	// newest first: never in a ◆N badge, never in Reviews (the Branches
  	// sub-rows). Unmatched — matching reads files (WorkingReviews).
  	WorkingReviews []ReviewHead
  ```
- In the `NoteCounts` loop, before `if n.IsReviewNote()`:
  ```go
  		if n.IsWorkingReview() {
  			if sameWorktreePath(n.Address.Worktree, cur) {
  				c.WorkingReviews = append(c.WorkingReviews, ReviewHead{ID: n.ID, Agent: n.Author, Summary: n.Summary, Created: n.Created})
  			}
  			continue
  		}
  ```
  Sort it like `Reviews`, after the loop.
- In `resolveOne`, as the first statement:
  ```go
  	// A note about a whole object (an AI review, a shelf entry, a worktree's
  	// changes) has no lines to find: it is where it is.
  	if n.IsEntryLevel() {
  		return model.NoteActive, n.Range
  	}
  ```

`notes_overview.go`:
- Add the field to `NotesOverview`: `WorkingReviews []WorkingReview // this worktree's reviews of uncommitted changes, newest first; Current false = outdated`.
- In `Count()`: `n += len(o.WorkingReviews)`.
- In the root scan: `if n.IsReviewNote() || n.IsWorkingReview() { review[n.ID] = true }`.
- In the `revs` loop: `if r.Kind == ReviewOnWorktree { continue }`.
- After that loop: `ov.WorkingReviews, _ = s.WorkingReviews(ctx)`.
- In `ShownOn` (the filtered copy), keep `WorkingReviews` unchanged. They belong to the worktree, not to a branch.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ ./internal/notes/ 2>&1 | tail -5`
Expected: `ok`. If an existing test asserted a commit review's resolved status was `stale`, it relied on the `git show <sha>:` tree-listing accident. Rule it in the ledger and update the assertion to `active`.

- [ ] **Step 5: Commit**

Message: `feat(domain): working reviews in the sweep, the counts and the all-notes overview`

---

### Task 7: The review view's reads + annotations on matching files

**Files:**
- Modify: `internal/domain/review_doc.go`, `internal/domain/notes.go`, `internal/domain/working_review.go`
- Test: `internal/domain/working_review_test.go` (append)

**Interfaces:**
- Consumes: `WorkingReviews`, `WorkingReviewState`, `CompareFiles`.
- Produces:
  - `ReviewRevs(working)` → `base` = HEAD's full sha, `tip` = "", `isRange` = false
  - `ReviewFiles(working)` = `CompareFiles(HEAD, WorkTreeEndpoint())`
  - `ReviewOtherNote.Changed bool`
  - `ReviewFileCounts` counts matching files only
  - `ReviewNotesFor(working)` returns notes only for a matching file, addressed `{StateUnstaged, Worktree, Path}`
  - `NotesFor`/`NotesAt` on a live file address add the matching working reviews' new-side notes (`review:<id>:<n>` ids)

- [ ] **Step 1: Write the failing tests**

Append:

```go
// workingReviewOf commits a.txt/c.txt, edits both, stores a structured
// working review with one note on each (new side) and one old-side note on a.txt.
func workingReviewOf(t *testing.T) (string, *Service, string) {
	t.Helper()
	dir, svc := newRealRepo(t)
	svc.UseNotesDir(t.TempDir())
	commitFile(t, dir, "a.txt", "a1\na2\n", "a")
	commitFile(t, dir, "c.txt", "c1\n", "c")
	writeAt(t, dir, "a.txt", "a1\nA2\n")
	writeAt(t, dir, "c.txt", "c1\nC2\n")
	writeAt(t, dir, "u.txt", "u1\n")
	doc := `{"version":1,"summary":"ok","files":[` +
		`{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"on a"},{"oldRange":[2,2],"summary":"old a"}]},` +
		`{"path":"c.txt","annotations":[{"newRange":[2,2],"summary":"on c"}]}]}`
	files := []model.NoteFile{{Path: "a.txt", Blob: blob("a1\nA2\n")}, {Path: "c.txt", Blob: blob("c1\nC2\n")}, {Path: "u.txt", Blob: blob("u1\n")}}
	id, _, err := svc.SaveReview(context.Background(), SaveReview{Target: WorkingReviewTarget(), Text: doc, Files: files})
	if err != nil {
		t.Fatal(err)
	}
	return dir, svc, id
}

func TestReviewFilesOfAWorkingReviewIsHeadToWorktree(t *testing.T) {
	t.Parallel()
	_, svc, id := workingReviewOf(t)
	ctx := context.Background()
	r, _ := svc.Review(ctx, id)
	files, err := svc.ReviewFiles(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, f := range files {
		got[f.Path] = true
	}
	if !got["a.txt"] || !got["c.txt"] || !got["u.txt"] {
		t.Fatalf("files = %+v, want the modified and the untracked ones", files)
	}
	base, tip, isRange := svc.ReviewRevs(ctx, r)
	if len(base) < 40 || tip != "" || isRange {
		t.Fatalf("revs = %q %q %v, want HEAD's sha, no tip, no range", base, tip, isRange)
	}
}

func TestWorkingReviewCountsOnlyMatchingFiles(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	writeAt(t, dir, "c.txt", "c1\nC2 edited\n")
	counts, err := svc.ReviewFileCounts(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if counts["a.txt"] != 2 || counts["c.txt"] != 0 {
		t.Fatalf("counts = %v, want a.txt:2 (both sides fit) and nothing on the changed c.txt", counts)
	}
	other, _ := svc.ReviewOtherNotes(ctx, id)
	if len(other) != 1 || other[0].Path != "c.txt" || !other[0].Changed {
		t.Fatalf("other = %+v, want c.txt's note marked changed", other)
	}
}

func TestReviewNotesForAWorkingReviewUseTheWorktreeAddress(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	d := Diff{Result: textdiff.Result{Rows: []textdiff.Row{
		{LeftNo: 1, RightNo: 1, Left: "a1", Right: "a1"}, {LeftNo: 2, RightNo: 2, Left: "a2", Right: "A2"}}}}
	ns, err := svc.ReviewNotesFor(ctx, id, "a.txt", d)
	if err != nil || len(ns) != 2 {
		t.Fatalf("notes = %+v %v", ns, err)
	}
	if a := ns[0].Note.Address; a.State != model.StateUnstaged || a.Path != "a.txt" || a.Worktree == "" {
		t.Fatalf("address = %+v", a)
	}
	writeAt(t, dir, "a.txt", "changed\n")
	if ns, _ = svc.ReviewNotesFor(ctx, id, "a.txt", d); len(ns) != 0 {
		t.Fatalf("a changed file still shows %d notes", len(ns))
	}
}

// Spec §7: a matching file's working-tree diff draws the review's notes —
// new side only (the old side the review read is HEAD, not the index).
func TestNotesAtAddsAMatchingWorkingReviewsNotes(t *testing.T) {
	t.Parallel()
	dir, svc, id := workingReviewOf(t)
	ctx := context.Background()
	addr := model.FileAddress{State: model.StateUnstaged, Path: "a.txt"}
	ns, err := svc.NotesAt(ctx, addr)
	if err != nil {
		t.Fatal(err)
	}
	if len(ns) != 1 || ns[0].Note.ID != model.ReviewNoteIDPrefix+id+":0" || ns[0].Note.Side != model.NoteSideNew {
		t.Fatalf("notes = %+v, want only the new-side review note", ns)
	}
	writeAt(t, dir, "a.txt", "edited\n")
	if ns, _ = svc.NotesAt(ctx, addr); len(ns) != 0 {
		t.Fatalf("an edited file still shows %+v", ns)
	}
}
```

(Import `textdiff` = `github.com/homeend/gigagit/internal/textdiff`. Check the `textdiff.Row` field names against `diffSideLines`.)

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ -run 'ReviewFilesOfAWorking|WorkingReviewCounts|ReviewNotesForAWorking|NotesAtAddsAMatching' 2>&1 | tail -15`
Expected: failures. For example, `ReviewFiles` currently calls `CommitFiles("")`, and `Changed` is undefined.

- [ ] **Step 3: Implement**

`review_doc.go`:

- `ReviewOtherNote` gains:
  ```go
  	// Changed: the note is on a file a working review read that changed
  	// since — it is not drawn (spec §7), only listed.
  	Changed bool
  ```
- `reviewRevs`, first lines:
  ```go
  	if r.Kind == ReviewOnWorktree {
  		head, found, err := s.ResolveRev(ctx, "HEAD")
  		if err != nil || !found {
  			return "", "", false
  		}
  		return strings.TrimSpace(head), "", false
  	}
  ```
- `ReviewFiles`, first lines:
  ```go
  	if r.Kind == ReviewOnWorktree {
  		base, _, _ := s.reviewRevs(ctx, r)
  		left, err := model.CommitEndpoint(base)
  		if err != nil {
  			return nil, err
  		}
  		return s.CompareFiles(ctx, left, model.WorkTreeEndpoint())
  	}
  ```
- Extract the note builder shared by `ReviewNotesFor` and the injection:
  ```go
  // reviewDocNotes are review r's document notes on path, numbered in document
  // order ("review:<id>:<n>") and anchored at addr. A note whose side is absent
  // or too short is left out; newOnly drops old-side notes.
  func reviewDocNotes(r Review, path string, addr model.FileAddress, oldLines, newLines []string, newOnly bool) []ResolvedNote {
  	if r.Doc == nil {
  		return nil
  	}
  	want := reviewPath(path)
  	var out []ResolvedNote
  	i := 0
  	for _, f := range r.Doc.Files {
  		for _, dn := range f.Notes {
  			id := fmt.Sprintf("%s%s:%d", model.ReviewNoteIDPrefix, r.ID, i)
  			i++
  			if reviewPath(f.Path) != want {
  				continue
  			}
  			side := reviewSide(dn.Side)
  			if newOnly && side == model.NoteSideOld {
  				continue
  			}
  			lines := newLines
  			if side == model.NoteSideOld {
  				lines = oldLines
  			}
  			if !fitsSide(lines, dn.Range) {
  				continue
  			}
  			n := model.Note{ID: id, Source: model.NoteSourceAgent, Author: r.Agent, Address: addr,
  				Side: side, Range: dn.Range, Summary: dn.Summary, Rationale: dn.Rationale,
  				Created: r.Created, Updated: r.Updated}
  			for _, kv := range dn.Meta {
  				n.Tags = append(n.Tags, kv.Key+": "+kv.Value)
  			}
  			out = append(out, ResolvedNote{Note: n, Status: model.NoteActive, Range: dn.Range})
  		}
  	}
  	sortReviewNotes(out)
  	return out
  }

  func sortReviewNotes(out []ResolvedNote) {
  	sort.SliceStable(out, func(a, b int) bool {
  		if out[a].Note.Side != out[b].Note.Side {
  			return out[a].Note.Side == model.NoteSideNew
  		}
  		return out[a].Range[0] < out[b].Range[0]
  	})
  }
  ```
- `ReviewNotesFor` becomes:
  ```go
  	r, err := s.Review(ctx, reviewID)
  	if err != nil || r.Doc == nil {
  		return nil, err
  	}
  	addr := model.FileAddress{State: model.StateCommitted, Commit: r.Commit, Path: path}
  	if r.Kind == ReviewOnWorktree {
  		// Annotations show only on a file that still matches (spec §7).
  		if WorkingReviewState(r.Worktree, r.Files).States[reviewPath(path)] != WorkingFileMatches {
  			return nil, nil
  		}
  		addr = model.FileAddress{State: model.StateUnstaged, Worktree: r.Worktree, Path: path}
  	}
  	oldLines, newLines := diffSideLines(d)
  	return reviewDocNotes(r, path, addr, oldLines, newLines, false), nil
  ```
- `reviewSplit` for a working review. Branch after loading `files`:
  ```go
  	var match WorkingReviewMatch
  	working := r.Kind == ReviewOnWorktree
  	if working {
  		match = WorkingReviewState(r.Worktree, r.Files)
  	}
  ```
  - `sideOf` for a working review reads `old = s.revLines(ctx, base, path)` and `new = s.worktreeLines(ctx, r.Worktree, path)` (see below).
  - In the loop:
    ```go
    			if working && match.States[p] != WorkingFileMatches {
    				_, reviewed := match.States[p]
    				other = append(other, ReviewOtherNote{Path: p, Side: side, Range: dn.Range, Summary: dn.Summary, Changed: reviewed})
    				continue
    			}
    ```
    This goes before the existing `inView` placement test.
  - Add the helper:
    ```go
    // worktreeLines is path's text in worktree split into lines; nil when absent.
    func (s *Service) worktreeLines(ctx context.Context, worktree, path string) []string {
    	b, err := s.worktreeFileIn(ctx, worktree, path)
    	if err != nil {
    		return nil
    	}
    	return splitLines(b)
    }
    ```
    (Check `worktreeFileIn`'s signature in `notes.go` and match it.)

`working_review.go`: add the injection:

```go
// workingReviewNotes are the notes the current working reviews of worktree
// place on path, new side only — the old side a review read is HEAD, not the
// index a Files-panel diff shows (spec §7). lines reads the file's new side,
// lazily: an address no review matches costs no file read.
func (s *Service) workingReviewNotes(ctx context.Context, worktree, path string, lines func() []string) []ResolvedNote {
	revs, err := s.WorkingReviews(ctx)
	if err != nil {
		return nil
	}
	var out []ResolvedNote
	var newLines []string
	read := false
	for _, wr := range revs {
		if wr.Doc == nil || !sameWorktreePath(wr.Worktree, worktree) || wr.States[reviewPath(path)] != WorkingFileMatches {
			continue
		}
		if !read {
			newLines, read = lines(), true
		}
		addr := model.FileAddress{State: model.StateUnstaged, Worktree: wr.Worktree, Path: path}
		out = append(out, reviewDocNotes(wr.Review, path, addr, nil, newLines, true)...)
	}
	return out
}
```

`notes.go`:

- `NotesFor`:
  ```go
  	oldLines, newLines := diffSideLines(d)
  	out := keepResolved(resolveNotes(mine, oldLines, newLines))
  	if worktreeScopedNote(addr) && addr.Path != "" {
  		wt, _ := s.noteWorktree(ctx, addr)
  		out = append(out, s.workingReviewNotes(ctx, wt, addr.Path, func() []string { return newLines })...)
  		sortReviewNotes(out)
  	}
  	return out, nil
  ```
  `sortReviewNotes` orders roots by side then by line. It is stable, so threads keep their order.
- `NotesAt`: after `loadNotesAt`, compute
  ```go
  	var extra []ResolvedNote
  	if worktreeScopedNote(addr) && addr.Path != "" {
  		wt, _ := s.noteWorktree(ctx, addr)
  		extra = s.workingReviewNotes(ctx, wt, addr.Path, func() []string {
  			l, _ := s.noteSideLines(ctx, addr, model.NoteSideNew)
  			return l
  		})
  	}
  	if len(mine) == 0 {
  		return extra, nil
  	}
  ```
  Then append `extra` to the resolved result and call `sortReviewNotes`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/domain/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

Message: `feat(domain): the review view's reads and file annotations for a working review`

---

### Task 8: CLI

**Files:**
- Modify: `internal/cli/review.go`, `internal/cli/note.go`
- Test: `internal/cli/review_test.go`

**Interfaces:**
- Consumes: `ReviewReport` storing working reviews (Task 4), `NotesAt` injection (Task 7), `model.Note.IsWorkingReview`.
- Produces:
  - `gg review --working` stores the review and prints `note: <id>` on stderr
  - `--working --notes` → exit 2
  - `renderNoteLine` labels a working review "review working changes"

- [ ] **Step 1: Write the failing tests**

In `internal/cli/review_test.go`:

Replace `TestReviewWorkingPrintsWithoutANote` with:

```go
// A working-changes review is stored as a note and listed like any review.
func TestReviewWorkingStoresANote(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "FAKE WORKING REVIEW\n"`)
	code, out, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working")
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, errb)
	}
	if !strings.Contains(out, "FAKE WORKING REVIEW") || !strings.Contains(errb, "note: ") {
		t.Fatalf("stdout=%q stderr=%q, want the review printed and its note id", out, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list")
	if !strings.Contains(list, "] review working changes") {
		t.Fatalf("note list = %q, want the working review row", list)
	}
}

func TestReviewWorkingWithNotesIsAUsageError(t *testing.T) {
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	writeReviewTool(t, dir, "Echo", `printf "x\n"`)
	code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working", "--notes")
	if code != 2 || !strings.Contains(errb, "a working review is stored; its notes show on the files") {
		t.Fatalf("exit=%d stderr=%q, want 2 and the documented message", code, errb)
	}
}

// The stored review's notes show on a matching file's `note list --file`, and
// leave once the file is edited.
func TestNoteListFileShowsAWorkingReviewsNotesWhileTheFileMatches(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses sh/printf")
	}
	isolateReviewEnv(t)
	dir := newRepoDir(t)
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\ntwo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "a.txt")
	runGit(t, dir, "commit", "-m", "seed")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("one\nTWO\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeReviewTool(t, dir, "Echo",
		`printf '{"version":1,"summary":"R","files":[{"path":"a.txt","annotations":[{"newRange":[2,2],"summary":"shouty"}]}]}' > "$GG_MESSAGE_FILE"`)
	if code, _, errb := runCLI(t, dir, "review", "--tool", "Echo", "--working"); code != 0 {
		t.Fatalf("review exit=%d stderr=%s", code, errb)
	}
	_, list, _ := runCLI(t, dir, "note", "list", "--file", "a.txt")
	if !strings.Contains(list, "shouty") || !strings.Contains(list, "review:") {
		t.Fatalf("note list --file = %q, want the review's note", list)
	}
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, list, _ = runCLI(t, dir, "note", "list", "--file", "a.txt"); strings.Contains(list, "shouty") {
		t.Fatalf("an edited file still lists the review's note: %q", list)
	}
}
```

Retarget the four `--working --notes` tests to a commit target:
- In `TestReviewNotesImportsTheDocument` and `TestReviewNotesReadsTheDocumentFromStdout`:
  - After the second `WriteFile`, add `runGit(t, dir, "commit", "-am", "change")`.
  - Run `review --tool Echo --notes HEAD`.
  - List with `note list --rev HEAD --file a.txt`.
  - Keep the `new:2-2` assertion. A single commit addresses both sides, and line 2 of the new side is `TWO`.
- In `TestReviewNotesNoNotesIsExit1` and `TestReviewNotesStoringNothingPostsNoReload`, run `review --tool Echo --notes HEAD` instead of `--working --notes`.
- Update the doc comment above `reviewImportTarget` (drop the `--working` line) and the `ReviewWorking` branch inside it. A working target can no longer reach the import, so remove the branch and let the compiler confirm nothing else depends on it.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/cli/ -run 'Review|NoteList' 2>&1 | tail -15`
Expected: `TestReviewWorkingWithNotesIsAUsageError` fails (exit 0 or 1, not 2) and `TestReviewWorkingStoresANote` fails (there is no "review working changes" row). Tasks 4 and 7 already store the note and inject its notes, so `TestNoteListFileShows…` may already pass. That is expected; it pins the end-to-end path.

- [ ] **Step 3: Implement**

`internal/cli/review.go`, in `cmdReview` after the `fs.NArg() > 1` check:

```go
	if *working && *wantNotes {
		fmt.Fprintln(stderr, "gg review: --notes does not apply to --working: a working review is stored; its notes show on the files")
		return 2
	}
```

Update the `--notes` flag help to "also import the review's notes as permanent notes (not with --working)". Update the `cmdReview` doc comment: a `--working` review is stored as a note in this worktree.

`internal/cli/note.go`, in `renderNoteLine` before `if r.Note.IsReviewNote()`:

```go
	if r.Note.IsWorkingReview() {
		// A review of uncommitted changes: no commit, file or range to show.
		fmt.Fprintf(w, "%s [%s] review working changes  %s\n", r.Note.ID, r.Note.Source, r.Note.Summary)
		return
	}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/cli/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Commit**

Message: `feat(cli): gg review --working stores the review; --working --notes is a usage error`

---

### Task 9: TUI — open a working review, the finished-run handoff, View all notes

**Files:**
- Modify: `internal/tui/review_view.go`, `internal/tui/review.go`, `internal/tui/all_notes_popup.go`, `internal/i18n/*.toml` (all four bundles)
- Test: `internal/tui/review_view_test.go`, `internal/tui/all_notes_popup_test.go`, plus the `applyReviewResult` test (`grep -ln "applyReviewResult\|not a note" internal/tui/*_test.go`)

**Interfaces:**
- Consumes: `domain.Review{Kind: ReviewOnWorktree, Worktree, Files}`, `domain.WorkingReviewState`, `domain.WorkingFileState`, `NotesOverview.WorkingReviews`, `ReviewOtherNote.Changed`, `ReviewRevs` (base = HEAD sha, tip = "").
- Produces:
  - `reviewViewState.states map[string]domain.WorkingFileState` (nil for a commit review)
  - `reviewViewMsg.states`
  - A working review opens as `openCompareFiles(CommitEndpoint(HEAD), WorkTreeEndpoint())` in review mode

Read `review_view_test.go` and `all_notes_popup_test.go` first and reuse their model/service fixtures. The tests below name the behaviour; adapt the fixture calls to the helpers you find.

- [ ] **Step 1: Write the failing tests**

```go
// A working review's tree marks every file against the review: changed ones
// say so, files the review never read say so, matching ones carry ◆n.
func TestReviewTreeLinesMarksAWorkingReviewsFiles(t *testing.T) {
	t.Parallel()
	st := &reviewViewState{review: domain.Review{Kind: domain.ReviewOnWorktree, Doc: &notebatch.ReviewDoc{}},
		counts: map[string]int{"a.txt": 1},
		states: map[string]domain.WorkingFileState{"a.txt": domain.WorkingFileMatches, "c.txt": domain.WorkingFileChanged}}
	out := reviewTreeLines(st, []contentLine{{text: "M a.txt", path: "a.txt"}, {text: "M c.txt", path: "c.txt"}, {text: "A n.txt", path: "n.txt"}})
	byPath := map[string]string{}
	for _, l := range out {
		byPath[l.path] = l.text
	}
	if !strings.Contains(byPath["a.txt"], noteBadge(1)) || strings.Contains(byPath["a.txt"], i18n.T("changed since the review")) {
		t.Errorf("a.txt = %q", byPath["a.txt"])
	}
	if !strings.Contains(byPath["c.txt"], i18n.T("changed since the review")) {
		t.Errorf("c.txt = %q", byPath["c.txt"])
	}
	if !strings.Contains(byPath["n.txt"], i18n.T("not reviewed")) {
		t.Errorf("n.txt = %q", byPath["n.txt"])
	}
}

// A diff opened from a working review is addressed at the worktree, where the
// review's notes anchor.
func TestStampReviewNotesAddressesAWorkingReviewAtTheWorktree(t *testing.T) {
	t.Parallel()
	m := Model{filesReview: &reviewViewState{id: "rv", review: domain.Review{Kind: domain.ReviewOnWorktree, Worktree: "/wt"}}}
	dv := &diffView{}
	m.stampReviewNotes(dv, "a.txt")
	want := model.FileAddress{State: model.StateUnstaged, Worktree: "/wt", Path: "a.txt"}
	if dv.noteAddr != want || dv.reviewID != "rv" {
		t.Fatalf("noteAddr %+v reviewID %q, want %+v rv", dv.noteAddr, dv.reviewID, want)
	}
}
```

Then add these tests, using the package's real-repo fixtures:
- **`TestOpenReviewOpensAWorkingReviewAsAHeadToWorktreeCompare`**
  - Setup: a repo with a modified file and an untracked file, and a review stored by `svc.SaveReview(ctx, domain.SaveReview{Target: domain.WorkingReviewTarget(), Text: <a doc with one note>, Files: …})`.
  - Run `m.openReview(id, "")` and its cmd, then feed the `reviewViewMsg` back.
  - Assert `m.filesMode == filesModeCompare`, `m.filesRight.Kind() == model.EndpointWorkTree`, `m.filesReview != nil` and `m.filesReview.states != nil`.
  - Assert `m.filesTitle` holds `i18n.T("working changes")`.
- **`TestApplyReviewResultOpensAStoredWorkingReview`**
  - Flip the existing working-result test: an `info` with `NoteID` set and Key `"review — wt working changes"` now goes through `openReview` (a `reviewLoadingPopup` is pushed), not the result viewer.
- **`TestAllNotesListsWorkingReviewsAndMarksTheOutdated`**
  - Build `buildAllNotesRows` over `domain.NotesOverview{WorkingReviews: []domain.WorkingReview{{Review: domain.Review{ID: "a", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes"}, WorkingReviewMatch: domain.WorkingReviewMatch{Current: true}}, {Review: domain.Review{ID: "b", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes"}}}}`.
  - Assert a Working-tree group row exists, two `anReview` rows follow, and only "b"'s rendered text holds `i18n.T("outdated")`.
  - Enter on a row calls `openReview` with its id.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/tui/ -run 'ReviewTreeLinesMarks|StampReviewNotesAddresses|OpenReviewOpensAWorking|ApplyReviewResultOpens|AllNotesListsWorking' 2>&1 | tail -15`
Expected: compile errors (`states`), then failures.

- [ ] **Step 3: Implement**

`review_view.go`:
- `reviewViewState` and `reviewViewMsg` gain `states map[string]domain.WorkingFileState`.
- In `openReviewFrom`'s closure, after `ReviewRevs`:
  ```go
  		if out.review.Kind == domain.ReviewOnWorktree {
  			out.states = domain.WorkingReviewState(out.review.Worktree, out.review.Files).States
  		}
  ```
- In `handleReviewViewMsg`, copy `states` into `st`. Make the `open` func's first branch:
  ```go
  		switch {
  		case msg.review.Kind == domain.ReviewOnWorktree:
  			// A review of uncommitted changes opens as HEAD ↔ the working
  			// tree, untracked files included (CompareFiles adds them).
  			left, lerr := model.CommitEndpoint(msg.base)
  			if lerr != nil {
  				m.statusMsg = i18n.T("review: %s", lerr.Error())
  				return m, nil
  			}
  			m.compareTag = ""
  			m, cmd = m.openCompareFiles(left, model.WorkTreeEndpoint())
  		case msg.isRange:
  ```
  Keep the existing range and commit branches as the next cases.
- `reviewLabel`: `if r.Kind == domain.ReviewOnWorktree { return i18n.T("working changes") }`.
- `reviewTreeLines`, in the file loop, after the badge:
  ```go
  		if st.states != nil {
  			switch state, reviewed := st.states[l.path]; {
  			case !reviewed:
  				l.text += " · " + i18n.T("not reviewed")
  			case state != domain.WorkingFileMatches:
  				l.text += " · " + i18n.T("changed since the review")
  			}
  		}
  ```
- `stampReviewNotes`: the working review gets the worktree address:
  ```go
  	if st.review.Kind == domain.ReviewOnWorktree {
  		dv.noteAddr = model.FileAddress{State: model.StateUnstaged, Worktree: st.review.Worktree, Path: path}
  		return
  	}
  ```
- `reviewOtherNoteLines`: append ` (` + `i18n.T("changed since the review")` + `)` when `o.Changed`.
- `reviewOtherNotesPopup`: its enter opens the file at the reviewed commit. With `tip == ""` (a working review), set `m.statusMsg = i18n.T("the file changed since the review")` and open nothing.
- `reviewReadOnlyNotice`: take the state. For a working review return `i18n.T("▸ review notes are read-only")`, because `--notes` does not apply there. Update its callers (`grep -n reviewReadOnlyNotice internal/tui/*.go`).

`review.go` (`applyReviewResult`): keep the `info.NoteID == ""` branch only for a run whose save failed or produced no note. Reword its comment; a working review now arrives with a `NoteID` and opens through the existing `openReview` path.

`all_notes_popup.go`:
- `anRow` gains `outdated bool`.
- The Working-tree group gate becomes `len(ov.Unstaged)+len(ov.Staged)+len(ov.Untracked)+len(ov.WorkingReviews) > 0`.
- After the group's subs, add:
  ```go
  		if len(ov.WorkingReviews) > 0 {
  			rows = append(rows, anRow{kind: anDir, depth: 1, text: i18n.T("Reviews")})
  			for i := range ov.WorkingReviews {
  				wr := &ov.WorkingReviews[i]
  				rows = append(rows, anRow{kind: anReview, depth: 2, review: &wr.Review, outdated: !wr.Current,
  					filter: wr.Summary + " " + wr.Agent})
  			}
  		}
  ```
  Copy the depth and filter conventions of the commit review rows at `:203-209`.
- `anReviewParts` / `anReviewCells`: for `v.Kind == domain.ReviewOnWorktree`, the "where" column is `i18n.T("working changes")`, or `i18n.T("outdated")` when `r.outdated`.

i18n: add every new key to `internal/i18n/{ja,ko,zh,ru}.toml` (follow the adding-translations skill): "not reviewed", "changed since the review", "the file changed since the review", "▸ review notes are read-only", "outdated", and "working changes" plus "Reviews" if they are not already keys. Check with `grep -n '"working changes"\|"Reviews"\|"outdated"' internal/i18n/*.toml`.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/tui/ 2>&1 | tail -10`
Expected: `ok`. That includes the i18n AST-gate tests (`i18n_scan_test.go` and friends).

- [ ] **Step 5: Commit**

Message: `feat(tui): open a working review as HEAD ↔ worktree; list it in View all notes`

---

### Task 10: TUI — the Files panel's Review row and ✎ markers

**Files:**
- Create: `internal/tui/working_review_row.go`, `internal/tui/working_review_row_test.go`
- Modify:
  - `internal/tui/model.go` (field, msg handling, enter)
  - `internal/tui/viewstate.go` (`statusList`, `displayIndices`, `backingIndex`, `listFor`)
  - Every `displayIndices(panelFiles|p)` walker that indexes `m.status.Files[u]` (list below)
  - The `.` menu dispatcher
  - i18n bundles

**Interfaces:**
- Consumes: `domain.Service.WorkingReviews`, `NoteCounts.WorkingReviews`, `confirmStoredDelete`, `openReview`.
- Produces:
  - `Model.workingReviews []domain.WorkingReview`
  - `const reviewRowIdx = -1`, the sentinel backing index of the Review row in `displayIndices(panelFiles)`
  - `func (m Model) workingReviewRow() (domain.WorkingReview, bool)` — the newest current review
  - `func (m Model) reviewedPaths() map[string]bool`

**Rulings baked in:**
- The row shows only while a review is current and no `/` filter is active on the Files panel. A filter narrows to files.
- The cursor stays on the same file when the row appears or disappears.
- `.` on the row offers Open review and Delete review only (user ruling 2026-10-05 for note rows).

- [ ] **Step 1: Write the failing tests**

Create `internal/tui/working_review_row_test.go`. Build the Model the way existing Files-panel tests do; find one with `grep -ln "panelFiles" internal/tui/*_test.go | head` and reuse its constructor. The status is two unstaged files, `a.txt` and `b.txt`; `m.workingReviews` holds one current review whose States match `a.txt`.

```go
func reviewRowModel(t *testing.T) Model {
	t.Helper()
	m := filesPanelTestModel(t, "a.txt", "b.txt") // the package's constructor — see note above
	m = m.withWorkingReviews([]domain.WorkingReview{{
		Review:             domain.Review{ID: "rv1", Kind: domain.ReviewOnWorktree, Summary: "Review: working changes", Agent: "Claude Code"},
		WorkingReviewMatch: domain.WorkingReviewMatch{Current: true, States: map[string]domain.WorkingFileState{"a.txt": domain.WorkingFileMatches, "b.txt": domain.WorkingFileChanged}},
	}})
	m.focus = panelFiles
	return m
}

func TestFilesPanelShowsTheReviewRowFirstAndMarksMatchingFiles(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	idx := m.displayIndices(panelFiles)
	if len(idx) != 3 || idx[0] != reviewRowIdx {
		t.Fatalf("indices = %v, want the review row first", idx)
	}
	l := m.listFor(panelFiles)
	if !strings.Contains(l.Row(idx[0]), i18n.T("Review")) {
		t.Errorf("row 0 = %q", l.Row(idx[0]))
	}
	if !strings.Contains(l.Row(idx[1]), "a.txt") || !strings.Contains(l.Row(idx[1]), "✎") {
		t.Errorf("a.txt row = %q, want ✎", l.Row(idx[1]))
	}
	if strings.Contains(l.Row(idx[2]), "✎") {
		t.Errorf("b.txt changed since the review: %q must carry no ✎", l.Row(idx[2]))
	}
	if got := m.panelLen(panelFiles); got != 2 {
		t.Errorf("panelLen = %d, want 2 (the review row is not a file)", got)
	}
}

func TestFilesPanelReviewRowIsNotAFile(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.sel[panelFiles] = 0
	if _, ok := m.backingIndex(panelFiles); ok {
		t.Fatal("backingIndex resolved the review row to a file")
	}
	if m.canShowFileDiff() {
		t.Fatal("the review row offers a file diff")
	}
	if got := m.rowKeyAt(panelFiles, 0); got != "" {
		t.Fatalf("rowKeyAt = %q, want none", got)
	}
	for _, f := range m.buildStatusStack(false) {
		if f.path == "" {
			t.Fatal("the stacked view gained a pathless entry")
		}
	}
}

func TestFilesPanelEnterOnTheReviewRowOpensTheReview(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.sel[panelFiles] = 0
	nm, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !nm.(Model).hasReviewLoading(nm.(Model).reviewOpenGen) {
		t.Fatal("enter did not open the review")
	}
}

func TestFilesPanelCursorKeepsItsFileWhenTheRowComesAndGoes(t *testing.T) {
	t.Parallel()
	m := filesPanelTestModel(t, "a.txt", "b.txt")
	m.focus = panelFiles
	m.sel[panelFiles] = 1 // b.txt
	m = reviewRowModelFrom(m) // same review as reviewRowModel, applied to this model
	if bi, ok := m.backingIndex(panelFiles); !ok || m.status.Files[bi].Path != "b.txt" {
		t.Fatalf("after the row appeared the cursor is on %v", bi)
	}
	m = m.withWorkingReviews(nil)
	if bi, ok := m.backingIndex(panelFiles); !ok || m.status.Files[bi].Path != "b.txt" {
		t.Fatalf("after the row left the cursor is on %v", bi)
	}
}

func TestFilesPanelHidesTheReviewRowWhileFiltering(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m = m.withFilter(panelFiles, "b") // the package's filter setter (grep filterActive)
	for _, u := range m.displayIndices(panelFiles) {
		if u == reviewRowIdx {
			t.Fatal("the review row shows under a / filter")
		}
	}
}

func TestFilesPanelReviewRowMenuIsOpenAndDelete(t *testing.T) {
	t.Parallel()
	m := reviewRowModel(t)
	m.sel[panelFiles] = 0
	rows, ok := m.workingReviewRowMenu()
	if !ok || len(rows) != 2 || rows[0].id != "open-review" || rows[1].id != "delete-review" {
		t.Fatalf("menu = %+v", rows)
	}
}
```

`filesPanelTestModel`, `reviewRowModelFrom` and `withFilter` stand for the package's existing helpers, or thin ones you add in this test file on top of them. Write them first, from what the package has.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/tui/ -run 'FilesPanel.*Review' 2>&1 | tail -15`
Expected: compile errors (`withWorkingReviews`, `reviewRowIdx`, `workingReviewRowMenu`).

- [ ] **Step 3: Implement**

Create `internal/tui/working_review_row.go`:

```go
package tui

import (
	"context"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/clock"
	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/i18n"
)

// The Files panel's Review row (spec 2026-10-04 working reviews §7): while a
// review of this worktree's uncommitted changes is current, a row at the top
// of the panel opens it, and each file it still matches carries ✎. The row
// is a pseudo row: its backing index is reviewRowIdx, which backingIndex
// refuses — every file action already guards that ok.

const reviewRowIdx = -1

// workingReviewsMsg is a fresh read of this worktree's working reviews.
type workingReviewsMsg struct {
	reviews []domain.WorkingReview
	err     error
}

// loadWorkingReviewsCmd reads and matches the working reviews off-thread.
func (m Model) loadWorkingReviewsCmd() tea.Cmd {
	svc := m.svc
	if svc == nil {
		return nil
	}
	return func() tea.Msg {
		rs, err := svc.WorkingReviews(context.Background())
		return workingReviewsMsg{reviews: rs, err: err}
	}
}

// workingReviewRow is the review the row opens: the newest current one.
func (m Model) workingReviewRow() (domain.WorkingReview, bool) {
	for _, r := range m.workingReviews { // newest first
		if r.Current {
			return r, true
		}
	}
	return domain.WorkingReview{}, false
}

// reviewRowShown: the row is in the Files panel — a review is current and
// no / filter narrows the panel to files.
func (m Model) reviewRowShown() bool {
	_, ok := m.workingReviewRow()
	return ok && !m.filterActive(panelFiles)
}

// reviewedPaths are the files a current review still matches: their ✎.
func (m Model) reviewedPaths() map[string]bool {
	var out map[string]bool
	for _, r := range m.workingReviews {
		if !r.Current {
			continue
		}
		for p, st := range r.States {
			if st == domain.WorkingFileMatches {
				if out == nil {
					out = map[string]bool{}
				}
				out[p] = true
			}
		}
	}
	return out
}

// withWorkingReviews installs a fresh read, keeping the Files cursor on the
// file it was on when the row comes or goes.
func (m Model) withWorkingReviews(rs []domain.WorkingReview) Model {
	before := m.reviewRowShown()
	m.workingReviews = rs
	after := m.reviewRowShown()
	switch {
	case !before && after:
		m.sel[panelFiles]++
	case before && !after && m.sel[panelFiles] > 0:
		m.sel[panelFiles]--
	}
	m.filesIdxReview = nil // displayIndices rebuilds the prefixed fast path
	return m
}

// workingReviewRowText is the row: "✎ Review: 10-05 14:12 Claude Code".
func workingReviewRowText(r domain.WorkingReview) string {
	parts := []string{"✎", i18n.T("Review:")}
	if !r.Created.IsZero() {
		parts = append(parts, reviewStampAt(r.Created, clock.Now()))
	}
	if a := strings.TrimSpace(r.Agent); a != "" {
		parts = append(parts, sanitizeLine(a))
	}
	return strings.Join(parts, " ")
}

// onReviewRow: the Files cursor is on the Review row.
func (m Model) onReviewRow() bool {
	if m.focus != panelFiles {
		return false
	}
	idx := m.displayIndices(panelFiles)
	s := m.sel[panelFiles]
	return s >= 0 && s < len(idx) && idx[s] == reviewRowIdx
}

// workingReviewRowMenu is the whole "." menu on the Review row: Open and
// Delete (user ruling 2026-10-05 for note rows).
func (m Model) workingReviewRowMenu() ([]actionRow, bool) {
	if !m.onReviewRow() {
		return nil, false
	}
	r, _ := m.workingReviewRow()
	id, summary := r.ID, r.Summary
	return []actionRow{
		{id: "open-review", label: i18n.T("Open review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.openReview(id, summary)
		}},
		{id: "delete-review", label: i18n.T("Delete review"), run: func(m Model) (tea.Model, tea.Cmd) {
			return m.confirmStoredDelete(id, i18n.T("Delete this review?")+"\n"+reviewQuote(r.Agent, summary), true)
		}},
	}, true
}
```

(Check the exact `confirmStoredDelete` return types and `reviewQuote`'s signature in `note_row_menu.go`, and match them.)

`model.go`:
- Field: `workingReviews []domain.WorkingReview // this worktree's working reviews, matched (Files panel Review row, ✎)`. Add `filesIdxReview []int // filesIdx with the Review row's sentinel in front; nil = rebuild`.
- Where `m.noteCounts = msg.value.(domain.NoteCounts)` (≈ line 2041), batch `m.loadWorkingReviewsCmd()` when `len(m.noteCounts.WorkingReviews) > 0`; otherwise apply `m = m.withWorkingReviews(nil)`.
- Where a status refresh lands (the `withStatus` call on a status message), also batch `m.loadWorkingReviewsCmd()` when `len(m.noteCounts.WorkingReviews) > 0`. File edits change the states.
- Handle `workingReviewsMsg`: on success, `m = m.withWorkingReviews(msg.reviews)`.
- In the enter handler, before the `panelFiles` branch (≈ line 2852):
  ```go
  	if m.onReviewRow() {
  		r, _ := m.workingReviewRow()
  		return m.openReview(r.ID, r.Summary)
  	}
  ```
- In the `.` menu dispatcher (where `noteRowMenu()` is consulted; `grep -n "noteRowMenu()" internal/tui/*.go`), consult `workingReviewRowMenu()` first when the focus is `panelFiles`.
- `withStatus` sets `m.filesIdxReview = nil`.

`viewstate.go`:
- `statusList` gains `review string` (the row's text) and `reviewed map[string]bool`:
  ```go
  func (l statusList) Row(i int) string {
  	if i < 0 {
  		return l.review
  	}
  	mark := ""
  	if l.reviewed[l.files[i].Path] {
  		mark = " ✎"
  	}
  	return l.Haystack(i) + mark + noteBadge(l.notes[l.files[i].Path])
  }
  ```
  `Haystack`, `Name`, `Key` and `Date` return `""`/`0` for `i < 0`.
- `listFor` for `panelFiles` fills `reviewed: m.reviewedPaths()`, and `review: workingReviewRowText(r)` when `m.reviewRowShown()`. `panelStaged` gets neither.
- `displayIndices(panelFiles)`: wrap the result:
  ```go
  	if p == panelFiles && m.reviewRowShown() {
  		if m.sortModes[p] == sortDefault && m.filesIdxReview != nil {
  			return m.filesIdxReview
  		}
  		return append([]int{reviewRowIdx}, base...)
  	}
  ```
  Here `base` is today's computation, moved into `displayIndicesBase`. The unsorted, unfiltered fast path is cached. `displayIndices` has a value receiver, so the cache is filled where the model is mutable: in `withWorkingReviews` and `withStatus` (`m.filesIdxReview = append([]int{reviewRowIdx}, m.filesIdx...)` when shown).
- `backingIndex`: after `u := idx[s]`, add `if p == panelFiles && u == reviewRowIdx { return 0, false }`.
- `panelLen(panelFiles)` (`model.go:4513`): subtract 1 while `m.reviewRowShown()`.

Walkers to guard, each skipping `u == reviewRowIdx` before indexing `m.status.Files[u]`:
- `selection.go:53` (`rowKeyAt`)
- `diff_stack_keys.go:56` and `:224` (`buildStatusStack`)
- `steer_nav.go:517`
- `diff_filenav.go:161`, `:229`
- `mark.go:219`, `:251`: they skip an empty `Key`
- `tooltip.go:64`
- `filter_anchor.go:16`, `:43`

Then sweep for any walker the explorer missed: run `grep -n "displayIndices(" internal/tui/*.go | grep -v _test` and check every hit that can see `panelFiles`.

i18n: "Review:" exists (Branches sub-rows). Add any new key to all four bundles.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/tui/ 2>&1 | tail -10`
Expected: `ok`. All existing Files-panel tests must stay green: no review means no sentinel, so their indices are unchanged.

- [ ] **Step 5: Commit**

Message: `feat(tui): the Files panel shows a current working review as a row and ✎ on its files`

---

### Task 11: Web — Review row, ✎, the review view, View all notes

**Files:**
- Modify:
  - `internal/web/notes.go` (counts wire)
  - `internal/web/reviews.go` (`handleReview`, `handleReviewNotes`)
  - `internal/web/diff.go` (a `wt=head` lane)
  - `internal/web/allnotes.go` (wire)
  - `internal/web/static/files.js`, `static/reviews.js`, `static/allnotes.js`
- Test: `internal/web/*_test.go`. Find the review handler tests with `grep -ln "api/review" internal/web/*_test.go` and reuse their server fixture.

**Interfaces:**
- Consumes: `svc.WorkingReviews`, `ReviewRevs`/`ReviewFiles`/`ReviewFileCounts`/`ReviewOtherNotes` (working), `ReviewNotesFor`, `NotesOverview.WorkingReviews`.
- Produces (wire):
  - Counts JSON `working_reviews: [{id, summary, agent, created, current, matches: [path…]}]`.
  - `/api/review/{id}` gains `working: true`, `states: {path: "matches"|"changed"|"gone"}`, `other[].changed`.
  - `/api/diff?wt=head&path=` compares HEAD → working tree. It has no staging tags, and an untracked file shows as an add.
  - All-notes JSON `working_reviews: [{…review wire, outdated}]`.

- [ ] **Step 1: Write the failing Go tests**

Use the package's server and repo fixture:
- **`TestNotesCountsCarryWorkingReviews`**
  - Store a working review whose file matches.
  - GET the counts endpoint (`internal/web/notes.go:129`'s route).
  - Assert `working_reviews[0].current == true` and `matches` holds the path.
- **`TestReviewEndpointServesAWorkingReview`**
  - GET `/api/review/<id>`.
  - Assert `working == true`, `base` is 40 hex characters, `files` holds the untracked file, and `states["a.txt"] == "matches"`.
- **`TestReviewNotesServesAWorkingReviewsNotesWhileTheFileMatches`**
  - GET `/api/review/notes?id=<id>&path=a.txt` and assert one note.
  - Edit `a.txt` and repeat; assert zero notes.
- **`TestDiffHeadLaneComparesHeadToTheWorkingTree`**
  - GET `/api/diff?wt=head&path=a.txt` and assert rows from HEAD's text to the working text.
  - On an untracked path, assert a pure add.
- **`TestAllNotesWireCarriesWorkingReviews`**
  - With one current and one outdated review, assert `working_reviews[1].outdated == true`.

- [ ] **Step 2: Run them to verify they fail**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/web/ -run 'WorkingReview|HeadLane|WorkingReviews' 2>&1 | tail -15`
Expected: failures. The wire fields are absent and the `wt=head` lane is refused with `errBadLane`.

- [ ] **Step 3: Implement the handlers**

- **`diff.go` `worktreeDiff`:** add the `"head"` lane next to `"unstaged"`/`"staged"`.
  - Old side: `svc.ShowFile(ctx, "HEAD", oldPath)`. Treat a not-found as an absent side, the way the staged lane treats a new file.
  - New side: the working file, read the way the unstaged lane reads it.
  - No staging hunks (`wantHunks` is ignored for `head`).
  - Update `errBadLane`'s text to "wt must be unstaged, staged or head".
- **`reviews.go` `handleReview`:**
  - When `rv.Kind == domain.ReviewOnWorktree`, add `working: true`.
  - Add `states`, built from `domain.WorkingReviewState(rv.Worktree, rv.Files)` and mapped to the three words.
  - Add `changed` to each `other` entry.
  - `base` comes from `ReviewRevs`; `tip` stays `""`.
- **`reviews.go` `handleReviewNotes`:** for a working review, build `d` with `worktreeDiff(ctx, svc, "head", path, oldPath, nil, false)` instead of the rev-keyed `Differ` request. Then call `svc.ReviewNotesFor`.
- **`notes.go`:** when `len(c.WorkingReviews) > 0`, add `working_reviews` from `svc.WorkingReviews(ctx)`.
  - Each entry carries id, summary, agent, created, current, and `matches` (sorted paths whose state is matches).
  - Send an empty array, never null.
- **`allnotes.go`:** add `WorkingReviews []overviewWorkingReviewWire` to `notesOverviewWire`. It is the existing review wire plus `Outdated bool \`json:"outdated"\``.

- [ ] **Step 4: Run the Go tests to verify they pass**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && go test ./internal/web/ 2>&1 | tail -5`
Expected: `ok`.

- [ ] **Step 5: Implement the page**

**`static/files.js` `renderFiles()`, status branch (`:1096-1119`):**
- When `state.noteCounts.working_reviews` has a `current` entry, emit a first row before the sections.
  - The row: `<li class="rvw" data-review="<id>" title="the review of these changes">✎ Review · <agent> · <age></li>`, styled like `reviewRowsHTML` (`reviews.js:197`).
  - Only the newest current review gets a row.
- Append ` ✎` after the path of each file listed in that review's `matches` (next to `noteBadgeHTML`).
- The click router (`:4955`) must route `data-review` in status mode too: `openReview(id, {kind: "list"})`.

**`static/reviews.js` `openReview()`:** when `d.working`, set
```js
    cmp = { a: d.base.slice(0, 7), b: "working tree", aHash: d.base, bHash: "", worktree: true,
            all: files, filter: "all", previewBar: "reviewed working changes" };
    state.compare = cmp;
    state.filesMode = "compare";
```
- **`fileDiffURL`:** in the compare branch, before the hash lane, add `if (state.filesMode === "compare" && c.worktree) return "/api/diff?" + new URLSearchParams({ wt: "head", path: f.path });`.
- **`openFile`:** read its compare branch. If it builds its own URL instead of calling `fileDiffURL`, route `c.worktree` there too.
- **`renderReviewFiles`:** for `d.working`, append ` · not reviewed` or ` · changed since the review` from `d.states`, matching the TUI.

**`static/allnotes.js` `anBuildRows`:**
- The Working-tree group gate adds `(ov.working_reviews || []).length`.
- Add a "Reviews" dir row and one review row per working review.
- `reviewCells` shows "working changes", or "outdated" when `r.outdated`.
- Enter and Delete reuse the existing review-row handlers (`:385-389`, `:471`).

- [ ] **Step 6: Verify in a browser**

Follow the project's web-verification memories: rebuild, restart, hard-reload, and assert VISIBILITY, not presence.
1. Run `./build.sh` in the worktree. In a scratch repo with a modified file and an untracked file, store a working review with `bin/gg review --tool <echo tool> --working`. Start `bin/gg web`.
2. With playwright (see `playwright-web-verification` memory), assert each of these is visible:
   - (a) a "✎ Review" row is the first visible row of the working list;
   - (b) the reviewed file's row shows ✎;
   - (c) clicking the Review row opens the review with ≡ Overview and the files;
   - (d) opening the reviewed file shows the review's note.
3. Edit the file, refresh, and assert:
   - (e) the ✎ on that file is gone and the review is still current through the other file, OR the row is gone if it was the only file;
   - (f) View all notes lists the review, labelled "outdated" once no file matches.
4. Record the probe commands and their output in the ledger.

- [ ] **Step 7: Commit**

Message: `feat(web): working reviews — Review row, ✎ markers, the review view, View all notes`

---

### Task 12: e2e, golden screen, docs

**Files:**
- Create: `e2e/scenarios/s<next>_working_review.toml`. Take the next free number with `ls e2e/scenarios | sort | tail`.
- Create: `e2e/scenarios/tui_working_review.toml` (+ `.screens`, generated with `-update`).
- Modify: `CHANGELOG.md`, `docs/CLAUDE-details.md`, `internal/agentskill/using-gg.md`, `internal/agentskill/reviewing-with-gg.md` (if it mentions `--working --notes`), `internal/agentskill/agentskill.go` (bump `Version` 135 → 136, and `ReviewVersion` if `reviewing-with-gg.md` changed), and `README.md` if it describes `gg review --working`.

- [ ] **Step 1: Write the e2e scenario (spec §9)**

Load the writing-e2e-scenarios skill and copy the shape of `e2e/scenarios/s80_cli_review.toml`. The scenario:
- seeds `a.txt` and `c.txt` and commits them;
- edits both and adds an untracked `n.txt`;
- configures a review tool. Its command writes a review document with annotations on `a.txt` line 2 and `n.txt` line 1 to `$GG_MESSAGE_FILE`;
- runs `gg review --tool <name> --working`. Assert exit 0 and `note: ` on stderr;
- runs `gg note list --file a.txt` and `--file n.txt`. Assert both annotations are present;
- edits `a.txt`;
- runs `gg note list --file a.txt` and asserts the annotation is absent;
- runs `gg note list --file n.txt` and asserts it is still present.

- [ ] **Step 2: Write the TUI golden screen**

Copy the shape of `e2e/scenarios/tui_review_mixed.toml`. Use the same seeded review. The screens:
1. The Files panel with the "✎ Review: …" row on top and ✎ on `a.txt` and `n.txt`.
2. Enter on the row, showing the review view (≡ Overview + files with ◆n).
3. After editing both files, View all notes showing the review labelled outdated.

Generate with `go test ./e2e/ -run TUIWorkingReview -update`, then rerun without `-update`. Read the `.screens` file and check that each screen shows what it should.

- [ ] **Step 3: Run the e2e stage**

Run: `cd /work/gigagit/.claude/worktrees/working-reviews && ./test.sh e2e 2>&1 | tail -15`
Expected: all green.

- [ ] **Step 4: Docs**

- **`CHANGELOG.md`:** a new top section, "Working-changes reviews are notes". It covers:
  - the review is stored in the worktree's notes and matched by path + blob id;
  - untracked files are now reviewed;
  - the Review row and ✎ markers in TUI and web;
  - outdated reviews in View all notes, swept after `max_age_days`;
  - `--working --notes` is now a usage error.
- **`docs/CLAUDE-details.md`:** a section "Working-changes reviews (2026-10-05)" covering:
  - the note shape;
  - the fingerprint channel (Prepare → TaskInputs → Result → Store → SaveReview);
  - the blob cache and its 2 s freshness rule;
  - the Files panel's `reviewRowIdx` sentinel and the walkers that must skip it;
  - the `wt=head` web lane;
  - the `resolveOne` entry-level rule.
- **`internal/agentskill/using-gg.md`:**
  - `gg review --working` stores the review as a note (`note: <id>`) and includes untracked files;
  - `--notes` does not combine with `--working`;
  - `gg note list --file` shows a working review's notes on files that still match.
  - Bump `agentskill.Version`.
- **`reviewing-with-gg.md`:** fix any `--working --notes` usage and bump `ReviewVersion` if it changed.
- **CLAUDE.md:** the `notes` package row is unchanged (the routing is unchanged). Update the map only if a package responsibility changed. It did not.

- [ ] **Step 5: Full gates**

Run in the background and wait. Per memory, a race run is green ONLY on "all green" in the log:
```bash
cd /work/gigagit/.claude/worktrees/working-reviews && ./test.sh > /tmp/claude-1000/wr-test.log 2>&1; tail -5 /tmp/claude-1000/wr-test.log
cd /work/gigagit/.claude/worktrees/working-reviews && ./test.sh race > /tmp/claude-1000/wr-race.log 2>&1; tail -5 /tmp/claude-1000/wr-race.log
```
Expected: both logs end with "all green".

- [ ] **Step 6: Commit**

Message: `docs+e2e: working-changes reviews as notes`

---

## After the last task

1. Final whole-branch review: run `review-package` against `git merge-base main HEAD` and dispatch ONE read-only review subagent on the most capable model. Give it the package path, this plan, the spec, the Review Focus section verbatim and the ledger's `Ruling:` lines. It edits nothing, commits nothing and runs nothing that writes into the worktree.
2. One fix pass for Critical/Important findings. Each fix goes test RED → GREEN, then the full suite runs green. Minors go to the ledger.
3. `./test.sh` and `./test.sh race`, both "all green".
4. Ask the user before merging (`gg merge -F <msgfile> --into main feat/working-reviews`). After the merge: `./build.sh install`, check that main is clean, remove the worktree, update memory.
