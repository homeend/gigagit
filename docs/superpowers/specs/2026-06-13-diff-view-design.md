# Full-screen side-by-side diff viewer — design

**Date:** 2026-06-13 · **Branch:** `feat/diff-view`

## Goal

A full-screen, dual-pane file diff viewer for the TUI: left pane = old version,
right pane = new version, aligned line-by-line with the differences
highlighted. Opened from two places:

1. **Status panel** — `enter` on a file row shows that file's working-tree
   change (HEAD → disk).
2. **Commit files tree** — `enter` while the tree side is focused
   (`filesTreeFocused`) and the cursor is on a file row shows that file's
   change in the viewed commit (parent → commit).

Both entries feed the same pipeline:

```
entry point ──builds──▶ diffInput (two versions) ──▶ textdiff.Compare ──▶ diffView (full screen)
```

## Decisions (brainstormed)

- **Full file both sides** — the panes show the entire old and new versions,
  aligned, not hunks-with-context.
- **Line-level highlighting only** — removed = red background (left pane),
  added = green (right pane), gap filler dimmed. Intraline word emphasis is a
  later feature.
- **Long lines truncate with `…`** — matching every other gg surface. No
  horizontal scroll, no wrap (wrap would break pane alignment).
- **Compare in Go, fetch with git** — entry points fetch the two versions
  (one tiny git verb + disk reads); a new pure package `internal/textdiff`
  computes the line alignment. This replaces the earlier "parse
  `git diff -U999999`" idea: the panes show the versions verbatim by
  construction, there is no unified-diff parsing (headers, `\ No newline`
  markers), and the engine can compare *any* two texts — the M3 conflict
  editor will want exactly that. A display-purpose text diff is presentation,
  not git semantics, so it does not violate "don't reimplement git".

## Components

### 1. `internal/git`: one new verb

```go
// ShowFile returns the content of path at rev (`git show <rev>:<path>`).
func (r *Repo) ShowFile(ctx context.Context, rev, path string) ([]byte, error)
```

One invocation, built with `gitcmd`. Callers only invoke it for sides they
expect to exist (the entry context says which), so a missing-path error is a
real error, not a control-flow signal.

### 2. `internal/textdiff`: the pure comparison engine

New package. No git, no TUI, no bubbletea imports.

```go
type Kind int
const (
    Same    Kind = iota // line present on both sides
    Changed             // paired del/add line (left and right both present, differ)
    Del                 // left only; right is a gap
    Add                 // right only; left is a gap
)

type Row struct {
    Kind          Kind
    Left, Right   string // line text ("" for the gap side of Del/Add)
    LeftNo, RightNo int  // 1-based line numbers; 0 = gap (no line on that side)
}

type Result struct {
    Rows   []Row
    Blocks []int // row index of the first row of each contiguous changed block
                 // (Changed/Del/Add runs) — the ctrl+↑/↓ jump targets
    Truncated bool // true when the size guard replaced alignment (see below)
}

func Compare(old, new []byte) Result
func IsBinary(b []byte) bool // NUL byte within the first 8000 bytes (git's heuristic)
```

Algorithm: split both sides into lines, trim the common prefix and suffix,
run Myers O(ND) on the middle, then align: equal lines → `Same`; a run of
deletions followed by a run of insertions is zipped pairwise into `Changed`
rows, with the longer run's tail emitted as `Del`/`Add` rows (gap on the
other side). Line numbers are assigned during emission.

**Size guard:** if either side exceeds 50 000 lines, skip Myers and emit the
trimmed middle as one replace block (all left lines as `Del`, all right lines
as `Add`), with `Truncated: true` so the UI can note "alignment skipped
(large file)" in the title. Prefix/suffix trimming still applies, so the
common case — a small change in a huge file — still renders perfectly.

Empty-side semantics: `Compare(nil, content)` yields all-`Add` rows (new
file); `Compare(content, nil)` all-`Del` (deleted file). An empty final line
caused by a trailing newline is not rendered as a phantom row.

### 3. `internal/tui`: input, loading, view

