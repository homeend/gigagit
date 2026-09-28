# Review view: structured AI reviews rendered as a commit view (design)

Status: agreed in brainstorm 2026-09-28; awaiting spec review.
Builds on `2026-09-27-review-notes-design.md` (merged `553e0926`): a review is
ONE note on the reviewed commit (a branch review also carries the branch). This
design changes what is inside that note and how it is shown — not where it is
stored or how it is found.

## Problem

Today a review note holds whatever the agent printed. Claude's `/code-review`
returns prose plus a fenced JSON array in its own shape
(`file`/`line`/`summary`/`failure_scenario`), and gg shows the text as is: a
plain-text viewer, or an all-green "added file" diff under `@notes/`. Nothing
is rendered, and per-file findings are not attached to their files.

## Goal

1. Every review agent returns ONE document in gg's format: a markdown overview
   plus per-file notes (agent-context v1, extended with `meta`).
2. Opening a review builds a **review view** on the fly — the reviewed commit's
   file tree with the review's notes attached to their files and lines, the way
   a pull request's comments are shown — and the overview rendered as markdown.
3. The review's notes are NOT stored as separate notes; they exist only while
   the view is open (the `forge_notes.go` precedent: PR comments become notes at
   read time and are never stored).

## Rulings (user, 2026-09-28)

1. Scope: commit reviews first; a range or branch review uses the same view
   over its range's files (the stored document is the same).
2. The document format is agent-context v1 (already parsed by `gg note apply`,
   `gg review --notes` and MCP), with the top-level `summary` as the markdown
   overview, `files[].summary` as a one-line per-file summary, and a generic
   `meta` object replacing `tags`/`confidence` in the prompt (the parser keeps
   accepting the old fields).
3. `meta` (an open string → value object) is allowed on the document, each file
   and each annotation; gg shows every key as a `key: value` chip and never
   depends on a specific key. The prompt suggests `severity` and `confidence`.
4. Storage and delivery are unchanged: the review stays one note. Only the
   document shape and its processing change.
5. Delivery channel: the agent writes ONLY the JSON document to
   `$GG_MESSAGE_FILE`; the schema and one example go into the context document
   every review agent already receives. Built-in templates change (Claude drops
   `/code-review`).
6. The view: see §4 and the mockups (`/mnt/t/others/review-mock-{1-tree,
   2-stacked,3-overview-popup}.png`, local).
7. A reply that is not valid gg JSON is stored as text and shown as a rendered
   markdown document; the run's notice says so.
8. Config copies of the old built-in review commands are upgraded through the
   preflight migration feature (consent, not automatic) — §2.

## 1. The document

```json
{
  "version": 1,
  "summary": "## Overview\nMarkdown …",
  "meta": { "verdict": "request changes" },
  "files": [
    {
      "path": "src/app/Main2.kt",
      "summary": "duplicate starter; won't compile",
      "meta": {},
      "annotations": [
        {
          "newRange": [3, 3],
          "summary": "Adds another top-level fun main()",
          "rationale": "Kotlin rejects it as CONFLICTING_OVERLOADS …",
          "meta": { "severity": "bug", "confidence": "high" }
        }
      ]
    }
  ]
}
```

- `version` must be 1. `summary` (the overview) is required and non-empty.
- `files[].path` is repo-relative (slash notation). `annotations[]` each carry
  exactly one of `newRange` (the commit side) / `oldRange` (the parent side),
  1-based inclusive, plus a non-empty `summary`; `rationale` optional.
- `meta` values are shown as text (strings as is; numbers/bools formatted;
  nested objects as compact JSON). Old `tags` (array) and `confidence`
  (low/medium/high) on an annotation are folded into `meta` on read.
- Unknown top-level/file/annotation keys are ignored (forward compatible).

Parser: `notebatch.ParseReview(data) (ReviewDoc, error)` — a new entry point in
the existing DAG-leaf package, beside `Parse` (which keeps its behaviour for
`gg note apply`):

```go
type ReviewDoc struct {
    Overview string
    Meta     []MetaKV
    Files    []ReviewFile
}
type ReviewFile struct {
    Path    string
    Summary string
    Meta    []MetaKV
    Notes   []ReviewNote
}
type ReviewNote struct {
    Side      string // "new" | "old"
    Range     [2]int
    Summary   string
    Rationale string
    Meta      []MetaKV
}
type MetaKV struct{ Key, Value string } // sorted by key: stable display order
```

A fenced block around the JSON (```` ```json … ``` ````) or a Claude
`--output-format json` envelope is unwrapped before parsing (the existing
`exttool.ParseCaptureReport` handles the envelope).

