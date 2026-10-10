# Overview split in gg web

Date: 2026-10-11 · The web half of `2026-10-10-review-overview-split-design.md`
and `2026-10-10-overview-split-all-design.md` (TUI, merged `e7c75734`). One
spec, two plans: plan 1 = the split with a single file, the review's remarks
and the colour mark; plan 2 = the stacked file column (`S`). Each plan merges
on its own.

## 1. What the user gets

Today an overview — a review's stored one (the **≡ Overview** row of a
review's file list) or an agent's (`gg session overview`, the open-files
switcher ctrl+\\, `gg session files focus`) — opens in **the viewer overlay**:
the dark box that covers most of the page and shows one document at a time.
Enter on a link replaces the overview with the file; backspace (or the
browser's Back) brings the overview back. The review's remarks never show in
a file opened that way.

After this change:

1. **The viewer box splits into two columns** when it is wide enough: the
   overview in the **left column**, the file its selected link points to in
   the **right column**. Every way an overview opens today lands in it.
2. **The right column follows the selection**: stepping the overview's links
   shows each link's file on the right at once, at the link's line, with the
   overview's links drawn as bands.
3. **The review's remarks show beside the code**, as read-only boxes under
   their lines, whenever a file was opened from a review's overview.
4. **The left column only moves when you act on it.** Moving in the right
   column never moves the overview's selection; the link whose band holds the
   right column's cursor is marked on the overview in that band's colour.
5. (plan 2) **`S` stacks the files** into one scroll in the right column.

## 2. User rulings (2026-10-11, do not re-ask)

- W1 The split lives in the viewer overlay's box (not in the page's files /
  diff panes), so every existing way in — the ≡ Overview row, the switcher,
  `gg session files focus`, `gg session overview add`, backspace / Back in a
  file an overview opened — gets it.
- W2 Keys copy the TUI: Tab / shift+Tab switch columns; ↑ / ↓ (j / k) on the
  overview step its links and the right column follows; Enter moves into the
  right column; Backspace in the right column returns to the overview; N / P
  in the right column (single file) step the overview's links; a click on a
  link selects it and shows its file on the right without leaving the
  overview.
- W3 Esc closes the whole split from either column. An agent's overview steps
  into the background (it stays in the switcher, as today); a stored overview
  closes (it was never registered); the right column's file goes to the
  background.
- W4 One spec, two plans: plan 1 the single-file split, remarks, the colour
  mark; plan 2 the stacked file column.
- W5 (R2 on the web) n / p in the right column move the cursor and the
  current band only — never the overview's selection — in the split AND in
  the one-document viewer (the narrow fallback), so the rule does not depend
  on the window's width. The overview's selection moves only by actions on
  the overview (↑ / ↓, a click on a link, Enter) and by N / P in the right column.
