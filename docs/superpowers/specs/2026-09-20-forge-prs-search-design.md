# Forge pull requests — search (closed, merged, any state)

Date: 2026-09-20 · Branch: `feat/forge-prs-closed-search` · Builds on
`2026-09-19-forge-prs-design.md` and `2026-09-19-forge-prs-web-design.md`.

## 1. Problem

Every surface lists the OPEN pull requests (`gh pr list --state open`) plus
the "known" ones that are no longer open — a PR seen open this session, or one
with a local `refs/gg/pr/<n>`. A closed or merged PR that was never fetched
cannot be found from the TUI or the web at all; from the CLI only when its
number is already known (`gg pr fetch <n>`).

## 2. Rulings (user, 2026-09-20 — do not re-ask)

1. **Query = free text + a state.** The text goes to the forge's own search
   unparsed (GitHub qualifiers such as `author:x` or `head:branch` just
   work). A text that is only a number — `123` or `#123` — looks that PR up
   directly.
2. **State** is one of `all | open | closed | merged`, default **`all`**; the
   TUI and web cycle it, the CLI takes `--state`.
3. **Surfaces: TUI + CLI + web**, in this stage.
4. **Results are a transient set**, shown in their own view, never mixed into
   the polled list. Opening a result fetches its head, so the existing
   known-by-ref rule then keeps it in the normal list until it is forgotten.
5. **50 newest-updated, no paging.** When the forge has more, the surface says
   so ("more results — narrow the search"). The CLI's `--limit` overrides it,
   1…200.
6. **Result rows act like PRs-tab rows**: open the diff, open the details
   hub, copy the URL. Closing the diff or hub returns to the results.
7. **Memory is session-only**: reopening the search shows the last query and
   its results at once (stale-first); nothing is written to disk.
8. **TUI key `A`** in the PRs tab (`S` is the stash window; `/` stays the
   local row filter), plus a `.` menu row and a palette entry. The web uses
   `A` too.

Unchanged rulings that bind this stage: read-only; the web wire value is the
PR NUMBER (R1); a web GET never calls the forge (R2); the web hides by id
(R3); no usable forge CLI = no PR UI, so no search UI either.

## 3. Design

### 3.1 The seam — `internal/forge`

```go
// PRQuery is a forge-neutral pull-request search.
type PRQuery struct {
    State string // StateAll | StateOpen | StateClosed | StateMerged
    Text  string // opaque to gg; the forge's own search syntax
    Limit int    // rows wanted, 1…MaxSearchLimit
}

const (
    StateAll = "all"; StateOpen = "open"; StateClosed = "closed"; StateMerged = "merged"
    DefaultSearchLimit = 50
    MaxSearchLimit     = 200
)

// Search lists the pull requests matching q, newest-updated first. more
// reports that the forge had at least one row beyond q.Limit.
Search(ctx context.Context, q PRQuery) (prs []model.PullRequest, more bool, err error)
```

`Provider` gains `Search`. The gh implementation is one invocation:

```
gh pr list --state <state> --limit <limit+1> --json <prFields> [--search <text>]
```

- The extra row is how `more` is known; it is cut before returning.
- The text is the VALUE of `--search`, so a leading `-` cannot become a flag.
- `parsePRList` is reused; rows carry no body (as in the open listing).
- **Order.** `gh pr list` orders by CREATION date, and by "best match" once
  `--search` is given (checked against a real repository, 2026-09-20), so a
  cut at N would not be the N newest-updated. The gh provider therefore
  ALWAYS passes `--search`, appending `sort:updated-desc` unless the text
  already carries a `sort:` qualifier (empty text → `--search
  sort:updated-desc`). This is GitHub syntax and stays below the seam.
- An unknown `State` or a `Limit` outside 1…`MaxSearchLimit` is an error from
  `PRQuery.Normalize()` (`""` → `all`, `0` → `DefaultSearchLimit`), which
  every caller runs first; the provider never sees a bad query.