## 2. Asking agents for it

- `engine.ReviewChanges` context document gains a **"Review output"** section
  (always, not only with `--notes`): the schema above, one short example, the
  rules (only JSON, no prose outside it; paths repo-relative; ranges on the
  side the line lives on; `meta` optional with suggested keys `severity`
  (bug/risk/design/nit) and `confidence` (low/medium/high)), and "write it to
  the file at `$GG_MESSAGE_FILE`".
- `$GG_MESSAGE_FILE` is always set for a review (capture mode), so a custom
  command that reads the context document gets the same instruction.
- Built-in templates (`internal/exttool`):
  - Claude: `claude -p "<review prompt>" --output-format json
    --permission-mode acceptEdits --allowedTools "Read" "Write"
    "Bash(git diff *)" "Bash(git log *)" "Bash(git show *)"
    "Bash(git status *)"` where the prompt says: review the change described in
    `$GG_CONTEXT_FILE` (range `<range>`), follow its "Review output" section,
    write the JSON to `$GG_MESSAGE_FILE`, change nothing in the repository.
  - Junie and Kimi: their review prompts change from "write a concise code
    review" to the same instruction.
  - Each changed template's catalog entry is re-verified against the live tool
    where available; otherwise the verification date comment says "not
    re-verified".
- **Stored copies of the old built-ins are migrated** (ruling 8). First-run
  detection writes the built-in templates INTO the config, so a machine that
  ran gg before this change holds the old commands (the user's global config
  holds the Claude `/code-review` one). A new optional preflight feature
  `structured-reviews` declares `preflight.LegacyStore{Store:
  "review-commands"}`; its probe (`legacyReviewCommandsPresent`) reports
  present when the global or the active repo config holds a `review`
  `[[tools.command]]` whose `command` is byte-identical (after trimming) to an
  entry of `exttool.SupersededReviewCommands` — every previous built-in review
  template, kept as a list. Its `Migration{Store: "review-commands", From: 1,
  To: 2, Action: "upgrade-review-commands"}` is NOT lossless (it rewrites the
  user's config), so it goes through the existing consent screens (TUI
  notification, CLI, web) and never runs automatically. The action rewrites
  only the matching entries' `command` to the current built-in of the same
  catalog agent and mode, via the config package's line-edit writer; commands
  the user edited never match and are never touched. After it the probe finds
  nothing, so the notice does not come back in any repo.
- A user's own (edited) `[[tools.command]]` is not rewritten; it gets the
  context-document instruction and, if it still returns prose, the fallback.
- `internal/agentskill/reviewing-with-gg.md` documents the format;
  `agentskill.ReviewVersion` is bumped and `gg init --update` refreshes the
  installed copies.
- The `--notes` second channel (`$GG_NOTES_FILE`) is removed: the notes travel
  in the document. `gg review --notes` now means "also keep this review's notes
  as permanent line notes" (imported with `domain.ApplyNoteBatch` from the
  parsed document, exactly as today's import does from the notes file).

## 3. Storing and validating