- W6 Review remarks show as read-only boxes (no dismiss, no copy reference);
  a remark on a line the commit deleted is left out entirely (the TUI's R1).
- W7 The box falls back to today's one-document viewer below 900 CSS pixels
  of box width (≈ 60 characters a column at the viewer's 12px monospace).

## 3. Layout

`#viewer-box` gains a left column (`#viewer-ov`: its own title line and body)
before today's title and body, which become the right column. Split mode is a
class on the box (`.split`): the two columns take equal halves with a 1px
divider; without it the left column is hidden and the box is exactly today's.

- **Left column title:** `Overview: <title>` (stored) / `Overview <id> ·
  <title>` (agent's) — today's document-mode title.
- **Right column title:** today's file title (`View <path> (<version>)`), the
  path cut in the middle to the column's width.
- **Focus:** the focused column's title line wears the accent underline; the
  other's is dim. The page's keyboard focus sits in the focused column's body.
- **Bottom bar** (the footer chips): the focused column's keys (§9).

The breakpoint (W7) is measured on the box (`#viewer-box.clientWidth`) when
an overview opens and on every window `resize`: an open split whose box
narrows below it collapses to the one-document viewer showing the overview
(the right column's file goes to the background — the TUI's `narrowSplits`);
widening never re-splits on its own.

## 4. Model

`view` stays the ONE file the viewer shows — in the split, the right
column's — so every file behaviour (cursor, search, line marking, `L`, the .
menu, notes, reloads, `reportCursor`) works unchanged there. A new
page-local **`split`** holds the left column:

```js
// split is the overview beside the file (null = no split): its document
// (the shape view.ov has today: {title, text, blocks, anchors, sel, want,
// stamp, stored?}), its open-files id ("" for a stored overview), which
// column has the keys, and the anchor marked from the right (-1 = none).
let split = null; // {id, ov, left: true, here: -1}
```

The file on the right carries `view.from` exactly as an anchor's open sets it
today (`{id, sel, dest, anchors, cur, stamp, stored}`), built from `split`,
so `viewBands`, `bandOf`, `stepViewerAnchor` and the remarks (§6) read the
same field in the split and in the one-document viewer.

**Open-files list.** A tab shows one file (`tabShows`): in the split it is
the right column's file — the one esc lets go of. An agent's overview in the
split is listed (pinned, as every overview is) and backgrounded, so the
switcher still lists it; a stored overview is never registered (today's
rule). No server change.

## 5. Opening the split

A door, **`showOverviewDoc(f, ov)`**, replaces the two places that put an
overview on screen today (`showOverview` for an agent's, `showStoredOverview`
for a stored one): wide box → `openSplit`, narrow → today's document mode.

`openSplit(doc)`:

- sets `split` (left focused) and the box's `.split` class, paints the left
  column;
- selects the overview's first link that can open when none is selected
  (stepAnchor from -1, as ↓ would — the TUI's rule);
- steers the right column to the selected link (`steerSplit`);
- a split already on screen for the same overview is reused (its selection
  and scroll kept), never stacked twice.

| Way in | Today | After |
|---|---|---|
| ≡ Overview row (`openReviewOverview`) | document mode | `showOverviewDoc` |
| switcher enter on an overview / `gg session files focus` / `overview add` (`openViewer({id})`) | document mode | `showOverviewDoc` |
| backspace / Back in a file an overview opened (one-document viewer) | document mode, the anchor selected | `showOverviewDoc`; the file it left is the right column |

`steerSplit()` opens the selected link's target in the right column through
the same path an anchor's open uses today (`storedAnchorOpen` for a stored
overview, `anchorTarget` for an agent's — a note link lands on the note's
line, no band current), with `view.from` built from `split`. It does not
push a history entry (§8) and leaves the focus where it was. A link that
cannot open (plain, missing, a gone note) says why on the status line
(`anchorStatus`) and leaves the right column as it was; a missing one is
painted struck through, as today. Steps are sequenced by `loadSeq`: a quick
run of ↓ shows only the last link's file.

## 6. Review remarks (W6)

**Server.** `GET /api/review/{id}/file-notes?path=<p>` answers
`{notes: [...]}`: the server reads `path` at the review's tip (a working
review: the working tree) and calls `domain.ReviewFileNotes(ctx, id, path,
lines)`. Each note is the viewer's note wire (`id, start, end, summary,
rationale, author`) plus `readonly: true`. A gone review is 404; a path the
review never saw answers no notes.

**Client.** A file whose `view.from.stored` names a review fetches its
remarks after its lines land (and after a reload of the file), tagged by
`loadSeq` so a late answer for another file drops. They live in
`view.remarks`; every note reader (`boxesAfter`, `noteAtLine`,
`nextNoteLine`, the gutter mark, `}` / `{`) reads `shownNotes()` = the
agent's notes then the remarks, by start line; the agent-notes code paths
that talk to the store (`dismissNote`, reloads' `notesOf`) keep `view.notes`.

**Boxes.** A remark's box is `noteBoxHTML` without the action buttons, titled
`review · <lines>` (no internal id). `d` / `r` on a remark (keys and the
footer chips) say `review remarks are read-only`.

Remarks show wherever the file carries a stored overview's `from`: the split's
right column and the one-document viewer an anchor opened.

## 7. The right column marks, never moves, the left (W5)

`split.here` is the anchor whose band covers the right column's cursor line
(the current band wins an overlap, then the first), or -1; equal to the
selection → -1. It is recomputed after every key and mouse action in the
right column (`syncHere`) and painted on the left as class **`ahere`**: the
link wears the bands' amber (`#d4a72c`, the `.vline.vanchor` colour, at the
current band's 24%) as its background, plus underline and bold — the TUI's
`mdAnchorHere`. The selected link keeps today's `asel`. Nothing scrolls on the
left.

`stepViewerAnchor` (n / p) changes for W5: it moves the cursor and
`from.cur` (the current band) only — `from.sel` and `from.dest` are no
longer written. In the one-document viewer, backspace therefore returns to
the link the file was opened from, not the last band stepped to.
(`backAnchor` already resolves by `dest`.)

## 8. Keys and mouse

**Left column (overview focused):**

| Key | Action |
|---|---|
| ↓ j / ↑ k | next / previous link that can open; the right column follows |
| Enter | steer (if not shown yet) and move into the right column |
| Tab / shift+Tab | into the right column |
| PageDown / Space, PageUp, Home g, End G | scroll the overview |
| r | copy the selected link's reference (agent's overview; a stored one says why not, as today) |
| y | copy the overview's text |
| Esc, ctrl+] | close the split (W3) |
| ctrl+\\ | the open-files switcher |

