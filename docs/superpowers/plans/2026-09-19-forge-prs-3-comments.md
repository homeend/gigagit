# Forge PRs — plan 3 of 3: review comments inside the PR diff

> **For agentic workers:** executed INLINE by the session that wrote it
> (project rule: never subagents), with superpowers:executing-plans. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** while a pull request's diff is open, show its inline and file-level
review threads as read-only note boxes on their lines, let any note box
collapse to one line, keep the comments fresh, and reach the PR hub from the
diff.

**Architecture:** a PR diff is a one-off preview whose source is
`refs/gg/pr/<n>` — `git.ParsePRRef(set.Source)` is how `domain` knows a preview
is a PR, with no new plumbing through the TUI. `domain` converts cached
`PRComments.Inline` into `ResolvedNote`s (`NoteSourceForge`, id `forge:<id>`,
status active — the forge already vouched for the position) and appends them in
`PreviewNotesFor` / `PreviewNotesAt` / `PreviewNotesAll` / `PreviewNoteCounts`,
so the diff, the file-list badges, `}`/`{`, search and "List notes…" pick them
up through the code that already serves store notes. Comments are cached per PR
on the Service (one network read per open; `PRCommentsRefresh` re-reads and
reports whether anything changed). Collapse is view state on `diffView`
(`collapsed map[rootID]bool`), rendered as a new one-row note kind.

**Tech stack:** Go 1.26, Bubble Tea; plan 1/2 APIs (`PRComments`,
`PRPair`, `prsLoadedMsg`, `previewOpenState`).

**Spec:** `docs/superpowers/specs/2026-09-19-forge-prs-design.md` §4
(conversion), §5 Inline comments. Plans 1 and 2 beside this file.

## Global constraints

- READ-ONLY. A `forge:` note can never be edited, replied to, removed, or
  written to the note store; `Remove all notes…` never counts or touches one.
- Forge text is never translated; every new TUI string is `i18n.T` + four bundles.
- No network call on the UI thread; a failed comment read never breaks the
  diff's own (store) notes — it degrades to "no forge comments" and is logged.
- Collapse is generic: it works on store notes exactly as on forge notes.
- `internal/tui` never imports `internal/forge` / `internal/git` (non-test).
- New TUI tests `t.Parallel()`; tests use the `fakeForge` provider
  (`domain` tests) or hand-built `ResolvedNote`s (`tui` tests) — no real `gh`.

## Amendments to the spec (recorded, not re-asked)

1. `PRComments.Inline` stays `[]model.ForgeComment` (plan 1's shipped JSON
   shape); conversion is a separate domain step, not a type change.
2. The diff view is side-by-side only. A file-level comment's box hangs off
   the view's FIRST line (right pane), titled as a file comment — i.e. at the
   top of the file, one row below where the spec drew it.
3. Collapse keys: `o` toggles the note at the cursor, `O` collapses/expands
   all (both also in the `.` menu). Resolved forge threads start collapsed.
4. An inline comment whose line is not in the PR diff (the forge positions
   against its own diff) is simply not drawn; it remains readable in the hub
   through `gg pr comments`. No re-bucketing into Outdated.
