# PR Follow-ups 4 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (native execution) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix every deferred minor from the follow-ups-3 final review. Also let a link to a PR that is listed but not yet fetched open: today it is refused before any frontend sees it.

**Architecture:** The TUI landing fixes stay in `pr_link.go`, `pr_actions.go` and `steer_nav.go`. The web landing's order of steps moves into a pure `landPR` in `prfresh.js`, so node can test that order rather than grepping the source. The resolver gains a navigation-only option, `ResolveOpts.UnfetchedPR`. With it, a preview link whose source is `refs/gg/pr/<n>` and is absent here still locates a checkout that holds the base. The landing then fetches the PR through the paths built in follow-ups 3. Only `linknav.Opts` turns the option on, so `gg review`, MCP, compare and the other resolver callers are unchanged.

**Tech Stack:** Go 1.26, Bubble Tea, ES modules tested under node, real `git` in temp repos.

**Spec:** none. The authority is:
- the follow-ups-3 final review's deferred minors and its resolver finding (memory `github-write-feature.md`, "FOLLOW-UPS 3");
- the user's "address all follow ups" (2026-10-08);
- ruling A: the refusal stands, and only its wording changes.

## Global Constraints

- No implementer subagents. One read-only final review subagent runs at the end.
- No GitHub sends. Tests use the fake forge only.
- Every new user-visible TUI string goes through `i18n.T`, with a literal key present in all four bundles (ja, ko, zh, ru).
- Steer replies and web/CLI prose stay English.
- `gg commit` has no `-F`: commit with `gg add <paths>` followed by `git commit -F <msgfile>`.
- Before each commit, check the suite's result explicitly. The race gate counts as green only when its log says "all green".
- A test fixture that writes a PR note must stamp it.

## Review Focus

1. **A user presses enter on PR #n while an agent's landing on #n is fetching.** One fetch should run, the view should open once, and the landing should succeed.
2. **A fetch that takes longer than 5s.** The parked landing must not expire while the op runs.
3. **An unfetched PR link opened from a checkout that lacks the base too.** It should still be refused with the existing message.
4. **A resolver caller other than navigation, given an unfetched PR link.** `gg review`, MCP link tools and compare must still refuse it. Only `linknav.Opts` sets `UnfetchedPR`.
5. **A `prFetchReadyMsg` that arrives after a repo switch.** It must be dropped, and no fetch op may start in the new repo.

---

### Task 1: The web refusal says how to get the PR back

**Files:**
- Modify: `internal/web/notes.go` (the `cachedPR` miss)
- Test: `internal/web/pr_note_scope_test.go`

- [ ] **Step 1:** In `TestWebNoteForAnUnknownPRReadsNoForge`, also assert that the error contains `"search for it"`. In both "not stored" checks, call `t.Fatal` when `NoteCounts` returns an error, so the check can no longer pass on an error. This covers deferred minor 5's second half.
- [ ] **Step 2:** Run `go test ./internal/web/ -run 'TestWebNoteForAnUnknownPR|TestWebPRNoteWithTheDiffGone|TestWebPRNoteOffThePR' -count=1`.
  Expected: FAIL, because the message has no "search for it".
- [ ] **Step 3:** Change the message to:

  ```go
  fmt.Errorf("pull request #%d is not in the pull request list any more — search for it and open it again", req.PR)
  ```

  The list drops a PR once it is merged or closed on GitHub, so searching is the way back.
- [ ] **Step 4:** Run the same command. Expected: PASS. Then run `go test ./internal/web/ -count=1`. Expected: `ok`.
- [ ] **Step 5:** Commit: `fix(web): the off-list PR note refusal says to search for the PR`.

---

### Task 2: `exclusive` never leaves an unhandled rejection

**Files:**
- Modify: `internal/web/static/prfresh.js` (`exclusive`)
- Test: `internal/web/prfreshjs_test.go`

- [ ] **Step 1:** Add `TestPRFreshJSExclusiveRejects`. It counts `process.on("unhandledRejection")`, runs `ex.try(() => Promise.reject(new Error("x"))).catch(() => {})`, and waits 20ms. It asserts `count === 0` and that `ex.idle()` settles.
- [ ] **Step 2:** Run it. Expected: FAIL, with count 1, because `running = p.finally(...)` rejects with no handler.
- [ ] **Step 3:** Replace that line with:

  ```js
  const clear = () => { running = null; };
  running = p.then(clear, clear); // never rejects: the caller holds p
  ```

  Then make `idle()` read `while (running) await running;`.
