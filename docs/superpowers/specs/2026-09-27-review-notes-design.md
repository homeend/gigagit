# AI reviews stored as notes on commits and branches (design)

Status: agreed in brainstorm 2026-09-27; awaiting spec review.
Supersedes stage 2 ("Agent results are notes") of
`2026-09-27-notes-forge-agent-context-strategy.md` (branch
`feat/notes-forge-agent`): that stage kept a review's summary in the task
history; this design makes the whole review a note.

## Goal

An AI review's result lives in ONE place — the per-repo note store
(`notes.toml`) — and every frontend writes and reads it there. A review is
distinguishable as a **commit review** or a **branch review**, and is shown on
the thing it is about: the commit's diff, the Branches tab, View all notes…,
and the AI-tasks tab.

## Rulings (user, 2026-09-27)

1. Commit, range and branch reviews become notes. Commit-message and conflict
   results stay in the AI-tasks history. A review of **working changes** (no
   commit) stays in the AI-tasks history as today.
2. A range review is a note on the range's last commit, with the range recorded.
3. A **branch note** is a commit note on the branch tip at review time plus the
   branch name. It is shown as a branch note while that commit is still the
   branch's tip; after the branch moves, it is shown on its commit as a commit
   note marked "was the tip of `<branch>`". The Branches tab lists only notes on
   the current tip.
4. Deleting a branch in gg deletes every note carrying that branch name.
5. Review notes never expire: no stale sweep, no age limit, no entry cap.
6. One store, one writer, one reader, no fallback to the old files. Nothing else holds a review's text: the
   `reviews/<repo>/` reports, the `task-results/` viewer copies and the
   history's `.result` file stop being written for reviews.
7. Migration is by typed write/read models with a notes implementation; callers
   move one path per commit; old writers/readers are deleted last. No dual
   write.
8. Commit diff: stacked layout shows the reviews inline above the files;
   non-stacked layout shows an `@notes` entry at the top of the file list whose
   pane shows the review text.
9. Branches tab: a marker on a branch with reviews; review sub-rows under the
   branch, folded by default, →/← to unfold/fold, enter opens; the `.` menu
   gets "Show review" (newest).
10. Web display of reviews in its commit/branch views is the NEXT spec. In this
    one the web writes and reads through notes and shows the review in the op
    result only.

## 1. What exists today

| Path | Writes | Reads |
|---|---|---|
| TUI review task (`domain.Tasks()`, `ReviewTask`) | taskhist `<id>.result`; `task-results/<id>.md` (`SaveTaskResult`, via `openResultViewer`); `reviews/<repo>/…md` (`SaveReviewReport` in `tui/review.go`) | AI-tasks tab (`historyText`), file viewer (`openResultFile`) |
| Web review (`web/review.go` → `ReviewReport`, not Tasks) | `reviews/<repo>/…md` | op response `{report, path, label}` |
| CLI `gg review` (`ReviewReportNotes`) | `reviews/<repo>/…md`; line notes with `--notes` | stdout + `report: <path>` on stderr |

`--notes` line notes (anchored findings imported through `notebatch`) are
already notes and are unchanged by this design.

## 2. The review note

A review is a `model.Note` in the existing store:

