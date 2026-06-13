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

One invocation, built with `gitcmd`. `<rev>:<path>` interprets a plain path
relative to the repo root regardless of cwd, so the verb is cwd-safe. It
emits the raw blob — no textconv, no smudge filters — which is the source of
the autocrlf/LFS caveat below. Callers only invoke it for sides they expect
to exist (the entry context says which), so a missing-path error is a real
error, not a control-flow signal.

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

Algorithm: split both sides into lines, trim the common prefix and suffix
(capped so `prefix + suffix ≤ min(len(old), len(new))` — one side being a
prefix of the other must not double-count the shared lines), run Myers O(ND)
on the middle, then align: equal lines → `Same`; a run of deletions followed
by a run of insertions is zipped pairwise into `Changed` rows, with the
longer run's tail emitted as `Del`/`Add` rows (gap on the other side). Line
numbers are assigned during emission. Comparison is on raw lines — CRLF-vs-LF
differences are real changes (display sanitizing happens at render time, see
below).

**Size guard — two layers, both on the *trimmed middle*, both yielding the
same fallback** (emit the middle as one replace block: all left lines `Del`,
all right lines `Add`, `Truncated: true`):

1. *Line cap:* skip Myers when either trimmed side exceeds 50 000 lines.
2. *Edit-distance budget:* inside Myers, bail out when D exceeds 2 000.
   Myers is O((N+M)·D); two large mostly-different files (lockfiles,
   generated code — common in the target monorepos) would otherwise spin
   the loader goroutine for minutes.

Because the guard runs after trimming, the common case — a small change in
a huge file — still aligns perfectly and is *not* marked `Truncated`.

Empty-side semantics: `Compare(nil, content)` yields all-`Add` rows (new
file); `Compare(content, nil)` all-`Del` (deleted file). A trailing newline's
phantom empty last line is dropped — but only when *both* sides agree; when
exactly one side ends with a newline, that side keeps a final row so the
newline-at-EOF-only change is visible as a block instead of producing two
identical panes for a file git reports as modified.

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
`panelStatus`, a row is selected (`backingIndex` ok), the row is not
conflicted (`Kind != KindUnmerged` — conflicts are the M3 conflict editor's
job; the footer then honestly hides `[enter] diff` on them), and the
terminal is wide enough using the codebase idiom `!(m.width > 0 &&
m.width < 60)` (a bare `≥ 60` would wrongly refuse before the first
`WindowSizeMsg`, e.g. in tests). The `enter` arm in `Update` is gated by the
same predicate (never-looser contract).

The selected `model.FileStatus` decides the sides:

- **old side:** empty when the file is not in HEAD — `Kind == KindUntracked`
  or `Staged == 'A'` (a staged-new file; `ShowFile("HEAD", …)` would fail).
  Otherwise `ShowFile("HEAD", OrigPath-if-set-else-Path)` (renames fetch the
  old name).
- **new side:** `os.ReadFile(filepath.Join(m.currentWorktree, Path))` —
  porcelain paths are repo-root-relative and the process cwd may be a
  subdirectory; `statusList.Date` already establishes this join convention.
  A not-exists error means the file is deleted from the working tree →
  empty side (this also absorbs the delete/re-create porcelain combinations
  and races). Any other read error renders in the view.

The new side reads the disk (not the index): the viewer answers "what changed
relative to HEAD", combining staged + unstaged. Context label:
`HEAD → working tree`. Tag: `status:<path>`.

**Known race (accepted, documented):** `opFinishedMsg` fires `loadCmd()`
without setting `loading`, so `opsIdle()` holds during that silent reload and
the side-selection rule can read a just-stale `m.status` (e.g. a file an op
just committed still marked untracked). The snapshot renders transiently
wrong for milliseconds and self-corrects on reopen; errors stay visible
rather than being demoted.

**Caveat (v1, accepted):** the HEAD side is the raw blob while the disk side
is smudged content — repos with `core.autocrlf` show every line as Changed,
and LFS files show pointer-vs-content. Stated here so it isn't refiled as a
bug.

