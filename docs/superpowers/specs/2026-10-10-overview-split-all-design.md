# Overview split for every overview, review remarks beside the code, the right side highlights the left

Date: 2026-10-10 · Builds on `2026-10-10-review-overview-split-design.md` (the
split layer, `splitView` + `pane`) and its first follow-up (N/P from the file
pane). TUI only; the web split is its own spec once this ships.

## 1. What the user gets

Today the split (overview on the left, code on the right) opens in one place:
enter on a review's **≡ Overview** row. An agent's walkthrough
(`gg session overview`) always opens full-screen, and each anchor replaces it
with the file. The split's file pane shows the code without the review's
remarks. Moving the right-hand cursor (or `n`/`p`) drags the overview's
selection along.

After this change:

1. **Every overview opens as the split** (60+ columns): a review's stored
   overview, and an agent's temporary overview, from every way in — the
   review tree's enter, the open-files switcher (ctrl+\\), an agent's
   `gg session files focus`, and `gg session overview` showing one.
2. **The review's remarks show beside the code**, as read-only boxes under
   the lines they talk about, in the single file pane and in the stacked one.
3. **The left side only moves when you act on it.** The right-hand cursor
   (including `n`/`p`) never moves the overview's selection; when it sits in
   another anchor's block, that anchor is marked on the overview in a second
   style.
4. **`S` stacks the files the overview links to** whenever there is no review
   tree to take the list from.

## 2. User rulings (2026-10-10, do not re-ask)

- R1 (item 6) Review remarks show as read-only boxes in the single and the
  stacked file pane. A remark on a line the commit DELETED is left out of the
  pane entirely — no count, no box; the file's diff (enter in the review tree)
  still shows it.
- R2 (item 8) The overview's selection moves only by actions on the overview
  (↑/↓, enter, a click on an anchor) and by `N`/`P` on the single file pane
  (which name the overview). Everything else on the right — alt+↑↓, a click,
  a search jump, paging, the wheel, AND `n`/`p` (single and stacked) — leaves
  the selection where it is. When the right-hand cursor sits in another
  anchor's band, that anchor is marked on the overview in a second style.
- R3 (items 3 + 7) Every overview opens as the split when the screen is wide
  enough, from every way in. An agent overview's right side shows
  working-tree files with their agent notes; a note anchor steers to the
  note's line.
- R4 (S) With no review tree to list from — an agent overview, or a review
  overview reopened after its review view closed — `S` stacks the files the
  overview's anchors name, in the order they are first anchored.

## 3. Opening the split for any overview

`openReviewOverviewSplit` splits in two: the review-specific caller (the
review tree's enter, unchanged) and a general **`openOverviewSplit(ov)`**:

- the overview `ov` (an `openFile` with `ov.ov != nil`, either
  `srcReviewOverview` or `srcOverview`) is detached from any other frame and
  laid out at the left pane's size;
- the right pane is single (`docPane`) or stacked (`stackPane`) by the `S`
  preference (`reviewStacked`, one preference for both kinds);
- the selected anchor steers the right pane at once; an overview with no
  selection selects its first steerable anchor first (`stepAnchor(1)` from
  -1, as ↓ would);
- `onClose` (esc) backgrounds both documents; when the files view is the
  review view of THIS overview it lands its cursor on the ≡ Overview row.

**Ways in.** A shared door, `showOverview(d)`, opens the split when the
terminal is 60+ columns wide and the full-screen overview viewer otherwise:

| Way in | Today | After |
|---|---|---|
| enter on ≡ Overview (review tree) | split | split (unchanged) |
| ctrl+\\ switcher: enter on an overview | full-screen viewer | `showOverview` |
| `gg session files focus <overview id>` | full-screen viewer | `showOverview` |
| `gg session overview add` (not `--background`) | full-screen viewer | `showOverview` |
| backspace in a file an overview opened (full-screen) | the overview's viewer | `showOverview`, the file on the right |

`bringToFront(d)` routes an overview (`d.ov != nil`) to `showOverview`;
every other document is unchanged. A split already on the stack that shows
`d` is raised (removed and pushed again) instead of building a second one.

## 4. An agent overview's right side

`anchorSource` already gives the working tree for an overview without a tip,
so file and line anchors steer exactly as a working review's do (`docSteer`,
`stackSteer`).