| Field | Commit review | Branch review |
|---|---|---|
| `Address.State` | `StateCommitted` | `StateCommitted` |
| `Address.Commit` | full sha of the reviewed commit (a range's last commit) | full sha of the branch tip at review time |
| `Address.Branch` | `""` | branch name (local `feat/x` or remote `origin/x`) |
| `Address.Path` | `""` | `""` |
| `Tags` | `["review"]` | `["review"]` |
| `Source` / `Author` | `agent` / the tool name | same |
| `Summary` | `Review: <short sha> <subject> (<short range>)` | `Review: <branch> (<short range>)` |
| `Rationale` | the full review text (markdown) | same |
| `Side`, `Range`, `ContextHash` | zero | zero |

`Address.Branch` exists in `model.FileAddress`, but it is NOT free: the TUI
diff view already fills it on working-tree line notes (`diff_view.go`
`noteAddr`: `Branch: m.status.Branch`). So `Branch` alone never marks a branch
review. A note is a **review note** only when it is path-less, has a commit,
and carries the `review` tag; every rule below (kind, branch cleanup, display)
is scoped to review notes. A path-less note with a commit is a
**commit-level note**; the store rules in this section apply to all of them.

The reviewed range is recorded in a new `Note` field, `Scope string`
(`toml:"scope,omitempty"`, hex range like `a1b2…..e4f5…`), so the display does
not have to parse the summary. Empty for everything but reviews.

### Kind at read time

`ReviewKind` is computed, never stored:

- `ReviewOnCommit` — `Branch == ""`.
- `ReviewOnBranch` — `Branch != ""` and the branch currently resolves to
  `Commit`.
- `ReviewWasTip` — `Branch != ""` and the branch is missing or points
  elsewhere. Shown on the commit with "was the tip of `<branch>`".

### Store rules for path-less notes

In `internal/notes` and `domain/notes_sweep.go`:

- `capOldestFirst` never drops a path-less root (it is not counted toward the
  cap, and replies to it are kept with it).
- The age limit skips path-less notes.
- The resolve sweep skips path-less notes: there is no line to re-anchor. A
  review whose commit no longer exists is kept and shown as missing (§4).

## 3. Write and read models (domain)

New file `internal/domain/review_notes.go`:

```go
// SaveReview is the one command every review path issues.
type SaveReview struct {
    Target ReviewTarget // Range (hex), Label, Kind, plus Commit and Branch (new)
    Agent  string       // tool name, becomes Note.Author
    Text   string       // the review, markdown
}

type Review struct {
    ID      string     // note id
    Kind    ReviewKind // computed
    Commit  string
    Branch  string
    Scope   string     // hex range
    Agent   string
    Summary string
    Text    string
    Created time.Time
}

func (s *Service) SaveReview(ctx context.Context, cmd SaveReview) (string, error) // note id
func (s *Service) Review(ctx context.Context, id string) (Review, error)
func (s *Service) ReviewsForCommit(ctx context.Context, sha string) ([]Review, error)
func (s *Service) ReviewsForBranch(ctx context.Context, name string) ([]Review, error) // current-tip only
func (s *Service) Reviews(ctx context.Context) ([]Review, error)                       // all, newest first
func (s *Service) ReviewCounts(ctx context.Context) (map[string]int, error)            // by branch, current tip only
```

The frontends see `Review` values only; nothing outside `domain` knows reviews
are notes. `SaveReview` refuses a working-changes target with `ErrNoReviewCommit`; no
caller sends one (below).

**Working-changes reviews are outside this design (ruling 1).** A
`ReviewWorking` target gets no `Store` hook, keeps today's `.result` file and
viewer copy (`SaveTaskResult`), and `ReviewReport` / `gg review --working`
skip the save and return no note id. Everything below about "no `.result`"
applies to commit, range and branch reviews only.

`ReviewTarget` gains `Commit string` (the full sha the note anchors to; the
range's right side resolved, or the tip) and `Branch string` (set only by
`BranchReviewTarget` when its argument names a branch ref; a sha argument
leaves it empty). Both are data, never executed; `Range` stays the only value
spliced into the command.

A review task stores its note through a new `TaskSpec` hook,
`Store func(result string) (noteID string, err error)`, which `ReviewTask`
binds to `s.SaveReview` for every target except `ReviewWorking`. The task
manager is process-global and repo-agnostic;
the closure carries the repo.

## 4. Behaviour by path

### TUI / task scheduler

- On each new result the manager calls `spec.Store`. Both result paths go
  through `setResult` — headless (`runHeadless`, once) and interactive (the
  `$GG_MESSAGE_FILE` watcher, each write) — so the hook lives there and an
  interactive review is saved as soon as it lands. The first call creates the
  note; later results of the same task update it (same id). `TaskInfo` and
  `taskhist.Record` gain `NoteID`.
- For a task with a `Store` hook, the history writes **no** `.result` file,
  ever.
- **No fallback to files.** A review is written to notes or not at all. The
  write is designed not to fail short of a full or read-only disk:
  - **Lock contention** (another gg process holding `notes.toml.lock` longer
    than `filelock.Wait`, 2 s): `SaveReview` retries with backoff for up to
    10 s before giving up.
  - **Corrupt `notes.toml`**: `SaveReview` does NOT refuse like other note
    writes. It moves the file aside to `notes.toml.corrupt-<unix time>`
    (nothing is deleted), starts a fresh store, writes the review, and reports
    the move as a warning.
  - **Disk full / read-only / no state dir**: the task ends failed with the
    error. The review text stays in the running gg's memory (`TaskInfo.Result`)
    and the AI-tasks row offers "Retry save"; quitting gg loses it.
- `tui/review.go` stops calling `SaveReviewReport`; it opens the review by note
  id.

### Web

`web/review.go` calls `ReviewReport`, which now saves through `SaveReview`
(except for working changes) and returns `{report, noteId, label}`; `path` is
dropped and the op summary reads "review saved as note <id>". `static/review.js`
shows the report path in the report dialog (`#report-path`, `state.task.path`);
that line shows the note id instead (empty for working changes).

### CLI

`gg review` prints the review on stdout as today and `note: <id>` on stderr
instead of `report: <path>`. `--notes` still imports line notes. A working-
changes review (`--working`) prints the review and saves nothing (ruling 1).
`internal/agentskill/using-gg.md` is updated and `agentskill.Version` bumped.

### Branch changes (all frontends)

`domain.Execute`, after a successful op:

- `DeleteBranch{Name}` → delete review notes with `Address.Branch == Name`.
- `DeleteRemoteBranch{Remote, Branch}` → delete review notes with
  `Address.Branch == Remote + "/" + Branch`.
- `RenameBranch{Old, New}` → rewrite `Address.Branch` Old → New on review
  notes (the renamed branch keeps its reviews).

Line notes are never touched, even when their address carries the branch.

A failure here does not fail the op; it is reported as a warning event.
Branches deleted outside gg leave their notes, which then read as
`ReviewWasTip`.

## 5. Display (TUI)

**Viewer.** A review opens in the existing file viewer through a new
`fileSource` kind, `srcNote` (key: note id). `loadDoc` reads `s.Review(id)`;
the doc reloads when `notes.toml` changes (the file-watch set gains that
path). Title: the summary. Copy and search work as for any file. A deleted
note shows "review deleted".

**Commit diff, stacked.** A "Reviews" block above the first file: one row per
review, `when · agent · summary` (+ "was the tip of `<b>`" / "branch `<b>`"),
enter expands the text inline; the block counts as a stack slot for n/p/N/P.

**Commit diff, non-stacked.** An `@notes` entry at the top of the file list
(dir row, one child per review). Selecting a child shows the review text in
the diff pane, rendered as the viewer renders it (read-only, cursor + copy).

**Branches tab.** A branch with current-tip reviews shows `◆<n>` (from
`ReviewCounts`). →/← unfolds/folds review sub-rows under it
(`when · agent · summary`); folded by default; enter on a sub-row opens the
viewer. The `.` menu on a branch with reviews gets "Show review", which opens
the newest.

**Commit list.** No change: `NoteCounts.ByCommit` already counts path-less
notes, so the ◆ badge includes reviews.

**View all notes….** `NotesOverview` adds path-less notes to their commit as an
`@notes` pseudo-file listed first; each note's WHERE column reads
`branch <b>` / `was tip <b>` / `commit`. Enter opens the viewer (not a diff).

**AI-tasks tab.** Enter on a review run opens its note; a run with a `NoteID`
whose note is gone shows "review deleted". Runs recorded before this change
keep their `.result` and open as today until they age out.

Every new string goes through `i18n.T` in all four bundles.

## 6. Migration (one commit per step, suite green after each)

1. Store rules for path-less notes (cap/age/sweep) + `Note.Scope`.
2. `SaveReview` / read models + `ReviewTarget.Commit/Branch`, with tests; no
   callers.
3. Task path: `TaskSpec.Store`, `NoteID` on `TaskInfo`/`Record`, no `.result`
   for stored tasks, failure fallback; TUI opens reviews by note id
   (`srcNote` viewer).
4. Web path (`web/review.go` + `static/review.js` report dialog).
5. CLI path + skill doc.
6. Branch delete/rename follow-ups in `Execute`.
7. Display: commit diff (stacked + non-stacked), Branches tab, View all
   notes…, AI-tasks tab.
8. Delete the old paths: `writeReviewReport` / `SaveReviewReport` and the
   `reviews` state dir, `SaveTaskResult` use for commit/range/branch reviews
   (the helper stays for conflict results and working-changes reviews), and
   their tests; update `statebasedir` caller lists.
9. Docs: CHANGELOG, README, `docs/CLAUDE-details.md`. (The strategy doc lives
   on `feat/notes-forge-agent`; its supersession is recorded in memory, not
   edited from this branch.)

## 7. Testing

- `notes`: cap and age skip path-less roots (watch fail with the skip removed).
- Branch cleanup leaves a line note whose address carries the deleted
  branch's name (the TUI's working-tree notes do).