- [ ] **Step 4:** Run `go test ./internal/web/ -run JS -count=1`. Expected: `ok`.
- [ ] **Step 5:** Commit: `fix(web): a failed PR open logs no second unhandled rejection`.

---

### Task 3: The web landing's order is a pure function

**Files:**
- Modify: `internal/web/static/prfresh.js` (new `landPR`)
- Modify: `internal/web/static/prs.js` (`openPRLanding` calls it)
- Test: `internal/web/prfreshjs_test.go`

**Interfaces:**
- Produces: `landPR(d, n) → Promise<true|false|null>`, where `d = {idle, known(n), fetchList, waitList, tryOpen(pr)}`. `tryOpen` returns `null` while another open runs; otherwise it returns a promise of a boolean.

- [ ] **Step 1:** Add `TestPRFreshJSLandPR`. It drives `landPR` with stub dependencies that log each call, and covers four cases:
  - The PR is known at once. Expected calls: `idle`, `tryOpen`. Result: `true`.
  - The PR becomes known after `fetchList`. Expected calls: `idle`, `fetchList`, `tryOpen`.
  - The PR is known only after `waitList`. Expected calls: `idle`, `fetchList`, `waitList`, `tryOpen`.
  - The PR is never known. Expected calls: `idle`, `fetchList`, `waitList`. Result: `null`, and `tryOpen` is not called.

  A fifth case checks that busy waits: `tryOpen` returns `null` once and then a promise of `false`. Expected calls: `idle`, `tryOpen`, `idle`, `tryOpen`. Result: `false`.
- [ ] **Step 2:** Run it. Expected: FAIL, because `landPR` is not exported.
- [ ] **Step 3:** Implement it in `prfresh.js`:

  ```js
  // landPR is a PR link landing's order (prs.js openPRLanding): a running
  // open first; then the list (a read newer than the call, then the server's
  // first live listing); then the open itself, waiting while another runs —
  // null = busy, a resolved false = failed and already said why.
  export async function landPR(d, n) {
    await d.idle();
    if (!d.known(n)) await d.fetchList();
    if (!d.known(n)) await d.waitList();
    const pr = d.known(n);
    if (!pr) return null;
    let p;
    while (!(p = d.tryOpen(pr))) await d.idle();
    return await p;
  }
  ```

  `openPRLanding(n)` becomes:

  ```js
  return landPR({ idle: () => opens.idle(), known: knownPR, fetchList: fetchPRs,
    waitList: () => listLoaded.wait(LANDING_LIST_MS), tryOpen: (pr) => opens.try(() => openPRNow(pr)) }, n);
  ```

  Keep its comment. Update `TestPRsJSLandingWaitsForAnOpenInFlight` and `TestPRsJSLandingWaitsForTheList` to assert the `landPR(` wiring instead of the inline body.
- [ ] **Step 4:** Run `go test ./internal/web/ -count=1`. Expected: `ok`.
- [ ] **Step 5:** Commit: `test(web): the PR landing's order is a node-tested landPR`.

---

### Task 4: TUI landing fetch fixes (minors 2, 3, 4, 7)

**Files:**
- Modify: `internal/tui/pr_actions.go`, `internal/tui/pr_link.go`, `internal/tui/steer_nav.go` (`expirePendingSteer`), `internal/tui/model.go` (the `prFetchReadyMsg` case, if the gen check lives there)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (one new key)
- Test: `internal/tui/pr_link_test.go`

**Interfaces:**
- Produces: `prFetchReadyMsg.gen int`, which is `m.forgeGen` at the time `openPRCmd` or `openPRLandingCmd` runs.

