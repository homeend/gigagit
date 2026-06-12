# Commit files view — design

Date: 2026-06-12
Status: approved

## What

On the Commits panel (the full-height right column), `l` replaces the panel
in place with a read-only file list of the selected commit: a static indented
tree (one heading level — full directory paths as headings, files beneath
them with status letters), with the panel-consistent `/`-gated search.
`esc`/`q`/`l` return to the commit list. TUI-only — no CLI command, no engine
operation (read-only query, same class as `Log`).

## Approach (decided)

Reuse the `contentPopup` engine — the struct (`contentLine{text, heading}`
lines, query, typing mode, cursor), its heading-survival filter (`visible()`),
its movement/windowing — but render it inside the Commits panel's box
geometry instead of as a centered overlay. This keeps the project rule
"read-only viewers use contentPopup, not custom UI" while delivering the
requested in-place replacement.

Rejected: a `panelList` "panel mode" for `panelCommits` (headings break the
row filter; global action keys would act on a fake commits panel), and a
standalone view struct (duplicates filter/scroll logic the popup already has).

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
- `filesPopup *contentPopup` — nil = closed; pointer so it persists across
  the value-receiver copy (existing convention).
- `filesTitle string` — `Files in <short-hash> <subject>`, captured at open.

**Opening:** `l` in the normal-key section, gated like `m`
(`!running && !loading`), only when `m.focus == panelCommits` and
`backingIndex(panelCommits)` resolves. Returns an async `tea.Cmd` that runs
`CommitFiles`; the result message sets `filesPopup` (and `filesTitle`).
Error → `statusMsg`, view not opened. `l` on any other panel or an empty
list is a no-op.

**Routing:** in the key-routing chain, after `pairPopup` and before
`filterTyping`:

```
modal → popup → repoPopup → settings → branchPopup → contentPopup
      → pairPopup → filesPopup → filterTyping → normal keys
```

The handler is the contentPopup key handler pointed at `filesPopup` with two
deltas: `l` also closes (toggle), and closing means `m.filesPopup = nil`
(commit list reappears). Shared logic is extracted so the help window and the
files view use one handler parameterized by which field to close; behavior:
`/` starts search, esc clears the search then closes, `q` closes, `enter`
closes, j/k/arrows ±1, ctrl+↑/↓ ±5, pgup/pgdn page, ctrl+c quits, everything
else swallowed. Mouse wheel scrolls it (same scoping as the help window).

**Rendering** (`view.go`): when `filesPopup != nil`, the right column draws
the file view instead of the Commits panel — same bordered box and geometry
(`g.rightW × g.boxH[panelCommits]`), title `filesTitle` plus the standard
`/query` suffix (block cursor while typing), headings bold, cursor row
reversed, rows truncated to the box's text width, windowed via `windowRows`.
Bottom line inside the box: `n/m  [/] search  [esc] close` (count only when
content overflows). Left panels render unchanged; the truncation tooltip is
suppressed while the view is open (it reflects panel selections).

**Invalidation:** `reRoot` and the `r` reload clear `filesPopup` (the commit
list may change underneath it). Opening any popup/modal over it is allowed —
they sit earlier in the routing chain and render above.

**Help/footer:** `footerText` is unchanged (panel-scoped key; the footer
lists global keys). `help.go` gains a `Commits panel` section with the `l`
row and a `Commit files view (l)` section (search/scroll/close rows). If `l`
is ever added to `footerText`, `TestHelpFooterCoverage` enforces the rows.

## Testing

- `internal/git`: `ParseNameStatus` table tests (A/M/D, rename with score,
  type change, malformed lines); `CommitFiles` argv assertion via
  `FakeRunner`; one real-repo test (`newRepo`) covering a normal commit, the
  root commit, and a rename.
- `internal/tui`: `commitFileLines` table tests (grouping, root files,
  renames, empty); interaction tests via the established pattern — `l` opens
  with fed data, title shows hash+subject, `/` narrows and keeps headings,
  esc clears search then closes, `q` and `l` close, `l` no-ops on other
  panels / empty commits / while running, reload and reRoot clear the view,
  render snapshot shows headings + status letters in the right column.

## Not doing (YAGNI)

Collapsible tree nodes; diff-on-enter; CLI command; engine operation; sort
modes inside the view; following the commit selection while open (the view
is pinned to the commit it was opened for).
