# Go to line — design

Date: 2026-10-23 · Branch: `feat/go-to-line`

## Intent

The user opens a file from **F** (View file content) and wants to see what is
at line N of the current version without scrolling there. Today no gg surface
can jump to a typed line number; the only ways to land on a line are a `gg://`
link, an overview anchor or a search hit.

"Go to line" is one key, **`:`**, on every window that shows a file's
content, in the TUI and in gg web, behaving the same everywhere.

## Goals

- `:` opens a small "Go to line" prompt; typing a number and enter lands the
  line cursor on that line, centred, exactly as a `gg://` link landing does
  (`landPendingLine` in the TUI, `landLine` in the viewer on the web). From
  there space-select, copy, notes and `/` work as usual.
- Works on every file-content surface (list below), TUI and web.
- In a diff, a line the diff currently shows is jumped to in place; a line the
  diff hides **opens the file at that line** in the viewer (the user's ruling),
  and esc returns to the diff.
- Advertised in `?` help and in each surface's footer/hint (TUI and web), in
  all four translation bundles.

## Non-goals

- Jumping to another file (`gg://` links and the `#` paste prompt do that).
- Relative jumps (`+10`, `-5`), columns, `path:line` typed into F's filter
  (possible follow-up — noted, not built).
- CLI/MCP surface: none (they already address lines through links).

## Anti-goals

- A prompt that traps: esc and an empty enter always cancel, leaving the view
  exactly as it was.
- A jump that silently lands somewhere else: past the end lands on the last
  line WITH the existing "past the end" notice.

## The prompt

- **TUI:** inline, in the surface's hint/footer line, like the in-view `/`
  search (`textsearch.go`): `Go to line: 42▏`. Digits and backspace edit;
  anything else is ignored; enter jumps; esc / empty enter cancels. Builder's
  choice: a `lineJump` twin of `textSearch` hooked into each host's
  first-refusal key path is the expected shape. In a split, `docPane.captures`
  (and `stackPane`'s equivalent) must report true while the prompt is open.
- **Web:** `openPrompt({title, placeholder, validate, onSubmit})`
  (`layers.js`, as `#` go-to-commit uses), digits-only validation.
- Line numbers are 1-based. 0 or empty = cancel.

## Surfaces and what "line N" means on each

| Surface | TUI | Web | Line N is | Behaviour |
|---|---|---|---|---|
| File viewer (F → View file content; open files; files opened from an overview/tour anchor) | `fileViewer` / `openFile` | `viewer.js` file mode | the shown file | cursor → N in place |
| Files-view preview (a commit's / shelf's file version) | `m.filesPreview` | viewer (commit src) | the shown version | in place |
| Split pane showing a file | `docPane` | `viewer.js` split | the pane's file | in place, in the focused pane |
| Split pane showing the file's diff (`D`) | `docDiff` | viewer diff rows (`splitdiff.js`) | new-side line | shown → in place; folded → open the file at N |
| Review stack pane (stream of files) | `stackPane` | split stack (`splitstack.js`) | line N of the file under the cursor (`streamLine`) | in place |
| Blame | `blameView` | `filehist.js` blame | the blamed file | in place (TUI cursor `sel`; web scroll + mark the row) |
| File history | `historyView` | `filehist.js` history | the file at the selected commit | opens the file at that commit at N (the right pane is passive) |
| Diff view (single, incl. one opened from history) | `diffView` | `files.js` diff pane | new-side line | shown → in place; folded/absent → open the file at N |
| Stacked diff | `diffView.stk` | `stackview.js` | new-side line of the file under the cursor | as single diff |
| Overview prose pane | `overviewPane` | overview | — | `:` not bound (prose has no source lines) |

"New side" = the diff's newer version. For a deleted file (no new side) the
old side's line is used.

### "Open the file at N" from a diff

**The one rule (my call):** a hidden line opens the file only when the diff's
newer side IS a version the viewer can open — the working tree or a commit.
Every other diff reveals the line in place instead: open the fold
(`expandFoldFor` / `revealDiffRow`) and land on it. That covers the index (a
staged diff), shelf/bookmark/stash sides, two-endpoint compares, merge
previews, PR diffs, and a deleted file's old side.

The rule must not be inferred from `rev == ""` alone (several constructors
share it — staged, unstaged, shelf → worktree, bookmark → worktree). In the
TUI one method, `diffView.newerSide()`, answers it from the identity a diff
already carries, most specific first: a compare stamp's right endpoint
(work tree → worktree, commit → that commit, anything else → none), then the
note address's state (unstaged/untracked → worktree, committed → its commit,
staged/shelf → none), then a bare `rev` (a commit). Anything it cannot place
answers "none" = reveal in place — the safe default. The web does the same
from the diff context (`state.diffCtx` / a stack slot's context).

When the file does open, the newer version opens in the full-screen viewer at
line N, pushed over the diff (esc returns):

- working tree → `openFileViewer(path, N)` (TUI) / `openViewer({src:
  "worktree", path, line})` (web);
- a commit → `openFileAtCommitLine(rev, path, N, 0)` / `openViewer({src:
  "commit", rev, path, line})`.
- If N is past the end of the newer version, the viewer's own past-the-end
  notice applies.

**Split pane diff (`docDiff`, web `splitdiff.js`) — my call:** the pane
already holds the file under its diff, so "open the file" there means switch
the pane from the diff back to its file (as `D` does) and land on N. The
split layout stays; `D` returns to the diff. A diff whose newer side is not
that file (per the rule above) reveals in place.

Recipes to reuse (from the surface map):
- TUI diffView: `lineAnchorIn(lo, hi, no, old)` → `visible` true = in place via
  `setCursorLine` + `alignCursor(alignCenter)`; false / -1 = hidden. Stacked:
  restrict to `fileLineRange(fi)` of the file under the cursor (the
  `landSteer` recipe in `steer_nav.go`).
- TUI docDiff: a non-fold row in `dd.rows` with `nu == N` = shown.
- Web main diff: `diffRowAt(root, "new", N)` present = shown; mark + scroll
  like `live.js` `landLine`.

## Constraints (project rules that apply)

- Every user-visible TUI string through `i18n.T`, keys in ja/ko/zh/ru.
- New key in BOTH `?` help (`help.go`) and the footer/hint of each surface;
  web: `index.html` help rows / `registerHelp` + footer chips. Diff footer
  must still fit its width budget (`TestDiffHintFitsTheBudget`, 140 cols):
  **`[:] line` takes the place of `[f] part`** in both the single and the
  stacked diff line (user's ruling); `f` keeps its `?` help row and `.` menu
  row. Update `diffHintFor`'s doc comment with the trade.
- `:` must not fire while another prompt/search is typing, while a charsel is
  active, or inside a text field.
- TDD; tests `t.Parallel()` where allowed. At least one TUI e2e golden
  scenario (F → view → `:` → N) and web JS coverage in whatever form the web
  package already tests keys.
- CHANGELOG, README (user-facing), `docs/CLAUDE-details.md` entry.

## Left to the builder

- Exact prompt rendering and the shared helper's shape.
- Order of the `:` check inside each surface's key routing.
- How the web blame marks the landed row (reuse the search-hit mark).
