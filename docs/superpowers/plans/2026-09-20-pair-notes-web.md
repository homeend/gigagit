# Pair notes in `gg web` — implementation plan

**Spec:** `docs/superpowers/specs/2026-09-20-pair-notes-web-design.md` (N1–N10).
**Executor:** this session, sequentially — no subagents.

**Global constraints.** No domain change. Read-only route: no `writeGuard`.
Hide by `#id.hidden`. No browser storage. A copy row goes through `copyLink(`.
Stay out of `prdetails.js` and the notebox files. Every task has a break table;
a break must BUILD and must change behaviour. Exit codes via `> log 2>&1; echo
EXIT=$?`.

---

## Task 1 — the server: `pair` on the door, `/api/pair/notes`

**Files:** `internal/web/linkcompare.go`, new `internal/web/pairnotes.go`, new
`internal/web/pairnotes_test.go`.

- `writeLinkComparison` gains a `pair *[2]string` argument; when non-nil the
  body carries `"pair": {"a": …, "b": …}`. `handleCompareLinks` passes it from
  the `a + b` form and from `id` when `savedEntry` says PAIR; nil for a saved
  comparison's `id` and for `left + right` (N1, N2).
- `GET /api/pair/notes?a=&b=[&path=]`: `isFullSha` on both (400), `isGitArgSafe`
  on a given path (400), `PairNotes` → zero set answers the empty shape 200,
  else `PreviewNotesAt` (only with a path) + `PreviewNoteCounts`, the same
  `{notes, tip, counts, total}` as `/api/preview/notes` (N3).

**Fixture** `pairNotesRepo`: commits `c0` (a) → `c1` → `c2` (b) on one line,
each touching `a.txt`; a note on `c0`, on `c1` and on `c2`, injected notes dir,
`t.Parallel()`.

**Tests**
1. counts-only: `total == 2`, `counts["a.txt"] == 2`, `tip == c2`, `notes` is `[]`.
   (c1 and c2 count, c0 does not — the look-alike commits disagree.)
2. with a path: two notes.
3. abbreviated `a` with a VALID `b` and path → 400; unsafe path with VALID ids → 400.
4. absent pair (two well-formed ids not in the repo) → 200, empty shape.
5. `/api/compare-links?a=&b=` carries `pair`; a saved pair's `?id=` carries it;
   `left + right` = `pairSides(a, b)` does NOT; a saved comparison's `?id=` does NOT.

| break | expect red |
|---|---|
| drop the `isFullSha` check | test 3 (id row) |
| drop the path check | test 3 (path row) |
| `PairNotes(ctx, b, a)` | tests 1, 2 |
| pass `pair` from the `left + right` arm too | test 5 |
| pass nil from the saved-pair `id` arm | test 5 |

## Task 2 — `linkFor` learns a pair

**Files:** `internal/web/static/links.js`, `internal/web/linksjs_test.go`.

`preview.pair = {a, b}` is tested BEFORE the `linkRefOK(source/target)` refusal
and before the PR arm: both ids ≥ 40 hex or `""`; target text `@<a>..<b>`; an
old-side line degrades to the file form (N8).

**Tests** (the node lane): a pair ctx with path + new-side line round-trips
through `model.ParseLink` to the same pair, path and line; old side → no line;
a 39-char id → `""`; a ctx carrying BOTH `pair` and names yields the PAIR.

| break | expect red |
|---|---|
| move the pair arm below the `linkRefOK` refusal | round-trip row (`""`) |
| drop the length check | short-id row |
| drop the old-side degrade | old-side row |

## Task 3 — the page

**Files:** `internal/web/static/files.js`, `live.js`, `linkcompare.js`;
probe `scratchpad/web/probe-pair.mjs` (not committed).

- `openLinkCompare`: `state.compare.pair = body.pair || null`;
  `state.previewCounts = null`; when armed, `loadPairCounts(a, b)` (counts-only,
  superseded-checked against `state.compare.pair`).
- `pairCtx()`: `filesMode === "compare" && state.compare && state.compare.pair
  && state.layout !== "list"` → the pair, else null. Badge site: `prev || pair`.
- `openFile` links arm: `ctx` when `state.compare.pair && !f.right_spec` (N5).
- `openEntryFileDiff({…, ctx})`: `state.diffCtx = ctx || null`, `state.notes =
  []`, and `fetchNotes(false)` in the `Promise.all` with the diff when ctx.
- `noteQuery`: pair arm before the names arm. `fetchNotes`: `/api/pair/notes?`.
- `live.js`: on `notes`, when `pairCtx()` → `loadPairCounts`.
- `saveComparisonPrompt`: `{a, b, label}` when `state.compare.pair` (N10).

**Probe** (broken build first, then twice): agent note shows as ◆ row + file
badge on the pair from the Previews tab · `c` adds, badge follows · old-side
click refuses · `-u` stash pair's untracked row has no note lane · `left +
right` of the same points shows none · a preview opened first leaves no badge ·
esc to the list + a notes event fetches nothing · save chip on a steered pair
makes a PAIR row · the line copy row's link resolves as a pair.

Broken builds: (a) server never sends `pair`; (b) `openEntryFileDiff` ignores
`ctx`. Each must turn the matching probe rows red.

## Task 4 — docs and the gate

`CHANGELOG.md`, `README.md` (web notes paragraph), `docs/web-tui-parity.md`,
`docs/CLAUDE-details.md` ("Links in gg web": W9 closed), spec "As built".
`./test.sh race` on the merged tree; ask before merging; then `./build.sh
install`, `./build.sh web`, `gg init --update`. `using-gg` is not bumped (no
CLI change).
