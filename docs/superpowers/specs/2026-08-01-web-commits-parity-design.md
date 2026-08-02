# Web commits-panel parity — quick filter, goto-sha, file history/blame

Date: 2026-08-01 · Branch: `feat/web-commits-parity` (off `web-dev`) · Status: approved

## Goal

Bring the web UI's commits surface up to daily-driver parity with the TUI:
a `/` quick filter over the loaded feed, a goto-sha that reveals a commit in
the list, and file history / blame views reachable from file rows, the
palette, and the open diff.

The domain layer already provides everything except a full-sha resolve:
`FileLog(ctx, rev, path, limit)`, `Blame(ctx, rev, path)` (LRU-cached for
commit revs, uncached for the working tree), and `git.ResolveCommit` behind
the repo. This stage is web endpoints + client UI.

## 1. Quick filter (`/`) — client-only

- Pressing `/` (no layer open, no form field focused — the existing
  form-field keydown guard applies) opens a one-line filter bar above the
  commits list. Typing narrows the **loaded** rows: case-insensitive
  substring match against subject and author, plus sha **prefix** match.
- Filtered rows render flat (the existing line-mode one-cell dot), never
  the lane graph — lanes are meaningless on a subset. Clearing the filter
  restores the normal render (graph intact, scroll preserved to the
  previously selected row when possible).
- Below the matches, a hint row: `N of M loaded commits match — load more`.
  Clicking it fetches `/api/commits?more=1` and re-filters. The row
  disappears when a load returns no new commits (feed exhausted). No
  automatic deeper paging — every git walk is an explicit click.
- Esc clears the filter and closes the bar. Clicking a filtered row opens
  the commit exactly as an unfiltered row does.
- The filter is display-only client state; it never reaches the server and
  never survives a re-root.

## 2. Goto-sha (`#` key + palette "Goto commit…")

- New read-only endpoint `GET /api/resolve?rev=` → `{hash, subject}`;
  404 when git can't resolve it. `rev` is `isGitArgSafe`-guarded before it
  reaches argv. Backed by a new domain query
  `ResolveRev(ctx, rev) (string, error)` — a thin `query()`-wrapped call to
  the existing `repo.ResolveCommit` (FULL sha; `CommitLookup`'s short sha
  cannot be matched against feed rows). Subject read via the existing
  `CommitLine` alongside.
- Client: `#` (same guard as `/`) or the palette entry opens the shared
  `openPrompt` for a rev. On resolve:
  - Row already loaded → scroll it into view, select it, flash-highlight
    (a CSS class that self-removes).
  - Not loaded → keep calling `loadCommits(more)`; stop when the sha
    appears or a page adds no new rows (exhausted).
  - Exhausted without a hit (e.g. solo scope excludes it) → fall back to
    `openCommitByHash(hash)` so the user always lands on the commit, with a
    status note that it isn't in the current list scope.
  - 404 → status-line error, prompt stays closed.

## 3. File history — `GET /api/filelog` + `#history` overlay

- Endpoint: `GET /api/filelog?path=&rev=&limit=` → rows
  `{hash, short, subject, author, time, status, path, old_path}` from
  `domain.FileLog` (follows renames). `rev` optional ("" = HEAD).
  Both `rev` and `path` `isGitArgSafe`-guarded. A nonexistent path returns
  an empty list — no pre-validation, the TUI's "(no history)" rule.
- Client: `#history` is a full-screen overlay on the layer stack (the
  report-viewer precedent, NOT a fourth pane-layout mode — the compare-mode
  integration lesson). Left column: the file's commits (status letter,
  short sha, subject, author, age). Right: that file's diff at the selected
  commit, via the existing `/api/diff` rev-form with `left` = the commit's
  first parent, `right` = the commit, `path` from the row (the post-rename
  name at that commit), `status` from the row.
  - Root commit / status "A": `left` is empty — the rev-form must accept an
    empty `left` and render the add. If it doesn't today, extend it (server
    detail for the plan).
  - Esc closes the overlay back to the previous screen. Row click only
    changes the right-hand diff; a "show commit" affordance per row opens
    the full commit detail (closing the overlay first).
- Empty result renders a single "(no history)" row inside the overlay.

## 4. Blame — `GET /api/blame` + `#blame` overlay

- Endpoint: `GET /api/blame?path=&rev=` → lines
  `{hash, author, time, summary, line, text}` from `domain.Blame`.
  `rev` "" = working-tree blame (never cached, matching the domain rule);
  a commit rev hits the blame LRU. Guards as above.
- Client: `#blame` full-screen overlay, monospace. Each line: a gutter
  (short sha · author · age) and the source text. Consecutive lines from
  the same commit dim/blank the repeated gutter so commit blocks read as
  groups. Lines with empty hash (not yet committed) show "working" in the
  gutter.
- Clicking a line's gutter closes the overlay and opens that commit's
  detail via `openCommitByHash` (no-op for "working" lines).
- Blame errors (binary file, path not tracked) surface as a status-line
  error; the overlay does not open.

## 5. Entry points

All three, sharing the same two open functions
(`openFileHistory(path, rev)` / `openFileBlame(path, rev)`):

1. **File-row right-click menu** — the file lists in commit detail,
   working-tree/status mode, and compare mode get ctx-menu rows
   "file history" and "blame". Commit context passes the commit's sha as
   `rev`; working-tree rows pass "" (history then starts at HEAD, blame is
   working-tree). Compare rows pass the right-side tip hash.
2. **Command palette** — "File history…" / "File blame…" open the shared
   `openPrompt` for a repo-relative path (sent as typed; the server/git
   answer for a bad path is the empty state, not a client rejection).
3. **Diff toolbar** — `#diff-nav` gains history/blame buttons for the file
   whose diff is open, using the diff's own path/rev context (working-tree
   diffs pass rev "").

## 6. Guards, errors, non-goals

- All three endpoints are read-only GETs: hostGuard applies (as everywhere),
  no writeGuard (matching `/api/compare`, `/api/versions`).
- `isGitArgSafe` on every `rev` and `path` before argv; 400 on failure.
- Non-goals (YAGNI): server-side LogScope filters (path/author/date),
  auto-paging while typing, blame cross-highlighting on hover, markdown or
  syntax coloring in blame content.

## 7. Testing

- **Go (httptest, real git fixture):** per endpoint — happy path;
  `isGitArgSafe` refusal (400); resolve 404 on unknown rev; filelog rename
  row carries `old_path`; filelog on a nonexistent path → empty list, 200;
  blame with an uncommitted line (empty hash); blame on a commit rev twice
  (second serves from cache — assert equality, not timing).
- **CDP browser checks** (headless chromium, both loopback hosts,
  visibility via `elementFromPoint`; bugfix-style checks run against the
  unfixed build first): `/` narrows the list + load-more hint pages deeper;
  `#` reveals + highlights a not-yet-loaded commit; goto falls back to
  detail under solo; history overlay opens from a file row with a visible
  diff; blame overlay opens from the palette path prompt; esc closes each
  overlay back to the prior screen.
