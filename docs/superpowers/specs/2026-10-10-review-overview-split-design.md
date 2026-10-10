# Review overview split — the overview beside the files it points at

Date: 2026-10-10 · Branch: `feat/review-overview-split` · Frontend: TUI (web later)

## Intent (the user's words, condensed)

In a review, the stored overview today swaps between a full-screen overview
and a full-screen file: enter on an anchor replaces the overview with the
file, backspace replaces the file with the overview. The user wants both on
screen at once, **the overview fixed on the left, the file(s) on the right**,
the right side stacked or single. The overview steers what the right side
shows and where it is scrolled; focus moves between the two sides; every key
the right side has today keeps its meaning.

What is reused from the diff view is its **layout and movement mechanics**
(two viewports, one focused side, the keys that move within a viewport and
between them), not its content. The layout is extracted so other left/right
pairings can use it later.

## Rulings (asked and answered 2026-10-10)

1. **Right side content.** Single: one file's content at the reviewed tip,
   exactly what an anchor opens today. Stacked: all the review's files
   (branch, commit or diff review alike) as one stream.
2. **Independent viewports.** The two sides scroll independently, each with
   its own cursor; only the focused side moves.
3. **Tab switches sides.** Tab is no longer the anchor walk. "Unfocused" means
   only "not receiving keys, cursor hidden"; nothing else depends on it.
4. **Focus keys.** tab / shift+tab / alt+left / alt+right switch sides,
   backspace focuses left, esc closes the split from either side.
5. **n/p on the right** walk the anchor bands of the file, as the file viewer
   does today, and move the overview's selection with them.
6. **Width.** Equal halves, the diff view's split. Below 60 columns the split
   is refused and today's full-screen swap is used instead.
7. **Scope.** The review's stored overview only; temporary agent overviews
   (`gg session overview`) and the web keep today's behaviour.
8. **Left-side movement.** Up/down step anchor to anchor (the overview is a
   navigator), the right side follows live, enter focuses right. Page keys,
   home and end scroll the text.
9. **Approach A** (below): a generic split layer with a pane interface; the
   diff view is not refactored.

## Why not carve the diff view

The diff view's two columns are **one aligned row stream drawn in two
halves** with a cursor side (`diffView.onOld`): the same display row on both
sides is the same line pair, fold separators span both halves, one cursor
serves both. Those are not two viewports. The independent-pane precedents in
gg are the files view (tree left, preview right, one focused border) and the
full-screen file viewer, whose `renderPreviewBox` draws a document at any
size with its own cursor, selection, search, wrap modes, note boxes, bands
and keys. The split therefore lifts a **generic two-pane layer** out of those,
sharing the diff view's key vocabulary and look, not its code.

Rejected: refactoring the diff view onto the split (a large, risky change to
~1,450 lines and ~40 test files for no visible gain) and giving the file
viewer an optional left document (not reusable, tangles the viewer with
overview logic).

## 1. The split layer

`splitView` is a full-screen layer (`isFullScreenLayer` true) on the ordinary
layer stack, pushed and popped like the diff view: it preserves whatever it
was opened over and popups open over it.

```go
// pane is one side of a split: a surface with its own content, cursor and keys.
type pane interface {
    update(m Model, msg tea.KeyMsg) (Model, tea.Cmd)
    render(m Model, w, h int, focused bool) string // a bordered box w×h
    geom(m Model) (rows, innerW int)               // its content size in the split
    title() string
    hint(m Model) string                            // its keys for the hint line
}

type splitView struct {
    left, right pane
    onLeft      bool // the focused side
    onClose     func(Model) Model // what esc does after popping (nil = nothing)
}
```

### Geometry

