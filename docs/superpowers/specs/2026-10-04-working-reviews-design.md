# Working-changes reviews as notes — design

Status: agreed in brainstorm 2026-10-04, awaiting spec review.
Spec 2 of 2; builds on `2026-10-04-notes-partitions-design.md` (the store
split) and is implemented after it.

## 1. Goal

A review of uncommitted changes is stored as a note, like a commit, branch or
range review: one place, one engine, readable by agents. Its identity is
carried by the note itself — for each reviewed file a PATH and a CHECKSUM — so
it shows for a file only while that file's content still matches, with no
pointer to an external result file.

## 2. Today (what happens on "Review working changes")

1. gg builds the input from `git diff HEAD` (staged + unstaged changes of
   TRACKED files) and `git diff --numstat`, writes both to temp files and
   starts the review tool with a brief that says "review the changes below"
   (`engine.ReviewChanges.Prepare`, `internal/engine/review_changes.go`).
2. Untracked files are in neither; the agent is never told about them and no
   warning says they were skipped. A staged new file is tracked and included.
3. The result is shown (TUI/web review viewer, CLI stdout).
4. It is NOT stored as a note — `SaveReview` refuses with
   `ErrNoReviewCommit`, `ReviewTask` attaches no store for `ReviewWorking`.
   It survives only in AI-task history (last 50 tasks, all repos).
   `gg review --working --notes` additionally imports the document's
   annotations as loose line notes on the working tree, which expire like any
   note.

## 3. User rulings (2026-10-04)

1. **Per file.** One review note keeps a (path, checksum) list; each file's
   part shows while THAT file matches; edited files drop out.
2. **Checksum = the reviewed content** (the working-tree bytes). Staging an
   unchanged file keeps the review.
3. **Outdated, then swept.** Once no file matches, the review leaves the
   working surfaces, stays in View all notes marked outdated, and the sweep
   deletes it after `[notes] max_age_days`.
4. **Own worktree only.** A review shows only in the worktree it was made in.
5. **Untracked files are reviewed.** The review input gains every untracked,
   non-ignored file as a new-file patch.
6. **Display** as in §7 (approved as proposed).

## 4. The note

One root note per review:

```go
model.Note{
	Source:  model.NoteSourceAgent,
	Author:  "<tool name>",
	Address: model.FileAddress{State: model.StateUnstaged, Worktree: "<abs top-level>"}, // no Path
	Tags:    []string{model.ReviewTag},
	Summary: "Review: working changes",
	Rationale: "<canonical review document, or prose>",
	Files:   []model.NoteFile{{Path: "a/b.go", Blob: "<hex>"}, {Path: "gone.go", Deleted: true}},
}
```

- New field `Files []NoteFile` (`toml:"files,omitempty"`), `NoteFile{Path,
  Blob string; Deleted bool}`. Empty on every other note.
- `model.Note.IsWorkingReview()`: tag `review`, no path, live state,
  `Worktree` set. `IsEntryLevel()` includes it — cap-exempt, never
  line-re-anchored. `IsReviewNote()` stays commit-only; the review read model
  checks both.
- It routes to `worktrees/<key>.toml` (spec 1 `PartOf`) — that is what makes
  ruling 4 hold.
- `ReviewNoteKind` gains `ReviewOnWorktree`.

## 5. Fingerprints

- **Taken by gg, not the agent**, for every path in the reviewed input — the
  agent's document may omit files.
- **Taken when the input is built** (`ReviewChanges.Prepare`), not when the
  reply arrives: the agent runs for minutes, and a file edited meanwhile
  correctly does not match (the review judged the old text). The op returns
  the list with its result; `SaveReview` stores it.
- **Blob** = git's blob id of the working-tree bytes (`"blob <n>\0" + bytes`),
  hashed in-process with the repo's object format (sha1, or sha256 when
  `extensions.objectFormat = sha256`). No `git hash-object` per file. It is
  never compared with an index blob id: clean filters / autocrlf make those
  differ.
- **Deleted** file: `Deleted: true`, no blob; it matches while the path stays
  absent.

## 6. Untracked files in the input

- One `git ls-files --others --exclude-standard -z` lists them.
- gg synthesizes a new-file patch per file in-process from the bytes it reads
  to fingerprint (`--- /dev/null` / `+++ b/<path>`, every line added); a
  binary file contributes `Binary files /dev/null and b/<path> differ`. The
  numstat list gains their lines too.
- They count toward the existing `MaxDiffBytes` cap: past it, the diff is
  truncated exactly as today and the brief says so.

## 7. Matching and display

**Match** (`domain.WorkingReviewState`): per file `matches` / `changed` /
`gone`; the review is `current` while ≥ 1 file matches, `outdated` when none
does. Hashing is cached process-wide by (worktree, path, mtime, size), so a
status refresh re-hashes only files that changed on disk.

**Surfaces** (TUI and web alike):

- **Files panel / web working file list:** a "Review" row at the top while a
  review is current; enter opens it in the existing review view — the
  review's files, each marked matches / changed / gone, the diff against HEAD,
  the annotations drawn only on matching files. A ✎ marker on each file row a
  current review still matches.
- **A matching file's working-tree diff** draws the review's annotations as
  read-only notes (the `review:<id>:<n>` expansion, anchored to the worktree
  address, new side).
- **View all notes** lists every working review; one with no matching file
  is labelled outdated.
- **CLI / agents:** `gg review --working` stores the review and prints
  `note: <id>`; `gg note list` and MCP read it like any review. `--notes`
  together with `--working` is a usage error ("a working review is stored;
  its notes show on the files") — the stored review draws its own notes, so
  the loose-note import goes away for working changes only.
- AI-task history keeps its record of every review, unchanged.

## 8. Every special case gains a branch

`IsWorkingReview` must be handled wherever entry-level notes are special:
`sweepNotes` (no line resolve; drop when outdated AND older than
`max_age_days`), `NoteCounts` (its own field, never in the ◆N badges, like
commit reviews), `NotesOverview`, `reviewOf` / `reviewNotes`, the
`review:<id>:<n>` expansion (`ReviewNotesFor`, `reviewSplit`,
`ReviewFiles`), `capOldestFirst` (exempt via `IsEntryLevel`).

## 9. Testing

- `internal/model`: `IsWorkingReview` / `IsEntryLevel` truth table.
- `internal/engine`: Prepare includes untracked files as new-file patches,
  respects `.gitignore` and the cap, returns fingerprints of the bytes it
  read (a file rewritten after Prepare keeps the old blob).
- `internal/domain`: blob ids equal `git hash-object` on sha1 and sha256
  repos; match states (edit, stage-unchanged, delete, recreate); own-worktree
  only; outdated → swept after max age, current never swept; the cache
  re-hashes only on mtime/size change; `SaveReview` for working changes.
- TUI / web: the Review row, ✎ markers, annotations on a matching file only,
  outdated label in View all notes (golden screen + web test).
- CLI: `gg review --working` prints a note id; `--working --notes` exits 2.
- e2e: review working changes with an untracked file → edit one file →
  that file's annotations disappear, the other file's stay.
