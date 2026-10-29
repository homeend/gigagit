# Overview-link minors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix the five minors deferred by the gg web overview links review.

**Architecture:** Server-side changes in `internal/web` (lookup made pure, an
attach-on-miss, an id-only landing tail, a refusal reply) plus one client
change in `internal/steer.PostHTTP` so the CLI reads a 200 refusal.

**Tech Stack:** Go 1.26, net/http, httptest.

**Spec:** docs/superpowers/specs/2026-10-29-overview-link-minors-design.md

## Global Constraints

- Refusal wording is the TUI's: `overview <id> no longer exists`.
- No TUI change, no new i18n strings, no static/*.js change.
- Tests that pin XDG state (`sessOvServer`) are not parallel; new pure tests call `t.Parallel()`.
- Race gate `./test.sh race` green ("all green") before asking to merge.

## Review Focus

- A Switch replay into a worktree whose docs dir holds the overview, reached before `adoptService` attaches: lands it (Task 4 test).
- A hosted page (`s.hosted`) never attaches on its own — the terminal's store is shared (Task 4 guard).
- `PostHTTP` on a 200 with an unparseable / empty body: stays nil (delivered), never an error (Task 3 table row).
- `NotifyReload` uses `PostHTTP` and swallows errors: unaffected (no test change needed; read the call).
- A permanent overview of that id that exists: the navigate still answers 202 (Task 3 test).

---

### Task 1: Pure session-overview lookup (spec §5, §2)

**Files:** Modify `internal/web/session_overview.go`, `internal/web/steer.go` (handleStartAt), `internal/web/linkcommand.go`. Test: `internal/web/session_overview_test.go`.

**Interfaces — Produces:** `func (s *Server) sessionOverviewFocus(ctx context.Context, w steerWire) steerWire` — replaces `landSessionOverview`; the lookup with no `ofs.focus` / `broadcastOpenFiles`. Used by link-command and handleStartAt (`readCtx(r)`).

- [ ] **Step 1: Failing test** `TestLinkCommandSessionOverviewLeavesTheListOrder`: sessOvServer; write `ov1.md`, `s.followDocs()`; open a worktree file `ofKey{Src:"worktree",Path:"f.txt"}` so it is first; record `s.ofList(root)` ids; GET `/api/link-command` for ov1 → 200 file_focus ov1; `s.ofList(root)` ids unchanged.
- [ ] **Step 2: Run** `go test ./internal/web -run 'TestLinkCommandSessionOverview' -count=1` — Expected: FAIL (ov1 moved first).
- [ ] **Step 3:** Rename to `sessionOverviewFocus`, drop the focus + broadcast; update the doc comments and both callers; handleStartAt passes `readCtx(r)` (Ruling: no honest RED for readCtx — `rootc` is cached by attach before any request).
- [ ] **Step 4: Run** `go test ./internal/web -run 'SessionOverview|StartAt|LinkCommand' -count=1` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(web): a session overview link's lookup moves nothing`.

### Task 2: Id-only landing in steer (spec §3)

**Files:** Modify `internal/web/steer_files.go`, `internal/web/steer.go`. Test: `internal/web/session_overview_test.go`.

**Interfaces — Produces:** `func (s *Server) landOpenFile(ctx context.Context, c steer.Command, wire steerWire, f steer.OpenFile) steer.Reply` — the tail of `steerFileFocus` from `cursor` on; `ofs.focus` returning false → `steerFail(c, "overview "+f.ID+" no longer exists")` when `f.Source == "overview"`, else `steerFail(c, "no open file "+f.Path)`, nothing emitted. `steerFileFocus` calls it after its resolve; handleSteer's overview branch calls it with `findSessionOverview`'s file.

- [ ] **Step 1: Failing test** `TestLandOpenFileGoneSinceTheLookup`: sessOvServer + startLive; `landOpenFile` with `steer.OpenFile{ID:"ov1",Source:"overview"}` not in the list (and a worktree file `ov1` open, the path-fallback bait) → `!rep.OK`, `rep.Error == "overview ov1 no longer exists"`, focused entry is not the bait.
- [ ] **Step 2: Run** — Expected: FAIL to compile (no `landOpenFile`).
- [ ] **Step 3:** Extract; wire both callers.
- [ ] **Step 4: Run** `go test ./internal/web -count=1 -run 'Steer|SessionOverview|FileFocus'` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(web): a steered session overview lands by id only`.

### Task 3: Refuse a gone overview; the CLI reads it (spec §4)

**Files:** Modify `internal/web/steer.go`, `internal/steer/notify.go`. Tests: `internal/web/session_overview_test.go`, `internal/steer/notify_test.go`, `internal/cli/overview_link_nav_test.go`.

**Interfaces — Consumes:** `svc.Overview(ctx, id)`, `domain.ErrOverviewNotFound`. **Produces:** `PostHTTP` returns `errors.New(rep.Error)` for a 200 whose body decodes to a Reply with `!OK && Error != ""`.

- [ ] **Step 1: Failing tests**
  - Rewrite `TestSteerSessionOverviewMissIsTheNavigate` → `TestSteerSessionOverviewGoneIsRefused`: navigate hint ab12cd34 → 200, `!rep.OK`, `rep.Error == "overview ab12cd34 no longer exists"`, and the navigate still reaches the tabs (eventsFor).
  - `TestSteerPermanentOverviewNavigateIsSent`: `svc.UseNotesDir`, `SaveOverview` at HEAD, navigate with its id → 202.
  - `TestPostHTTPReadsARefusal` (steer): 200 `{"ok":false,"error":"overview x no longer exists"}` → that error; 200 `{"ok":true}` → nil; 200 empty body → nil; 202 → nil.
  - CLI: `gg session navigate` to a lone fake web answering the refusal → stderr `web: overview … no longer exists`, exit 1.
- [ ] **Step 2: Run** each — Expected: FAIL (202 / nil error / "web: sent").
- [ ] **Step 3:** Implement both halves; grep `web: sent` pins (open_web_test, session_test, s89) still hold.
- [ ] **Step 4: Run** `go test ./internal/web ./internal/steer ./internal/cli -count=1` — Expected: PASS.
- [ ] **Step 5: Commit** `fix: a gone session overview is refused to the agent, not "web: sent"`.

### Task 4: Attach on a miss (spec §1)

**Files:** Modify `internal/web/session_overview.go`. Test: `internal/web/session_overview_test.go`.

- [ ] **Step 1: Failing test** `TestStartAtBeforeTheDocsAttach`: sessOvServer; write `ov1.md`; `s.detachDocs(ctx)` (the window adoptService leaves); setStartAt navigate ov1; GET start-at → file_focus ov1, and `s.docs.AttachedWorktree(root) != ""`.
- [ ] **Step 2: Run** — Expected: FAIL (navigate comes back).
- [ ] **Step 3:** In `findSessionOverview`'s miss path: `if !s.hosted && s.docs.AttachedWorktree(root) == "" { s.attachDocs(ctx) }` before `Reload`.
- [ ] **Step 4: Run** `go test ./internal/web -count=1` — Expected: PASS.
- [ ] **Step 5: Commit** `fix(web): a Switch replay finds docs not yet attached`.

### Task 5: Docs + gate

- [ ] CHANGELOG entry; docs/CLAUDE-details.md:876 notes a 200 refusal is printed; check using-gg.md for "web: sent" promises (bump Version only if changed).
- [ ] `./test.sh race > <ws>/race.log 2>&1`; read verdict ("all green"); commit `docs: overview-link minors`.
- [ ] Final read-only review subagent; ask before merge.