Width `w` and height `h` from `overlayDims()`. The left box is `w/2` columns,
the right box the rest; both are `h-1` rows tall (the last row is the split's
hint line). A box has a border and a title row but no hint row of its own —
the split's one hint line is the only one — so each pane's `geom` is
`rows = h-1-3`, `innerW = boxW-4` (the preview box's math less its hint row). A split is refused below 60 columns (`i18n.T("terminal too narrow for
the split view")` in the status line; the caller falls back).

### Rendering

Two `renderPreviewBox`-style boxes joined horizontally (`lipgloss.JoinHorizontal`),
the focused one's border in the focused style, the other's plain — the
one-focused-border rule. The unfocused pane draws no cursor row (the
`cursorOff` argument the preview box already takes). Under the boxes one hint
line: the focused pane's `hint` followed by the split's own
`[tab] other side  [bksp] overview  [esc] close`, truncated to `w`.

### Keys (the split's own, before the pane's)

| key | effect |
|---|---|
| ctrl+c | quit |
| tab, shift+tab, alt+left, alt+right | switch the focused side |
| backspace | focus left (inert when already left) |
| esc | pop the split, run `onClose` |
| ctrl+t | inert (a full-screen surface is never maximized) |
| `.` | the focused pane's action menu (the pane handles it) |

Everything else goes to the focused pane's `update`. The split never
interprets a pane's keys and a pane never sees the split's.

### Mouse

A click inside a box focuses that side, then the click is handed to that
pane as a click at box-local coordinates (`overviewClick` for the overview
pane, the preview box's line click for a document pane). The wheel scrolls
the side under the pointer without moving focus.

## 2. Document panes

`docPane{*openFile}` is the pane for an open-file document. Its `render` is
`renderPreviewBox(d.p, title, w, h, focused, true)`, its `update` is the file
viewer's key order (selection, search, note keys, bands, pager/cursor), its
`geom` the box math above. It is what makes every viewer feature arrive in
the split unchanged.

Four places today ask "which document is the keyboard on" by type-asserting
`*fileViewer` on top of the stack. Each gains one case, a split whose focused
pane is a `docPane`:

- `activePreview()` — the focused document and its size (every preview key).
- `focusedDoc()` — the `.` menu rows, the background row, `bandKey`.
- `liveDoc(tag)` — where a load result lands (either pane of a split, focused
  or not, like a covered viewer).
- `topDoc(m)` — the anchor machinery's "the viewer on top".

One helper `m.splitFocus() (*openFile, rows, innerW, ok)` answers for all
four, so no other code learns about the split.

`overviewPane` is a `docPane` whose document is an overview (`d.ov != nil`)
plus the steering of §3; its `update` runs `overviewKey` first, less tab and
shift+tab (the split's) and enter (§3 redefines it: steer and focus right,
never a pushed viewer).

## 3. The overview pane (left)

Opened from the review's "≡ Overview" row with the document
`openReviewOverviewDoc` builds today (source kind `srcReviewOverview`, laid
out at the pane's reading width, plain anchors marked), the first resolved
anchor selected.

| key | effect |
|---|---|
| down / up | select the next / previous resolvable anchor (plain and clipped ones skipped, wrapping at the ends as `stepAnchor` does) and **steer** the right pane to it |
| enter | steer to the selected anchor (a no-op when already shown) and focus right |
| pgdown / pgup / home / end | scroll the text; the selection is unchanged |
| shift+←/→ | pan in the horizontal-scroll view (as today) |
| ctrl+w | cycle the view mode, re-laid out at the pane's width (as today) |
| `/` | in-view search of the overview text (as today) |
| `y` | copy the overview's markdown (as today) |
| `.` | the menu: Next anchor, Previous anchor, Open anchor, Copy overview |

The selected anchor's row is the cursor row (`selectAnchor` already scrolls it
into view and pans to it). `r` (copy anchor reference) stays absent: a stored
overview has no store reference (A9 of the PR review spec).

### Steering

`steerTo(i)` shows anchor `i`'s target on the right:

- **single mode** — the file at the reviewed tip (`srcCommit`, `rev = tip`) or
  the working tree for a working review, found among the open files or newly
  made, with `pendingLine/pendingEnd` the anchor's line/range, `from` the
  overview and `anchorCur` the anchor's destination (so the bands and n/p
  work as they do in the viewer). The right pane's document is REPLACED by
  it; the previous one stays open in the background (it was registered). The
  load runs off-thread as today; `liveDoc` finds the pane.
- **stacked mode** — the stream scrolls to the anchor's file and line
  (§4.2), loading and unfolding the file first when needed.

A plain anchor is never selected, so steering never fails; a file that is
gone at the tip (the load errors) shows the viewer's error placeholder in the
right pane and the status line names it, the anchor marked `missing` as
`openAnchor` marks it.

Steering never moves focus; only enter and the focus keys do.

## 4. The file pane (right)

### 4.1 Single mode

The right pane is a `docPane` over the steered document. It behaves exactly
like the full-screen viewer of that file: cursor, selection, search, note
boxes, `n`/`p` over the anchor bands (which move the overview's selection,
`stepAnchorBand` unchanged), `.` menu, ctrl+w, `h`/`b` history and blame
(pushed as layers over the split), `X` closes the document (the pane shows
the "no file" placeholder until the next steer), ctrl+] sends it to the
background (same). Backspace is the split's (focus left) and replaces the
viewer's `anchorBack`; the document's `from` still points at the overview,
which is what the bands need.

Documents opened here are the same registered open files an anchor opens
today: ctrl+\ lists them, they stay open after the split closes, and a file
opened from the overview earlier is found and reused.

### 4.2 Stacked mode

The right pane is a `stackPane`: one unregistered document (`srcReviewStack`,
rev = the review id) whose line stream is, per review file in the review
tree's order:

```
                      ← blank (not for the first file)
━━ path  (status)  ◆n ← header row, kind stackHeader, a cursor stop
──────────────────── ← rule, full width, not a cursor stop
<the file's content lines, or one placeholder line>
```

Placeholders: `(loading…)`, `(folded)`, `(binary file)`, `(file too large)`,
`(not at this commit)` (a deleted path), `error: …`. The content lines are
`fileContentLinesTok` of the file at the tip (working tree for a working
review), syntax-coloured per `[ui] diff_syntax`.

