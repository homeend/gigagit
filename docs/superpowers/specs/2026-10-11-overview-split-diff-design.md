# Overview split: the file or its review diff

Date: 2026-10-11 · Follows `2026-10-10-overview-split-all-design.md` (TUI,
merged `e7c75734`) and `2026-10-11-web-overview-split-design.md` (gg web plan
1, merged `852fbcfd`). One spec, two plans: plan 1 = gg web, plan 2 = the
TUI. Each plan merges on its own.

## 1. What the user gets

An overview's split shows the overview in the **left column** and, in the
**right column**, the file the selected link points to — today always the
file as it stands at the reviewed tip (for PR #18249, the review of
`main...refs/gg/pr/18249`, that is commit `774ace5`). A file read alone does
not say what the review changed in it: two links can open files that look
the same, and nothing on screen shows the change.

After this change:

1. **D (shift+d) flips the right column between the file and its diff.** The
   diff is the change the review covered for that file — for PR #18249 the
   file at `3a4d954` (the merge base) against `774ace5`, GitHub's "Files
   changed" for that file.
2. **The right column's title carries a switch** reading `file | diff`; the
   half in force is highlighted and a click on the other half flips.
3. **The choice holds while you step links**: with the diff showing, every
   link the overview steps to opens as a diff, until D flips back or the
   overview's split closes.