GitLab/Gitea later map the same four states and hand `Text` to their own
search; nothing above the provider knows GitHub's syntax.

### 3.2 Domain

```go
type PRSearchResult struct {
    Query forge.PRQuery  // normalized
    PRs   []model.PullRequest
    More  bool
    At    time.Time
}
func (s *Service) PRSearch(ctx context.Context, q forge.PRQuery) (PRSearchResult, error)
func (s *Service) PRSearchLast() (PRSearchResult, bool) // never calls the forge
```

- `domain` re-exports the query type and state constants
  (`domain.PRQuery = forge.PRQuery`), since frontends never import `forge`.
- **Number lookup:** a trimmed text matching `^#?[0-9]+$` (n > 0) skips
  `Search` and asks `Provider.PR(n)`: one row; `forge.ErrNotFound` → an empty
  result, no error. The state is ignored for a number lookup.
- Single-flight per normalized query (`"forge-search:" + state + limit + text`).
- Every success replaces the one remembered result (`PRSearchLast`). A failed
  search leaves the previous one in place.
- Found rows seed the PR cache (`putPRLocked(pr, false)`) so a following open
  is served stale-first. They are NOT added to `forgeSeen`: a search never
  grows the normal list by itself (ruling 4).
- Sorted newest-updated first, number as the tie-break — the same comparator
  the list uses, applied after the forge's own relevance order.

### 3.3 CLI — `gg pr list`

```
gg pr list [--state all|open|closed|merged] [--search <text>] [--limit N] [--json]
```

- No search flag → today's behaviour exactly (the open + known list).
- Any of `--state` / `--search` / `--limit` → a search. State defaults to
  `all`, limit to 50; a bad state or a limit outside 1…200 is a usage error.
- Text output: the same `prLine` rows; `(no pull requests)` when empty; when
  `More`, `more results — narrow the search` on STDERR.
- `--json`: the same array of PRs as today (no envelope — agents already
  parse it); `More` is only the stderr line.
- `using-gg.md` documents it; `agentskill.Version` is bumped.

### 3.4 TUI

`A` with the PRs tab focused (also the `.` menu's "Search pull requests…" and
the palette) opens the **PR search popup** (a `popupMax` layer):

```
╭ Search pull requests ─────────────────────────────╮
│ [all]  fix login author:kim█                      │
│───────────────────────────────────────────────────│
│ #412  ✓ merged  Fix login redirect     kim  3d    │
│ #398  ✗ closed  Login: drop legacy…    ana  2w    │
│ more results — narrow the search                  │
╰ enter search · tab state · ↓ results · esc close ─╯
```

- Two focus zones: the **prompt** and the **rows**.
  - Prompt: typing edits the text; `tab` cycles the state chip
    `all → closed → merged → open`; `enter` runs the search; `↓` moves to the
    rows (when there are any); `esc` closes the popup.
  - Rows: `j/k/↑/↓/pgup/pgdn/home/end` move; `enter` opens the PR diff
    (the existing `openPRCmd` path — fetch, then the preview surface); `i`
    opens the hub; `y` copies the URL; `↑` on the first row and `esc` return
    to the prompt.
- Rows are painted by the PRs tab's row painter (`prRows` refactored to take
  a slice), so marks, state words and dimming match the tab.
- While a search runs the rows area shows `searching…`; the previous rows are
  dropped only when the answer lands. An error shows in the rows area
  (`errText`), the prompt stays editable.
- Opening the popup shows `PRSearchLast()` at once — query, state and rows —
  without a forge call.
- The diff and the hub are opened THROUGH the layer stack
  (`pushLayer`/`handOffToFilesView`), so closing them returns to the popup
  with its cursor in place (the project convention).
- After a result was opened, the normal list is re-read (the fetch op already
  maps to the `prs` source) and the PR appears there as a known row.
