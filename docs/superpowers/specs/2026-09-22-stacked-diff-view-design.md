# Stacked diff view — design

Date: 2026-09-22 · Branch: `feat/stacked-diff-view` · Status: approved in brainstorm, awaiting spec review

## 1. What this is

Today every multi-file diff in gg is **one file at a time**: pick a file in the
tree (web) or files view (TUI), its diff opens; to read the next file you pick
it (or step with `n`/`p`). This spec adds a **stacked** presentation — GitHub's
"Files changed" tab: every changed file of the change set in ONE continuous
scroll, each file a **header** (status, path, counts, collapse control) with
its **diff below**.

```
┌ ▾ M  src/pkg/a.go                    +12 −3 ┐   ← header (sticky on web)
│   …a.go's diff (side-by-side / unified)…    │
├ ▾ R  old/b.go → src/b.go               +4 −4 ┤
│   …b.go's diff…                             │
├ ▸ A  vendor/huge.json                +9120   ┤   ← collapsed: header only
└ …                                           ┘
```

It is a **toggle** next to today's single-file mode, not a replacement.

## 2. Rulings (from brainstorm — do not re-open)

| # | Ruling |
|---|--------|
| R1 | Toggle (`S`), per-machine preference. Single-file mode stays exactly as it is. |
| R2 | Loading is **lazy per file**; a stack of **more than 100 files opens fully collapsed**. |
| R3 | Header = status + path (+ `old → new` on renames), `+adds −dels`, collapse control; plus collapse-all / expand-all. No "viewed" checkbox. |
| R4 | v1 = rendering + navigation. Review notes + line cursor, in-view search, working-tree hunk staging, line select/copy + `f` fold are **follow-ups**, but the model below is designed so each attaches per file without a second refactor. |
| R5 | TUI: the **full-screen** diff view becomes one continuous stream (no split with the files list). |
| R6 | Web symmetric compare: **both aligned lists stay** as navigators; the stack replaces the middle diff column. |
| R7 | Web and TUI preferences are **independent**. |
| R8 | Working tree: stack the cursor's **section** (all unstaged OR all staged), separately. Conflict rows are header-only with an "open resolver" action. |
| R9 | Web = client-side per-file slot stack + lazy fetch queue (no batch diff endpoint). TUI = one `v.lines` stream with header lines. |
| R10 | Delivery phases: web core → web symmetric → TUI → follow-ups. |

## 3. Where it applies

Every screen whose diff is backed by a file list:

| Frontend | Screens |
|----------|---------|
| Web | commit files (`openFile`), working tree staged / unstaged (`openStatusDiff`), branch / entry / link compare (`openCompare`, `openEntryCompare`, `openLinkCompare`), Previews + saved pairs + PRs (all route through `openCompare` / `openPreviewBody`), the symmetric compare (`symcompare.js`). |
| TUI | the full-screen `diffView` opened from the files view — every source the files view has (commit, compare, entry/bookmark/shelf compare, preview, working-tree sections). |

Not in scope: the web file-history overlay (one file across revisions — a
different axis), the hunk picker / conflict resolver, blame, file preview.

## 4. Toggle

- Key **`S`** in both frontends (free in the web key map and in the TUI diff
  view, where it is currently swallowed). Advertised in the footer chip, the web
  `?` help, the TUI context help, command palette and `.` menu (memory rule:
  advertise in help AND footer).
- Web stores `stacked_diff` in `/api/uistate` (localStorage cannot persist —
  random port). TUI stores it in its machine-local UI memory (`promptstate`),
  independent of the web (R7).
- `S` works from the file list and from the diff. Flipping **keeps position**:
  single → stacked scrolls the stack to the file that was open; stacked →
  single opens the file whose header is at the top of the viewport.
- When the pref is on, opening ANY file of a change set (click, enter, the
  file stepper, a link) opens the stack scrolled to that file; the change
  set's file-list stage itself is unchanged.

## 5. Data model

### 5.1 Web: the slot

A stack is an ordered array of **slots** built from the rows the file list
shows (`state.files`, or `symRows(c)` in the symmetric view — so filters and
flip already apply):

```
slot = {
  key,          // stable identity: path (+ section for the working tree)
  path, oldPath, status,
  src,          // how to fetch: {sha} | {left,right} | {entry specs} | {wt: staged|unstaged}
  counts,       // {add, del} | null until known
  load,         // idle | queued | loading | ok | binary | tooLarge | error | none
  collapsed,
  diff,         // the /api/diff (or entry-diff) response, once loaded
  // follow-up hooks — present from v1, unused until their phase:
  ctx,          // what state.diffCtx is today, for THIS file
  notes, row,   // this file's notes and cursor row
  hunks,        // this file's inline-staging state (today's diffHunks)
  fold,         // this file's `f` changes-only fold state
}
```

