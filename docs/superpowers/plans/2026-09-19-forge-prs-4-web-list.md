# Forge PRs 4 — web: list, open, forget, live `prs` — Implementation Plan

> Executed inline by the session that wrote it (project rule: NO subagents).
> Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `gg web` lists pull requests in a sidebar section, opens one as a PR
diff on the compare page, forgets one, and re-reads the list on `[refresh] prs`.

**Architecture:** A server-side `prCache` (the `remoteTagCache` posture) is
the only thing `GET /api/pr` reads; one lane (`loadPRs`) talks to the forge
and emits the live source `prs`. Two registered ops wrap
`PRFetchOp`/`PRForgetOp`; `GET /api/pr/open?n=` resolves `PRPair`
server-side and answers the preview-open shape, so the client reuses
`openPreviewBody`.

**Tech stack:** Go `net/http`, the embedded vanilla-JS SPA, node-run JS tests.

**Spec:** `docs/superpowers/specs/2026-09-19-forge-prs-web-design.md` (§2–§5),
under `docs/superpowers/specs/2026-09-19-forge-prs-design.md`.

## Global constraints

- Read-only. The page sends a PR NUMBER only (R1); no PR endpoint takes a ref.
- `GET` handlers never call the forge (R2).
- No usable forge → section never shown (R3). Hide by ID rule, not `.hidden`.
- `prs` polls regardless of `[refresh] enabled` (R4); `0` = off; floored by
  `min_seconds`; still skipped while an op is in flight.
- `internal/web` non-test code must not import `internal/forge` or
  `internal/git` (archtest).
- New tests call `t.Parallel()` unless they touch globals.
- Commits carry the two trailers.

## File map

| File | Role |
|---|---|
| `internal/web/prs.go` (new) | `prCache`, `prRow`, `loadPRs`, `GET /api/pr`, `POST /api/pr/refresh`, `GET /api/pr/open` |
| `internal/web/op_pr.go` (new) | `pr-fetch`, `pr-forget` builders |
| `internal/web/ophttp.go` | `opStartRequest.Number` |
| `internal/web/live.go` | `prs` lane outside the enabled gate; `onPRs` hook |
| `internal/web/server.go` | `prs prCache` field |
| `internal/web/prs_test.go`, `op_pr_test.go`, `live_prs_test.go` (new) | handler/op/lane tests, fake provider |
| `internal/web/opcommitedit_test.go` | `TestMain`: `domain.ForgeDisabled = true` |
| `internal/web/static/prs.js` (new) | section, rows, open, menu, refresh |
| `internal/web/static/prsrow.js` (new, import-free) | `prRowParts(pr, now)` — node-testable |
| `internal/web/prsrowjs_test.go` (new) | node test for `prsrow.js` |
| `static/index.html`, `style.css`, `core.js` (SECTIONS), `sidebar.js`, `live.js`, `previews.js`, `app.js`, help source | wiring |
| `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md`, `docs/web-tui-parity.md` | docs |

---

### Task 1: the PR list lane — `prCache`, `GET /api/pr`, `POST /api/pr/refresh`

**Files:** create `internal/web/prs.go`, `internal/web/prs_test.go`; modify
`server.go` (field), `opcommitedit_test.go` (TestMain).

**Interfaces produced:**
```go
type prRow struct {
	Number      int    `json:"number"`
	Title       string `json:"title"`
	Author      string `json:"author"`
	State       string `json:"state"`        // open|closed|merged|unavailable
	Draft       bool   `json:"draft"`
	ReviewState string `json:"review_state"` // lower-case, as model has it
	Source      string `json:"source"`
	Target      string `json:"target"`
	URL         string `json:"url"`
	Updated     string `json:"updated"`      // RFC3339, "" when zero
	HeadSHA     string `json:"head_sha"`
	Fetched     bool   `json:"fetched"`
}
func (s *Server) loadPRs(svc *domain.Service)             // forge call + store + emit "prs"
func (s *Server) prsSnapshot(svc *domain.Service) (state string, rows []model.PullRequest, errText string, loading bool)
func (s *Server) cachedPR(svc *domain.Service, n int) (model.PullRequest, bool)
func (s *Server) dropCachedPR(svc *domain.Service, n int)  // a non-open row leaves on forget
func (s *Server) kickPRs(svc *domain.Service)             // one background loadPRs per service
```
`Fetched` comes from a domain query; if none exists add
`func (s *Service) PRFetched(ctx) map[int]bool` in `internal/domain/forge.go`
(ForEachRef over `git.PRRefPrefix`, fail-open) with a domain test.

