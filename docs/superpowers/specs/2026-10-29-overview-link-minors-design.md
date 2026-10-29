# Overview-link minors — design

The five minors the final review of gg web overview links deferred
(merge e888a746; spec 2026-10-28-web-overview-links-design.md). Each is a
small server fix; one branch, one commit each. No TUI change, no new
strings, no page change.

## 1. A Switch replay lost in a standalone gg web

`adoptService` (internal/web/reroot.go) tells the tabs "switched" before
it calls `attachDocs`. A tab fast enough to reach
`/api/session/start-at` in that gap looks the replayed session overview
up in a store not yet attached to the new worktree, misses, and the page
says "overview <id> no longer exists".

Fix: `findSessionOverview`'s miss path attaches the shown worktree's docs
when this standalone server's store has none attached for it
(`!s.hosted && s.docs.AttachedWorktree(root) == ""` → `s.attachDocs(ctx)`)
before its one `Reload` + `followDocs`. `agentdocs.Attach` on a root
already attached to the same directory is a no-op under its lock, so the
`attachDocs` adoptService runs a moment later stays harmless. Any lookup
path is covered, not only the replay; `adoptService` is unchanged.

## 2. `handleStartAt` reads with the request's context

It calls `landSessionOverview(r.Context(), …)`; every other caller uses
`readCtx(r)` (a tab reload cancels the request, and a read must not fail
half-way because of that). Change it to `readCtx(r)`.

## 3. The steer path's re-resolve by path

`handleSteer` finds a session overview by id only (`findSessionOverview`),
then hands it to `steerFileFocus`, which resolves again with
`openFiles.resolve`, whose path fallback can match an open file NAMED
like the id when the overview went in between.

Fix: split `steerFileFocus` so its landing tail (cursor, focus, broadcast,
emit, reply) takes the already-resolved `steer.OpenFile`. The overview
branch passes the file `findSessionOverview` found; `steerFileFocus`
keeps its own resolve and calls the same tail. The tail checks
`ofs.focus`'s bool: an entry gone since the lookup answers
`steerFail` ("overview <id> no longer exists" for an overview, else
"no open file <name>") and emits nothing.

## 4. "web: sent" for an overview that is gone

A session overview navigate whose id is neither a session document nor a
permanent overview still answers 202, so the agent reads "web: sent"
while the page says "overview <id> no longer exists". The TUI answers it
with a refusal (`onOverviewHint`).

- Server: on the session miss, `handleSteer` asks `svc.Overview(ctx, id)`.
  `domain.ErrOverviewNotFound` → the navigate still goes to the tabs (the
  page shows its line, as the TUI shows its status) and the answer is
  `steerFail(c, "overview <id> no longer exists")` — the TUI's words.
  Found, or any other error, → 202 as today.
- CLI: `steer.PostHTTP` treats every 2xx as delivered. It now decodes a
  200 body as a `steer.Reply` and returns its `Error` when `!OK`; 202 and
  a 200 with `OK` stay nil. `gg session navigate` / `gg open --web` then
  print `web: overview <id> no longer exists` and exit 1 when only the
  page was live.
- Side effect (intended, TUI parity): the server's other 200 refusals on
  this path — the ask-to-switch ("gg web is showing worktree X; asked the
  user to switch to Y") and a retired verb's fix — now reach the agent
  instead of "web: sent", as the TUI's replies already do.

## 5. `GET /api/link-command` reorders the open files

`landSessionOverview` focuses the overview in the server's list and
broadcasts it. Its two callers, the # prompt's GET and the start-at GET,
hand the page a `file_focus` the page lands with `openViewer({id})`,
which already POSTs `focus` itself. So the lookup becomes pure: it
returns the `file_focus` wire and changes nothing (the docs `Reload` on a
miss stays: it syncs a cache with the disk, it moves nothing). A GET of
either endpoint leaves the open-files order as it was.

## Docs

CHANGELOG entry. using-gg.md only if it promises "web: sent" for these
cases (then a line + Version bump + `gg init --update`).
