# PR follow-ups 5 + skill text Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the follow-ups-4 fix pass's "declined to judge" items and the web's lingering fetch line, then ship plan 5's skill text (its read-only MCP tools are postponed — user ruling 2026-10-08).

**Architecture:** Small, independent fixes in domain (link resolution), TUI (repo switch), CLI (`gg session navigate`), web (`prs.js`), then the `reviewing-with-gg` and `using-gg` skill text.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla JS (node-tested helpers), embedded skills.

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` (§7 item 5 = read tools + skill text; read tools postponed by the user 2026-10-08).

## Global Constraints

- An AI agent never sends anything to GitHub; nothing posts to real GitHub.
- A test fixture that writes a PR note stamps it (`Preview "main...refs/gg/pr/7"`, CLI `--preview main...refs/gg/pr/7`, web `"pr":7`); a review a test sends is saved on the PR.
- `ResolveOpts.UnfetchedPR` stays set ONLY by `linknav.Resolve` and CLI open/navigate.
- CLI/engine prose stays English; every new TUI string goes through `i18n.T` in all four bundles.
- Skill change → bump the counter, regenerate the dogfood copy with an isolated HOME.

## Review Focus

1. A `?review=` link to a PR whose ref was forgotten but whose objects remain: must navigate (the landing fetches), not refuse with "review X compared …".
2. `gg link resolve` / `gg compare` on a link to an unfetched PR: the refusal must say how to fix it (`gg pr fetch <n>`), not "holds both main and refs/gg/pr/7".
3. A repo switch while a PR fetch runs: the old repo's PR must never open in the new one.
4. `gg session navigate` on an unfetched PR link: the agent learns the fetch is the reason, and gets the real answer when the fetch is quick.
5. The web status line after a PR fetch succeeds: no "⟳ fetching…" left behind.

---

### Task 1: A review link to an unfetched PR navigates

**Files:** Modify `internal/domain/review_link.go` (`checkReviewHint`); Test `internal/domain/linkresolve_pr_test.go`.

The resolver hands a navigation an unfetched PR as `Resolved.Preview{Source, Target}` with an empty `Tip` and empty `Commit`; `checkReviewHint` then compares that empty commit with the review's tip and refuses ("review X compared <tip>") whenever the tip's objects are still here (`gg pr forget` deletes the ref, not the objects).

- [ ] **Step 1: failing test** `TestAReviewLinkToAnUnfetchedPRNavigates`: repo with `feat/x`, `update-ref refs/gg/pr/7 feat/x`, save a review on the PR's preview (`main...refs/gg/pr/7`, stamped as the constraints say), then `update-ref -d refs/gg/pr/7`; `ResolveLink(link?review=<id>, ResolveOpts{UnfetchedPR: true})` returns no error and `res.Preview.Tip == ""`.
- [ ] **Step 2: run** `go test ./internal/domain -run TestAReviewLinkToAnUnfetchedPRNavigates` — Expected: FAIL with `review … compared`.
- [ ] **Step 3: implement** — in `checkReviewHint`, before the match: an unfetched PR's preview (`res.Preview != nil && res.Preview.Tip == ""`) passes — there is nothing here yet to compare; the landing fetches the head and the review view says what it finds.
- [ ] **Step 4: run** the test and `go test ./internal/domain` — Expected: PASS.
- [ ] **Step 5: commit** `fix(links): a review link to an unfetched PR navigates`.

### Task 2: Inspections of an unfetched PR link say how to fix it

**Files:** Modify `internal/domain/linkresolve.go` (the "holds both" refusal in candidate selection); Test `internal/domain/linkresolve_pr_test.go`.

Ruling (product call, recommended): `gg link resolve`, `gg link text`, compare, review and MCP keep refusing an unfetched PR's link — they inspect real data — but the refusal names the fix.

- [ ] **Step 1: failing test** `TestAnUnfetchedPRLinkInspectionSaysFetchIt`: a link `@main...refs/gg/pr/7` on a repo without the ref, `ResolveLink(..., ResolveOpts{})` → error contains `pull request #7 is not fetched here` and `gg pr fetch 7`.
- [ ] **Step 2: run** — Expected: FAIL (message is "holds both main and refs/gg/pr/7").
- [ ] **Step 3: implement** — when the preview's source parses as a PR ref (`git.ParsePRRef`) and some candidate holds the target, the refusal reads `%w: pull request #%d is not fetched here — run gg pr fetch %d first (gg open fetches it)`; other previews keep today's text.
- [ ] **Step 4: run** the test, `go test ./internal/domain ./internal/cli ./internal/linknav ./internal/mcp` — Expected: PASS (update any test pinning the old text for a PR source only).
- [ ] **Step 5: commit** `fix(links): an unfetched PR link's refusal says gg pr fetch`.

### Task 3: A repo switch drops a pending PR open

**Files:** Modify `internal/tui/model.go` (`reRoot`); Test `internal/tui/pr_link_test.go`.

