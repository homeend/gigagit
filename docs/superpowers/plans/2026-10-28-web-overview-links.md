# gg web overview links — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans (inline, this session — the repo forbids implementer subagents). Steps use checkbox (`- [ ]`) syntax.

**Goal:** gg web copies every overview's `gg://` link from the same places the TUI does, and opens a session overview link instead of refusing it.

**Architecture:** One server helper, `landSessionOverview`, finds a session overview by id only (reloading the docs once) and, when found, focuses it and hands the page a `file_focus` wire instead of the navigate. `/api/session/steer`, `/api/link-command` and the start-at hand-out (which also carries a Switch replay) run it. A miss leaves the navigate as it was, so the page falls back to the permanent overview, and the page's 404 arm gets its own words for an address-less navigate. Links are built server-side; the page only copies them with `copyLink`.

**Tech stack:** Go (`internal/web`, `internal/cli`, `internal/tui`), the embedded SPA (`internal/web/static/*.js`), node-based JS tests and source pins in `internal/web/*js_test.go`.

**Spec:** `docs/superpowers/specs/2026-10-28-web-overview-links-design.md`

## Global constraints

- Refusal copy (verbatim): `this overview is not saved to a worktree — no link`; `overview <id> no longer exists`; `a session overview link opens in the gg terminal or gg web, not the IDE`.
- The lookup is by id only. Never use `openFiles.resolve`'s path fallback to find the overview.
- Every copy goes through `links.js` `copyLink(link, desc)`. The desc is `overview: <title>`; review rows keep their `review: <id>` desc.
- Every new TUI string goes through `i18n.T` and into all four bundles (`internal/i18n/lang/{ja,ko,zh,ru}.toml`).
- Run `go test ./internal/web` after every `static/*.js` edit (source pins).
- TDD. "Watch it fail" means taking out the guard with sed and putting it back with sed, never `git checkout --`.
- Probe servers run with a scratch `XDG_STATE_HOME` and `XDG_CONFIG_HOME`.
- Commits use `gg add` + `git commit -F` and end with the session's Co-Authored-By and Claude-Session lines.

## Review focus

1. A session overview link steered at a page that has never loaded the docs (written a moment ago) is found after the one reload, not answered "no longer exists". Covered by Task 1's test.
2. An open working-tree FILE named like the overview id must not be focused in its place (the TUI fix bea3f794). Covered by Task 1's test.
3. A Switch from the "wants to show" notice replays the session overview after the re-root and opens it, rather than 404ing as a permanent overview. Covered by Task 1's start-at test.
4. A permanent overview link (`@<sha>?overview=`) must still open the permanent overview and never take the session path. Covered by Task 1's negative case.
5. A memory-only store (`gg web` outside a worktree inbox, steering off) refuses copy with the TUI's words, and never copies `gg://?overview=` with an empty path. Covered by Task 4's test.

---

### Task 1: Server — land a session overview navigate

**Files:**
- Create: `internal/web/session_overview.go`
- Modify: `internal/web/openfiles.go` (add `overviewByID`), `internal/web/steer.go` (`/api/session/steer` navigate branch, `handleStartAt`), `internal/web/linkcommand.go` (drop the 422 refusal, call the helper), `internal/web/steer_ask.go` (`steerWorktreeBound` clause, notice `where`)
- Test: `internal/web/session_overview_test.go`; flip the refusal in `internal/web/linkcommand_test.go`

**Interfaces:**
- Produces:
  - `func isSessionOverviewNav(w steerWire) bool`: `w.Cmd == "navigate" && w.HintKind == model.OverviewHintKind && w.File == "" && w.Commit == "" && w.State == ""`.
  - `func (r *openFiles) overviewByID(wt, id string) (steer.OpenFile, bool)`: `findLocked` by id, `Source == "overview"`, with no path fallback.
  - `func (s *Server) findSessionOverview(ctx context.Context, id string) (steer.OpenFile, bool)`: `overviewByID` on `s.service().Root()`; on a miss with `agentdocs.ValidID(id)`, `s.docs.Reload(s.docsRoot(ctx))` + `s.followDocs()` and try once more.
  - `func (s *Server) landSessionOverview(ctx context.Context, w steerWire) steerWire`: when `isSessionOverviewNav(w)` and the overview is found, it runs `ofs.focus(wt, id, "")` + `broadcastOpenFiles(wt, "")` and returns `steerWire{Cmd: "file_focus", FileID: id}`; otherwise it returns `w` unchanged.