- [ ] **Step 1: Write the failing tests.**
  - `TestAPRLandingDoesNotExpireWhileItsFetchRuns` (minor 3): park PR #7's landing with `at` 10s ago, `m.pendingPROpen = &pr7` and `m.running = true`. After `expirePendingSteer(time.Now())`, the landing must still be parked. Once `m.running` is false, it expires as before.
  - `TestEnterOnTheSamePRDuringALandingFetchKeepsTheLanding` (minor 4): park PR #7's landing with `pendingPROpen` set to PR #7 and `running = true`. Feed `prFetchReadyMsg{pr: pr7, op: …, gen: m.forgeGen}`. Expected: the landing is still parked and no reply is written. Then feed the same message with no pending open: the landing fails, as now.
  - `TestAFileLessPRLandingSaysWhyWhenBusy` (minor 2): with no pending steer, `running = true` and `pendingPROpen = nil`, feed `prFetchReadyMsg{pr: pr7, op: …}`. Expected: `m.statusMsg == "PR #7: an operation is running — open it again when it finishes"`.
  - `TestAPRFetchReadyFromALeftRepoIsDropped` (minor 7): feed `prFetchReadyMsg{gen: m.forgeGen - 1, …}` with a valid op. Expected: `pendingPROpen` stays nil, `running` stays false, and the returned cmd is nil.
  - Update `TestPRFetchReadyChainsTheOpen` and the three landing tests from follow-ups 3 so they pass `gen: m.forgeGen`.
- [ ] **Step 2:** Run `go test ./internal/tui/ -run 'TestAPRLandingDoesNotExpire|TestEnterOnTheSamePR|TestAFileLessPRLandingSaysWhy|TestAPRFetchReadyFromALeftRepo' -count=1`.
  Expected: FAIL. The `gen` field does not exist yet, which is a build error.
- [ ] **Step 3: Implement.**
  - Add `gen int` to `prFetchReadyMsg`. `openPRCmd` and `openPRLandingCmd` capture `m.forgeGen` and set it.
  - `handlePRFetchReady`:
    - First line: `if msg.gen != m.forgeGen { return m, nil }`.
    - In the `!m.opsIdle()` arm: when `m.pendingPROpen != nil && m.pendingPROpen.Number == msg.pr.Number`, return `m, nil`. That PR's fetch is already running, and its finish opens the view the landing waits for.
    - Otherwise set `m.statusMsg = i18n.T("PR #%d: an operation is running — open it again when it finishes", msg.pr.Number)`, then call `failPRLanding` as now.
  - `expirePendingSteer`: when `ps.prNumber != 0 && m.pendingPROpen != nil && m.pendingPROpen.Number == ps.prNumber && m.running`, set `ps.at = now` and return. This holds the PR's fetch outside the TTL. Remove `restartPRLandingClock`'s call in `handlePRFetchReady`, since the hold covers it. Keep the call in op-finished, so the preview computation gets a fresh TTL. Reword both comments to match.
  - Add the new i18n key to all four bundles.
- [ ] **Step 4:** Run `go test ./internal/tui/ -count=1 > $WS/t4.log 2>&1; tail -3 $WS/t4.log`. Expected: `ok`.
- [ ] **Step 5:** Commit: `fix(tui): PR landing fetch — one fetch per PR, no TTL while it runs, stale repo dropped`.

---

### Task 5: The resolver opens a link to a listed but unfetched PR (navigation only)

**Files:**
- Modify: `internal/domain/linkresolve.go` (`ResolveOpts.UnfetchedPR`, `previewCandidates`, the preview finish in `ResolveLink`)
- Modify: `internal/linknav/linknav.go` (`Opts` sets it)
- Test: `internal/domain/linkresolve_pr_test.go` (new), `internal/linknav/linknav_test.go`, `internal/tui/pr_link_test.go`

**Interfaces:**
- Produces: `ResolveOpts.UnfetchedPR bool`. When it is set and a preview link's `Source` is a PR ref (`git.ParsePRRef`) that the checkout lacks, the link still resolves. A checkout that holds the `Target` qualifies. `Resolved.Preview` is `&PreviewNoteSet{Source, Target}` with an empty `Tip`, and `res.Commit` is empty. Without the option, behaviour is unchanged.