#### Entry point: commit files tree

`contentLine` gains three fields — `path`, `oldPath` and `status` (all
strings; `status` mirrors `model.CommitFile.Status`, which is a string) —
zero-valued for headings, help-window lines, etc.; the help window shares
the type and ignores them. `commitFileLines` fills them from each
`model.CommitFile` (`path` = new path), so the loader knows from the
selected line which sides exist. Payload-on-line is the only design that
survives the `/`-filter: `visible()` returns a reordered subset of
`contentLine` values, so a parallel index→file map would break.

In `updateFilesViewKey`, `enter` with `filesTreeFocused` and the selected
*visible* line carrying a non-empty `path` opens the diff for commit
`m.filesHash`:

| CommitFile.Status | old side | new side |
|---|---|---|
| `A` | empty | `ShowFile(hash, path)` |
| `D` | `ShowFile(hash+"^", path)` | empty |
| `R` / `C` | `ShowFile(hash+"^", oldPath)` | `ShowFile(hash, path)` |
| `M` / `T` | `ShowFile(hash+"^", path)` | `ShowFile(hash, path)` |

The root commit needs no special case: `CommitFiles` passes `--root`, so
every file in the root commit has status `A` (empty old side) and `hash^` is
never dereferenced. Merge commits DO reach here — `CommitFiles` passes
`--first-parent -m`, so the tree lists the merge's first-parent diff — and
the table is correct for them because `hash^` *is* the first parent,
matching the status letters the tree was built from (a loader test pins
this). `enter` on a heading row, on the commits side, or in `/`-typing mode
is swallowed as today; at widths 40–59 (files view open, diff too narrow)
`enter` is refused with a `statusMsg` ("terminal too narrow for the diff
view"), following the `l`-key precedent of explanatory refusal in dispatch.
Context label: `@ <short-hash> <subject>`. Tag: `commit:<hash>:<path>`.
The files view stays in `Model` untouched underneath; closing the diff
returns to the tree exactly as it was.

#### Rendering (full screen)

**Routing invariant (structural, not incidental):** the `diffView` check
sits *immediately after the modal check* in all three dispatch sites —
`Update`'s key routing, the `tea.MouseMsg` arm (else a wheel event over the
diff would scroll the files-view tree underneath), and `render()`. Today no
popup can coexist with an open diff (popups only open from the main
dispatch, unreachable while a full-screen view owns the keyboard), but the
upcoming workspace-group-sync feature introduces background ops whose
decision modal can pop at any time; keeping "key owner == top visible
surface" aligned across all three sites is what makes that arrival safe.

`render()` therefore short-circuits to `renderDiffView()` when
`m.diffView != nil`, after the modal check and before everything else. The
view owns the whole screen; the registry footer is not drawn.

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
- Body: `height - 2` rows (header + hint are the only chrome; the mockup's
  horizontal rules are illustrative, not rendered). Each row: left pane,
  `│` separator, right pane;
  panes are `(width-1)/2` wide, each with a dim right-aligned line-number
  gutter (width of the largest line number, min 3) and the line text
  truncated with `…`.
- **Display sanitizing** (render/row-build time only — `Compare` sees raw
  lines): expand tabs to a fixed 4-column stop, strip a trailing `\r`, and
  replace remaining control characters with `·`. Raw file content is the one
  thing gg panels have never rendered; `truncate`/`padRight` measure with
  `lipgloss.Width`, which does not expand tabs, so an unexpanded `\t` (every
  indented line of a Go file) would push text through the `│` separator.
- Styles (new lipgloss styles alongside the existing ones): `Del` rows — red
  background on the left cell; `Add` — green on the right cell; `Changed` —
  red left cell *and* green right cell; gap cells — dimmed `·` fill; `Same` —
  plain. The selected-row/panel styles are not reused; the view has no cursor.