4. **The diff is drawn in one column**, the whole file with the change
   marked in it: deleted lines in red with `−`, added lines in green with
   `+`. The link's lines carry their band, the review's remarks sit under
   their lines, and the cursor, n / p, } / {, search and copying work as in
   the file.
5. **Two links in a row that open identical files say so**: the status line
   reads `same content as <the previous link's file>`.

## 2. User rulings (2026-10-11, do not re-ask)

- D1 One spec for gg web and the TUI, two plans, gg web first.
- D2 The key is D (shift+d), plus a clickable `file | diff` switch in the
  right column's title. Footer and help list it.
- D3 The choice sticks while stepping links, until D flips it back or the
  split closes. Closing the split forgets it: the next overview opens on
  the file.
- D4 An agent's overview (not a stored review) diffs HEAD against the file
  on disk. A file with no change says `no changes in the working tree — D
  shows the file`.
- D5 The link's lines are banded on the new side, the review's remarks show
  in the diff, and the diff opens in full-file mode so the linked line is
  always there; f still collapses it to the changed lines.
- D6 The `same content as <path>` status line is in scope.

## 3. Where each diff comes from

The right column's file always comes from one overview. Its diff is:

| Overview | Left side | Right side |
|---|---|---|
| a stored review overview of a range review (a PR, a merge preview, a branch) | the left end of the review's STORED range (`ReviewRevs`; hashes saved with the review, so the diff never drifts as the target branch moves — for PR #18249 the merge base `3a4d954`) | the range's tip — the version the file view shows |
| a stored review overview of a single-commit review | the commit's parent (none for a root commit: every line is added) | the commit |
| a stored review overview of a working-changes review | HEAD | the file on disk |
| an agent's overview (`gg session overview`, a tour) | HEAD | the file on disk |

The right side is always the version the file view shows, so the cursor,
the bands and the remarks keep their lines across a flip. A renamed file
diffs against its old path (the review's own file list says which). A file
the overview links to that the review did not change shows the notice
`no change to this file in the review — D shows the file` (D4's agent text
for an agent overview). A file the review deleted has no file view (the
placeholder, as today); its diff shows every line deleted.

**Domain.** One query serves both frontends:
`Service.ReviewFileDiff(ctx, reviewID, path) (ReviewFileDiff, error)` —
`ReviewRevs` for the sides, `ReviewFiles` for the file's status and old
path (not listed = unchanged, no diff read), the cached `Differ` for the
aligned rows, `ReviewNotesFor` for the remarks on BOTH sides. A review that
no longer exists is an error the frontends show as `the review is gone — D
shows the file`. The agent overview's diff is the existing HEAD → working
tree lane (`worktreeDiff` "head" on the web; the TUI's equivalent read
through domain), with the open file's agent notes.

## 4. The screen

**Title.** The right column's title names what it shows:

- file: `View <path> (@774ace5)` — as today;
- diff: `Diff <path> (3a4d954 → 774ace5)`; a working tree diff reads
  `(HEAD → working tree)`; a renamed file `Diff <old> → <path> (…)`.

The path is cut in the middle (never at the end) to leave room for the
switch. The switch `file | diff` sits at the title's right end: the half in
force drawn highlighted (web: the selected-chip style; TUI: reverse video),
the other plain. Clicking the other half flips; clicking the highlighted
half does nothing. The switch shows only when D can act (§6).

**Rows.** One row per line of the file's whole aligned diff, top to bottom:

- an unchanged line: the new line number, a blank sign column, the text;
- a deleted line: the OLD line number dimmed, `−`, the text on the diff
  view's deleted tint;
- an added line: the new line number, `+`, the text on the added tint;
- a changed line (the aligner paired an old and a new line): its deleted
  row, then its added row, with the changed words marked as the diff view
  marks them.

Syntax colours ride the text as in the file view. One number column, not
two: the half-width column cannot spare a second one, and the sign plus the
dimmed number already tell a deleted row apart.

**Full file first; f collapses.** The diff opens showing every line. f
toggles to the changed lines only (three unchanged lines kept on each side
of a change, the rest folded into a `⋯ N unchanged lines` row a click — or
enter on the TUI — opens). A line inside a link's band or carrying a remark
is never folded. f's state sticks exactly like D's (§6). It is the split's
own, never the diff view's saved preference.

**Bands, remarks and notes** are keyed on new-side line numbers, as in the
file:

- a band paints the rows of its new lines, plus any deleted row lying
  between two of them; the current band keeps its stronger tint;
- a remark sits under the row of its line: a new-side remark under its new
  line, and — unlike the file view, which has no deleted lines — an
  old-side remark under its deleted row. Read-only as everywhere (d / r say
  `review remarks are read-only`);
- an agent overview's file shows its agent notes the same way, and d / r
  act on them as in the file.

**Notices** take the column's body (the title and switch stay):

- `no change to this file in the review — D shows the file`;
- `no changes in the working tree — D shows the file` (agent overview);
- `binary file — D shows the file` (images too: their pair is the review
  view's, not this column's);
- `diff too large — D shows the file`;
- `the review is gone — D shows the file`.

## 5. Keys in the diff

The right column keeps the file's keys, with these meanings in a diff:

- the cursor moves over every row, deleted ones included (web ↑ / ↓ j / k;
  TUI alt+↑ / ↓, ↑ / ↓ scroll), pgup / pgdn / home / end as in the file;
- n / p step the bands (W5 / R2 hold: never the overview's selection), } / {
  the note boxes;
- / searches the rows' text; the hit keys are the file's;
- the TUI's v selects characters over the rows' text (the sign column is
  not copied);
- L (web) and the line marking (web shift+↑ / ↓, TUI space) act on the new
  lines the marked rows cover: the copied link is the file's link at the
  tip, the same link the file view copies. A cursor or a marking on deleted
  rows only refuses with `a deleted line has no link in the reviewed
  version`;
- the `.` menu is the file's, acting on the cursor's new line; on a
  deleted row its line rows are left out;
- f collapses / expands (§4); w (web) / ctrl+w (TUI) the text mode as in the
  file;
- N / P, Tab, Backspace, Esc, X, ctrl+] keep their split meanings.

The overview's mark (`.ahere` / `overview.mark`) follows the cursor's new
line; a deleted row marks the band of the row after it.

## 6. When D acts, and how long the choice holds

D acts on a file opened from an overview's link — the split's right column,
and the one-document viewer (the narrow fallback) when its file came from an
overview link (it has a way back to the overview). Anywhere else D keeps
whatever it does today (nothing, in the viewer and the file panes).

**The flip keeps your place.** File → diff: the cursor lands on the row of
the same new line, on the same screen row when it fits, else centred. Diff →
file: the cursor's new line; from a deleted row, the next new line (the
previous one at the end of the file).

**Sticky (D3).** The choice — and f's — belongs to the overview showing in
the split or the narrow viewer — it is held by the overview itself, not by
the file — so every link stepped to (↑ / ↓ or a click on the overview, N / P
from the right column, Enter; in the narrow viewer Backspace to the overview
and Enter on another link) opens as the choice says, landing on the link's
line, and a resize between split and narrow viewer keeps it. It ends when
the overview's split or viewer closes (Esc, or the overview going away); a
later opening starts on the file. A file opened
any other way always shows as the file.

**Live files.** A working tree file that changes on disk reloads as today;
in a diff the diff is read again and the cursor keeps its new line.

## 7. Same content as the previous link's file (D6)

When a step to a link opens a file whose content is byte-identical to the
file the previous step showed, and its path differs, the status line reads
`same content as <previous path>` (path cut in the middle). Only steps from
the overview count (split or narrow viewer), only the immediately previous
file, and only the reviewed version's content (in a diff too: the new
sides). The message clears as the status line always does.

## 8. Gg web (plan 1)

- **Server:** `/api/diff` gains a `review=<id>&path=<p>` lane next to `wt=`
  and `left/right`: it answers `ReviewFileDiff` in the /api/diff shape plus
  `changed` (false = no change), `status`, `old_path`, `left_label` /
  `right_label` (short hashes, or `HEAD` / `working tree`) and `remarks`
  (both sides, `side` = old / new, read-only). The id is checked against the
  review store; a path is checked like every other lane's. The agent
  overview uses the existing `wt=head` lane plus the file's notes the page
  already holds.