- [ ] **Step 1 — test seam.** `TestMain`: `domain.ForgeDisabled = true`.
  Add a test fake in `prs_test.go`:
  ```go
  type fakeForge struct {
  	mu      sync.Mutex
  	detect  error
  	open    []model.PullRequest
  	byN     map[int]model.PullRequest
  	lists   int
  }
  ```
  implementing `forge.Provider` (`Name` "fake", `Comments` empty, `BaseRepo`
  returns `("", <bare repo path>, nil)`, `HeadRefspec` `refs/pull/<n>/head`),
  and `prServe(t, dir, f) (*httptest.Server, *Server)` that does
  `svc.SetForgeProviders([]forge.Provider{f})`.
- [ ] **Step 2 — failing tests** (`prs_test.go`):
  - `TestPRListUnavailableStaysHidden`: `detect` = error → poll `GET /api/pr`
    until `loaded`; expect `available:false`, `prs` = `[]`.
  - `TestPRListLoadsInBackgroundAndEmits`: subscribe to the hub, first GET
    answers `loaded:false` without blocking on the provider (provider blocks
    on a channel until released), release → a `prs` liveMsg arrives → GET has
    the row with lower-case `review_state`, `state:"open"`.
  - `TestPRListGetNeverCallsForge`: after load, 5 GETs → `f.lists == 1`.
  - `TestPRRefreshReloads`: `POST /api/pr/refresh` → `f.lists == 2`, answers
    the new list; a provider error keeps old rows and sets `error`.
  - `TestPRRefreshIsWriteGuarded`: a cross-origin POST is refused (copy the
    assertion shape from `writeguard_test.go`).
  - `TestPRCacheIsPerService`: after `s.setService(other)` (whatever reroot
    uses) the snapshot is unprobed again.
- [ ] **Step 3 — run, watch them fail** (`go test ./internal/web -run TestPR`).
- [ ] **Step 4 — implement `prs.go`.** Routes via `RegisterRoutes`.
  `loadPRs`: mark `loading`; `svc.ForgeStatus(ctx)` (30 s budget) →
  unavailable ⇒ state `off`; else `svc.PullRequests` ⇒ state `ready`, rows or
  `err`; clear `loading`; emit `liveMsg{Changed: []string{"prs"}, Reason:
  "prs"}` through `liveHubRef()` (nil-safe). Every mutation checks
  `c.svc == svc` and resets on a new service. `[]` never `null` on the wire.
- [ ] **Step 5 — green; `go vet`; commit** `feat(web): pull-request list lane — cached, forge-free reads`.

### Task 2: `pr-fetch`, `pr-forget`, `GET /api/pr/open`

**Files:** create `internal/web/op_pr.go`, `op_pr_test.go`; modify
`ophttp.go` (`Number int \`json:"number"\``), `prs.go` (open handler).