- [ ] **Step 1: Write the failing tests** in `session_overview_test.go`. Reuse the docs fixture from the `linkcommand_test.go` refusal test (write `ov1.md` into `linknav.SessionDocsDir(cd, top)`).
  - `TestLinkCommandOpensASessionOverview`: `GET /api/link-command?link=<SessionOverviewLink(top,"ov1")>` returns 200 with `steer.cmd == "file_focus"` and `steer.file_id == "ov1"`. Write the file AFTER `serve(...)` so the reload path is exercised.
  - `TestLinkCommandSessionOverviewGoneIsTheNavigate`: id `ov9`, which is not on disk. Expect 200 with `steer.cmd == "navigate"`, `hint_kind == "overview"`, `hint_id == "ov9"` and no `commit`.
  - `TestLinkCommandSessionOverviewNotAFileNamedLikeIt`: background-open a working-tree file `ov2` (create it in the repo; use the open-files test helper that adds a background entry). Link to `ov2`, which is not a doc. Expect `steer.cmd == "navigate"` and NOT `file_focus`.
  - `TestLinkCommandSessionOverviewOtherWorktree`: a link for another worktree returns `{"checkout": <wt2>}`. This is the unchanged `#` answer.
  - `TestSteerSessionOverviewNavigateFocusesIt`: POST `/api/session/steer` with the navigate from `linknav.Command` returns a reply with `ok:true` and a detail containing `focused overview`, and the hub emits a `file_focus` wire with `file_id ov1`. Use the steer test's hub-subscribe helper.
  - `TestStartAtSessionOverviewHandsFileFocus`: `setStartAt` the navigate, then `GET /api/session/start-at` returns `steer.cmd == "file_focus"`.
  - `TestSteerAskSessionOverviewOtherWorktree`: a navigate with `Worktree: wt2`, overview hint, and no file or commit is parked (`maybeAskSteer` ok), and the notice title is `an agent wants to show overview ov1 in <base>`.
  - Negative case (Review focus 4): `isSessionOverviewNav` is false for a wire with `Commit` set.
  - Flip `linkcommand_test.go`'s 422 test into `TestLinkCommandOpensASessionOverview`, or delete it if it is a duplicate.
- [ ] **Step 2: Run `go test ./internal/web -run 'SessionOverview|LinkCommand|StartAt|SteerAsk'`.** Expected: FAIL. You get the 422 "opens only in the gg terminal", a navigate where `file_focus` was expected, and an empty `where`.
- [ ] **Step 3: Implement** the four functions above.
  - `handleLinkCommand`: remove the refusal so a session overview goes through the checkout check. A same-checkout link: `linknav.Command`, then `toSteerWire`, then `freezePair`, then `wire = s.landSessionOverview(ctx, wire)`.
  - `/api/session/steer`: after `maybeAskSteer`, for `c.Cmd == "navigate" && isSessionOverviewNav(wire)`, if `findSessionOverview` finds it, rewrite `c` to `Cmd "file_focus"`, `FileID = c.HintID`, hint cleared, and answer `s.steerFileFocus(ctx, c, steerWire{Cmd: "file_focus", FileID: c.HintID})`. A miss falls through to the plain emit.
  - `handleStartAt`: `at = landSessionOverview(r.Context(), *at)` before writing. That covers `gg open --web` starting a server and a Switch replay.
  - `steerWorktreeBound`: add `|| c.HintKind == model.OverviewHintKind && c.Commit == ""`.
  - `steerAskNotice`: when `where == ""` and `c.HintKind == model.OverviewHintKind`, use `where = "overview " + c.HintID`.
- [ ] **Step 4: Run the Step 2 tests, then all of `go test ./internal/web`.** Expected: PASS. For each guard (the id-only check, the reload, the `steerWorktreeBound` clause), take it out with sed, watch its test fail, and put it back with sed.
- [ ] **Step 5: Commit** `feat(web): open a session overview link`.

### Task 2: Page — land the server's answer, and the gone case

**Files:**
- Modify: `internal/web/static/live.js` (`gotoLink`, `steerNavigateOverview`)
- Test: the source pins in `internal/web/steerhintjs_test.go` (update any that pin these bodies), plus a new pin in `internal/web/session_overview_test.go`

**Interfaces:** Consumes Task 1's `file_focus` answer from `/api/link-command`.

- [ ] **Step 1: Write the failing source pins.**
  - `gotoLink` contains `body.steer.cmd === "file_focus" ? steerFileFocus(body.steer) : steerNavigate(body.steer)`.
  - `steerNavigateOverview`'s catch contains `if (e.status === 404 && !s.commit && !s.file && !s.state) return opLine("overview " + s.hint_id + " no longer exists", true);`, and this guard comes BEFORE the "opened its commit" arm.