- **Model:** `view` stays the one file. A new `view.diff` holds the diff
  rows and their row map (`{rows, newRow, partial, open}`); null = the file
  shows. The sticky choice lives on the overview object the split and
  the narrow viewer share (`ov.diff`, `ov.partial` — the one `split.ov` and
  `view.from` point at), so the narrow viewer's Backspace → Enter,
  `narrowSplit` and `backIntoSplit` carry it; `closeViewer` / `leaveSplit`
  clear it. `openViewer({…, steer: true})` reads the diff after the file
  when the choice says diff.
- **Pure helpers** (guarded section, node-tested): `unifiedRows(d)` (the
  /api/diff rows → one-column rows: kind, old / new number, text, tokens,
  spans), `newLineRow(rows, line)`, `rowNewLine(rows, i)` (the deleted-row
  rule of §6), `diffBandRows(rows, bands)`, `diffSwitchState(view, split)`,
  `sameContent(prevLines, lines)`.
- **Why not `diffHTML`:** the files pane's table (files.js) already has a
  one-column layout, but the right column's cursor, search, note boxes,
  bands and overview mark all live on the viewer's `.vline` rows and
  `view.cur`; rendering the diff as `.vline` rows keeps every one of them,
  where the table would need each re-wired. The collapse rule
  (`collapseDiffRows`) and the tints are shared, not copied.
- **Render:** `renderViewer` gains a diff branch over the rows (the existing
  `.vline` markup plus `vadd` / `vdel` and the sign cell, the diff view's
  tints as CSS tokens), the note boxes after their rows, folds via the
  existing collapse rule with a keep for banded and noted rows.
- **Title:** `paintTitle` adds the switch (two buttons in `#viewer-title`'s
  row, `#viewer-mode`), hidden when D cannot act.
- **Keys:** D in `fileKey` (split right column and the narrow viewer), f in
  diff mode — consumed there, so the page's global f (the files pane's
  changed-only toggle behind the overlay) never also fires; in file mode f
  keeps today's behaviour. Every other key reads the cursor through the row
  map (`view.cur` stays a new line number in the file; in a diff the cursor
  is a row index, and `syncHere` / the bands read its new line).
- Footer (`viewerFoot`'s split branches) and the `?` help gain `D file /
  diff` and, in a diff, `f changed only`.

## 9. TUI (plan 2)

- **Model:** the open file (`openFile`) gains a diff preview: a second
  `contentPopup` built from the same `ReviewFileDiff` rows (text = the
  line's text, a per-row kind + old / new number for the gutter and tints),
  so the preview box's cursor, scroll, search and character selection work
  on it unchanged. A row map translates new line ↔ row for bands, note
  boxes, `overview.mark` and the flip. `docPane.render` and the narrow
  viewer draw the diff preview when the doc's diff is on.
- **Sticky:** on the split (`splitView`) and, in the narrow viewer, on the
  overview the file came from; `onClose` drops it.
- **Title:** the pane title gains `file | diff` at its right end (reverse
  video on the half in force); `splitMouse`'s left click on the title row
  over the switch flips.
- **Keys:** D and f in `docPane.update` (and the narrow viewer's key path —
  plan 2 first confirms D reaches nothing there today, so the claim of §6
  holds) before the pager; every string through `i18n.T` in all four bundles.
- The hint line, the footer and help list D (and f in a diff).

## 10. Testing

- Domain: `ReviewFileDiff` for a range review (merge base → tip), a
  single-commit review, a root commit, a working review, a renamed file, an
  unchanged file (`Changed` false, no diff read), a deleted file, a gone
  review; remarks on both sides.
- Web server: the `review=` lane's shape, an unknown id → 404, a bad path →
  400.
- Web JS: the pure helpers node-run (row building incl. changed pairs, the
  deleted-row rules, band rows, collapse keeping banded / noted rows,
  `sameContent`); static wiring guards for D, f, the switch, the sticky
  reset.
- Web visible checks (Playwright against a freshly built binary, fixture
  repo with a saved PR-style review): D flips to the diff and the title
  reads `Diff … (base → tip)`; a deleted row shows `−` and its tint; the
  linked line is banded; a remark shows under its row; stepping a link keeps
  the diff; Esc then reopening starts on the file; clicking `file` flips
  back; an unchanged file shows its notice; two identical files show `same
  content as …`; the narrow viewer flips too.
- TUI: model tests for the flip (cursor kept both ways), the sticky choice,
  bands / remarks / mark on rows, f folds keeping banded rows, the notices,
  the title switch click, `same content as`; i18n gates; an e2e golden
  screen of a split in diff mode.

## 11. Out of scope

- A side-by-side diff in the column (it would halve an already half-width
  column; the full diff stays one Enter away in the review view).
- D anywhere outside a file opened from an overview's link.
- The stacked file column (`S`, web plan 2 of the split spec) in diff mode —
  when the stack lands, D there is its own decision.
- Remembering the choice across splits or sessions.

## 12. Docs

CHANGELOG (each plan), README (the overview split paragraph),
`docs/CLAUDE-details.md` (the review overview split section: the diff mode,
`ReviewFileDiff`, the row map), and the gg-overview skill's sentence about
what the user sees, with its version bumped and `gg init --update` run.
