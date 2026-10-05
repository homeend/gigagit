# Review links — stage 1: link and read (design)

Date: 2026-10-05 · Status: agreed in chat, awaiting spec review

## Why

The user asks one agent (A) to review a change, then wants to hand that
review to a second agent (B) to check A's remarks. Today nothing carries a
stored review from one agent to the next: `gg review` prints the review only
while it runs (and its note id on stderr), no CLI verb reads a stored review
back, and no `gg://` link names one.

This is stage 1 of three (all agreed as wanted):

1. **Link and read** — this spec.
2. **Answers per remark** — B records a verdict (agree / disagree / fixed +
   reason) on each of A's remarks, shown beside them in the TUI and web.
   Designed together with the parked notes ↔ forge ↔ agent strategy.
3. **Second review** — `gg review` takes a review link as context; the new
   review records which review it answers; the two are shown together.

Stage 1 must give stage 2 a stable handle on each remark (see §3, `n`).

## User rulings this builds on

- A commit's note rows (Reviews, Range reviews, Notes) are not files; their
  menu was Open + Delete only (merged `cebffb7a`). This spec adds **Copy gg
  link** to all three (user, 2026-10-05).
- `gg review show` prints the overview plus a remark list in which every
  remark carries its own `gg://` line link (user picked option A over
  inlining code or dumping the raw document).
- A link can be CONSTRUCTED as well as copied: `gg link --review <id>`.

## 1. The link

A new hint kind `review` joins the closed set in `model/link.go`
(`linkHintKinds`: bookmark, shelf, stash, preview, view, version → + review).
The address is the change the review compared (`domain.ReviewRevs`):

| Review | Link |
|---|---|
| one commit (`<c>^..<c>`) | `gg://<repo>@<commit>?review=<id>` |
| a range or a branch review | `gg://<repo>@<base>..<tip>?review=<id>` |

- Shas are full. The id is the review note's id (8 hex), a lookup key only.
- Everything that ignores hints (`gg diff`, `gg compare`, `gg show`) sees
  exactly the reviewed change.
- A `?review=` hint with no address is refused at parse time (like
  `?preview=`). `gg://<repo>?review=<id>` is reserved for reviews of
  uncommitted changes (the working-reviews spec), not built here.
- Resolution: the review is found by id; when its revs do not match the
  link's address the link is refused (`the link does not match review <id>`)
  rather than guessed at.
- Machine-local, like `?version=`: reviews live in the local note store, so a
  link only resolves on this machine. Stated in the skill.

## 2. Domain

- `ReviewLink(ctx, id) (model.Link, error)` — builds the link above from the
  stored review (`ReviewRevs` + `LinkRepo`).
- `ReviewByLink(ctx, link) (Review, error)` — finds by id, checks the address
  (§1); `ErrNoteNotFound` for a deleted review.
- `ReviewRemarks(ctx, Review) ([]ReviewRemark, error)` — the remarks in
  document order: `N` (0-based index — the same index the read-time note
  ids `<ReviewNoteIDPrefix><id>:<n>` use, so stage 2 can answer by it),
  `Path`, `Side`, `Start`, `End`, `Summary`, `Rationale`, `Meta`, and
  `Link` — the remark's own line link: `gg://<repo>/<path>@<commit>:<a>[-<b>]`
  for a single-commit review, `…@<base>..<tip>:…` for a range; an old-side
  remark uses `:old:`. A prose review (no document) has no remarks.
- `ResolveLink` passes the hint through as for `version` (no new address
  logic).

## 3. CLI (agent B)

- `gg link --review <id>` — prints the review's link (the construct path,
  next to `--version`). Exit 1 for an unknown id.