- Every string goes through `i18n.T` with keys in all four bundles; help and
  footer advertise `A`.

### 3.5 Web

Endpoints (registered with the other PR routes, behind the same forge gate):

| Verb | Path | Forge call | Body / answer |
|---|---|---|---|
| `POST` | `/api/pr/search` (write-guarded) | yes | `{text, state}` → `{state, query:{text,state}, prs:[prRow…], more, error}` |
| `GET` | `/api/pr/search` | **never** (R2) | the last result of this server for this repo, or `{prs:[], query:null}` |

- Rows are the existing `prRow` wire shape (with `fetched`). The limit is
  fixed at 50 on the web.
- The server keeps the last result per repo root beside the `prCache`; it is
  dropped on re-root.
- A result row is opened through the existing `/api/pr/open?n=` flow — the
  wire value is the NUMBER (R1). `cachedPR` also consults the search result so
  the open path knows a row the polled list does not have.

Page:

- The PR section of the sidebar gains a search row under its header:
  `#pr-search` (a text input), `#pr-search-state` (a chip button cycling the
  four states), and a results block `#pr-search-results` with a header
  (`results · N` + a `✕` close) and an `<ul id="pr-search-list">`.
- `Enter` in the input runs the search (`runOnce("pr-search")`); the list
  shows `searching…` meanwhile. `Escape` in the input blurs it; the `✕` hides
  the results block (the last result stays on the server).
- `A` focuses the input (ignored while typing in a field, like every page
  key). On load the page GETs the last result and shows it if there is one.
- Row click = open (the same `openPR` with mask and spinner); right-click =
  the same PR menu as a list row (details, copy URL, forget when fetched and
  not open). `more` paints a last non-clickable line.
- Every element that hides gets its own `#id.hidden` rule (R3).
- `docs/web-tui-parity.md` gains the row.

### 3.6 Opening a closed or merged PR

Unchanged, and already correct: `PRFetchOp` fetches `refs/pull/<n>/head`
(GitHub keeps it for closed and merged PRs), `PRPair` diffs a merged PR
against its recorded base sha, and a head that cannot be fetched lands on the
existing "unavailable" path. This stage adds tests for a search → open of a
merged PR, no new behaviour.

## 4. Errors

- Forge unavailable → the key, menu row, CLI flags and web row do not exist /
  report the existing `gg pr:` error.
- A forge failure (network, bad qualifier) is shown verbatim (first line) in
  the results area / on stderr; nothing is cached.
- A query that times out (`forge.CallTimeout`) is an ordinary failure.
- Empty text + state `all` is legal: "the 50 newest-updated PRs of any state".

## 5. Testing

- `forge`: FakeRunner argv tests — state, limit+1, `--search` present/absent,
  a leading-dash text, `more` detection, `Normalize` errors.
- `domain`: number lookup (hit, not found), result memory, a failure keeps
  the old result, `forgeSeen` untouched (the list does not grow), the cache
  is seeded, single-flight.
- `cli`: flag parsing, usage errors, stderr `more` line; one e2e scenario
  against the fake gh (`GG_GH_BIN`).
- `tui`: popup key tests (state cycle, zones, enter/i/y, return-to-popup),
  stale-first reopen, i18n gates; a `tui-capture.sh` run against the fake gh.
- `web`: handler tests (POST guard, GET never calls the provider — a counting
  provider proves it, R1 open of a searched-only row); JS static tests;
  browser checks asserting VISIBILITY, each watched failing with its guard
  removed.
- `./test.sh race` from a clean tree before each merge.

## 6. Plans

1. **Core + CLI + TUI** — seam, domain, `gg pr list` flags, the popup, docs.
2. **Web** — endpoints, sidebar search, browser verification, docs.

Each is merged on its own after its gate.

## 7. Out of scope

Paging / load-more, a query history on disk, saved searches, MCP, searching
issues, any write to the forge.