- [ ] **Step 2: Run `go test ./internal/web -run 'Pin|SteerHint|SessionOverview'`.** Expected: FAIL (substring missing).
- [ ] **Step 3: Make both edits.**
- [ ] **Step 4: Run `go test ./internal/web`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): land a session overview from #, and say when it is gone`.

### Task 3: TUI notice names the overview

**Files:**
- Modify: `internal/tui/steer_switch_ask.go` (`steerAskNotice`); `internal/i18n/lang/{ja,ko,zh,ru}.toml` (`"overview %s"`)
- Test: `internal/tui/overview_link_nav_test.go`

- [ ] **Step 1: Write the failing test** `TestSteerAskNoticeNamesASessionOverview`. Call `steerAskNotice(&steerSwitchAsk{cmd: steer.Command{Cmd: "navigate", HintKind: model.OverviewHintKind, HintID: "ov-1", Worktree: "/x/wt2"}, from: "an agent"}, "")` and check that `.title` is `an agent wants to show overview ov-1 in wt2`.
- [ ] **Step 2: Run it** with `go test ./internal/tui -run SteerAskNotice` (timeout 400s). Expected: FAIL (`wants to show  in wt2`).
- [ ] **Step 3: Implement.** When `where == ""` and the hint is overview, use `where = i18n.T("overview %s", a.cmd.HintID)`. Add the key to all four bundles.
- [ ] **Step 4: Run `go test ./internal/tui`** (timeout 400s; this includes the i18n gates). Expected: PASS.
- [ ] **Step 5: Commit** `fix(tui): the switch notice names a session overview`.

### Task 4: Copy the link from every web overview surface

**Files:**
- Create: `GET /api/session-overview/link` in `internal/web/session_overview.go`
- Modify:
  - `internal/web/static/openfiles.js`: `treeKeyAction` `y` → `"copy-link"` on an overview node; the key switch; `FOOT_FILES` button `y copy link` after `enter open`; the foot click `data-oact="y"`; a `nodeMenu` row "Copy gg link".
  - `internal/web/static/overviews.js`: an `overviewMenu` row "Copy gg link" between Export as HTML and Delete overview.
  - `internal/web/static/viewer.js`: `L` in `overviewKey` and `splitOverviewKey`, plus chip `L copy link` in the split, stored and session foot variants.
  - `internal/web/static/index.html`: the help row at line 363 gets "copy gg link"; any switcher help lists `y`.
- Test: `internal/web/session_overview_test.go`, `internal/web/openfilesjs_test.go` (node, `treeKeyAction`), and source pins.

**Interfaces:**
- Produces `GET /api/session-overview/link?id=<id>`:
  - `400` when the id fails `agentdocs.ValidID`.
  - `404 "overview <id> no longer exists"` when `s.docs.OverviewIn(root, id)` misses.
  - `422 "this overview is not saved to a worktree — no link"` when `AttachedWorktree(root) == ""`.
  - Otherwise `{"link": domain.SessionOverviewLink(wt, id)}`.
- The page gets one helper, `copyOverviewLink(ov)` in `overviews.js`, which `viewer.js` and `openfiles.js` import:
  - a session overview: `/api/session-overview/link?id=`
  - `stored.kind === "overview"`: `/api/overview/{id}/link`
  - `stored.kind === "review"`: `/api/review/{id}/link`
  - It then calls `copyLink(d.link, "overview: " + title)`, or `"review: " + id` for a review. An error goes to the `opLine` error tone.
  - Signature: `copyOverviewLink({id, title, stored})`.

- [ ] **Step 1: Write the failing tests.**
  - Go `TestSessionOverviewLinkEndpoint`: one attached case returns 200 with the link equal to `domain.SessionOverviewLink(top, "ov1")`, plus the bad-id 400 and the gone 404.
  - Go `TestSessionOverviewLinkMemoryOnly`: a server whose docs store is not attached (steering off: `[ui] agent_steering = false` in the repo config) returns 422 with the exact copy.
  - Node: `treeKeyAction({kind:"overview"}, "y", false) === "copy-link"`, and `"none"` for a child, a group and a file.
  - Source pins: `FOOT_FILES` contains `<button data-oact="y">y copy link</button>` right after `enter open`; `overviewMenu`'s rows are in the order Open / Export as HTML / Copy gg link / Delete; the three viewer foot variants contain `<button data-vact="olink">L copy link</button>`; `overviewKey` and `splitOverviewKey` have `case "L": copyOverviewLink(`.