The single-file globals (`state.diffCtx`, `state.diffRow`, `state.notes`,
`state.lastDiff`, `diffHunks`) remain for single-file mode. Code that reads
them is routed through an **active-slot accessor** (`activeDiff()`), which in
single-file mode returns the globals and in stacked mode returns the slot under
the cursor / viewport top. v1 introduces the accessor and uses it for
navigation; each follow-up phase moves its feature onto it.

Stack state lives in one object (`state.stack = {gen, slots, byKey, queue,
observer}`), torn down by every path that tears down today's diff (screen
change, `drillOut`, repo switch).

### 5.2 TUI: header lines in one stream

`diffView` gains `stacked bool` and `files []stackFile`, where `stackFile`
carries the same fields as the web slot (path, oldPath, status, src, counts,
load state, collapsed, rows, tokens, plus the follow-up hooks).

`v.lines` interleaves, per file: one **header** `contentLine` (new kind
`lineHeader`, carrying the file index), then that file's body lines — or one
placeholder line while it is not loaded, or nothing when collapsed. Every line
carries its file index.

Line-index consumers skip header and placeholder lines: the line cursor
(`curLine`), `cursorRow()` (the notes anchor), the line selection (`lsel`),
search hits. `n`/`p` step header to header (replacing the change-block →
next-file seam in `diff_filenav.go` while stacked).

Header text goes through `i18n.T` with keys in all four bundles.

## 6. Loading

### 6.1 Web

- Every slot's header renders immediately. An unloaded, expanded slot's body
  is a placeholder with a height estimate (from counts when known) so the
  scrollbar stays stable.
- An `IntersectionObserver` (root = the diff pane, `rootMargin` ≈ one pane
  height) enqueues slots that are visible or near.
- The loader runs **at most 3 fetches in flight**, picking among the
  near-viewport slots the one nearest the current file (the set is re-read at
  every pick, so a slot scrolled away before its turn is simply never picked).
  The server serialises reads per repo (memory: web-runonce-task-gate), so an
  unbounded fan-out would stack and feel slower than switching files.
- Each fetch uses the endpoint that source already uses today (`/api/diff`
  with sha / left+right / wt, or the entry-diff endpoint). No new diff endpoint.
- `state.stack.gen` is bumped on toggle, screen change, filter/flip change and
  working-tree refresh; an answer for a stack no longer on screen is dropped.
- `binary`, `tooLarge`, `error` render a one-line notice as the body — the
  same wording the single-file view uses today.
- Notes (for the follow-up) ride alongside each slot's fetch, as today's
  `openFile` does, so they are in the first paint.

### 6.2 TUI

- Each file loads through the existing async diff command, tagged
  `(stack gen, file index)`; lexing stays async per file (memory: sync lex froze
  1.8 s at 1 MB).
- Files within ~2 screens of the viewport are requested, **at most 3
  outstanding**.
- On arrival the placeholder is replaced by the file's lines and the cursor /
  scroll offset are **remapped by (file index, line within file)** so the
  screen does not jump when a file above the viewport loads.

### 6.3 Auto-collapse

A stack of **more than 100 files** opens with every file collapsed (header
only). A collapsed file is never fetched. Collapse state is per stack, not
persisted.

Controls (both frontends):
- **`-`** collapse / expand the current file; web: also a click on the header's `▾/▸`.
- **`_`** collapse-all / expand-all (all collapsed → expand all; otherwise collapse all).
- Web: both also as buttons in the stack's header bar; TUI: `.` menu rows.

## 7. Header

`▾ M  src/pkg/file.go   +12 −3`

- Status letter coloured as in the file list; renames read `old/path → new/path`.
- **Counts**: a separate `GET /api/numstat` (`?sha=` | `?left=&right=` |
  `?wt=staged|unstaged`) answers one `--numstat -z` per change set, parsed by
  the existing `git.ParseNumstat`, wherever git has a tree pair: commit,
  hash compare, working-tree sections. It is asked ONLY while a stack is open,
  so the listing endpoints (and the status poll) pay nothing when the view is
  off.
  Entry / shelf / preview / link-set sources have no git pair; their counts are
  computed from the diff rows when the file loads, and the header shows no
  counts until then. Binary files show `bin`.
- Symmetric headers add the kind glyph (`≠ = ◁ ▷`) and each side's state; a row
  neither side has content for is header-only with today's
  "neither set has content — …" notice.
- Web headers are **sticky**: the header of the file being read pins to the top
  of the diff pane.

## 8. Navigation