- [ ] **Step 1: failing test** `TestARepoSwitchDropsAPendingPROpen`: model with `pendingPROpen = &PR #7`, `running = true`; `reRoot(otherRepo)`; then deliver a successful `opFinishedMsg` — no PR preview opens (`previewOpen == nil` after draining) and `pendingPROpen == nil`.
- [ ] **Step 2: run** `go test ./internal/tui -run TestARepoSwitchDropsAPendingPROpen` — Expected: FAIL.
- [ ] **Step 3: implement** — `m.pendingPROpen = nil` beside `m.pendingSteer = nil` in `reRoot`, commented like its neighbours.
- [ ] **Step 4: run** — Expected: PASS; `go test ./internal/tui -run 'PR|Steer|ReRoot'` green.
- [ ] **Step 5: commit** `fix(tui): a repo switch drops a pending PR open`.

### Task 4: `gg session navigate` waits for an unfetched PR's landing

**Files:** Modify `internal/cli/session.go` (navigate's wait), `internal/cli/link.go` if the resolution must report "unfetched PR"; Test `internal/cli/link_unfetched_pr_test.go`.

Ruling (product call, recommended): a navigate whose link resolved to an unfetched PR (`Preview != nil && Tip == ""`, PR source) waits up to 30 s (`steerPRFetchWaitForTest`) instead of 2 s, and its timeout line reads `queued: the TUI is fetching pull request #<n> — the view opens when the fetch finishes`. Exit 0 as today (a slow answer is not a failure). Every other navigate keeps 2 s and its line.

- [ ] **Step 1: failing test** `TestNavigateToAnUnfetchedPRWaitsForTheFetch` (serial, seams shortened): a live TUI presence that never answers; navigate an unfetched PR link → stdout holds `the TUI is fetching pull request #7`; a second case where the reply arrives after the short 2 s seam but before the PR seam → the reply is printed.
- [ ] **Step 2: run** — Expected: FAIL.
- [ ] **Step 3: implement** — pass the wait and the line into `sendSteer` from navigate (a small struct or two params); default call sites unchanged.
- [ ] **Step 4: run** `go test ./internal/cli -run 'Navigate|Session|Unfetched'` — Expected: PASS.
- [ ] **Step 5: commit** `feat(cli): session navigate waits for an unfetched PR's fetch`.

Item "a hung fetch parks the landing": Ruling — keep. The hold is deliberate; the op shows on the status line and esc/cancel ends it, which fails the landing (`failPRLanding`). Task 4 covers the agent's side. No code.

### Task 5: The web's PR fetch says it finished

**Files:** Modify `internal/web/static/prs.js` (`fetchPR`'s onDone); Test: node test if `prs.js`'s done handler can be reached from the existing node harness, else a helper in `prfresh.js` (`prFetchDoneLine(ev, n)`) node-tested in `prfreshjs_test.go`; browser probe `probe/pfu4.mjs` on old and new builds.

- [ ] **Step 1: failing test** — the done line for `{ok:true, summary:"fetched …"}` is the summary; for `{ok:true}` it is `fetched pull request #7`; for `{ok:false,error:"x"}` it is `error: x`.
- [ ] **Step 2: run** — Expected: FAIL (helper missing).
- [ ] **Step 3: implement** — `fetchPR`'s onDone calls `opLine(prFetchDoneLine(ev, n), !ev.ok)`, as `pr-forget` already does.
- [ ] **Step 4: run** node test + `go test ./internal/web`; browser probe: old build keeps `⟳ fetching pull request head #7…` after landing, new build shows the summary.
- [ ] **Step 5: commit** `fix(web): a PR fetch's status line says it finished`.

### Task 6: Skill text (plan 5) + docs

**Files:** `internal/agentskill/reviewing-with-gg.md` (ReviewVersion 15 → 16), `internal/agentskill/using-gg.md` + `agentskill.go` (Version 158 → 159), `.claude/skills/*` regenerated, `CHANGELOG.md`, `docs/CLAUDE-details.md`.

- [ ] **reviewing-with-gg** — new section "Reviewing a pull request": `gg pr list` → `gg pr view <n>` / `gg pr comments <n>` (what reviewers already said, thread ids) → `gg pr fetch <n>` → read `gg diff <base>...refs/gg/pr/<n>` → notes with `--preview <base>...refs/gg/pr/<n>` or one review with `gg review save "$(gg link --pr <n>)"` → draft replies `gg pr reply <n> <thread> <text>` (no `--send`) → `gg pr notes <n>` to check → hand back `gg link --pr <n>`. Sending is the user's: never `gg pr send`, never `gh`.
- [ ] **using-gg** — `gg note list`: the `[sending]` / `[failed: <error>]` tokens after `[source]`, and JSON `sync` (local|sending|failed|github), `send_error`, `group` (`mine`, `review:<id>`, `github:<id>`); the Task 2 refusal; the Task 4 wait.
- [ ] Skill tests (`go test ./internal/agentskill`), regenerate with `HOME=$T XDG_CONFIG_HOME=$T/c XDG_STATE_HOME=$T/s ./bin/gg init --update`.
- [ ] CHANGELOG "Pull request follow-ups 5" (skill v159, reviewing-with-gg v16; MCP read tools postponed); CLAUDE-details PR-links paragraph.
- [ ] Commit `docs: PR follow-ups 5 + PR review skill text (skill v159)`.

### Finish

Race gate (`./test.sh race`, "all green"), final whole-branch review (read-only subagent, most capable model), fix pass, ask before merging.