5. `gg://` pair-link copy for a PR is dropped: the link would name
   `refs/gg/pr/<n>`, a machine-local ref that resolves nowhere else. `y`
   (the PR's web URL) is the portable address.

## File map

| File | Change |
|---|---|
| `internal/model/note.go` | `NoteSourceForge`, `ForgeNoteIDPrefix`, `IsForgeNoteID`, `NoteTagResolved` |
| `internal/domain/forge_notes.go` (new) | cache, `PRCommentsRefresh`, `forgeNotesFor`, conversion, `ErrReadOnlyNote` |
| `internal/domain/previewnotes.go`, `notes.go` | append forge notes / counts; reject `forge:` ids |
| `internal/tui/diff_notes.go`, `diff_view.go`, `diff_render.go` | forge title, collapsed row kind, `collapsed` state, `o`/`O`, `i` |
| `internal/tui/note_keys.go`, `note_popup.go`, `notes_list_popup.go` | read-only guard, source mark, menu rows |
| `internal/tui/pr_comments.go` (new) | re-poll cmd/msg, hub-from-diff |
| `internal/tui/preview_open.go`, `pr_actions.go`, `pr_panel.go` | `prNumber` on the open state; re-poll trigger |
| `internal/tui/help.go`, footer hint (`diffHintFor` budget!), i18n bundles | advertise |
| docs | CHANGELOG, README, CLAUDE-details, spec amendments, memory |

---

### Task 1: model + domain conversion and cache

- [ ] **Tests** (`internal/domain/forge_notes_test.go`, `fakeForge` from
  `forge_test.go`): 
  - a PR preview set (`Source: "refs/gg/pr/7"`) → `forgeNotesFor(ctx, set,
    "a.go")` returns one root per thread with replies nested, `ID`
    `forge:<id>`, `Source` forge, `Author` login, `Side` from the comment,
    `Range` `{StartLine|Line, Line}`, file-level → `Range{0,0}`, resolved →
    tag `resolved`, `Status` active, body → `Summary` = first line,
    `Rationale` = the rest;
  - a non-PR set returns nil and makes NO provider call;
  - the provider is called once for two paths (cache), again after
    `PRCommentsRefresh`, whose `changed` is false for identical data and true
    when a comment is added;
  - a provider error → nil notes, nil error, and the next call retries;
  - `PreviewNotesFor` on a PR set returns store notes + forge notes;
    `PreviewNoteCounts` counts forge roots per path;
  - `NoteEdit` / `NoteReply` / `NoteRemove` with a `forge:` id →
    `ErrReadOnlyNote`, store untouched.
- [ ] Implement. Cache under `forgeMu`: `forgeComments map[int]PRComments`;
  the provider call runs OUTSIDE the lock (plan-1 rule). `reflect.DeepEqual`
  for `changed`. `ForgetPR` path drops the cache entry.
- [ ] `go test ./internal/domain/ ./internal/model/` → green; commit
  `feat(domain): forge review comments as read-only notes on a PR preview`.

### Task 2: forge note boxes in the diff (title, file-level anchor, read-only keys)

- [ ] **Tests** (`internal/tui/forge_notes_test.go`, building a `diffView`
  the way `diff_notes_test.go` does):
  - title of a forge root: `review · octocat · 2d ago · a.go R12`, plus
    ` · resolved` when tagged; a file-level one reads `review · octocat · … ·
    a.go (file)`;
  - a `Range{0,0}` forge note anchors on the first non-fold line, new side;
  - `E`, `R`, delete (and their `.` menu rows) on a forge note do nothing but
    set `forge comments are read-only`; `c` on the same line still adds a
    store note;
  - `Remove all notes…` row/counter ignores forge notes (`diffHasTipNotes`);
  - "List notes…" entries for forge notes carry the `review` mark.
- [ ] Implement; `noteAnchorLine` handles the file-level sentinel only for
  `NoteSourceForge` (a store note never has `Range{0,0}`).
- [ ] tests + i18n gates green; commit `feat(tui): forge review threads render as read-only note boxes`.

### Task 3: generic collapse

- [ ] **Tests**: `noteRowCollapsed` — a collapsed thread is exactly one row
  `▸ author: first line of summary… (2 replies)` truncated to the box;
  `hasNoteContent` true for it; `o` toggles the thread at the cursor (store
  AND forge), `O` collapses all when any is expanded else expands all; a
  resolved forge thread is collapsed the first time it is seen and stays as
  the user left it across a notes reload (`collapseSeeded`); relayout row
  count shrinks/grows accordingly and the cursor stays on its line; `}`/`{`
  still land on a collapsed thread.
- [ ] Implement in `diff_notes.go` (`v.collapsed`, `v.collapseSeeded`),
  render the new kind in `diff_render.go` beside the other note kinds, `.`
  menu rows `Collapse note` / `Expand note` / `Collapse all notes` /
  `Expand all notes`, help lines. Footer: check `diffHintFor`'s 140-column
  budget — if `o` does not fit, it is help-and-menu-only (the `L` precedent).
- [ ] commit `feat(tui): any note box collapses to one line (o / O)`.

### Task 4: comment re-poll + hub from the diff

- [ ] `previewOpenMsg.prNumber` / `previewOpenState.prNumber` (set by
  `openPRPreviewCmd`, carried by `reopenPreviewCmd` like `title`).
- [ ] **Tests**: a successful `prsLoadedMsg` while a PR diff is open returns
  a `prCommentsCmd`; `prCommentsMsg{n, changed: true}` for the open PR
  re-issues `loadNotesCmd` (when a diff layer is up) and refreshes
  `filesPreviewCounts`; `changed: false` or another PR's number does
  nothing; cursor and `collapsed` survive the reload; `i` in the diff view /
  files view of a PR opens the hub and esc returns to the diff.
- [ ] Implement in `pr_comments.go`; help + `.` menu row
  `Pull request details…` in the PR diff.
- [ ] commit `feat(tui): PR comments re-poll with the list; hub from the diff`.

### Task 5: headless verification, docs, gates

- [ ] `tui-capture.sh` with the plan-2 fixture recipe (memory
  `forge-prs-feature.md`): threads fixture with a current inline thread on
  `a.go`, a resolved one (starts collapsed), a file-level one, a LEFT-side
  one; capture: file list badges, boxes, `o`, `O`, `E` refusal, List notes,
  `i` hub from the diff. Read every snapshot; fix what they show.
- [ ] Docs: CHANGELOG, README PR section, CLAUDE-details, spec "Amendments
  made while implementing plan 3", memory. `using-gg.md` only if the CLI
  surface changed (it does not).
- [ ] `./test.sh race` from a clean tree; verify binary (stripped) sent with
  its absolute path; ask before merging; after merge `./build.sh install`,
  remove worktree + branch.

## Self-review

Spec §5 Inline comments: render (T2), read-only (T1/T2), `}`/`{`/search/list
(T1 via the shared loaders, T2 mark), file-level (T2, amendment 2), collapse
(T3), re-poll (T4). Carried from plan 2: hub-from-diff (T4); pair-link copy
dropped with a reason (amendment 5). Names: `NoteSourceForge`,
`IsForgeNoteID`, `ErrReadOnlyNote`, `PRCommentsRefresh`, `forgeNotesFor`,
`noteRowCollapsed`, `collapsed`, `collapseSeeded`, `prNumber`,
`prCommentsMsg`.