- [ ] **Step 2: Run `go test ./internal/web`.** Expected: FAIL on each.
- [ ] **Step 3: Implement** the endpoint, `copyOverviewLink`, and each surface.
  - The tree node passes `{id: n.doc.id, title: n.doc.title}`.
  - `overviewMenu` passes `{id, title, stored: {kind: "overview", id}}`. Look the title up the way `confirmDeleteOverview` does.
  - The viewer passes `{id: split ? split.id : view.id, title: ov.title, stored: ov.stored}`.
- [ ] **Step 4: Run `go test ./internal/web`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(web): copy an overview's gg link`.

### Task 5: CLI routes a session overview to gg web

**Files:**
- Modify: `internal/cli/open.go` (lines 71–74, 118–135, 144–146); `internal/cli/session.go` (lines 749–755)
- Test: `internal/cli/overview_link_nav_test.go`

- [ ] **Step 1: Flip the tests.**
  - `TestSessionNavigateSessionOverviewRefusesWebIDE` becomes `TestSessionNavigateSessionOverviewToWebAndIDE`. `--to ide` gives exit 2 with `a session overview link opens in the gg terminal or gg web, not the IDE`. `--to web` with a live web presence: model it on the existing TUI-presence test, using the web presence helper the session tests use. It must post to the web without a refusal.
  - `TestOpenSessionOverviewWebRefused` becomes `TestOpenWebSessionOverviewNotRefused`. With no live page and `LaunchWeb` stubbed (or whatever seam `openWeb` uses to start `gg web`; find it), the start-at command is the hint-only navigate.
  - New `TestOpenSessionOverviewReachesALoneWebPage`: with only a live web presence in wt2, `gg open <link>` steers the page and does NOT call `LaunchTUI`.
- [ ] **Step 2: Run `go test ./internal/cli -run 'SessionOverview'`.** Expected: FAIL.
- [ ] **Step 3: Implement.**
  - Drop the `--web` refusal and the `res.SessionOverview()` routing branch in `open.go`; the ordinary `routeFor(preferredInbox(dir))` applies.
  - In `session.go`, refuse only `dir.to == "ide"`, and remove `dir.to = "tui"`.
  - Rename the const to `sessionOverviewNotIDE`, with the new copy.
- [ ] **Step 4: Run `go test ./internal/cli`.** Expected: PASS.
- [ ] **Step 5: Commit** `feat(cli): a session overview link opens in gg web`.

### Task 6: Docs and skill

**Files:** `CHANGELOG.md`; `docs/web-tui-parity.md` (the overview-links row, or add one); `docs/CLAUDE-details.md:7550-7555` (replace "Web/IDE refuse it until the web follow-up." with the web landing: `landSessionOverview`, the copy endpoint, `L`/`y`); `internal/agentskill/using-gg.md:382` ("(not gg web or the IDE yet)" becomes "or gg web (not the IDE)"); bump `agentskill.Version`, run `gg init --update`, and commit the regenerated `.claude/skills`.

- [ ] **Step 1: Edit the docs.** Run `go test ./internal/agentskill`. Expected: PASS (the version guard).
- [ ] **Step 2: Commit** `docs: gg web overview links`.

### Task 7: Verify

- [ ] **Step 1: Probe.** Use Playwright on the scratch fixture `/tmp/claude-1000/-work-gigagit/b096b659-fbe3-466f-bdcc-00425577c928/scratchpad/fx`, with a scratch XDG and a second worktree. Cover:
  - `y` in ctrl+\ on the overview row, the foot button and the right-click row: the clipboard holds the link.
  - `L` in the session overview, the split view and a stored permanent overview.
  - The commit Overviews row menu "Copy gg link".
  - Open via `#`, `gg session navigate --to web`, and `gg open --web` with no server running.
  - The gone link: "overview … no longer exists".
  - The other-worktree notice wording, and Switch landing on the overview.
  
  Screenshot each, then compare with the TUI captures already taken (ctrl+\ footer `[y] copy link`, the overview `.` menu with Copy gg link between Export as HTML and Copy overview, the status line `Copied link: gg://…`).
- [ ] **Step 2: Race gate.** Run `./test.sh race > <log> 2>&1`, then read the verdict in a separate call. It must say "all green".
- [ ] **Step 3: Final review.** A read-only review subagent on the most capable model reviews `git diff main...HEAD` against the spec and this plan. Fix Critical and Important findings with RED→GREEN; ledger the minors.
- [ ] **Step 4:** Merge main into the branch if it moved, re-run the gate, then ask the user to merge.
