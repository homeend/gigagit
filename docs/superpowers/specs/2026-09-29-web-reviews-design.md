# Reviews in gg web — design

Status: agreed 2026-09-29. Ports the TUI's structured-review experience
(`internal/tui/review_view.go`, `review_delete.go`, `files_view.go`
`withReviewLines`/`reviewRowText`, `branch_reviews.go`) to gg web.

## Goal

The web shows the same stored AI reviews the TUI shows, with the same
behaviour: a commit's reviews above its files, a branch's reviews under its
row, a review view (overview + files + the review's read-only notes in the
diffs) and Delete review. Out of scope: review markers in the Commits list
(a separate follow-up).

## Background

A review is ONE commit-level note tagged "review" (`model.Note.IsReviewNote`).
Its text is the canonical review document (agent-context v1) that
`domain.Review.Doc` parses (`notebatch.ReviewDoc{Overview, Meta, Files}`); a
review whose text is not the document is prose (`Doc == nil`). Its line notes
are built at read time with read-only ids `review:<id>:<n>`
(`domain.ReviewNotesFor`). Domain reads already exist: `Reviews`,
`Review(id)`, `ReviewsForCommit`, `ReviewRevs`, `ReviewFiles`,
`ReviewNotesFor`, `ReviewFileCounts`, `ReviewOtherNotes`, `NoteRemove`, and
`NoteCounts.Reviews` (the heads the TUI's Branches sub-rows are drawn from).

## Server

1. **`GET /api/notes/counts`** adds `reviews: [{id, commit, branch, agent,
   summary, created}]` from `NoteCounts.Reviews` (always an array). The page
   already re-reads counts on every `notes` live event and after each note
   mutation, so branch sub-rows follow deletes from any client and from the
   hosted TUI.
2. **`GET /api/commit/{sha}`** adds `reviews: [...]` (same head shape) from
   `ReviewsForCommit`. It is read only when a commit is opened (a click) —
   never on cursor movement. A failed review read leaves `reviews: []`; the
   file list still answers.
3. **`GET /api/review/{id}`** — the whole view in one call:
   `{id, agent, created, branch, commit, label, structured, base, tip, range,
   files: [CommitFile wire rows], counts: {path: n}, summaries: {path: text},
   overviewMd (markdown.Parse tree), meta (text), text (raw, for copy and the
   prose fallback), notes: n, noteFiles: m, other: [{path, line, summary}]}`.
   `id` must be a stored review note id (404 when gone — `ErrReviewNotFound`);
   the id is never placed on a git argv.
4. **`GET /api/review/notes?id=&path=&status=&old=`** — the review's notes on
   one file as wire notes (`read_only: true`, ids `review:<id>:<n>`), resolved
   against the SAME diff the page shows: the handler builds that diff through
   the cached `Differ` with the request key `/api/diff` uses for the review's
   revs (a commit review: `sha^..sha:path`; a range review: the rev-diff key),
   so the read is a cache hit after the page's diff fetch. `path`/`old` pass
   `isGitArgSafe`.
5. **Delete** = the existing `POST /api/notes/remove {id}` with the review's
   note id (it already emits `notes`). No new write route.

## Page

### Reviews rows on a commit
- `renderFiles` in commit mode, when the open commit has reviews, first draws
  a `li.sect` heading **Reviews** and one row per review:
  `└ 2026-09-29 14:05 claude` (local time `YYYY-MM-DD HH:MM`, then the agent;
  the summary when both are missing — `reviewRowText`).
- Review rows carry `data-review="<id>"` and NO `data-i`: the file cursor,
  staging, the S stack, file stepping and `fileCols` ignore them.
- Click opens the review. Right-click → **Delete review**.

### Branch review sub-rows
- Under each branch row that survives the branch filter, one sub-row per
  review in `reviews` whose `branch` is that branch AND whose `commit` is the
  branch's current tip (`ReviewOnBranch`):
  `└ Review: 2026-09-29 14:05 claude`, class `brev`, `data-review`, no
  `data-n`, not draggable — the branch menu, drag and drop targets skip it.
- Click opens the review; right-click → **Delete review**.

### Review mode
- `state.review` (the `/api/review/{id}` payload plus `back`, where esc
  returns) OVERLAYS the existing files modes: a single-commit review is the
  commit mode on `tip`, a range review the compare mode on `base..tip`, so
  diff URLs, numstat and the S stack need no new arm. The overlay is live
  only while `state.files` (commit) / `state.compare` (range) is the object
  the review opened — any other open replaces it and the overlay lapses. While loading, the files pane shows
  "Opening the review…"; `detailGen` makes a newer open or esc supersede it.
- Title: `Review <label>`; meta line: `agent · date · N notes on M files`.
- List: **≡ Overview** (index 0 in a `reviewRows` list), then the files; a
  noted file shows `◆n` and, under it, its one-line summary dimmed (a
  non-selectable `li.rsum`). The view opens with the cursor on Overview and
  the Overview in the diff pane.
- **Overview pane** (the diff pane): rendered markdown (`mdHTML`), the meta
  line, an **Other notes** list (`path:line — summary`, hidden when empty) and
  a **Copy** button that copies the raw review text.
- **A file row** opens its diff exactly like a commit (single review) or a
  compare of `base..tip` (range review) file. Its note lane is the review's:
  `ctx.review = id`; `noteQuery`/`notesFor` read `/api/review/notes` and
  NOTHING ELSE (no local notes mixed in); `c` (add), edit, reply and remove
  refuse ("a review's notes are read-only"). The S stack uses the same ctx per
  slot through `rowNoteCtx`.
- `n`/`p` in the list step between noted files (stay at the ends); in the
  diff they keep their meaning (changes).
- A prose review (`structured: false`) opens with Overview only: its text
  rendered as markdown.
- **esc**: from a review opened from a commit's Reviews row → that commit's
  file list with the review row selected; from a branch sub-row (or anywhere
  else) → the list stage.
- Right-click on the Overview row → **Delete review**.

### Delete review
- Confirm via `showLocalConfirm("Delete review by <agent> (<date>)?",
  ["cancel", "delete"], …)` — Cancel listed first and focused (default).
- On delete: `POST /api/notes/remove`, then refresh note counts; a commit
  list re-reads that commit's reviews; deleting the open review leaves the
  view the way esc does. A 404 (already gone) is treated as done.

## Error handling
- `/api/review/{id}` 404 → op line "the review is gone" and the view does not
  open (or closes if open).
- Review notes read failure → the diff shows without notes (as other lanes).

## Testing
- Go handler tests (real repo + note store): counts carry `reviews`;
  `/api/commit` carries `reviews`; `/api/review/{id}` shape for a commit
  review, a prose review and 404; `/api/review/notes` returns read-only notes
  on the right lines, none for another path, and 400 on unsafe paths.
- Node/static tests: review-row text; rows have no `data-i`/`data-n`; the
  review note lane is exclusive and write paths refuse.
- Playwright against the NEW binary (md5 + a new-route 404→200 check) on a
  scratch clone holding a review created for the probe: Reviews row visible
  on the commit, branch sub-row visible, Overview rendered, a file diff
  showing the read-only note, Delete review → confirm → row gone. Run once on
  the unfixed build to watch the assertions fail. Remove the probe review
  afterwards.