- [ ] **Step 1 — failing tests:**
  - `TestPRFetchOpBringsTheHead`: fixture = work repo + bare `base.git`
    holding `refs/pull/7/head` (helper `prFixture(t)`, remote `origin` →
    bare; fake `BaseRepo` fallback = the bare path). POST `/api/op`
    `{op:"pr-fetch", number:7}` → follow the run to done ok →
    `refs/gg/pr/7` resolves; a `prs` liveMsg was emitted after the run;
    `GET /api/pr` row has `fetched:true`.
  - `TestPRFetchRejectsBadNumbers`: `0`, `-1` → 400; unknown `99` (fake
    returns `forge.ErrNotFound`) → 422.
  - `TestPROpenAnswersThePreviewShape`: after fetch, `GET /api/pr/open?n=7`
    → `state:"ok"`, 40-hex `left`/`right`, `source_hash` = the PR head,
    `pr:7`, `label` starts `PR #7`.
  - `TestPROpenUnfetched`: before fetch → `state:"unfetched"`, 200.
  - `TestPROpenUnknownNumber`: `n=99` → 404; `n=abc` → 400.
  - `TestPROpenMergedUsesBaseSHA`: row MERGED, target tip already contains
    the head → still `state:"ok"` with files (PRPair's base sha).
  - `TestPRForgetDropsRefAndRow`: closed row known only by its ref →
    `pr-forget` → ref gone, row gone from `GET /api/pr`, `prs` emitted.
- [ ] **Step 2 — watch them fail.**
- [ ] **Step 3 — implement.** Builders validate `req.Number > 0`. `pr-fetch`:
  `svc.PRFetchOp(r.Context(), n)`; `errors.Is(err, forge…)` is unreachable
  from web (no forge import) → any builder error is 422. The `cleanup` slot
  of `OpBuilder` is the post-run hook: `func() { s.kickPRsForce(svc) }` —
  reload + emit (for `pr-forget`, `dropCachedPR` first). Open handler:
  `cachedPR` → `svc.PRPair` → if the head ref does not resolve
  (`svc.PRFetched`) answer `unfetched`; else `svc.PreviewOpen(ctx, pair.Head,
  pair.Base)` (the `unfetched` verdict is a LIVE ref check — the post-run
  cache reload races `onDone → showPR`, so the cache's `fetched` is never
  consulted here) and write the shared shape (factor `previewOpenBody(eps, label,
  source, target) map[string]any` out of `writePreviewOpen`).
- [ ] **Step 4 — green; commit** `feat(web): pr-fetch / pr-forget ops and the PR open endpoint`.

### Task 3: live source `prs`, independent of `[refresh] enabled`

**Files:** modify `internal/web/live.go`; create `live_prs_test.go`.

- [ ] **Step 1 — failing tests** (use the `liveTick`/`liveNow` seams as
  `live_test.go` does; these are global → serial):
  - `TestLivePRsTicksWithRefreshDisabled`: `cfg.Enabled=false`, `PRs=nil`
    (→300): advance the clock 301 s, `tickOnce` → `onPRs` called once;
    no other source emitted.
  - `TestLivePRsZeroIsOff`: `PRs=&0` → never called.
  - `TestLivePRsFlooredByMinSeconds`: `PRs=&3`, min 10 → not at 5 s, yes at 10 s.
  - `TestLivePRsSkippedWhileOpInFlight`: gate true → not called, and
    `lastRun` not stamped.
  - `TestStartLiveRunsTickLoopForPRsAlone`: refresh disabled + prs on → the
    hub's tick loop is running (observable through `onPRs` with a tiny
    `liveTick`).
- [ ] **Step 2 — watch them fail.**
- [ ] **Step 3 — implement.** `liveHub.onPRs func(*domain.Service)`;
  `prsInterval(cfg) (int, bool)`; in `tickOnce`, after the op gate and BEFORE
  `!cfg.Enabled`, the `prs` due-check (own `lastRun["prs"]`, seeded in
  `newLiveHub`); `startLive`: `if !cfg.Enabled { if prs on { go h.tickLoop(svc) }; return }`.
  `startLive` wires `h.onPRs = func(svc) { if s.prsState(svc) == "ready" { s.kickPRs(svc, true) } }`
  — a KICK, never an inline load: `tickOnce` is one lane and a gh list takes
  seconds. An unprobed or `off` cache is never polled. The page needs no
  change for R4: `connectLive` applies `changed` whatever the hello's `live` says.
- [ ] **Step 4 — green; commit** `feat(web): live source prs polls on [refresh] prs, outside the master switch`.

### Task 4: the sidebar section

**Files:** create `static/prs.js`, `static/prsrow.js`, `prsrowjs_test.go`;
modify `index.html`, `style.css`, `core.js`, `sidebar.js`, `live.js`,
`previews.js`, `app.js`, the help source; check `uistate.go` for a
section-name allowlist and add `prs`.

- [ ] **Step 1 — failing node test** (`prsrowjs_test.go`, pattern of
  `rendercelljs_test.go`): `prRowParts(pr, nowMs)` returns
  `{mark, word, title, tip, dim}`:
  approved→`✓`, changes_requested→`✗`, review_required→`●`, else `""`;
  `word` = `draft` | `closed` | `merged` | `unavailable` | `""`;
  `dim` for non-open; `tip` = `author · source → target · 3h ago`;
  an upper-case `APPROVED` yields NO mark (guards the plan-2 mistake).
- [ ] **Step 2 — watch it fail; implement `prsrow.js`; green.**
- [ ] **Step 3 — markup + CSS.** `index.html`: after the previews pair,
  `<div id="prs-header" class="side-header hidden">pull requests</div><ul id="prs-list" class="hidden"></ul>`;
  `style.css`: `#prs-header.hidden, #prs-list.hidden { display: none; }`,
  `.prmark`, `.prword`, `li.prdim`. A Go static test
  (`prs_static_test.go`) asserts the ID-scoped rule exists — the
  `web-hidden-class-is-per-id` trap.
