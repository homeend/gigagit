# Task 3 — the notification centre, and the two push repairs

**Depends on task 0.** Read `README-web-parallel-tasks.md` first.

The TUI notices things and offers to fix them: a push that cannot be tracked
because the clone's fetch refspec does not cover the branch, tags on the tip
that the remote does not have, and a running list of such offers behind `!`.
The browser notices none of it — pushes succeed while the ↓↑ markers never
move, and nobody is told why.

## Your files

| Server | Client |
|--------|--------|
| `internal/web/notifications.go` (new) | `static/notifications.js` (new) |
| `internal/web/op_push_extra.go` (new) | |
| `internal/web/notifications_test.go`, `oppushextra_test.go` (new) | |

Rows go in with `registerRows("branch", …)`; the centre mounts itself with
`mountOverlay`. Do not edit `ophttp.go`, `server.go`, `sidebar.js`, `ops.js`
or `index.html`.

## What already exists

- `engine.AddFetchMappings{Remote string, Branches []string}` — adds a fetch
  refspec for exactly those branches and fetches them. Empty is a no-op; the
  op declares a RefWrite lock. This is the repair for "the push worked but the
  remote-tracking ref never moved", which happens on single-branch and shallow
  clones.
- `engine.PushTags{Remote string, Names []string}` — one `git push` for a set
  of tags. The TUI runs a fresh `git ls-remote --tags` with a 5-second budget
  before offering, and skips the prompt entirely if the check times out, so
  `P` never hangs. Mirror that budget; do not block a push on a slow remote.
- The TUI's related-option prompts and notification centre: ids are permanent
  (a suppressed prompt is remembered by id), and `promptstate` is where
  suppression lives — `internal/promptstate` already backs the web's other
  remembered answers (`/api/uistate`, approved tool commands).
- The tag rows already carry a `▲` marker for "this tag exists on the default
  remote", so the data the tip-tag prompt needs is partly there.

## Work

1. **Detect the unmapped-branch case** after a push: the branch pushed, but
   its remote-tracking ref did not move. Offer *add a mapping and fetch* /
   *not now*, and remember a declined answer by a stable id so it does not
   nag. `AddFetchMappings` does the work.
2. **Tip tags on push.** Before pushing the checked-out branch, if its tip
   commit carries local tags, run the bounded `ls-remote --tags` check; if any
   are missing upstream, offer *push branch + tags* (default) / *push branch
   only* / *cancel*. On the first, push the branch (the existing rejection
   recovery still applies) and then chain `PushTags` for the tip tags only.
   Tags further back in history are none of this prompt's business.
3. **The notification centre.** `GET /api/notifications` returns the current
   offers: every branch affected by the mapping problem, and anything else you
   add later. A bell/`!` control opens the list; each row has its action and a
   *dismiss* that persists. One batch action for "fix every affected branch"
   is the TUI's behaviour and the reason the centre exists.
4. **Suppression is permanent and per-id.** Store it through `promptstate`
   (machine-local), never in browser storage: `gg web` binds a random port, so
   every run is a different origin and `localStorage` starts empty.

## Acceptance

- Go tests: a repo whose fetch refspec covers only one branch reports the
  affected branches; running the repair adds the refspec and the tracking ref
  moves; the tip-tag check finds a local-only tag and pushing with tags pushes
  exactly it; a dismissed notification stays dismissed across a new `Server`.
- Browser, control run first: the centre opens with a row for the affected
  branch, the fix runs and the row disappears; dismissing a row and restarting
  the server **on a different port** leaves it dismissed (this is the test that
  catches storage in the wrong place).
- `./test.sh race` green. CHANGELOG bullet. `registerHelp` row.

## Notes

- The 5-second budget matters. A remote that is unreachable must not turn a
  push into a hang; skip the prompt and push normally.
- Notification ids are forever: once shipped, an id is a promise to the state
  file. Choose them as carefully as you would a config key.
- Do not let the centre poll on its own timer. Refresh it where the client
  already refreshes after an operation.