- `domain.SaveReview` is unchanged in what it stores (the note's `Rationale`)
  but now receives the text after `ParseReview`: a valid document is stored as
  its canonical JSON (re-marshalled, so fences/envelopes are gone); anything
  else is stored as the text received.
- `SaveReview` returns `Structured bool`. The task run / CLI / web report it:
  - TUI: when a review finishes unstructured, the notice reads
    "<key> ready — not in gg review format, shown as text".
  - CLI: `warning: the review is not in gg review format; stored as text` on
    stderr.
  - Web: the same sentence in the report dialog.
- `Review` (the read model) gains `Doc *notebatch.ReviewDoc` (nil = text) —
  parsed on read, so reviews stored before this change and text fallbacks need
  nothing special.
- The CLI prints, on stdout: the overview, a blank line, then one line per note
  `path:<line>[-<end>] — summary` (old side: `path:-<line>`). An unstructured
  review prints its text.

## 4. The review view (TUI)

### Opening it

Enter on a review from any of the four places — the `@notes/` entry in a
commit's file list, a Branches review row, a View all notes review row, the
AI-tasks tab — opens the review view. For an unstructured review it opens the
viewer with the text rendered as markdown (§4.5) instead.

### The tree (mockup 1)

The files view in a new **review mode** over the reviewed commit (`Commit` of the
review; a range review: the range's files, via the existing compare lane):

- Title `Review: <branch or short sha> <short sha>`, second line
  `<agent> · <age> · <n> notes on <m> files`.
- First row: `≡ Overview`.
- Then the commit's changed files, grouped by directory as today. A file with
  review notes shows `◆n` and, on the next row (dim), its `files[].summary`.
- Files the document names but the commit does not change are NOT added to the
  tree; they are listed under "Other notes" in the overview (§4.3).
- Keys: the files view's own (↑/↓, enter, `/`, `S`), plus `n`/`p` = next/prev
  file with notes; `esc` returns to where the view was opened from (the layer
  stack / `handOffToFilesView` rules).

### The overview (mockups 2 and 3)

The overview markdown is rendered with `internal/markdown` + `tui/md_render.go`
(the PR renderer). Below it, generated by gg:

- **Other notes** — notes on paths the commit does not change, or on a line
  past the end of the file: `path:line — summary` rows; enter on one opens that
  file at the commit (or the working tree when the commit lacks it).
- Document-level `meta` as chips under the title.

Single-file mode: enter on `≡ Overview` opens it as a popup (`popupMax`, `y`
copies the markdown source). Stacked mode: the overview is the first stack
element, above the first file, with its own header `━━ Overview`.

### Notes in the diff

A review's notes are `ResolvedNote`s built at read time
(`domain.ReviewNotesFor(ctx, reviewID, path, diff)`), like `forgeNotesFor`:

- id `review:<noteID>:<n>` (a new `model.IsReviewNoteID`), `Source: agent`,
  `Author` = the review's agent, anchored with `resolveNotes` against the
  diff's sides (a line that no longer matches shows as it does for any stale
  note — but the view is of the reviewed commit itself, so it matches).
- Read-only: `NoteEdit`/`NoteReply`/`NoteRemove` refuse `review:` ids with
  `ErrReadOnlyNote`, as for `forge:`.
- The note box shows the summary, the rationale and the `meta` chips.
- In the review view the diff shows ONLY the review's notes (the commit's own
  stored line notes are not mixed in); the normal commit view never shows them.
- `n`/`p` step through notes as today; the stack's `N`/`P` step files.

### Text fallback

An unstructured review opens in the `srcNote` viewer with markdown rendering on
(the viewer gains a markdown display mode, the one the PR body uses), not as
plain text; `@notes/` → enter opens this viewer, not the green diff.

### What does not change

The normal commit files view (only the `@notes/` entry, which now opens the
review view), the Branches rows and markers, View all notes' rows, and the
AI-tasks tab entries — only their enter target changes.

## 5. Web

- The review lane stores the same document (`ReviewReport` → `SaveReview`).
- The report dialog renders the overview with `static/markdown.js` (server-side
  parse via `internal/markdown`, as for PR bodies) and lists the notes as
  `path:line — summary` rows with their meta chips.
- The full web review view (tree + notes in the web diff) is out of scope.

## 6. Testing

- `notebatch.ParseReview`: a full document; missing `summary`; `newRange` and
  `oldRange` both / neither; old `tags`/`confidence` folded into `meta`; nested
  `meta` value; unknown keys ignored; a fenced block unwrapped; a non-JSON text
  rejected.
- `engine.ReviewChanges`: the context document always carries the "Review
  output" section; no `$GG_NOTES_FILE`.
- `exttool` templates: Claude no longer calls `/code-review` and allows `Write`;
  Junie/Kimi prompts name the JSON contract; every previous built-in review
  command is in `SupersededReviewCommands`.
- Migration: the probe finds an old built-in in the global / repo config and
  ignores an edited one; the action rewrites exactly the matching entries,
  leaves the rest of the file byte-identical, and the feature then resolves
  Satisfied; it is not run by `RunAutoMigrations`.
- `domain`: `SaveReview` stores canonical JSON for a valid document and the raw
  text otherwise, reporting `Structured`; `Review.Doc` parsed on read;
  `ReviewNotesFor` anchors notes on the commit's diff, refuses edits; "other
  notes" split (path not in the commit, line past the end).
- CLI: `gg review` prints overview + note lines; `--notes` imports the
  document's notes as permanent notes; an unstructured reply warns.
- TUI (`ansi.Strip(View())` + model tests): enter on `@notes/` opens the review
  view; the tree has `≡ Overview`, `◆n` and the per-file summary; overview
  popup renders markdown (a heading, inline code) and "Other notes"; stacked
  mode puts the overview first; a file's diff shows the review note box with
  meta chips and no stored notes; esc returns; an unstructured review opens the
  markdown viewer.
- Web: the review op returns a rendered overview + notes list.

## Out of scope

- A web review view (tree + notes in the web diff).
- Branch reviews over many commits beyond "the range's files" (no per-commit
  split).
- Editing, replying to or resolving a review note in place (a reply would need
  a stored thread; `gg review --notes` is the way to keep notes).
- Re-running a review from the view.
