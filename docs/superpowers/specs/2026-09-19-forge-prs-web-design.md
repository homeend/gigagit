# Forge pull requests in the web UI — design

Companion to `2026-09-19-forge-prs-design.md` (the feature spec, with its three
amendment sections). That document's rulings bind this one; nothing here
re-opens them. This spec covers only what `gg web` adds.

Agreed with the user 2026-09-19: a sidebar section, a help-style details
overlay, click-first collapse with free keys, two plans, a separate spec.

## 1. Scope

Read-only. `gg web` gains what the TUI has:

| TUI | Web |
|---|---|
| 5th left tab "Pull requests" | sidebar section `pull requests`, next to `previews` |
| enter = FetchPRHead → PR diff on the preview surface | row click = `pr-fetch` op → the compare page, preview-armed |
| PR hub popup (`i`) | details overlay (the `?` help overlay's shape) |
| Forget | row context menu → `pr-forget` op |
| `[refresh] prs` poll + `r` | live source `prs` + a ⟳ control on the section header |
| review threads as read-only note boxes | the same, through the web's note rows |
| `o` / `O` note collapse | click the note title; keys for one / all |

Out of scope (unchanged follow-ups): markdown, closed-PR search, write-back,
GitLab/Gitea, MCP, CI checks.

## 2. Rules that shape everything

**R1 — the wire value is the PR NUMBER.** The preview endpoints validate
`source`/`target` against the live branch lists (`knownRefName`). A PR's head
is `refs/gg/pr/<n>` and a merged PR's base is a raw sha; neither is a branch,
and neither may be accepted from the page. Every PR endpoint takes `n` (a
positive integer) and resolves the pair SERVER-side with `PRPair`. No PR
endpoint accepts a ref, a sha or a path it did not validate.

**R2 — reads never call the forge.** Web reads serialise under the repo gate
and overlapping fans stack. Only three things talk to `gh`: the list lane
(§3), the `pr-fetch` op's builder, and `POST /api/pr/comments/refresh`. `GET`
handlers answer from server memory.

**R3 — no usable forge → no UI.** Detected once per service
(`ForgeStatus`). The sidebar section is hidden until the server says
`available`; there is no notice, no empty section, no settings row.

**R4 — `prs` polls independently of `[refresh] enabled`** (ruling 13), on
`RefreshConfig.PRsSeconds()`, floored by `min_seconds`, `0` = off.

## 3. Server: the PR list lane (`internal/web/prs.go`)

`prCache` — keyed to the service pointer (the `remoteTagCache` posture: a
re-root starts unknown):

```go
type prCache struct {
	mu      sync.Mutex
	svc     *domain.Service
	state   string // "" unprobed · "off" · "ready"
	prs     []model.PullRequest
	err     string // last list failure, shown on the section header
	loading bool
}
```

`GET /api/pr` answers at once:

```json
{"available": true, "loaded": true, "error": "", "prs": [ {…} ]}
```

- unprobed → kicks ONE background `loadPRs` and answers
  `{"available": false, "loaded": false}`.
- `off` → `{"available": false, "loaded": true}` forever.
- `loadPRs(svc)`: `ForgeStatus` → `PullRequests`; stores; emits
  `liveMsg{Changed: ["prs"], Reason: "prs"}`. A list error keeps the previous
  rows and records `err`.

Row wire shape (`prRow`): `number, title, author, state` (`open` / `closed` /
`merged` / `unavailable`), `draft, review_state, source, target, url,
updated` (RFC3339), `fetched` (a local `refs/gg/pr/<n>` exists),
`head_sha`.

`POST /api/pr/refresh` (write-guarded: it spends a forge call) → `loadPRs`
synchronously, answers the list. This is the ⟳ control.

**Live:** `"prs"` joins the hub as a source handled OUTSIDE the
`cfg.Enabled` gate: `startLive` starts the tick loop when refresh is enabled
OR `PRsSeconds() > 0`; `tickOnce` runs the `prs` due-check before its
`!cfg.Enabled` return; the op-in-flight gate still applies. The tick calls
`s.loadPRs` (wired like `onRemoteTags`), which emits. The lane never runs
while the cache says `off`. `prs` is not added to `refreshSources` /
the settings table in this stage (the TUI rates row exists; a web row is a
follow-up).

The kick-then-emit race (memory `web-remote-tag-marker-feature`): the page
re-fetches on the `prs` event AND, while `loaded` is false, on a short
backoff (1 s, 2 s, 4 s, stop).

## 4. Server: open, fetch, forget

- `RegisterOp("pr-fetch")` — `req.Number` (new `opStartRequest` field
  `number`) → `svc.PRFetchOp`. A builder error is a 422 with the forge's
  message. After the run the server emits `prs` (the ref now makes the PR
  "known", and `fetched` flips).
- `RegisterOp("pr-forget")` — `svc.PRForgetOp(n)`; afterwards drops the row
  from the cache when it is not open, and emits `prs`.
- `GET /api/pr/open?n=7` — finds the PR in the cache (404 when absent: the
  page can only open a row it was shown), `PRPair` → `PreviewOpen(Head,
  Base)`, answers the `writePreviewOpen` shape plus `pr: 7` and
  `label: "PR #7 · <title>"`. `source`/`target` in the answer are DISPLAY
  names (`source` branch name / target), never sent back as refs.
  A missing local ref is `state: "unfetched"`; the page then runs the op.

Post-op emits hang off the op runner's existing completion path, by op name.

## 5. Client (`static/prs.js`)

- `index.html`: `#prs-header` + `#prs-list`, both born hidden by an
  `#prs-header.hidden, #prs-list.hidden` rule (hiding is per id in this app).
  `fetchPRs()` un-hides them only on `available`.
- Row: `#7  ✓ draft  title` — the status cell leads (TUI parity): `✓`
  approved, `✗` changes requested, `●` review required, `draft`; closed /
  merged / unavailable rows are dimmed and carry the word. `title` attribute =
  author · source → target · age.
- Click: `postJSON("/api/op", {op: "pr-fetch", number})` → `followOp(…,
  onDone)` → `GET /api/pr/open` → `openPreviewBody(body)` with a PR title.
  A closed/merged row whose ref exists opens without fetching (its head no
  longer moves); an open row always fetches first.
- `state.previewOpen.pr = n`; `previewTitle` reads the label for a PR.
  `reopenPreviewIfMoved` skips PR bodies (their refresh is §7).
- Context menu: open · details… (plan 5) · copy URL · forget.
- Header: ⟳ (manual refresh, `runOnce`-gated), error text as a `title`.
- `live.js`: `prs` in `refreshSources` → `fetchPRs()`.
- Sidebar collapse: `prs` joins the section list, collapsed by default like
  `previews`.
- Help overlay: a "Pull requests" block.

## 6. Plan 5 — review threads in the diff, collapse, details

**Domain:** `PreviewNotesAt` merges `forgeNotesFor(set, path)` exactly as
`PreviewNotesFor` does (appended after `keepResolved`; returned alone when the
file has no stored notes). Today it does not, so the counts a web read
returns already include forge threads the notes list omits.

**Server:** `GET /api/pr/notes?n=7&path=…` — the PR twin of
`/api/preview/notes` (R1: no ref on the wire). It builds the
`PreviewNoteSet` from `PRPair` and answers the same shape. `WireNote` gains
`read_only` (true for `NoteSourceForge`), `resolved`, `file_level`,
`created`. `POST /api/pr/comments/refresh?n=7` → `PRCommentsRefresh`,
answers `{changed}`. The PAGE posts it right after a PR diff opens (a GET
never calls the forge — R2); a failure leaves the diff without threads.

`POST /api/pr/details?n=7` → `PullRequest` (body) + `PRComments` (hub,
outdated, truncated). **Amended by plan 5:** this was drafted as a GET, which
breaks R2 — the PR body is not in the listing (`gh pr view` only), so the
overlay's load is always a forge call. It is therefore a write-guarded POST,
the overlay's explicit load and never a side effect.

**Client:**
- `noteQuery` uses `pr` when `state.diffCtx.preview.pr` is set.
- `noteBoxHTML`: forge title `review · author · age · path R12 [· resolved]`,
  class `forge`; the agent-layer toggle never hides it; the ◆ menu offers
  copy link / collapse only; `c` still adds a LOCAL note on a PR diff.
- File-level thread (`file_level`): one note row ABOVE the first diff row.
- **Collapse (generic, every note box):** `state.noteCollapsed` (a Set of
  root ids, per open diff), seeded once per diff with resolved forge threads.
  Click on the title toggles; collapsed = the title line + `▸`, body hidden.
  Keys chosen from what the diff view has free at plan time, advertised in
  the help overlay and the ◆ menu.
- On the `prs` live event with a PR diff open: `POST comments/refresh`; when
  `changed`, `fetchNotes()`.
- Details overlay (`prdetails.js`, the help overlay's frame): header (number,
  title, state, author, source → target, URL), description, conversation +
  review verdicts in time order, an "Outdated" section with each thread's
  hunk snippet, a truncation line. Esc closes; it never traps. Opened from
  the row menu and from a `details` chip on the PR diff's title bar.

## 7. Testing

- `internal/web` `TestMain` sets `domain.ForgeDisabled`, so no test shells
  out to the real `gh`. Forge-positive handler tests inject a fake
  `forge.Provider` with `svc.SetForgeProviders` (archtest guards non-test
  imports only, so a `_test.go` may import `internal/forge`); they need no
  env and run parallel.
- JS logic that can run under node gets a `*js_test.go` (row text, the
  collapse set, the note-title builder).
- Browser verification against the scratch fixture (memory
  `forge-prs-feature`): assert the section is INVISIBLE with no gh and
  VISIBLE with the fake one, the PR diff opens, a thread box is visible, a
  resolved one is collapsed, the overlay shows the outdated hunk — each
  assertion watched failing first.
- `./test.sh race` from a clean tree before each merge; `./build.sh web`
  after each merge.

## 8. Plans

- **Plan 4** — §3, §4, §5: list, open, forget, live `prs`, help, docs.
- **Plan 5** — §6: threads in the diff, collapse, details overlay, re-poll.