- [ ] **Step 1: Write the failing tests.** First check which helpers build a registry-backed resolve in the existing tests: `grep -n "func Test.*Preview" internal/domain/linkresolve*_test.go`.
  - `TestAnUnfetchedPRLinkResolvesForNavigation`: a repo with `main` and no `refs/gg/pr/7`, and the link `gg://<path>/a.txt@main...refs/gg/pr/7:3`.
    - With `UnfetchedPR: true`, the result is no error and `res.Preview.Source == "refs/gg/pr/7"`.
    - With the option off, the error is `ErrLinkUnknownRepo` containing "holds both", as today.
  - `TestAnUnfetchedPRLinkStillNeedsTheBase`: the target `nosuch` gives an error even with the option on.
  - `TestAPlainPreviewLinkIsNotRelaxed`: the source `feat/missing` gives an error with the option on. Only PR refs are relaxed.
  - linknav: `linknav.Opts(...)` has `UnfetchedPR == true`.
  - TUI end to end, in `pr_link_test.go`: `prLinkUnfetchedModel`, then `linknav.Resolve` of the link with the model's registry (or `domain.ResolveLink` with `UnfetchedPR`), then `linknav.Command`, then `applySteer`. The cmd must yield a `prFetchReadyMsg`. If `linknav.Resolve` needs the MRU registry, write one with the repo's entry, following the existing `#` paste tests (`grep -n "linknav.Resolve" internal/tui/*.go`).
- [ ] **Step 2:** Run `go test ./internal/domain/ ./internal/linknav/ -run 'UnfetchedPR|PlainPreviewLinkIsNotRelaxed|Opts' -count=1`. Expected: FAIL.
- [ ] **Step 3: Implement.**
  - `previewCandidates`: when `opts.UnfetchedPR` is set and `git.ParsePRRef(p.Source)` succeeds, a missing source does not drop the candidate. The target must still resolve.
  - `ResolveLink`'s preview arm: when `opts.UnfetchedPR` is set, the source is a PR ref, and it is missing on the chosen checkout, set `res.Preview = &PreviewNoteSet{Source: p.Source, Target: p.Target}` and return. Skip `PreviewNotes`: there is nothing to number, and the landing fetches it.
  - `linknav.Opts`: set `UnfetchedPR: true`, with a comment saying navigation lands in the PR view, which fetches the head first.
  - Check what `linknav.Command` and `AtLink` read from `res.Preview`. They read `Source` and `Target`, which is fine. A `#<hunk>` link still needs `PreviewHunkAnchor`, which fails with its own error, and that is acceptable.
  - Check every other `ResolveOpts{` site and confirm none sets the option: `linkdesc.go`, `mcp/links.go`, `web/linkcompare.go`.
- [ ] **Step 4:** Run `go test ./internal/domain/ ./internal/linknav/ ./internal/tui/ ./internal/cli/ -count=1`. Expected: all `ok`.
- [ ] **Step 5:** Commit: `feat(links): a link to a listed but unfetched PR opens and fetches it`.

---

### Task 6: Probes, docs, race gate

- [ ] **Step 1: Browser probe.** Run it on the OLD build (`~/go/bin/gg` at `5e15e268`) first, then on the new build. Use `probe/pfu4.mjs`:
  - (R-web) Delete `refs/gg/pr/7` and keep the PR listed. Run `gg session navigate <PR #7 link to big.go:5>`.
    - Old build: the CLI refuses with "holds both".
    - New build: the page fetches the PR and lands on line 5 in the PR #7 view.
  - (T1) After the PR leaves the list (`pr-list.json` set to `[]`, then a list refresh), add a note in the open view. The red line must contain "search for it".
- [ ] **Step 2: TUI probe.** Use the `tui3-*` fixtures (the PR is listed and `refs/gg/pr/7` is absent; `insteadOf` points the fetch at a local bare repo). Paste PR #7's link with `#`.
  - Old build: "cannot open link: … holds both".
  - New build: "fetching PR #7…", then the PR view on `big.go`.
- [ ] **Step 3: Docs.**
  - `CHANGELOG.md`: add a "Pull request follow-ups 4" section.
  - `docs/CLAUDE-details.md`: add a sentence about `UnfetchedPR` to "Pull request links", and remove the "Note: the link resolver itself refuses…" line.
  - `internal/agentskill/using-gg.md`: if it says a PR link needs `gg pr fetch` first, update it, bump `agentskill.Version` and regenerate the skills (`gg init --update` after merge). Otherwise leave it.
- [ ] **Step 4: Race gate.** Run `./test.sh race > $WS/race.log 2>&1; grep -c "all green" $WS/race.log`. Expected: `1`.
- [ ] **Step 5:** Commit: `docs: PR follow-ups 4`.
