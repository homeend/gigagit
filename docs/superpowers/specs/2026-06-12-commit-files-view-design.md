# Commit files view — design

Date: 2026-06-12
Status: approved (rev 2 — left-column replacement, follow-live)

## What

On the Commits panel (the full-height right column), `l` opens a read-only
file list of the selected commit **in the left column**: the three left
panels (Branches / Worktrees / Status) are replaced by one full-height box
showing a static indented tree — full directory paths as headings, files
beneath them with status letters. The Commits panel stays visible and keeps
focus: moving the commit selection (j/k/↑/↓) reloads the file view for the
newly selected commit (**follow-live**). `esc`/`l` close the view and the
left panels come back. TUI-only — no CLI command, no engine operation
(read-only query, same class as `Log`).

## Approach (decided)

Reuse the `contentPopup` engine — the struct (`contentLine{text, heading}`
lines, query, typing mode, cursor), its heading-survival filter (`visible()`),
its movement/windowing — but render it inside the left column's geometry
instead of as a centered overlay, with a thin key-intercept layer that splits
keys between the tree and the commit list. This keeps the project rule
"read-only viewers use contentPopup, not custom UI" while delivering the
in-place replacement.

Rejected: a `panelList` "panel mode" (headings break the row filter; global
action keys would act on a fake panel), and a standalone view struct
(duplicates filter/scroll logic the popup already has).

## Git verb

`internal/git/log.go` gains:

```go
// CommitFiles returns the files changed by commit hash (first-parent diff
// for merge commits), in git's path order. One invocation.
func (r *Repo) CommitFiles(ctx context.Context, hash string) ([]model.CommitFile, error)
```

argv: `git diff-tree -r --root --no-commit-id --name-status -M --first-parent -m <hash>`

- `--first-parent -m`: merge commits show their diff against the first parent
  (plain `diff-tree` prints nothing for merges).
- `--root`: the initial commit lists its files instead of nothing.
- `-M`: renames detected, reported as `R<score>\told\tnew`.

New model type in `internal/model/model.go`:

```go
// CommitFile is one changed path within a commit.
type CommitFile struct {
	Status  string // single letter: A M D R C T (score stripped from R/C)
	Path    string // new path
	OldPath string // set only for renames/copies
}
```

Parser `ParseNameStatus([]byte) []model.CommitFile` is a pure function:
lines are `<status>\t<path>` or `R<score>\t<old>\t<new>` (same for `C`);
blank lines and malformed lines are skipped; the status letter is the first
byte of the status field.

## Tree building (pure)

`internal/tui`: `commitFileLines(files []model.CommitFile) []contentLine`.

- Sort files by `Path`.
- Root-level files (no `/` in path) come first, no heading, rendered
  `<letter>  <name>`.
- Then group consecutive files by directory (`path.Dir` + `/`); each group
  emits one heading line with the full directory path (`internal/tui/`),
  files beneath indented as `  <letter>  <basename>`.
- Renames/copies group under the NEW path's directory and render as
  `  R  <full-old-path> → <new-basename>`.
- Exactly one heading level — no nesting, no fold/unfold (YAGNI).
- Empty input → a single non-heading line `(no files)`.

## TUI wiring

**Model state** (`model.go`):
- `filesView *contentPopup` — nil = closed; pointer so it persists across
  the value-receiver copy (existing convention).
- `filesTitle string` — `Files <short-hash> <subject>` (hash truncated to 7),
  updated together with the content so title and tree never disagree.
- `filesHash string` — the commit the view currently WANTS (the selected
  commit); used to drop stale async results.

**Opening:** `l` in the normal-key section, gated like `m`
(`!running && !loading`), only when `m.focus == panelCommits` and
`backingIndex(panelCommits)` resolves. Fires the async load (below). `l` on
any other panel or an empty list is a no-op. On a narrow terminal (< 40
cols, where the layout has no left column) `l` sets a statusMsg
("terminal too narrow for the files view") and does not open.

**Follow-live loading:** every load is a `tea.Cmd` running `CommitFiles`,
its result message tagged with the commit hash. The model applies a result
only when its hash equals `filesHash` (stale results from fast j/k movement
are dropped). While a load is in flight the previous commit's tree stays on
screen (no flicker); the title updates only when its content arrives. A load
error sets statusMsg; the view stays on its previous content. The search
query is KEPT across commit changes (so you can track one file through
history); the cursor resets to the top.

**Routing:** in the key-routing chain, after `pairPopup` and before
`filterTyping`:

```
modal → popup → repoPopup → settings → branchPopup → contentPopup
      → pairPopup → filesView → filterTyping → normal keys
```

While the view is open, focus stays on the Commits panel conceptually; the
filesView layer splits the keys:

| Key | Action |
|---|---|
| `j/k/↑/↓` | fall through to normal commit-selection movement, then trigger a follow-live reload for the newly selected commit |
| `ctrl+↑/ctrl+↓` | scroll the file tree by 1 |
| `pgup/pgdn` | page the file tree (reassigned from commit paging while open) |
| mouse wheel | scroll the file tree (same scoping as the help window) |
| `/` | start tree search (typing mode captures all keys; enter commits, esc cancels — identical to the help window) |
| `esc` | clear a committed search; if none, close the view |
| `l` | close the view (toggle) |
| `q`/`ctrl+c` | quit the app (unchanged) |
| anything else | swallowed — no panel actions or popups from inside the view |

**Rendering** (`view.go`): when `filesView != nil`, the left column draws ONE
bordered box (`g.leftW × bodyH` at the left column's position) instead of the
Branches/Worktrees/Status panels — title `filesTitle` plus the standard
`/query` suffix (block cursor while typing), headings bold, cursor row
reversed, rows truncated to the box's text width, windowed via `windowRows`.
Bottom line inside the box: `n/m  [/] search  [esc] close` (count only when
content overflows). The Commits panel renders unchanged and still shows its
focus/selection. The truncation tooltip stays active for the Commits panel
only.

**Invalidation:** `reRoot` clears `filesView` (different repo). `r` (reload)
is swallowed while the view is open, like other action keys.

**Help/footer:** `footerText` is unchanged (panel-scoped key; the footer
lists global keys). `help.go` gains a `Commits panel` section with the `l`
row and a `Commit files view (l)` section (move-commit/scroll/search/close
rows). If `l` is ever added to `footerText`, `TestHelpFooterCoverage`
enforces the rows.

## Testing

- `internal/git`: `ParseNameStatus` table tests (A/M/D, rename with score,
  type change, malformed lines); `CommitFiles` argv assertion via
  `FakeRunner`; one real-repo test (`newRepo`) covering a normal commit, the
  root commit, and a rename.
- `internal/tui`: `commitFileLines` table tests (grouping, root files,
  renames, empty); interaction tests via the established pattern — `l` opens
  with fed data and the left column shows the tree while Commits stays
  rendered; j/k moves the commit selection AND triggers a reload for the new
  hash; a stale result (hash ≠ current) is dropped; `/` narrows and keeps
  headings; the query survives a commit change, the cursor resets; esc clears
  search then closes; `l` toggles closed; `l` no-ops on other panels / empty
  commits / while running; narrow-terminal no-op with statusMsg; action keys
  (`p`, `m`, `tab`, …) are swallowed while open; reRoot clears the view.

## Not doing (YAGNI)

Collapsible tree nodes; diff-on-enter; CLI command; engine operation; sort
modes inside the view; running operations or opening popups from inside the
view; debouncing follow-live loads (stale-drop is enough at git speed).