**Right column (file focused):** today's file keys, plus:

| Key | Action |
|---|---|
| Tab / shift+Tab, Backspace | back to the overview |
| N / P | the overview's next / previous link; the right column follows, the focus stays right |
| n / p | the next / previous band in this file — the cursor and the current band only (W5) |
| Esc | clear a marked range first (today's rule), else close the split (W3) |
| ctrl+] | close the split |

**Mouse:** a click in a column focuses it; a click on a link selects it and
steers (the focus goes left); a click on a line moves the cursor there (today)
and updates the mark.

**Browser history:** the split adds no history entry, and the browser's Back
does nothing special inside it (as on any other overlay of the page). A
one-document viewer that already armed one (§5's backspace way in) uses it up
when it turns into the split, so Back never re-opens the overview the split
already shows.

## 9. Bottom bar and help

- Left focused: `↑↓ links` `enter open` `tab file` `r reference` (agent's) `y
  copy text` `esc close` `ctrl+\ open files`.
- Right focused: `tab overview` `N P overview links` `n p bands` (when the
  file has bands) `↑↓ j k line` `/ find` `] [ next / prev` `} { notes` (when
  it has any) `d dismiss` `r reference` (when it has an agent's notes) `w long
  lines` `. menu` `esc close` `ctrl+\ open files`.
- The `view file` help entry gains a sentence: an overview opens beside its
  file when the window is wide enough — ↑↓ step its links, tab switches
  sides, N/P step the overview from the file, the link whose band holds the
  cursor is marked in that band's colour, a review's remarks show as
  read-only boxes; `S` stacks the files (plan 2).

## 10. Live updates

- An agent's overview on the left changes (`overviewStale`) →
  `refreshOverview` re-reads it into `split.ov`, keeping the selection by
  its destination (`keepAnchor`); the right column's bands follow (its
  `view.from.anchors` is rebuilt from the new list).
- It is closed (the switcher's x, `overview close`) → the split collapses to
  the one-document viewer on the right column's file, its bands gone (the
  `from.closed` path).
- The right column's file changes on disk → today's reload (`viewerFileChanged`),
  remarks re-fetched.
- Another way of opening a plain file in this tab (switcher enter on a file,
  `gg session navigate`, a pasted content link) → the split closes the way
  esc closes it and the file opens in the one-document viewer.

## 11. Plan 2 — the stacked file column (`S`)

`S` (either column) toggles the right column between the single file and
**the stack**: every file in one scroll, each under a header line (status
letter when known, the path cut in the middle). The choice is remembered in
the web layout state as `stacked_review_files` (web-only, like
`stacked_diff`; the TUI keeps its own).

- **Which files:** the review's files, in the review's order, when the page's
  review view is this overview's review (`state.review.id`); otherwise the
  files the overview's links name — a file link's path, a note link's note
  file — each once, in the order first linked. A file deleted at the
  reviewed commit shows `(deleted at the reviewed commit)` under its header.
- **Loading:** lazily as headers come near the view (an
  `IntersectionObserver`, at most 4 files in flight — the stacked diff's
  rule), a placeholder sized by line count when known.
- **Each file:** its lines (syntax tokens as the single file), the
  overview's bands for that file, its remarks (stored overview, §6's
  endpoint) or its agent notes (agent's overview, working tree) as boxes —
  all read-only in the stack (`d` says `notes are read-only in the stack — S
  opens the file`).
- **Steering:** a step on the overview scrolls the stack to that link's file
  and line (loading it first) and makes its band current; the right column's
  cursor moves there.
- **Keys:** ↑↓ j k line across files, PageUp / PageDown, n / p bands across
  the stack (mark only, W5), N / P the next / previous file header, `-` folds
  the file under the cursor, `_` folds / unfolds all, `S` back to the single
  file on the cursor's file and line. `/` search, `L` and the . menu act on
  the single file only (`S` opens it) — the stack says so on the status line.
- **The mark:** §7's `here` reads the stack's bands at the cursor.

## 12. Text

Web strings are English (no i18n bundles on the web). New status-line words:
`review remarks are read-only`, `notes are read-only in the stack — S opens
the file`, `this acts on a single file — S opens it`.

Skill: `gg-overview` gains one sentence — gg web also shows an overview
beside the file its link points to — with a version bump and the dogfood
copy regenerated by the embedded renderer (never `gg init --update` on the
branch).

Docs: CHANGELOG (each plan), README (the web overview paragraph),
`docs/CLAUDE-details.md` (the "Review overview split" section gains a web
paragraph).

## 13. Tests

**Pure JS (node-run, guarded sections):** `hereOf(bands, curBand, line,
sel)` (overlaps, equal-to-selection → -1, no bands); the breakpoint decision;
the stack's file list (review order vs first-linked order, note links,
duplicates); stack line ↔ file/line mapping; the remark box title.

**Go:** `/api/review/{id}/file-notes` — a remark on a kept line comes back,
one on a deleted line does not, a working review's changed file answers
none, a gone review is 404, a bad path is refused.

**Browser (rebuilt binary, assertions on what is VISIBLE):**
- a stored overview opens as two visible columns at 1400px and as today's
  single document at 800px;
- ↓ on the overview shows the next link's file in the right column; the
  left column's selection does not move on n / p, cursor moves or clicks in
  the right column; the `ahere` mark is visible on the expected
  link;
- a remark box is visible under its line, has no buttons, and `d` on it says
  read-only; a remark on a deleted line is absent;
- an agent's overview (`gg session overview add`) opens as the split; `gg
  session files focus` does too; esc leaves it in the switcher;
- resizing below the breakpoint collapses to the one-document viewer;
- (plan 2) `S` shows every file's header; N / P move between headers; a step
  on the overview scrolls the stack to the link's line.

## 14. Out of scope

A resizable divider; anchor bands in the diff view; search, `L` and the .
menu inside the stack; the TUI's one-document viewer adopting W5 (today the
TUI's full-screen viewer still moves the overview's selection on n / p — a
TUI follow-up can align it); the deferred TUI minors listed in
`review-overview-split-feature.md`.