The stream is rebuilt from a per-file table `stackFile{path, status, notes,
start, hdr, body, lines []contentLine, folded, loaded, err}`; `start` is the
first stream line of the file's block, `body` the first content line. Every
anchor is file-scoped: `(file, line) ↔ stream index` goes through this table
(`streamLine(i, line)`, `fileAt(stream)`), never through a line number alone
— the stacked diff's lesson.

Loading is lazy: a file loads when the steer, the cursor, a band walk or an
unfold reaches it, at most 3 in flight, results by `tag` + file index
(`stackFileMsg`, never the single document's `fileContentMsg`). Over 100
files every file opens folded except the target; otherwise unfolded. Loading
a file above the cursor keeps the cursor's (file, line), the stacked diff's
`applyStackFile` rule: the viewport is pinned to the cursor's screen row.

Keys, in addition to the viewer's:

| key | effect |
|---|---|
| `-` | fold / unfold the cursor's file |
| `_` | fold all / unfold all |
| `N` / `P` | next / previous file header |
| `n` / `p` | next / previous anchor band across the WHOLE stream in order, loading and unfolding the band's file first (the step is parked until the load lands, any other key abandons it — the stacked diff's hunt rule); the overview's selection follows |
| `S` | back to single mode on the cursor's file |

Bands, note boxes (the review's notes on each file, read-only as the review
view shows them), the cursor and the selection are all per file through the
table. The title is `Files: <review label>  file k/N`.

### 4.3 `S`

`S` on either pane toggles single ↔ stacked and keeps the cursor's file and
line: single → stacked lands the stream on that file and line; stacked →
single opens that file as the single document at that line. The choice is
remembered in promptstate as `StackedReviewFiles` / `SetStackedReviewFiles`
(its own key beside `StackedDiff`; the TUI test harness pins
`XDG_STATE_HOME`, so the suite never reads the machine's choice).

## 5. Entry and exit

- Enter (or double click) on the review tree's "≡ Overview" row opens the
  split: `openReviewOverviewSplit` builds the overview document as today,
  selects the first resolved anchor, pushes the split with the overview pane
  left and a file pane right in the remembered mode, and steers to the
  selected anchor. Focus starts on the LEFT. Below 60 columns it calls
  `openReviewOverviewDoc` (today's full-screen viewer) instead.
- Esc pops the split; `onClose` lands the review tree's cursor on the
  Overview row (the `filesLandNote` precedent). The overview document and the
  single-mode documents stay in the open files list, backgrounded, exactly
  as today after backspace; the stacked stream document is dropped.
- Bringing the review overview to the front later (ctrl+\ switcher, `gg
  session files`) opens it in today's full-screen viewer, not the split —
  the split is the review tree's door only (v1).
- A review deleted while the split is open (the notes store changes) closes
  the split, as the review view closes today.
- Temporary overviews (`srcOverview`), the web and `gg session` steering
  are untouched.

## 6. Hint and status

Hint line while focused left: `[↑↓] anchor  [enter] open  [tab] files
[/] find  [y] copy  [.] menu  [esc] close`; while focused right the viewer's
hint (`[n/p] anchor …`) plus `[S] stacked|single  [tab] overview  [bksp]
overview  [esc] close`. The status line names each steer: `anchor k/N · <label>`
(the band walk's wording), and `the file changed since the review` /
`not at this commit` where today's paths say so.

Every new string is an `i18n.T` literal in all four bundles.

## 7. Tests

- `split_view_test.go`: geometry at several widths (halves, the 60-column
  refusal), focus routing (tab/shift+tab/alt arrows/backspace), esc pops and
  runs `onClose`, keys reach only the focused pane, the four "focused
  document" seams answer for a split, one-focused-border render.
- `overview_split_test.go`: up/down step over plain anchors and wrap, each
  step steers (single: the right document is the anchor's file at the tip
  with `pendingLine` and `from` set; stacked: the stream lands on the file
  and line), enter focuses right, page keys do not change the selection,
  esc lands the review tree on the Overview row.
- `stack_pane_test.go`: the pure stream build and `(file, line) ↔ stream`
  mapping, lazy load by tag + index, folding, the pinned cursor on a load
  above it, n/p across files with a parked step, `S` round trips keeping
  file and line, >100 files fold by default.
- `review_overview_test.go`: the existing tests move to the split (the row
  opens a split, not a viewer); the narrow-terminal test keeps the viewer.
- e2e `tui_stored_overview.toml` is rewritten for the split (both panes on
  one screen, down steers, enter focuses, bspace returns, S stacks); a second
  scenario at 50 columns keeps today's screens.

## 8. Docs

CHANGELOG (always); README's review section (the overview is shown beside
the files); `internal/agentskill/gg-overview.md` where it says tab walks the
anchors and enter opens the file (in a review split: up/down walk, tab
switches sides; a temporary overview keeps tab); CLAUDE.md package map is
unchanged (the layer lives in `tui`); `docs/CLAUDE-details.md` gains the
split layer's contract and the stacked stream's file-scoped rule.

## Not built (follow-ups, ask first)

The split for temporary agent overviews (right side = working-tree files);
the web; a resizable divider; the overview's selection following the right
cursor outside n/p; bands in the diff view; opening the split from the
open-files switcher.