```go
// diffInput is everything the viewer needs; entry points build it async.
type diffInput struct {
    title    string // e.g. "internal/tui/model.go" — shown in the header
    context  string // "HEAD → working tree" or "@ <short-hash> <subject>"
    old, new []byte
}

// diffMsg carries the loaded input; tag gates stale results (same pattern
// as commitFilesMsg).
type diffMsg struct {
    tag string // the request key set when loading started
    in  diffInput
    err error
}

// diffView is the open viewer's state (pointer field on Model).
type diffView struct {
    title, context string
    rows   []textdiff.Row
    blocks []int
    offset int  // top visible row (pure scroll; no cursor row)
    truncated bool
    binary    bool
    loading   bool
    err       error // load failure, rendered in place of content
}
```

`Model` gains `diffView *diffView` and `diffTag string`. Opening sets a
loading `diffView` plus the tag and returns a `tea.Cmd` that fetches the
versions off the UI thread, then `Compare`s (also off-thread — it's pure)
and delivers `diffMsg`. A result whose tag does not match, or arriving when
`diffView == nil`, is dropped.

#### Entry point: Status panel

Predicate `canShowFileDiff()` in `avail.go`: `opsIdle()`, focus on
`panelStatus`, a row is selected (`backingIndex` ok), and the terminal is
≥ 60 columns. The `enter` arm in `Update` is gated by the same predicate
(never-looser contract).

The selected `model.FileStatus` decides the sides:

- **old side:** empty when the file is not in HEAD — `Kind == KindUntracked`
  or `Staged == 'A'` (a staged-new file; `ShowFile("HEAD", …)` would fail).
  Otherwise `ShowFile("HEAD", OrigPath-if-set-else-Path)` (renames fetch the
  old name).
- **new side:** `os.ReadFile(Path)`; a not-exists error means the file is
  deleted from the working tree → empty side (this also absorbs the
  delete/re-create porcelain combinations and races). Any other read error
  renders in the view.

The new side reads the disk (not the index): the viewer answers "what changed
relative to HEAD", combining staged + unstaged. Context label:
`HEAD → working tree`. Tag: `status:<path>`.

#### Entry point: commit files tree

`contentLine` gains three fields — `path`, `oldPath` (strings) and `status`
(byte, the `A M D R C T` letter) — zero-valued for headings, help-window
lines, etc.; the help window shares the type and ignores them.
`commitFileLines` fills them from each `model.CommitFile` (`path` = new
path), so the loader knows from the selected line which sides exist.

In `updateFilesViewKey`, `enter` with `filesTreeFocused` and the selected
*visible* line carrying a non-empty `path` opens the diff for commit
`m.filesHash`:

| CommitFile.Status | old side | new side |
|---|---|---|
| `A` | empty | `ShowFile(hash, path)` |
| `D` | `ShowFile(hash+"^", path)` | empty |
| `R` / `C` | `ShowFile(hash+"^", oldPath)` | `ShowFile(hash, path)` |
| `M` / `T` | `ShowFile(hash+"^", path)` | `ShowFile(hash, path)` |

The root commit needs no special case: every file in it has status `A`
(empty old side), so `hash^` is never dereferenced. Merge commits never
reach here — `CommitFiles` (diff-tree) lists no files for them. `enter` on a
heading row, on the commits side, or in `/`-typing mode is swallowed as
today. Context label: `@ <short-hash> <subject>`. Tag: `commit:<hash>:<path>`.
The files view stays in `Model` untouched underneath; closing the diff
returns to the tree exactly as it was.

#### Rendering (full screen)

`render()` short-circuits to `renderDiffView()` when `m.diffView != nil`,
after the modal check (decision modals stay supreme) and before everything
else. The view owns the whole screen; the registry footer is not drawn.

```
 diff: internal/tui/model.go   HEAD → working tree              rows 41–64/312
 ──────────────────────────────────────┬──────────────────────────────────────
  41  func (m Model) update() {        │  41  func (m Model) update() {
  42  old line removed                 │      ································
  43  shared context line              │  42  shared context line
      ································ │  43  new line added
  44  changed old text                 │  44  changed new text
 ──────────────────────────────────────┴──────────────────────────────────────
 [↑↓] scroll  [pgup/pgdn] page  [ctrl+↑↓] prev/next change  [esc] close  [q] quit
```

- Line 1: title + context, right-aligned visible-row range `rows a–b/N`.
- Body: `height - 3` rows. Each row: left pane, `│` separator, right pane;
  panes are `(width-1)/2` wide, each with a dim right-aligned line-number
  gutter (width of the largest line number, min 3) and the line text
  truncated with `…`.
- Styles (new lipgloss styles alongside the existing ones): `Del` rows — red
  background on the left cell; `Add` — green on the right cell; `Changed` —
  red left cell *and* green right cell; gap cells — dimmed `·` fill; `Same` —
  plain. The selected-row/panel styles are not reused; the view has no cursor.
- Last line: the key hint (constant). When `truncated`, the title gains
  ` (alignment skipped: large file)`; when `binary`, the body is the single
  line `(binary file)`; when `err != nil`, the body shows the error; while
  `loading`, `(loading…)`.

#### Keys (`updateDiffViewKey`, routed before the files-view routing)

| key | action |
|---|---|
| `up` / `k` | scroll −1 |
| `down` / `j` | scroll +1 |
| `pgup` / `pgdown` | scroll ∓ page (body height) |
| `ctrl+up` / `ctrl+down` | jump to previous / next change block (offset = block start, clamped) |
| mouse wheel | scroll ±3 (the existing `contentWheelStep`) |
| `esc` | close (back to whatever was beneath — main interface or files view) |
| `q` | quit the app (top-level convention) |
| `ctrl+c` | quit |
| anything else | swallowed |

Offset clamps to `[0, max(0, len(rows)-bodyRows)]`.

#### Resize guard

A `WindowSizeMsg` with `width < 60` while the view is open closes it with a
status message (`diff closed: terminal too narrow`), mirroring the files
view's `< 40` rule. Opening is prevented below 60 by `canShowFileDiff` /
the tree-enter gate.

#### Footer & help

- Registry: `{"enter", "[enter] diff", panelStatus && canShowFileDiff}` in
  `contextBindings`. `TestHelpFooterCoverage` picks the key up automatically.
- The files-view footer override and the tree's in-box hint line gain
  `[enter] diff` (override: always listed, like the other files-view keys —
  the hint line is a static string and the tree always has file rows when
  non-empty; `enter` on a heading is simply a no-op).
- `helpContent()`: a row for the Status-panel `enter`, a row for the
  files-view `enter`, and a new "Diff view" section listing the table above.

## State interactions

- **Ops:** `canShowFileDiff` requires `opsIdle()`. The tree entry inherits
  idleness for free: the files view only opens via `canShowCommitFiles`
  (which requires `opsIdle()`) and swallows every op key while open. So no
  op can start or run behind a full-screen view, and no decision modal can
  be needed while one is up; the modal check stays first in `render()` as
  defense in depth.
- **reRoot** (`R` popup is unreachable while the view is open, but reRoot is
  also triggered by worktree switch decisions): `reRoot` nils `diffView`
  alongside `filesView`.
- **Reload (`r`):** unreachable while open (swallowed) — the snapshot nature
  of the view is fine; reopening re-reads.

## Testing

- `internal/textdiff`: table tests for `Compare` — equal inputs, pure
  insert, pure delete, replace runs of unequal length (zip + tail), multiple
  blocks (`Blocks` indices), empty sides, trailing-newline handling,
  prefix/suffix trim correctness, the 50k size guard, `IsBinary`.
- `internal/git`: `ShowFile` against a real repo (`newRepo` helper): content
  at HEAD, content at a previous commit, error for a missing path. Plus a
  `FakeRunner` argv assertion (`show <rev>:<path>`).
- `internal/tui`:
  - loaders: status-entry side selection per the rules above (untracked,
    staged-new, deleted, rename, modified) and tree-entry side selection per
    the table (A/D/R/M), using a real repo;
  - key handling: scroll clamps, page step, block jumps (first/last block
    edge), esc closes (files view intact beneath), q quits, swallowing;
  - stale gating: mismatched tag dropped, nil view dropped;
  - rendering: pane widths sum to terminal width, alignment of a known
    diff, truncation, gutter, binary/error/loading/truncated states, resize
    guard closes below 60;
  - footer: `[enter] diff` visible only on Status focus with a row +
    `opsIdle`; files-view override contains `[enter] diff`;
  - help: drift guard covers the new binding automatically; new rows exist.
- No e2e scenario: the feature is TUI-only; the e2e harness drives the CLI.

## Out of scope (explicitly later)

- Intraline (word-level) emphasis within `Changed` pairs.
- Horizontal scrolling for long lines.
- Staged-vs-unstaged split for the status entry (currently combined vs HEAD).
- Diffing arbitrary pairs (stash versions, two refs) — `diffInput` is already
  shaped for it.
