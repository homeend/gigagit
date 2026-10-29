# gg web: opened review / commit overviews join the "Overviews & notes" tree — design

Parity row `docs/web-tui-parity.md` "Overviews & notes tree: stored
overviews" (WEAKER). The TUI registers a stored overview — a review's
(`srcReviewOverview`) or a commit's permanent one (`srcCommitOverview`) —
among its open files when it opens, so ctrl+\'s tree lists it with the files
it uses at its commit (`@ <sha>`). The web opens it page-local ("never
registered", spec 2026-10-11-web-overview-split §4, plan
2026-10-19-documents-tree-2-web ruling 1 — kept simple then, no deeper
reason), so the tree never shows it. This makes the web do what the TUI does.

## TUI behaviour (the spec)

- Opening one (the review view's ≡ Overview row, a commit's / branch's
  Overviews row, a steered or pasted link) registers it; the row reads
  `○ Overview: <label>  overview · <n> anchors` (`●` while on screen; no
  author / age). It is never evicted over the cap (`touch` skips `d.ov`).
- Expanded, one child per distinct PATH anchor (a stored overview's note
  anchors name nothing), in the order first named; a child not open reads
  `○ <path>  @ <sha7>` (a working review: `working tree`); an open child is
  the open file at that commit.
- `enter` on the row shows it again (a permanent one is read again: an
  agent may have re-saved it); on a child, the overview beside that file at
  its first anchor there, at the overview's commit.
- `esc` on it closes it (out of the list); `ctrl+]` backgrounds it (stays
  listed); after a `ctrl+]`, `esc` backgrounds it again (`escDoc`'s
  `backgrounded`). `x` in the tree closes it. `D`: "nothing to delete on
  this row". `y`: its gg link.
- A deleted permanent overview drops out of the list.

## Web design

### Server (`internal/web`)

- New key sources `review-overview` / `commit-overview`: `ofKey{Src, Rev:
  <id>, Path: "overview-<id>.md"}` — never `overview`, so `followDocs`
  (which drops `overview` ids the store lacks), `overviewByID` and
  `findSessionOverview` never see one.
- The entry carries `stored {kind: "review"|"overview", id, tip}`, a title
  (`Overview: <label>` — the review's label as the review view words it;
  the permanent overview's title), its anchor count and its children
  (`{path, anchor, rev: tip}`), filled when it is registered. Never
  evicted over the cap; closeable (not `pinned`).
- Wire: the steer row says `source: "overview"` with the title (what the
  TUI's `openFilesProto` sends for all three kinds — `gg session files`
  reads the same); the page's row (`ofRow`) adds `stored` and its children.
- `POST /api/open-files {op: "stored", kind, id, tab}`: the server reads the
  overview (`svc.Overview` / `svc.Review` + `ReviewOverview` + `ReviewRevs`)
  — never values from the wire — registers or refreshes the entry, fronts
  it, sets the tab's shown file, answers `{file}`. A gone one: 404.
- `background` marks the entry `backgrounded`; `close` without `everywhere`
  on a backgrounded stored entry backgrounds it (the TUI's `escDoc`); `x`
  (everywhere) always closes.
- `focus` works on a stored entry as on any other (another tab's enter, an
  agent's `file_focus f<n>`).

### Page (`static/`)

- `openStoredOverview` registers (`op: "stored"`) and keeps the answered id
  on the stored doc; `viewerFileId` answers it; `esc` sends `close`,
  `ctrl+]` `background` (both lanes: the one-document viewer and the
  split). The rest of the stored mode (anchors at the tip, remarks, Back,
  the `!f.stored` guards on list changes) is unchanged.
- Tree: a stored row draws the TUI's text; its children match an open
  `commit` row of `(tip, path)` (a working review's: the worktree row);
  an unopened child's meta is `@ <sha7>` / `working tree`. `enter` / the
  row menu's open re-read it (`/api/review/<id>` / `/api/overview/<id>`)
  and show it; `open-at` shows it at that child's anchor. `y` copies its
  link (`copyOverviewLink` with `stored`). `D` says "nothing to delete on
  this row". A read that 404s closes the row and says the overview (review)
  no longer exists.
- Gone: the counts' `overview_ids` refresh (`closeGoneOverview`) also
  closes list rows of permanent overviews no longer among them.

## Rulings

- Server reads the document on register (a few git note reads per open,
  off the page's paint: the overview shows first, the register rides the
  open-files queue) — the wire never supplies a title, tip or path.
- Wire `source` stays `overview` for steer readers (TUI parity); the page
  tells a stored row by `stored`, never by source alone.
- A deleted review's overview row is found gone on its next open (404), not
  by a watcher — the TUI drops only gone permanent overviews too.

## Tests

- Server: register lists it (title, anchors, children with rev, wire
  source `overview`, `stored`); followDocs keeps it; cap never evicts it;
  esc-close removes; background then close keeps; everywhere-close removes;
  a gone id 404s; a review's tip; a working review's children carry no rev.
- Page: source pins for register/close/background wiring, the tree's
  stored row/child text, open-at, `D`, the gone close. Browser probe: open
  a permanent overview and a review overview, ctrl+] each, ctrl+\ tree
  lists both, expand → `@ sha` children, enter re-shows, child enter lands
  at the anchor, x closes, a second tab sees the rows.
