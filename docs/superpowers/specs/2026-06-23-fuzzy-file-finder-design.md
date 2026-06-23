# Design: fuzzy file finder

**Status:** Design spec (approved to plan).
**Date:** 2026-06-23.
**Branch:** `feat/fuzzy-file-finder` (off `main` @ `5880140`).
**Gap:** #4 of `docs/feature-gaps-lazygit-gitkraken.md` (jump-to-file over 80k–100k files).

## Goal

A global fuzzy file finder: press **`F`** anywhere to open a filterable popup over
every tracked file in the repo, type to fuzzy-match a path, and `enter` to open a
per-file **action menu** (View content / Diff / History / Blame / Open in editor /
Copy path). Mirrors the existing `g`/`G`/`R` quick-switchers; targets the pain of
tab-cycling tens of thousands of files in a monorepo.

## Data source

- **`git`:** new verb `Repo.LsFiles(ctx) ([]string, error)` =
  `git ls-files -z` (NUL-separated, handles special chars; tracked files only).
- **`domain`:** `Service.LsFiles(ctx) ([]string, error)` under a Read reservation,
  singleflight-coalesced (key `ls-files`). Returns repo-relative paths.
- **Tracked only** (v1): the per-file actions (History/Blame) need a tracked path.
  An untracked mode (`--others --exclude-standard`) is a possible later toggle.

## The fuzzy matcher — `internal/fuzzy` (pure, no git/TUI deps)

A new pure package, in the family of `textdiff`/`commitgraph`/`template`.

```go
package fuzzy
// Score reports whether query matches candidate as a case-insensitive
// subsequence, and a rank score (higher = better); ok=false when no match.
func Score(query, candidate string) (score int, ok bool)
// Rank filters+sorts candidates by Score (best first), keeping at most limit.
// Returns the matched candidates (with their match positions for highlighting).
func Rank(query string, candidates []string, limit int) []Match
type Match struct { S string; Score int; Pos []int }
```

**Scoring (simple, deterministic):** subsequence match; bonuses for a match at a
**path/word boundary** (start, or right after `/ _ - . space`), for **contiguous**
runs, and for matches in the **basename** (after the last `/`); penalty for gaps
and for leading unmatched chars. So `fvgo` ranks `internal/tui/files_view.go`
above `favorites/go.mod`. Empty query → all candidates, original order.

## The finder popup — `fileFinderPopup` (stack layer, mirrors `repoPopup`)

```go
type fileFinderPopup struct {
    all     []string      // every tracked path (loaded once, async)
    loading bool
    query   string
    matches []fuzzy.Match // ranked, capped at the display limit
    sel     int           // index into matches
    mode    dispMode      // z cycles; hscroll for long paths
    hscroll int
}
```

- **Open (`F`):** global, like `openBookmarkSwitcher` — wired in every content
  surface's key handler (base, diff, files view, history, blame, stash) so `F`
  works anywhere. `openFileFinder` pushes the popup (loading) + dispatches
  `loadLsFilesCmd`; the async `lsFilesMsg{paths}` fills `all` and clears loading.
- **Filter:** every keystroke re-ranks via `fuzzy.Rank(query, all, limit)` with a
  display cap (`limit` ≈ 200). `↑/↓` move `sel`; `z` cycles display mode; `esc`
  closes (`popLayer`).
- **Render:** window-then-build (the files-view perf lesson) — build `winRow`s only
  for the visible slice; matched positions optionally styled (bold) — a
  nice-to-have, not required for v1.

**Perf (94k–100k files):** `Rank` is a single pass (score each, partial-select the
top `limit`) — no full sort of 100k per keystroke. Loading `ls-files` once is
~tens of ms; holding ~100k short strings is a few MB. **Measured on the linux repo
(94k files) before merge** per the project's perf discipline; the load is
off-thread and re-ranking must stay well under a frame.

## On `enter` → per-file action menu

`enter` builds the file-action rows for the selected path and opens them through
the existing **action-menu slot** (`m.actionMenu`), which dispatches/render above
the stack — so it appears over the finder. Each row's handler **pops the finder**
and opens a **self-contained surface/layer** (the files-view preview is embedded
in that view's right column and cannot be reused from a global popup):

| Row | Opens | Reuses |
|---|---|---|
| **View content** | a `contentPopup` pushed as a **layer** showing the file at HEAD | `newContentPopup` (the help-viewer pattern); a NEW tag-gated load that fills *the pushed layer's* lines (`domain.ShowFile` at HEAD → `contentLines`) — NOT the files-view-coupled `openPreview`/`loadFileContentCmd`, which target `m.filesPreview` |
| **Diff** | full-screen `diffView` layer: the path's **working tree vs HEAD** ("no local changes" if clean) | the compare-diff path (`Endpoint{HEAD}` ↔ `Endpoint{WorkTree}`) |
| **History** | `historyView` layer (`navContext{path}`) | `newHistoryView` + `loadHistoryListCmd` |
| **Blame** | `blameView` layer (`navContext{path}`) | `newBlameView` + `loadBlameCmd` |
| **Open in editor** | $EDITOR on the file at HEAD (read-only temp) | `openInEditorCmd` |
| **Copy path** | repo-relative path to the clipboard | `copyToClipboardCmd` |

All six open existing self-contained surfaces/layers — no new view machinery
beyond wiring.

## Layers / files

- `internal/git/ls_files.go` (or extend `compare.go`): `LsFiles` verb + `-z` parse.
- `internal/domain/query.go`: `LsFiles` query (Read reservation, singleflight).
- `internal/fuzzy/fuzzy.go`: the pure matcher (`Score`/`Rank`/`Match`).
- `internal/tui/file_finder.go`: `fileFinderPopup`, `openFileFinder`,
  `loadLsFilesCmd`/`lsFilesMsg`, the file-action rows, and the `F` key wiring in
  the content surfaces.

## Out of scope (v1)

Untracked files; content/grep search (gap #2, separate); match-position
highlighting (optional polish); a CLI surface (`gg find` — possible later, TUI is
the value here); fuzzy config knobs.

## Testing

- **`internal/fuzzy`** (pure, the highest-value tests): subsequence correctness;
  ranking order (basename > boundary > scattered; `fvgo` → `files_view.go`);
  empty query → identity; `limit` respected; no-match → empty.
- **`git.LsFiles`:** FakeRunner argv (`ls-files -z`) + `-z` parse incl. a path with
  a special char (NUL split, no quoting).
- **`domain.LsFiles`:** returns paths; singleflight key.
- **TUI:** `F` opens the finder (from base + a content surface); `lsFilesMsg` fills
  the list + clears loading; typing re-ranks (`sel` clamps); `enter` opens the
  action menu with the six rows; each row pops the finder and opens the right
  surface (assert the resulting top layer / cmd); `esc` closes.
- **Perf sanity:** `Rank` over a synthetic 100k-path slice stays within budget
  (microbench / timed unit test).

## Acceptance

- `F` anywhere opens the finder; fuzzy-typing narrows to the intended file fast;
  `enter` → action menu → each action lands in the right surface.
- Responsive on the linux repo (94k files) — load off-thread, re-rank under a frame.
- `internal/tui`/`internal/cli` still don't import `internal/git`; `internal/fuzzy`
  imports nothing project-specific (archtest stays green).
- Full suite + `./test.sh race` green; CHANGELOG updated.
