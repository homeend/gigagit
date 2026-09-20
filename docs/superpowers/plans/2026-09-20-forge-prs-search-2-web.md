# Forge PR search — plan 2: the web

> **Execution:** inline, by the session that wrote it (NEVER subagents), TDD,
> one commit per task.

**Goal:** the `gg web` sidebar can search pull requests of any state and open a
result, under the web rules (R1 wire value = the PR NUMBER, R2 a GET never
calls the forge, R3 hide by id).

**Spec:** `docs/superpowers/specs/2026-09-20-forge-prs-search-design.md` §3.5.
Plan 1 (merged `4fe6cfbf`) supplies `domain.PRSearch` / `PRSearchLast`.

## Constraints

- The server keeps NO result cache of its own: `domain.PRSearchLast` is
  per-service, session-only and never calls the forge — it IS the GET.
- Limit fixed at `domain.PRSearchDefaultLimit`.
- Every hidden element has its own `#id.hidden` rule.
- Browser checks assert VISIBILITY; each is watched failing with its guard
  removed; a script abort is a failure.

### Task 1 — endpoints (`internal/web/prsearch.go`, `prsearch_test.go`, `prs.go`)

- `POST /api/pr/search` (writeGuard) body `{text, state}` → `svc.PRSearch`.
  Bad state → 400; forge unavailable → 404-class refusal like the other PR
  routes; forge failure → 502 with the first line.
- `GET /api/pr/search` → `svc.PRSearchLast()` only.
- Answer: `{query: {text,state} | null, prs: [prRow…], more}` (prRow with
  `fetched`).
- `cachedPR` falls back to the last search's rows, so `/api/pr/open`,
  `/api/pr/revalidate` and the details routes accept a searched-only number.
- Tests: GET before any search = `query:null`, zero provider calls; POST
  calls the provider once with `{state, text, 50}`; GET after = same rows,
  still one call; POST without the write guard header refused; bad state 400;
  forge error 502; `/api/pr/open?n=5` for a searched-only PR = `unfetched`
  (not 404), unknown number still 404.

### Task 2 — the page (`static/index.html`, `prs.js`, `keys.js`, `style.css`, static tests)

- Markup under `#prs-header`: `#pr-search-row` (button `#pr-search-state`,
  input `#pr-search`), `#pr-search-results` (`#pr-search-head` with
  `#pr-search-count` + `#pr-search-close`, `ul#pr-search-list`). All born
  `hidden`; the row un-hides with the section, the results with an answer.
- `prs.js`: one `prRowHTML(pr, now)` for both lists; `runSearch()` via
  `runOnce("pr-search")`; `searching…` row; error row; `more` row; boot GET
  shows the last result; click = `openPR`, right-click = `showPRMenu`; the
  open-PR header menu looks in both lists; after a forget the results drop the
  row's `fetched`; `A` focuses the input (`focusPRSearch`, exported); Enter
  runs, Escape blurs; ✕ hides the block; the section's collapse hides row and
  results too.
- Help text + `docs/web-tui-parity.md` row.

### Task 3 — browser verification (scratchpad `prweb/`, `pw/search.mjs`)

Checks (each VISIBLE-asserted): search row visible with the section; `A`
focuses; state chip cycles all→closed→merged→open; Enter shows 3 rows incl.
merged/closed dimmed; `more` line; ✕ hides; reload shows the last result
(GET only — assert zero gh calls via the fake's call log); clicking merged #5
fetches and opens the diff titled `PR #5 · …`; afterwards #5 is a dimmed row
of the normal list; no forge → no search row. Guards removed one at a time.

### Task 4 — docs, gates

CHANGELOG, README, `docs/web-tui-parity.md`, `docs/CLAUDE-details.md`;
`./test.sh race` from a clean tree; verify binary; ask; after the merge
`./build.sh install` + `./build.sh web`.