- Last line: the key hint (constant). When `truncated`, the title gains
  ` (alignment skipped: large file)`; when `binary`, the body is the single
  line `(binary file)`; when the size cap is hit, `(file too large)`; when
  `err != nil`, the body shows the error; while `loading`, `(loading…)`;
  when the load succeeded but `Blocks` is empty and both sides are equal
  (mode-only change, or a stale snapshot), `(no content difference)` is
  appended to the context label so two identical panes are explained.

**Fetch size cap:** the loaders refuse sides larger than 10 MB — the disk
side via `os.Stat` before reading, the blob side by checking the returned
length (the target repos hold multi-hundred-MB assets; `Runner.Run` buffers
all of stdout, so the cap can't prevent one buffered read of the blob side,
but it prevents feeding it to `Compare` and rendering). The body state is
`(file too large)`. `IsBinary` runs on whatever was fetched within the cap.

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
  also triggered by worktree switch decisions): `reRoot` nils `diffView` and
  clears `diffTag`, alongside `filesView`/`filesHash`.
- **Background data reload:** `dataLoadedMsg` from the post-op silent reload
  *can* arrive behind an open view (see the documented race above); the diff
  snapshot is self-contained bytes, so rendering is unaffected.
- **Reload (`r`):** unreachable while open (swallowed) — the snapshot nature
  of the view is fine; reopening re-reads.

## Testing

- `internal/textdiff`: table tests for `Compare` — equal inputs, pure
  insert, pure delete, replace runs of unequal length (zip + tail), multiple
  blocks (`Blocks` indices), empty sides, trailing-newline handling
  (both-sides phantom drop AND the one-sided newline-at-EOF block), the
  prefix-of-the-other trim-overlap case (`"a\na\n"` vs `"a\n"` — no
  double-count), the 50k line cap, the edit-distance budget (two large
  fully-different inputs return `Truncated` quickly — assert a time bound),
  `IsBinary`.
- `internal/git`: `ShowFile` against a real repo (`newRepo` helper): content
  at HEAD, content at a previous commit, error for a missing path. Plus a
  `FakeRunner` argv assertion (`show <rev>:<path>`).
- `internal/tui`:
  - loaders: status-entry side selection per the rules above (untracked,
    staged-new, deleted, rename, modified, conflicted-excluded) and
    tree-entry side selection per the table (A/D/R/M, plus a merge commit
    asserting first-parent vs commit), using a real repo; the disk read with
    a cwd different from the repo root (the `filepath.Join` convention);
    the 10 MB cap path;
  - key handling: scroll clamps, page step, block jumps (first/last block
    edge), esc closes (files view intact beneath), q quits, swallowing;
  - stale gating: mismatched tag dropped, nil view dropped;
  - rendering: pane widths sum to terminal width, alignment of a known
    diff, truncation, gutter, tab-indented content stays inside its pane
    (the sanitize step), binary/error/loading/truncated/too-large/
    no-content-difference states, resize guard closes below 60 (and the
    files-view < 40 guard still fires in the same resize);
  - footer: `[enter] diff` visible only on Status focus with a row +
    `opsIdle`; files-view override contains `[enter] diff`;
  - help: drift guard covers the new binding automatically; new rows exist.
- No e2e scenario: the feature is TUI-only; the e2e harness drives the CLI.

## Documentation deliverables

A third surface kind (full-screen view) changes the TUI taxonomy, so per
CLAUDE.md: `CHANGELOG.md` (always), `README.md` (new keys + the viewer),
`.claude/skills/adding-tui-windows/SKILL.md` (add the full-screen-view kind
with its routing invariant), and `CLAUDE.md`'s package map (`textdiff`).
The CLI surface is unchanged — no agentskill bump.

## Out of scope (explicitly later)

- Intraline (word-level) emphasis within `Changed` pairs.
- Horizontal scrolling for long lines.
- Staged-vs-unstaged split for the status entry (currently combined vs HEAD).
- Diffing arbitrary pairs (stash versions, two refs) — `diffInput` is already
  shaped for it.