- [ ] **Step 4 — `prs.js`.** `fetchPRs()` (`runOnce("prs", …)`): GET; when
  `!loaded` schedule the 1/2/4 s backoff; `available` → un-hide + render;
  header error → `title`. `applySection` draws a `⟳` control for `prs`
  (label "pull requests"); its click → `postJSON("/api/pr/refresh")` →
  render, `opLine` on failure. Row click → `openPR(pr)`:
  open row, or no `fetched` → POST `/api/op {op:"pr-fetch", number}` +
  `followOp(id, "fetching PR #n", "pr-fetch", onDone)`; `onDone` ok →
  `showPR(n)`; not ok → `opLine(error)`; both → `fetchPRs()`.
  `showPR(n)`: GET `/api/pr/open?n=` → `unfetched` ⇒ fetch path once;
  else `openPreviewBody(body)`. Context menu: open · copy URL
  (`navigator.clipboard` through the app's existing copy helper) · forget
  (non-open or fetched rows; confirm through `showLocalConfirm`).
- [ ] **Step 5 — seams in existing files.** `previews.js`: `armPreview`
  stores `pr: body.pr || 0`; `previewTitle(body)` returns `body.label` for a
  PR; `reopenPreviewIfMoved` returns early for `po.pr` (its refresh is plan
  5's job); export `openPreviewBody`. A PR body's `source`/`target` are
  DISPLAY names — sent to `/api/preview/notes` they would 404 (a fork
  branch) or, worse, read a same-named LOCAL pair's notes. So for `po.pr`:
  `armPreview` skips `loadPreviewCounts` (counts = `{}`), and files.js
  `noteQuery`/`fetchNotes` treat the diff as a plain commit diff on the head
  sha (`rev` + `state=commit`, the `/api/notes` endpoint). Plan 5 replaces
  this with `pr=n`. Row click honours `opBusy()` before posting the op. `core.js` SECTIONS += `prs`;
  `sidebar.js` COLLAPSED_DEFAULT += `prs`, header label map; `live.js`:
  `if (want.has("prs")) jobs.push(fetchPRs())`; `app.js` boot: `fetchPRs()`.
  Help: a "Pull requests" block.
- [ ] **Step 6 — `go test ./internal/web`; commit** `feat(web): pull requests sidebar section — open, copy URL, forget, refresh`.

### Task 5: browser verification, docs, gate

- [ ] **Step 1 — fixture.** Scratch `prweb/` built by a python script (no zsh
  `echo`): work repo, `base.git` with `refs/pull/7/head`, origin → the bare
  repo, `.git/fakegh/{pr-list,pr-view-7,pr-view-12,threads-7,repo-view}.json`
  with `repo-view.json` url = the bare path, a local `refs/gg/pr/12` +
  MERGED view. Wrapper script exports `GG_GH_BIN`, `XDG_STATE_HOME`,
  `XDG_CONFIG_HOME`, execs the worktree build on a fixed port; prove the
  port is mine with `curl /api/repo`.
- [ ] **Step 2 — playwright checks, each run FIRST against a build with the
  guard removed** (section un-hide commented out / `GG_GH_BIN` unset):
  section invisible without gh (`offsetParent === null` on both ids);
  visible with fake gh; rows `#7` open + `#12 merged` dimmed; click #7 →
  files title `PR #7 · …`, file list non-empty, `fetched` flips; ⟳ re-reads
  (edit `pr-list.json`, click, new title visible); forget #12 → row gone.
- [ ] **Step 3 — docs:** CHANGELOG, README web section, `docs/CLAUDE-details.md`
  (web PR section: R1–R4, the cleanup-slot hook, the prs lane),
  `docs/web-tui-parity.md`.
- [ ] **Step 4 — `./test.sh race` from a clean tree.**
- [ ] **Step 5 — stripped verify binary → SendUserFile + absolute path; ask
  before merging.** After merge: `./build.sh install`, `./build.sh web`.

## Self-review

Spec §3 → T1+T3, §4 → T2, §5 → T4, §7 → T1 seam + T5. Names are consistent:
`loadPRs`, `kickPRs`, `cachedPR`, `dropCachedPR`, `prRow`, `onPRs`,
`prRowParts`, `fetchPRs`, `openPR`, `showPR`. `kickPRsForce` in T2 = `kickPRs`
with a force flag (`kickPRs(svc, force bool)`, the `kickRemoteTags` shape).
