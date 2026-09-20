# Review notes on a commit pair in `gg web` — design

Addendum to `2026-09-20-links-web-design.md` (it parked this as W9) and to
`2026-09-20-pair-notes-design.md` (the domain + TUI + CLI + MCP half, shipped).
Status: **built** on `feat/web-pair-notes` — see §6.

## 1. The gap

A commit pair `a..b` is a note scope everywhere but the browser. `gg note add
--preview a..b`, MCP's `preview` arg and the TUI all read and write the pair's
notes; the web opens the same pair and shows none, and `c` does nothing there.
An agent that reviews a pair and hands it over with `gg open --web` lands the
reviewer on a diff with its findings missing.

## 2. What reading the code found

- **F1. The web's pair no longer rides the hash lane.** Since 3c a pair opens
  through `GET /api/compare-links` (`?id=` for a saved pair, `?a=&b=` for a
  `State:"pair"` navigate) into `openLinkCompare`. That screen addresses its
  sides by SPEC, opens a file through `openEntryFileDiff`, and that function
  sets `state.diffCtx = null` — so notes, the `c` key, the ◆ badges and the
  line copy-link are all structurally off, not merely unarmed.
- **F2. `state.previewOpen` is the wrong carrier.** It means "a thing whose
  tips can MOVE": `live.js` and `previews.js` re-resolve it by branch name on
  every refresh, `previewShowing()` compares `compare.bHash` to its source tip,
  and the PR lane already needed a `pr` escape hatch at every one of those sites. A pair is
  two frozen shas: nothing moves, nothing re-resolves, and a link comparison
  has `bHash === ""`. The TUI reached the same conclusion ("a pair has no
  `previewOpen`").
- **F3. `/api/preview/notes` cannot serve it.** It allowlists two BRANCH NAMES
  (`knownRefName`). A pair has none. The PR lane met the same wall and got its
  own twin endpoint (`/api/pr/notes?n=`).
- **F4. Domain needs nothing.** `PairNotes(a, b)` builds the scope;
  `PreviewNotesAt` and `PreviewNoteCounts` read only `Tip/Base/Commits`. The
  write is the ordinary `/api/notes/add` with `rev=b, state=commit` — that
  handler has no preview arm at all (checked), so a pair needs none either.
- **F5. Not every row of a pair is at `b`.** A `-u` stash is copied as a pair,
  and its untracked files live on the stash's THIRD parent; 3c carries that per
  row as `right_spec`. Such a row's new side is not the file at `b`, so it is
  not note-addressable.
- **F6. `linkFor` refuses `ctx.compare` without a `preview`.** A pair diff row
  therefore has no "copy gg link to this line" today. The grammar has the form
  (`/path@a..b:N`); only the producer is missing.
- **F7. No "late stamp" race here.** The TUI's scope arrives by message and can
  lose to the first diff. The web sets it synchronously on `state.compare`
  before any file can open.
- **F8. The dialog's swap cannot reverse an armed pair**: it swaps the two
  FIELDS and resubmits through `left + right`, which never arms (N2). The
  comparison screen has no in-place swap.

## 3. Decisions

| # | Decision |
|---|---|
| N1 | **The scope rides the comparison, not `previewOpen`.** `/api/compare-links` answers `"pair": {"a","b"}` (full ids) for the `id`-of-a-saved-pair and `a+b` forms ONLY. `openLinkCompare` stores it as `state.compare.pair`; it dies with the comparison. |
| N2 | **The `left + right` form never arms**, even when the two links happen to spell a pair — the TUI's rule (`steeredPairNotesCmd`: only a pair landing arms, so a generic comparison of two far-apart points never pays the rev-list). |
| N3 | **`GET /api/pair/notes?a=&b=[&path=]`** — the twin of `/api/preview/notes`, same answer shape `{notes, tip, counts, total}`, same counts-only form without `path`. `a`/`b` must pass `isFullSha` (400 otherwise); `path` must pass `isGitArgSafe`. A pair that is not here answers the empty shape, 200 (ruling 6). Read-only: no `writeGuard`. |
| N4 | **Writes are unchanged**: `/api/notes/add` with `rev = b`, `state = commit`, new side. The page takes `b` from `state.compare.pair`, never from a label. |
| N5 | **`openEntryFileDiff` takes an optional `ctx`.** The links arm of `openFile` passes one when `state.compare.pair` is set AND the row has no `right_spec` (F5): `{path, rev: b, state:"commit", notes:true, compare:true, preview:{pair:{a,b}, source:"", target:"", pr:0}}`. Every other caller passes none and keeps `diffCtx = null`. A third-parent row keeps `null` too. With a ctx, `fetchNotes(false)` rides the SAME `Promise.all` as `/api/entry-diff` (openFile's rule: the ◆ rows are in the first paint); today this function never fetches notes. |
| N6 | **`noteQuery` gets a pair arm before the names arm** (`a`, `b`, `rev`, `state`); `fetchNotes` picks `/api/pair/notes?`. The old-side refusal and the fall-forward to the first new-side row already key on `diffCtx.preview` and apply as they are — a pair's old side is commit `a`, which no note names. |
| N7 | **Badges**: a `pairCtx()` predicate (`filesMode === "compare" && state.compare.pair`, and `state.layout !== "list"` — `drillOut` leaves both standing, which is why `previewShowing()` checks the layout too) sits beside `openPreviewCtx()` at the badge site; counts land in `state.previewCounts` from a counts-only fetch in `openLinkCompare`, superseded-checked against `state.compare.pair`. `openLinkCompare` NULLS `state.previewCounts` first — today only `armPreview` does, and a preview's numbers would otherwise paint on the pair until the fetch lands. A `notes` live event re-fetches them while the pair is on screen (a preview gets this through `fetchPreviews`; a pair has no such ride). |
| N8 | **`linkFor` learns `preview.pair`**, tested BEFORE the `linkRefOK(source/target)` refusal (a pair's names are empty — the JS twin of "ask `IsPair()` first"): `…/path@<a>..<b>[:N]`, old side degrades to the file form exactly as a preview's does. This makes "copy gg link to this line" and the ◆ note menu's copy row work in a pair diff, and keeps them going through `copyLink(` (the copy-row gate). Both ids must be ≥ 40 hex or the place is inexpressible. |
| N10 | **Saving an armed pair saves a PAIR.** The `save comparison…` chip posts `{a, b, label}` when `state.compare.pair` is set (the endpoint already takes it), else `{left, right, label}`. Otherwise a steered `@a..b` landing arms, is saved as a two-link comparison, and re-opens by `?id=` with its notes gone. |
| N9 | **History / blame at `b`** become available on a pair row as a side effect of having a `rev`. Accepted: `b` is a real commit and the row's new side IS its content. |

## 4. Out of scope

A note-count column on the Previews tab's pair ROW · arming a typed `left +
right` pair (N2) · pair notes on a `right_spec` row (F5) · the `?preview=<id>`
hint · the TUI (already shipped) · `live.js`'s fallback to `openCompareForPair`
when a half is not a full id — that hash lane carries no `pair`, R2 makes it
unreachable for a steered navigate, and it is the parked "mixed sha/name pair
lane" item.

The old-side refusal keeps its text ("notes in a preview anchor on the new
side"): true of a pair as written.

## 5. Tests

Go (`internal/web`): the endpoint's counts-only and with-path forms; an
abbreviated id and an unsafe path are 400 with VALID other params (so only the
guard under test can refuse); a note on `b` AND a note on a commit inside
`a..b` both count, while a note on `a` does not (the two look-alike commits
disagree on one fixture); an absent pair answers the empty shape;
`/api/compare-links` carries `pair` for `?a=&b=` and for a saved pair's `?id=`,
and does NOT for `left + right` spelling the same pair, nor for a saved
comparison's `?id=`.

`links.js` HAS a node lane (`linksjs_test.go`): `linkFor` with a `preview.pair`
ctx yields text `model.ParseLink` reads back as the same pair, path and line; an
old-side line degrades to the file form; a short id refuses; and a ctx with
BOTH `pair` and names takes the pair (the ordering N8 depends on).

The rest of the page has no unit lane — one playwright probe, run against a deliberately broken
build first, then twice on the real one: a note added by `gg note add --preview
a..b` shows as a ◆ row and a file badge on the pair opened from the Previews
tab; `c` adds a note and the badge follows; an old-side click refuses; a `-u`
stash pair's untracked row shows no note lane; a `left + right` comparison of
the same two points shows none; a preview opened first leaves no badge on the
pair (N7's null); esc back to the list and a `notes` event fetches nothing; the
save chip on a steered pair produces a PAIR row (N10); the line copy row yields a `@a..b` link that
`gg link resolve` accepts.

Break table per task; each guard removed, not the feature.

## 6. As built

N1–N10 as written. Two notes: `pair` is set in `handleCompareLinks` wherever
`a` is non-empty after the form switch (both pair forms converge there), and
the live refresh hangs off `refreshNoteCounts` rather than `live.js` — one
call site, already on the `notes` event. The probe (22 checks) went red against
eight broken builds, one per guard: no `pair` on the wire · `ctx` ignored · the
stale-counts null · `pairCtx` without the layout check · an armed `right_spec`
row · the chip posting two links · no live count refresh · `noteQuery` without
its pair arm. The stale-counts check holds the pair's fetch back with a routed
delay — without it the window is too short to see.

**N9 is true of the DIFF only.** The open diff's history/blame read
`diffCtx.rev = b`. The file LIST's right-click menu still keys its rev rows on
`state.compare.bHash`, which is empty for a link comparison, so a pair's file
row offers no history / blame / bookmark / shelf there. Left as is.

The probe watched `c` and `E`; `R` and the ◆ menu's copy row ride the same
context (`linkFor`'s pair arm is pinned by the node gate) and were not driven.