- `gg review show <link|id> [--json]` — text:

  ```text
  review 1a2b3c4d · Claude Code · 2026-10-05 14:02 · abc1234..def5678 (feat/x)
  <overview markdown>

  <meta>

  [0] internal/a.go:12-14 — A is unused (severity: nit)
      nothing calls it
      gg://repo/internal/a.go@<base>..<tip>:12-14
  [1] …
  ```

  `--json`: `{id, agent, created, branch, base, tip, link, overview, meta,
  remarks: [{n, path, side, start, end, summary, rationale, meta, link}]}`.
  A prose review prints its text; `remarks` is empty. Flags precede the
  positional (as `gg review` already requires). Exit 0 / 1 (unknown or
  deleted review, address mismatch) / 2 (malformed link, usage).
- `gg link resolve <link>` reports `review <id>` beside the address.
- `gg open <link>` and `gg session navigate <link>` land on the review
  (§4) in a running TUI / gg web.
- MCP: `gg_review_show` (same JSON), and `gg_link_resolve` reports the hint.

## 4. TUI

Copy gg link on every surface that shows a review or note row (each copy
records in `gg links` via `RecordLink`, as every copy action does):

| Where | Menu | Copies |
|---|---|---|
| commit's Reviews row | Open review · **Copy gg link** · Delete review | the review link |
| commit's Range review row | Open range review · **Copy gg link** · Delete range review | the pair `gg://<repo>@<a>..<b>` (full shas, `ScopeAtCommit`) |
| commit's Notes row | Open notes · **Copy gg link** · Delete notes | `gg://<repo>/<path>@<sha>` |
| opened review view (`.`) | + **Copy review link** | the review link |
| Branches review sub-row | + **Copy gg link** | the review link |
| View all notes review row | + **Copy gg link** | the review link |

`gg note list <link>` already reads the pair and file-at-commit links, so
agent B reads a Range review or Notes row's notes with no new verb.

Landing: a pasted (`#`), `--at` or `gg session navigate` review link opens
the review view exactly as enter on its row does (`openReviewFrom`, back to
the commit). A deleted review: a notice ("That review no longer exists"),
then the address's commit or range opens as usual.

## 5. gg web

The same rows' right-click menus gain **Copy gg link** (the review row's
menu: Open review · Copy gg link · Delete review; the Range review and Notes
rows likewise; the Branches review sub-row too). A navigate to a review link
opens the review (`openReview`). The review link comes from the server — a
new `GET /api/review/{id}/link` (only the server knows the review's revs);
the pair and file-at-commit links are built the way the page already builds
links (`linkFor` over `/api/link-base`). Every copy records through
`copyLink` (`/api/linkhist`).

## 6. Docs

`internal/agentskill/using-gg.md`: the `?review=` link row, `gg review show`,
`gg link --review`, the machine-local note; bump `agentskill.Version`.
`reviewing-with-gg` skill: a short "checking another agent's review" recipe.
CHANGELOG, README (the review paragraph), `docs/CLAUDE-details.md`.

## 7. Errors

| Case | Behaviour |
|---|---|
| review deleted | `gg review show` exit 1 `review <id> not found`; TUI/web notice, then the address opens |
| address ≠ review's revs | refused everywhere (exit 1 / notice) |
| `?review=` without an address | parse error (exit 2) |
| prose review | text printed, no remarks; JSON `remarks: []` |
| repo unknown on this machine | the existing resolver errors (exit 1) |

## 8. Testing

- model: parse/print round-trip of both forms; address-less `?review=`
  refused; the hint-kind list in the parse error.
- domain: `ReviewLink` for a single-commit and a range review; `ReviewByLink`
  mismatch + deleted; `ReviewRemarks` order, `N`, old-side and range links.
- CLI: `gg link --review`, `gg review show` text and `--json` golden, exit
  codes.
- TUI: the three row menus (now three items each), Copy gg link text, the
  review view / Branches / All notes rows, paste landing + deleted-review
  notice; i18n gates.
- web: the link endpoint, right-click menus checked in a real browser
  (visibility asserted), navigate landing.
- e2e scenario: save a review → `gg link --review` → `gg review show` →
  `gg diff <link>` shows the reviewed change.

## Out of scope

Answers per remark (stage 2), a second review answering the first (stage 3),
links to reviews of uncommitted changes (needs the working-reviews store).
