# gg web `#` switch ask Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans.

**Goal:** the web `#` asks to switch to a link's other checkout and lands it there (TUI parity).
**Spec:** docs/superpowers/specs/2026-10-29-web-link-switch-design.md

## Global Constraints
- Words: `that link names another checkout: <checkout>`, options `switch there` / `stay`.
- The command is re-resolved server-side; the replay rides `steerAsk` (`steerReplayTTL`).
- After every static/*.js edit run `go test ./internal/web`.

## Review Focus
- A link into a worktree of THIS repo (soft switch) and into another repo (reload): both land.
- A session overview link into the other worktree lands as file_focus.
- `stay` / esc closes with nothing armed.
- A switch refused (op busy) leaves no surprise landing on a later unrelated switch (TTL + other-target drop).
- The page's checkout moved to the link's in between → 409, no replay.

### Task 1: `POST /api/link-switch` (internal/web/linkswitch.go)
**Produces:** `func (s *Server) handleLinkSwitch(w, r)`; `func (s *Server) armSwitchReplay(target string, c *steer.Command)` in steer_ask.go (nil c = arm nothing, drop any parked ask).
- [ ] RED tests in internal/web/linkswitch_test.go: `TestLinkSwitchLandsAfterTheSwitch` (file link into wt2 → POST → adoptService(wt2) → start-at navigate f.txt), `TestLinkSwitchSessionOverview` (→ file_focus ov1), `TestLinkSwitchRepoLinkArmsNothing`, `TestLinkSwitchThisCheckoutRefused` (409), `TestLinkSwitchBadLink` (422).
- [ ] Implement; `go test ./internal/web -run LinkSwitch` PASS; commit.

### Task 2: page confirm (static/live.js, static/ops.js)
- [ ] RED pin test (linkswitchjs_test.go): live.js gotoLink has the confirm words + posts `/api/link-switch` + `doReroot`; ops.js ESCAPE_WORDS has `stay`.
- [ ] Implement; `go test ./internal/web` PASS; commit.

### Task 3: docs, probe, gate
- [ ] CHANGELOG, docs/web-tui-parity.md row, CLAUDE-details note; browser probe (Playwright) both lanes; `./test.sh race` "all green"; final review; ask before merge.
