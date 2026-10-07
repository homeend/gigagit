# Copy links to a review's files and remarks — design

Date: 2026-10-07 · Branch: `feat/review-links-copy`

## Problem

In an opened AI review (TUI review view, gg web review), the user cannot
copy a link that brings someone — usually an agent — to **one file** or
**one remark** of that review:

- File rows offer only the plain commit/compare link (opens the diff, not
  the review); a working review has none; the web cannot build one for a
  range review.
- A remark offers nothing in the TUI (single-commit reviews even block
  `Copy link` / `L`: "no gg link for this place"); the web copies a plain
  `@tip path:line` link that names the wrong change for range/working
  reviews.
- No surface copies the remark's id `review:<id>:<n>` — the handle
  `gg note reply` / `gg note resolve` take. Only `gg review show` prints it.

## Outcome

From any review file row or remark, in TUI and web, the user copies:

1. **Copy file link** (file row) — `gg://<repo>@<review target>/<path>?review=<id>`.
2. **Copy remark link** (remark) — the same with the remark's line(s):
   `…/<path>:<start>[-<end>]?review=<id>` (old side: `:old:<n>`).
3. **Copy remark id** (remark) — `review:<id>:<n>`.

Opening such a link (TUI `#` paste / `gg open` / `gg session navigate`, web
`gg open --web` / the link dialog) opens **the review** on that file — and
at that line for a remark link — instead of the review's overview.

## Rulings (from the user's request + the code)

- One link grammar, no new syntax: the existing `?review=<id>` hint, with
  a path (and line) on the link — the parser already accepts it
  (`model/link.go`); `gg link --review` keeps refusing a path (out of
  scope).
- The link's target is the review's own target (`reviewTarget` — commit,
  pair, or working tree), so the path/line are lines of the reviewed
  change and the link still resolves when the review is gone (falls back
  to the plain change, as `onReviewHint` already does).
- A remark's line = its `newRange` (new side) or `oldRange` (old side), as
  `gg review show` prints it.
- Remarks stay read-only; these are copy actions only.

## Design

### 1. Domain (`internal/domain/review_link.go`)

- `func (s *Service) ReviewFileLink(ctx, id, path string, side model.NoteSide, start, end int) (string, error)`
  — `ReviewLink` plus `Path`, `Side`, `Line`/`End` (0 = no line). Built
  with the same `reviewTarget` + `linkText`. Errors as `ReviewLink`.
- `ReviewShow` remarks gain the review-aware link as well (`Remark.ReviewLink`),
  so `gg review show` prints it beside the plain line link and agents get
  a link that reopens the review at that remark.
- `func (s *Service) ReviewRemarkAt(ctx, id, path string, side, line)`
  — the remarks (with their `review:<id>:<n>` ids) covering that line; used
  by `gg review show <remark link>` (§4).

### 2. Opening a review at a file / line (TUI)

- `steerNavigateReview` keeps `c.Path` / `c.Side` / `c.Line` from the
  steer command; `reviewViewMsg` gains `land *reviewLanding{path, side,
  line}`.
- `handleReviewViewMsg`, once the review view is up, selects that file's
  row (the reviewed files list) and opens its diff — the review diff with
  stamped remarks — placing the cursor on `line` (side-aware) when given.
  A path not among the reviewed files → the overview, status
  `"<path> is not in this review"`. Reuses the files view's own open-file
  path and the diff's line-cursor placement used by `gg session navigate`.
- Same for the `#` paste prompt and `gg open` (both resolve through
  `linknav` → steer navigate with the hint).

### 3. TUI copy rows

- **Review file row** (`.` menu in the review files view): `Copy file link`
  (review-aware, `ReviewFileLink`, no line) — placed beside the existing
  `Copy review link`; the plain `Copy link` row stays (it names the
  change). Working reviews get it too.
- **Remark** (diff note menu when `v.reviewID != ""`): `Copy remark link`
  and `Copy remark id`. `L` (copy link at the cursor) on a remark line of a
  review diff copies the remark link instead of refusing.
- Labels through `i18n.T`, four bundles; status `"copied: <link>"` as the
  existing copy rows do (`asyncCopyLinkRow` / `copyRow`).

### 4. CLI

- `gg review show <link>`: a review link WITH a path shows only that file's
  remarks; with a line, only the remarks covering it (each still printed
  with its `review:<id>:<n>` and links). No path = today's whole review.
- `using-gg.md`: a `?review=` link with a path names a file of the review,
  with a line a remark; `gg review show <link>` prints it with its id;
  reply with `gg note reply review:<id>:<n>`. Skill version bump.

### 5. Web (`internal/web`)

- `GET /api/review/{id}/link` gains optional `path`, `side`, `start`,
  `end` → `ReviewFileLink`.
- Review file rows (`files.js` files-list menu when a review is active):
  `copy file link (review)` via `copyServerLink`.
- Remark menu (`files.js` note menu, ids starting `review:`): `copy remark
  link` (server-built) and `copy remark id`. For a review remark these
  REPLACE the existing `copy gg link to this note` (its plain `@tip` link
  names the wrong change for range/working reviews) — one row, not two
  near-duplicates; store notes keep their row unchanged.
- Opening a `?review=` link with a path on the page (`links.js` /
  `gg open --web`): open the review, then that file, scrolled to the line.

## Error handling

A link that cannot be built (review gone, path unknown) → the existing
copy-failure status/toast with the reason; nothing copied.

## Testing

- domain: `ReviewFileLink` for commit / pair / working reviews, old-side
  line, line range; round-trip through `ParseLink`; `ReviewRemarkAt`.
- cli: `gg review show <remark link>` prints only that remark; a file link
  only that file's.
- tui: file-row menu has `Copy file link` in a review; remark menu has the
  two rows; `L` on a single-commit review remark copies the remark link
  (was refused); a `?review=` link with path:line opens the review on that
  file at that line (Headless/Model test through `steerNavigateReview`).
- web: endpoint params; JS wiring checks for the new rows; a browser probe
  (copy → open) on a fixture review.
- e2e: a TUI golden of the review diff opened from a remark link.

## Out of scope

`gg link --review <id> <path>` (CLI building such links), editing remarks,
the `gg session list` skill wording bug (separate fix), Junie slash-command
install (separate proposal).
