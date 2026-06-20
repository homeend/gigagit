# Commit tooltip shows the branch — design

**Feature 3 of the commit-ops pipeline backlog** (redefined: the earlier `i`
info-view idea is dropped in favor of a tooltip enhancement, per the user —
"I would only wish to know from which branch the given commit is").

## Goal

The Commits-panel tooltip (the floating strip, today shown only when a row is cut
off) should **always** show the branch the selected commit is from, and **also**
the commit message when the row is truncated.

## Cheap branch source: `git log --source` / `%S`

Each commit's "which branch is it from" comes from `%S` (the ref the commit was
reached from in the walk), added to the existing commit-feed log call — **no
per-hover git call** (matters for big repos). Verified: `git log --branches HEAD
--source --format=…%S` yields the short branch name per commit
(`[feat]`, `[master]`). Caveat: for a commit reachable from several branches, `%S`
is the first ref that reached it in walk order — a single, reasonable "from"
branch for a hint, consistent enough with the lane it sits on.

## Plumbing (git → model → tui; domain passes through)

1. **`model.Commit`** gains `Source string` (the branch the commit was reached
   from; `""` when unknown).
2. **`internal/git/log.go`:**
   - `logFormat` appends a 7th field: `… %x1f%D%x1f%S`.
   - `LogScoped` adds the `--source` flag (required for `%S`).
   - `ParseLog` sets `Source = f[6]` **only when** `len(f) >= 7`, keeping the
     existing `len(f) < 6` skip guard — so 6-field test fixtures still parse
     (Source `""`) and only real git (7 fields) populates it.
3. **`CommitFeed`/domain** already pass `model.Commit` straight through to the
   TUI's `m.commits`, so `Source` flows with no domain change.
4. **`TestLogScopedArgv`** (and any argv assertion) updates to expect `--source`.
5. **Real-git test** (the `%D` lesson — argv tests can't prove format
   population): a `newTestRepo` with a branch asserts `LogScoped` returns commits
   whose `Source` is the branch name.

## Tooltip (`internal/tui/tooltip.go`)

Restructure `tooltip()` so the content is computed before geometry:

- **panelCommits:** always include a branch line `"⎇ " + commit.Source` when
  `Source != ""` (commit = `m.commits[idx[sel]]`, `idx` from `panelView`); when
  the row is also truncated (`rowTruncated("> "+rows[sel], innerW)`), append the
  message (the row text), wrapped, within `tooltipMaxLines`. If neither a branch
  nor a truncation applies, show nothing.
- **other panels:** unchanged — show the row text only when truncated.

The existing geometry/position/render code (origin, `windowRows`, `tooltipY`,
`tooltipStyle`) is reused as-is; only the `raw []string` content lines and the
early-return conditions change. The `modeCutoff`/`panelFocused` gates stay (a
wrapped/scrolled row needs no reveal; an unfocused panel's selection isn't the
active row).

## Testing (TDD)

- **git:** real-git `Source` populated (above); `ParseLog` with a 7-field line
  sets `Source`, with a 6-field line leaves it `""`; argv includes `--source`.
- **tui tooltip:**
  - With a focused commit whose `Source = "feat"` and a SHORT (untruncated) row,
    `tooltip()` returns `ok` with a line containing `"⎇ feat"` (the always-show
    behavior — the key new case).
  - With a truncated commit row, the tooltip contains both `"⎇ feat"` and the
    message text.
  - A commit with empty `Source` and an untruncated row → `ok == false`.
  - A non-commit panel (e.g. Branches) keeps the truncation-only behavior.

No CLI surface change → no e2e scenario. The agentskill/CLI is unaffected.

## Out of scope

- `git branch --contains` (the full multi-branch membership) — `%S`'s single
  source ref is what the tooltip needs; `--contains` stays available for a later
  feature if a complete list is ever wanted.
- Showing the branch anywhere other than the tooltip.