**Note anchors.** `steerSplit` skips anchors with `target.Note` today
(a stored overview's note anchors are `plain`). For a temporary overview a
note anchor steers to the note: `findFileNote(id)` gives the document and the
note, the pane shows that document with the cursor on `note.Start`, no band
current (a note is no band — `anchorCur ""`, as `openAnchor` does). A note
that is gone marks the anchor missing (`mark(true)`, painted struck-through)
and says `note %s is gone`, as `openAnchor` does.

**A missing file** marks its anchor missing and says `no file %s` (the pane
keeps what it showed). The check rides the load (the pane's load error), no
separate stat.

## 5. `S` with no review tree (R4)

`newStackPane` takes the file list from a **`stackFiles(ov)`** source:

- the review tree's file rows, as today, when the files view is the review
  view of this overview (`m.filesReview` and its overview is `ov`'s review);
- otherwise the files the overview's steerable anchors name — a file anchor's
  path, a note anchor's note file — each once, in the order first anchored.

The stack document's key and title follow the source: `files-<review id>` /
`Files: <review label>` for a review, `files-<overview id>` /
`Files: <overview title>` for an anchor list. `toggleReviewStacked` no longer
refuses without a review view. A deleted file (`D`) only exists in the review
list; a file anchor whose file is gone loads as `(load failed: …)`.

## 6. Review remarks in the file pane (R1)

**Domain.** `ReviewFileNotes(ctx, reviewID, path string, lines []string)
([]ResolvedNote, error)` is the review's remarks on `path` placed on the file
at the reviewed tip (`lines` = that file's text): `reviewDocNotes(r, path,
addr, nil, lines, true)` — new-side only, so a remark on a deleted line is
dropped (R1). A working review returns nothing for a file that no longer
matches (the same rule as `ReviewNotesFor`).

**Which documents carry them.** A document an overview steered to carries
that overview's review remarks for as long as it carries its bands — while
`d.from` is a review overview (`srcReviewOverview`) and not closed. They are
fetched off the UI thread once its lines are in (`reviewNotesMsg`, tagged by
the document's tag and the review id), and dropped when `d.from` changes.

**How they draw.** They become `agentdocs.Note` values on a separate field
(`d.reviewNotes`; ID `review:<review id>:<n>`, Start/End from the remark's
range, Summary, Rationale = the remark's body, Author = the review's agent)
and every note reader goes through **`d.shownNotes()`** = the agent's notes
then the review's, by start line. The box, the gutter mark, the pager's
`extraRows`, `}`/`{` and enter (full note) work on both. On a review remark,
`d` (dismiss) and `r` (reference) say `▸ review notes are read-only`
(`reviewReadOnlyNotice`'s words). The agent notes' store sync never touches
`d.reviewNotes`.

**Stacked.** Each stream file carries its remarks (review: `ReviewFileNotes`
after its load; agent overview: the store's notes on that working-tree file);
the stream document's `shownNotes` maps them to stream lines, as `bands()`
maps bands. In the stacked pane every box is read-only (`d` there says
`▸ notes are read-only in the stack — S opens the file`).

## 7. The right side highlights, never moves, the left (R2)

`overview` gains **`here int`** (-1 = none): the anchor whose band the
right-hand cursor sits in, when that is not the selected one. A new class
`mdAnchorHere` paints it with the band colour the right side uses for a
non-current band (`anchorBand`) plus underline — the link on the left wears
the colour of the block the cursor is in. The selected anchor keeps reverse
video.

`here` is recomputed after every key and mouse event the split handles
(`splitView.update`, `splitMouse`) from the right pane's cursor and bands:
the band covering the cursor line (the current band wins an overlap, then
the first), its anchor index, or -1; equal to `sel` → -1. A change repaints
the overview (`paint`). Nothing scrolls on the left.

`n`/`p` in the split change accordingly:

- single pane: `stepAnchorBand` in a split moves the cursor and the current
  band (`anchorCur`) only — no `selectAnchor`; the status line keeps
  `anchor k/n in this file · label`;
- stacked pane: `stackLand` moves the cursor and the current band only when
  called from `n`/`p` (`stepStreamAnchor`); a steer from the overview still
  lands on the selected anchor.

The full-screen viewer is unchanged: there the overview is not on screen and
`n`/`p` keep moving its selection so backspace returns to the anchor stepped
to.

## 8. Text

New and changed strings, all four bundles:

- `Files: %s` reused for an overview title; `▸ notes are read-only in the
  stack — S opens the file`.
- Help: the `enter (≡ Overview)` row says every overview opens beside its
  files, the right side marks the overview's anchor it is in, and the review's
  remarks show as boxes; the `tab / enter` agent-overview row says it opens
  beside the file when the screen is 60+ columns wide.
- The gg-overview skill (and its dogfood copy) says the TUI shows an overview
  beside the file it points to; version bump.

## 9. Tests

- Ways in: switcher enter, `file_focus`, `overview add` and backspace each
  open the split for an agent overview at 120 columns and the viewer at 50.
- A note anchor steers to its note's line; a gone note and a missing file
  mark the anchor.
- `S` on an agent overview stacks the anchored files in first-anchored order;
  a review overview after the review view closed does the same.
- Review remarks: a remark on an existing line draws a box (single and
  stacked), a remark on a deleted line draws nothing, `d` on a remark is
  refused, `}` reaches it; a working review's changed file shows none.
- Highlight: alt+↓ into another band sets `here` and leaves `sel`; `n`/`p`
  (single and stacked) leave `sel`; `N`/`P` and ↑/↓ on the overview move it;
  a band equal to the selection shows no second mark.
- e2e: the `tui_stored_overview` goldens are refreshed for the new marks
  and hint text.

## 10. Out of scope

The web split (its own spec next); a resizable divider; anchor bands in the
diff view; making the review tree accept rows below 60 columns.