- `domain`: `SaveReview` for commit/range/branch targets; kind computation
  across tip move, branch delete, missing commit; `ReviewsForBranch` current
  tip only; working target refused; branch delete/rename/remote-delete
  follow-ups through `Execute` on a real repo.
- Task manager: interactive review saved on first result and updated on the
  second (same id); a held lock released within the retry window → saved;
  a corrupt `notes.toml` → moved aside, review saved, warning; an unwritable
  state dir → failed task, no `.result`, "Retry save" succeeds once writable.
- CLI: `gg review` prints `note: <id>`, writes no `reviews/` file (state dir
  in `t.TempDir()`).
- Web: the review op returns `noteId`, no `path`.
- TUI: `ansi.Strip(View())` tests for the stacked block, the `@notes` entry,
  Branches sub-rows fold/unfold, `◆n` marker, `.` "Show review", AI-tasks tab
  enter → viewer with the review text, "review deleted".
- e2e: a scenario that reviews a branch (fake tool), asserts one note with the
  branch, deletes the branch, asserts no note.

## Risks

- **Older gg binaries.** The note store has no data-format marker in
  `preflight`. An older build (e.g. a stale Windows exe) run against the same
  store would treat a review note like any note: its sweep would drop it (a
  path-less note does not resolve) and a rewrite would drop `Scope`. Accepted:
  one user, binaries rebuilt on merge (`./build.sh install`, `./build.sh web`).
  Registering a notes format is the fix if this bites.

## Out of scope

- Web display of reviews in commit/branch views (next spec).
- Converting old `reviews/` / `task-results/` / `.result` files.
- Reviews following a commit through rebase/amend (a new sha is a new commit;
  the old note shows as `ReviewWasTip` or missing).
- Publishing reviews to a forge.