**Web**
- Clicking a file in the tree (or in either symmetric list) scrolls to its
  header, expanding (and so loading) it if collapsed.
- The tree / list highlight follows the file whose header is at the pane top.
- `j` / `k` (and the toolbar's `‹ file` / `file ›`) move the file cursor and
  scroll the stack to that header. (`n`/`p` are NOT used on the web: `p` is
  pull there.)
- Side-by-side vs unified is decided once per stack from the pane width (the
  existing rule), so every file in the stack uses the same layout; a resize
  re-renders loaded slots from their stored `diff`.

**TUI**
- `n` / `p` jump header to header; `g` / `G` top / bottom of the whole stack.
- A file-jump list (built from the existing file-sequence data) picks a file
  and scrolls to its header.
- The file whose header is at or above the cursor is the **current file** that
  per-file actions (copy path, open in editor, …) read.

**Links and steering** (`gg open`, `gg session navigate` / `highlight`, `?<kind>=<id>`
hints): in stacked mode the target file is scrolled to, expanded, loaded, then
anchored. v1 lands on the file header; landing on the exact line arrives with
the notes + cursor follow-up.

## 9. Working tree

- Stacked mode stacks the section holding the cursor: all **unstaged** files,
  or all **staged** files. Moving the cursor into the other section rebuilds the
  stack for it.
- Conflict rows are header-only with an **open resolver** action (the hunk
  picker cannot live inside a stack).
- A live refresh (status re-read, `r`, op done) **reconciles slot by slot**:
  files gone from the section drop out, new files are inserted in list order, a
  loaded file whose status or content changed is re-fetched; scroll is kept by
  anchoring on the current header's file. If the section empties, the view
  follows today's `reconcileStatusView` / exit-to-list behaviour.

Commit / compare / preview stacks are immutable; a saved-preview tip move
rebuilds the stack (today's single-file behaviour, applied to the stack).

## 10. Web symmetric compare specifics

- The two aligned lists stay; the middle column is the stack of the **visible**
  rows in list order. Changing the filter (1–4) or flipping (`x`) rebuilds the
  stack (new gen), keeping the current file if it is still visible.
- Each slot's `src` is the pair `openSymRow` computes today (`left_spec` /
  `right_spec` honouring the flip).
- Both lists' highlight follows the pane-top header; the lists' existing
  mirrored scroll stays.
- Below `SYM_MIN_WIDTH` the symmetric view falls back to the classic list,
  which is itself stackable.

## 11. Follow-up phases (designed for, not in v1)

Each is its own plan, attaching to the per-file slot / stackFile:

1. **Review notes + line cursor + link line-landing** — `ctx`/`notes`/`row` per
   slot; `activeDiff()` everywhere notes read the globals; ◆ rows per file.
2. **In-view search** (`/ @ ] [`) across loaded slots; a hit in an unloaded or
   collapsed file loads/expands it on step.
3. **Working-tree hunk staging** per slot (`hunks`), inline pick/stage in the
   unstaged stack.
4. **Line select / copy + `f` changes-only fold** per slot.

## 12. Testing

- **Web unit (node-imported, pure):** slot build from list rows and sym rows;
  queue ordering + max-in-flight + gen drop + dequeue-on-scroll-away; the >100
  collapse rule; working-tree reconcile (drop / insert / refetch / anchor).
- **Go:** the numstat git verb on a real temp repo (plain, rename, binary);
  handler tests that listing responses carry `add`/`del` for git-backed
  sources and omit them for entry sources.
- **Web browser probe (Playwright):** toggle `S`; a header exists for every
  file; only near-viewport bodies loaded (network count); a tree click scrolls
  to and expands its file; >100-file fixture opens collapsed; flip back to
  single keeps the file. Assert **visibility**, run against the unfixed build
  first (memory rules).
- **TUI:** `diffView` tests for header lines, cursor/selection skipping
  headers, remap on a late load above the viewport, `n`/`p` header jumps,
  `-`/`_`, `S` flip keeping the file; i18n gate tests stay green; a
  `tui-capture` snapshot of a stacked screen.

## 13. Phasing and docs

| Plan | Content |
|------|---------|
| 1 — web core | toggle + uistate, slot model + `activeDiff()`, lazy queue, headers + numstat, collapse, navigation, all non-symmetric screens incl. working tree |
| 2 — web symmetric | §10 |
| 3 — TUI | §5.2, §6.2, TUI toggle/pref/keys/menus/help, i18n |
| 4a–4d — follow-ups | §11, one plan each |

Each phase updates `CHANGELOG.md`, `README.md` (user-facing), help + footer
advertising, `docs/CLAUDE-details.md`, and — where a doc'd key or agent-facing
surface changes — `docs/web-tui-parity.md`.
