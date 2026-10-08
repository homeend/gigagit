# GitHub write-back — PR cache minors + follow-up minors Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (the user chose NATIVE execution: no implementer subagents; one read-only final review subagent on the most capable model). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the plan-1 (PR disk cache) final-review minors and the 9 minors the follow-ups stage deferred, before plan 5.

**Architecture:** No new subsystem.
- **TUI:** the open PR's forge reads and sends drop by a repo-only generation (`forgeGen`), not by the list generation that every list read bumps.
- **Domain:**
  - The optimistic forge verdict survives a cancelled listing.
  - Prefetch reads one ref list and only the entries it may warm.
  - Only an open stamps an entry's open time.
  - A moved head's reopen counts its new commits.
- **prcache:** retries a Windows rename that a reader blocks.
- **TUI + web:** a kept body is cleared only by its own box's send.
- **Web:** runs one PR read at a time, with no give-up timer.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla ES modules (node-run JS tests), playwright probe.

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` (§2 PR cache; amended 2026-10-08: agents never send; an emptied AI-review body posts empty). The items come from the plan-1 final review (memory `github-write-feature.md`, "PLAN-1 CACHE MINORS") and the follow-ups ledger (`Final: minor (deferred)` lines, copy in session a829a3fc's scratchpad `followups-ledger.md`).

## Global Constraints

- An AI agent never sends anything to GitHub (user ruling 2026-10-08). No change here adds a send path.
- Nothing posts to real GitHub during this work. Tests use `fakeForge` / `writerForge` / fake gh only.
- The repo gate is not re-entrant. `PRSendOp` is built outside ops (web handler, TUI `tea.Cmd`).
- Every user-visible TUI string goes through `i18n.T` with a literal key in all four bundles (ja/ko/zh/ru). Plurals use two keys; prose never rides in an arg.
- Engine/CLI prose stays English.
- `prcache` stays a DAG leaf (stdlib, filelock, model).
- `web` never imports `forge`/`git` (archtest).
- Race gate is green only when the log says "all green".
- gg commit has no `-F`: `gg add <paths>` then `git commit -F <msgfile>`. Never `git add -A` (a built `bin/gg` may sit in the worktree).
- Never `git checkout -- <file>` to undo a watch-it-fail probe: re-apply the guard with Edit.
- Never `pkill -f` with a pattern in the command line: kill probe servers by PID.

## Item map

Plan-1 minors are numbered as in the starter (1–8); follow-up minors as F-a…F-i, in the ledger's order.

| # | Item | Task |
|---|------|------|
| 1 | TUI revalidate answer clears in-flight flags before its gen check; reRoot keeps `prReland`; `relandPR` checks only the number | 1 |
| 2 | a listing the caller cancelled sets `forgeDistrust` | 2 |
| 3 | prefetch reads up to 50 entries + one rev-parse per cached PR on every list refresh | 3 |
| 4 | `persistEntry` stamps `OpenedAt` on every cache use | 4 |
| 5 | Windows: `writeJSON`'s rename over a file being read fails, write dropped; tear test ignores `Save`'s error | 5 |
| 6 | `cache_hours = 0` expires the visible listed row → the open costs a `PR()` call | 11 (docs, ruling C6) |
| 7 | TUI "PR #n updated: new commits" has no count | 6 |
| 8 | `PRCommentsCached`/`PRDetailsCached` re-read disk when memory has nothing | none — note only (ruling C12) |
| F-a | TUI (and web) kept body cleared by ANY changing send | 7 (TUI), 8 (web) |
| F-b | "the text you typed is kept" shown for a failed resolve/reply | 7 |
| F-c | docs + test comments: an in-flight read "neither shows nor absorbs" (it does show "updated") | 11 |
| F-d | web `revalidate` not under `runOnce("pr-comments")`; `refreshAfterSend` gives up after 10 s | 9 |
| F-e | TUI `prRefreshAgain` not cleared when its read lands with the PR closed; dropped on a `prsGen` bump | 1 |
| F-f | `interruptedNames`: a remark with an empty summary → a blank label | 10 |
| F-g | Reply & send's `noteMutatedMsg` not under the send generation | 1 |
| F-h | `prSendBudget` (30 s) also bounds local git work in planning | 10 |
| F-i | test gaps: TUI abort keeps the kept body; web kept-body / `ev.changed` checked by source greps only; group-bar test checks the cmd's msg, not the handler | 7, 8, 9, 10 |

## Rulings proposed (for the user's review)

- **C1 (items 1, F-e, F-g): TUI forge reads and sends use one repo generation.** `forgeSendGen` is renamed to `forgeGen`; `reRoot` remains the only place that bumps it.
  - `prRevalidatedMsg.gen` carries `forgeGen`. Today it carries `prsGen`, which every PR-list read bumps (`readPRsCmd`), so a list read that started mid-revalidation silently dropped the answer, including "moved".
  - An answer from another generation touches nothing.
  - `reRoot` resets `prRevalidateInflight`, `prCommentsInflight`, `prReland` and `prRevalidateSkip` (next to the existing `prRefreshing` / `prOfflineSince` resets). The new repo's first read is never blocked, and a same-numbered PR there cannot consume a stale reland.
  - Within one generation only one read runs at a time (`prRefreshCmd` refuses while one is in flight), so no per-read token is needed.
  - `prRefreshAgain` is consumed by whichever read of that PR lands. It is asked again only if that PR is still open.
  - A Reply & send whose draft was saved before `R` saves the draft (it is in the old repo's store) but does not send.
- **C2 (item 2): a cancelled listing is indecisive.** A listing whose caller's ctx is done, or whose error is `context.Canceled` (singleflight shares one caller's cancellation), neither proves nor undoes the optimistic verdict. `forgeOptimistic` stays set, so the next decisive listing still proves it or falls back to Detect.
  - A `DeadlineExceeded` on a live ctx is gh's own `CallTimeout`, i.e. the forge stalled: that stays decisive.
- **C3 (item 3): prefetch reads only what it may warm.** It does:
  1. One `for-each-ref refs/gg/pr/` for every local head.
  2. The entry names from a directory glob (no JSON read).
  3. A `Load` only for listed-open PRs whose local head differs from the listed head; those are then ordered by `OpenedAt`.

  If the ref read fails, every listed-open PR counts as moved (today's fail path: fetch it). `Store.Entries` loses its last caller and is deleted with its test; `Store.Numbers` replaces it.
- **C4 (item 4): only an open stamps the open time.**
  - Stamping calls: `PRFetchOp` (user opens in TUI/web/CLI, and a send's head fetch) and `PullRequest` (the details view).
  - Not stamping: `PRRevalidate` (the heartbeat, a send's plan) and prefetch (through an unexported `prFetchOp(ctx, n, opened=false)`).
  - A brand-new entry is stamped on first write whatever the caller, so a zero time never evicts it first.
- **C5 (item 5): retry a transient rename.** The rename is retried for `filelock.Wait` (2 s), every `filelock.Poll` (20 ms), on `fs.ErrPermission` (Windows `ERROR_ACCESS_DENIED`) and on Windows `ERROR_SHARING_VIOLATION`. `syscall` names no constant for the latter, so a `_windows.go` file names errno 32. Anything else fails at once.
  - The rename is a `Store` field (`os.Rename` by default) so Linux tests can inject the failure.
- **C6 (item 6): document, no code.** Spec §4.5 says `cache_hours = 0` = "always read from GitHub first", and `TestPRCacheZeroHoursAlwaysReads` pins it. At 0 the visible row also reads first; §2.3's "opens at once" holds for any `cache_hours > 0`. A sentence goes in spec §2.3, the `settingDoc` and README.
- **C7 (item 7): count the new commits against the head on screen.**
  - The count is the commits the new head has that the head on screen had not (`rev-list --count old..new`), computed at the reopen. The new head is only local after the fetch, so it cannot be counted at revalidate time.
  - A force-push counts every commit not in the old head. 0, or a failure, falls back to the countless line, which stays as a key.
  - TUI only. The page keeps "updated: new commits on the forge": a count there needs the page to send its screen head (against the page-sends-only-the-number rule) or the server to remember what it served. Your call if you want web parity.
- **C8 (F-a, F-b): a kept body belongs to its box.**
  - Only a send that changed GitHub and came from the same box clears it. Same box means the same PR and the same group, or Verdict….
    - TUI: `req.BodySet` plus a match.
    - Web: request `kind` `group` with the same group, or `verdict`. `body_set` is false for "mine", so it cannot be the test.
  - A resolve, a reply, or another PR's send leaves it.
  - "The text you typed is kept" is said only for a send from that box.
- **C9 (F-d): the page runs one PR forge read at a time.**
  - `revalidate` joins `runOnce("pr-comments")`.
  - A read that cannot start now waits for the running one, one waiting read per key with the newest winning, and runs when it ends. There is no try cap: a read always ends, and the server bounds it at 30 s.
  - A moved head is followed outside the gate (the reopen asks for the comments again).
  - prs.js is DOM-bound, so the scheduler is a pure function in prfresh.js and is node-tested there. prs.js gets a source guard that it uses it.
- **C10 (F-f): an empty summary falls back to the raw key.** A remark with an empty summary keeps that fallback (`review:<id>:<fp>`), which already exists for unknown notes.
- **C11 (F-h): a 2-minute backstop for the send plan.** `prSendBudget` becomes 2 minutes. Every gh call already carries `forge.CallTimeout` (30 s); the plan budget is a backstop over the forge reads, a gate wait and the local anchoring of a big PR.
  - `TestWebSendPlanHasAForgeBudget` (which overrides it to 300 ms) still proves the 504.
- **C12 (item 8): note only.** The disk read caches into memory on a hit, and a miss re-reads only on the next per-open call. No change; a ledger line.

## Review Focus

1. **A list read overlapping a revalidation:** `r` or the heartbeat starts a list read while a PR refresh is in flight. The refresh answer must still land, with "updated" / moved shown and the flags cleared (Task 1 test `TestAListReadDoesNotDropAPRRefresh`).
2. **A cancelled listing, then a live auth failure:** the 401 must still fall back to Detect (Task 2 test, second half).
3. **A prefetch racing a user open of the same PR:** both reach `cachedPR`. The open stamps and the prefetch does not (Task 4: `TestPRPrefetch` asserts the warmed entry's `OpenedAt` is unchanged).
4. **A force-pushed head:** the count is the commits not in the old head. A count of 0 or a git failure gives the countless line, never "0 new commits" (Task 6 tests).
5. **Two gg processes saving one entry while a third reads it (Windows):** the write is retried, not dropped. Tear test asserts every `Save` returned nil (Task 5).

---

### Task 1: TUI forge reads and sends drop by repo generation (items 1, F-e, F-g)

**Files:**
- Modify: `internal/tui/model.go`: rename `forgeSendGen` → `forgeGen`; reRoot resets; the `noteMutatedMsg` arm.
- Modify: `internal/tui/pr_revalidate.go` (`prRefreshCmd`, `handlePRRevalidatedMsg`).
- Modify: `internal/tui/note_keys.go` (`noteMutatedMsg.sendGen`).
- Modify: `internal/tui/note_popup.go` (`noteSubmitCmd` captures the gen).
- Modify: every `forgeSendGen` use: `forge_send.go`, `send_review_popup.go`, `preview_open.go`, tests.
- Test: `internal/tui/pr_fresh_own_send_test.go` (helper `revalidated`), new `internal/tui/pr_forge_gen_test.go`.

**Interfaces:**
- Produces: `Model.forgeGen int` (the old `forgeSendGen`), `prRevalidatedMsg.gen` = `forgeGen`, `noteMutatedMsg.sendGen int`. Later TUI tasks build messages with `gen: m.forgeGen`.

- [ ] **Step 1: Mechanical rename (no behavior change)**

```bash
cd /work/gigagit/.claude/worktrees/pr-cache-minors
grep -rl forgeSendGen internal/tui | xargs sed -i 's/forgeSendGen/forgeGen/g'
go build ./internal/tui/ && go vet ./internal/tui/
```
Expected: builds clean.

Then update the field's comment in model.go: `// forgeGen is bumped by reRoot only: a forge read, send plan, group list or body read started in the old repository drops (a PR-list read bumps prsGen, never this).`

- [ ] **Step 2: Write the failing tests**

Create `internal/tui/pr_forge_gen_test.go`:

```go
package tui

import (
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

// Item 1 / Review Focus 1: a PR-list read that starts while the open PR's
// refresh is in flight bumps prsGen; the refresh answer must still land.
func TestAListReadDoesNotDropAPRRefresh(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false) // the open's first read
	m.prRevalidateInflight, m.prCommentsInflight, m.prRefreshing = true, true, true
	m.prReadSeq = 2
	m.prsGen++ // r / the heartbeat started a list read
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, seq: 2, pr: model.PullRequest{Number: 7}, commentsChanged: true})
	mm := nm.(Model)
	if mm.prUpdated != 7 || mm.prRevalidateInflight || mm.prCommentsInflight || mm.prRefreshing {
		t.Fatalf("updated=%d inflight=%v/%v refreshing=%v", mm.prUpdated, mm.prRevalidateInflight, mm.prCommentsInflight, mm.prRefreshing)
	}
}

// Item 1: the old repo's answer landing after R must not free the new repo's
// read ("refreshing…" vanishing early, a second read starting).
func TestAnOldRepoAnswerLeavesTheNewReadAlone(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeGen
	m.forgeGen++ // R
	m.prRevalidateInflight, m.prCommentsInflight, m.prRefreshing = true, true, true // the new repo's read
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: old, pr: model.PullRequest{Number: 7}})
	mm := nm.(Model)
	if !mm.prRevalidateInflight || !mm.prCommentsInflight || !mm.prRefreshing {
		t.Fatalf("inflight=%v/%v refreshing=%v", mm.prRevalidateInflight, mm.prCommentsInflight, mm.prRefreshing)
	}
}

// Item 1: R frees the read slot and forgets the reland and the revalidate
// skip — a same-numbered PR in the next repo must not consume them.
func TestReRootFreesThePRReadSlot(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prRevalidateInflight, m.prCommentsInflight = true, true
	m.prReland, m.prRevalidateSkip = &prReland{n: 7, path: "a.go"}, 7
	nm, _ := m.reRoot(t.TempDir())
	mm := nm.(Model)
	if mm.prRevalidateInflight || mm.prCommentsInflight || mm.prReland != nil || mm.prRevalidateSkip != 0 {
		t.Fatalf("inflight=%v/%v reland=%v skip=%d", mm.prRevalidateInflight, mm.prCommentsInflight, mm.prReland, mm.prRevalidateSkip)
	}
}

// F-e: the queued post-send read is consumed by the read that lands, even
// when its PR is no longer on screen.
func TestARefreshAgainDiesWithAClosedView(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRefreshAgain = 7
	m.previewOpen = nil // the PR view closed while the read ran
	nm, _ := m.Update(prRevalidatedMsg{n: 7, gen: m.forgeGen, pr: model.PullRequest{Number: 7}})
	if mm := nm.(Model); mm.prRefreshAgain != 0 {
		t.Fatalf("prRefreshAgain = %d", mm.prRefreshAgain)
	}
}

// F-g: a Reply & send whose draft was written before R does not send in the
// new repository.
func TestReplyAndSendAfterRDoesNotSend(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeGen
	m.forgeGen++ // R
	nm, _ := m.Update(noteMutatedMsg{sendPR: 7, sendID: "d1", sendGen: old})
	if mm := nm.(Model); strings.Contains(mm.statusMsg, "preparing the send") {
		t.Fatalf("status = %q", mm.statusMsg)
	}
}
```

Change the helper in `pr_fresh_own_send_test.go`: `gen: m.prsGen` → `gen: m.forgeGen`. Same in every other test that builds `prRevalidatedMsg{… gen: m.prsGen …}` or `gen: m2.prsGen`:

```bash
grep -rln 'prRevalidatedMsg{' internal/tui/*_test.go | xargs sed -i 's/gen: m\.prsGen/gen: m.forgeGen/g; s/gen: m2\.prsGen/gen: m2.forgeGen/g'
grep -n 'gen := m.prsGen\|gen: gen' internal/tui/pr_cache_test.go
```
Then read `pr_cache_test.go` around line 81 and make the captured `gen` come from `forgeGen` if it is fed to a `prRevalidatedMsg`.

- [ ] **Step 3: Run the tests to verify they fail**

Run: `go test ./internal/tui/ -run 'TestAListReadDoesNotDropAPRRefresh|TestAnOldRepoAnswerLeavesTheNewReadAlone|TestReRootFreesThePRReadSlot|TestARefreshAgainDiesWithAClosedView|TestReplyAndSendAfterRDoesNotSend' -count=1`

Expected: each test fails for its own reason:

| Test | Failure |
|---|---|
| ListRead | `updated=0` (dropped on prsGen) |
| OldRepo | `inflight=false/…` |
| ReRoot | `inflight=true/true` |
| RefreshAgain | `prRefreshAgain = 7` |
| ReplyAndSend | compile error `unknown field sendGen` (then, after adding the field, status "preparing the send…") |

- [ ] **Step 4: Implement**

`note_keys.go`, in `noteMutatedMsg`, add after `sendID`:
```go
	sendGen int // m.forgeGen when the reply was written: a repo switch drops the send
```

`note_popup.go` `noteSubmitCmd`: capture `gen := m.forgeGen` beside `svc`, and return `noteMutatedMsg{clearMarks: ranged, sendPR: sendPR, sendID: d.ID, sendGen: gen}`.

`model.go` `noteMutatedMsg` arm:
```go
		if msg.sendPR != 0 && msg.sendID != "" && msg.sendGen == m.forgeGen { // Reply & send: the draft is saved, now send it
```

`pr_revalidate.go`:
```go
	gen             int // m.forgeGen when the read started: only a repo switch drops it
```
In `prRefreshCmd`: `svc, gen, seq := m.svc, m.forgeGen, m.prReadSeq`.

`handlePRRevalidatedMsg` head becomes:
```go
func (m Model) handlePRRevalidatedMsg(msg prRevalidatedMsg) (Model, tea.Cmd) {
	if msg.gen != m.forgeGen {
		return m, nil // the old repository's read: reRoot already freed this one's slot
	}
	// Cleared BEFORE the "still my view" checks: a read whose view closed
	// under it must not block every later one (the comment half clears
	// prCommentsInflight).
	m.prRevalidateInflight, m.prRefreshing = false, false
	again := m.prRefreshAgain == msg.n // a post-send read waited for this one
	if again {
		m.prRefreshAgain = 0
	}
```
Delete the old `if msg.gen != m.prsGen {…}` block. Replace the later block with:
```go
	if again && m.openPRNumber() == msg.n {
		// The post-send read dropped while this one ran: ask it now.
		var next tea.Cmd
		m, next = m.prRefreshCmd(msg.n, false)
		cmd = tea.Batch(cmd, next)
	}
```

`model.go` `reRoot`, after the `m.prOfflineSince, m.prRefreshing = …` line:
```go
	// …and its read slot: that read answers into the old generation.
	m.prRevalidateInflight, m.prCommentsInflight = false, false
	m.prReland, m.prRevalidateSkip = nil, 0
```
(gofmt alignment: keep it after the aligned block, as the follow-ups did.)

- [ ] **Step 5: Run the tests to verify they pass, then the package**

Run: `go test ./internal/tui/ -run 'PRRefresh|OldRepo|ReRoot|RefreshAgain|ReplyAndSend|Revalidat|OwnSend|PRCache|Fresh' -count=1` → PASS.
Then: `go test ./internal/tui/ -count=1 > $WS/t1.log 2>&1; tail -5 $WS/t1.log` → `ok`.

- [ ] **Step 6: Commit**

```bash
gg add internal/tui
git commit -F <msgfile>   # "fix(tui): PR reads and sends drop by repo generation, not by list reads"
```

---

### Task 2: A cancelled listing keeps the cached forge verdict (item 2)

**Files:**
- Modify: `internal/domain/forge.go` (`PullRequests`)
- Test: `internal/domain/prcache_test.go`

- [ ] **Step 1: Write the failing test** (after `TestPullRequestsFallsBackToDetect`)

```go
// Item 2 / Review Focus 2: a listing the CALLER cancelled (a closed page, a
// spent budget) says nothing about gh: no Detect, and the cached verdict
// stands — so a later REAL failure still falls back to Detect.
func TestACancelledListingKeepsTheCachedVerdict(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ok := &fakeForge{url: "u", open: []model.PullRequest{pr(3, "open", 1)}}
	if _, err := newCachedForgeSvc(t, ok, dir).PullRequests(context.Background()); err != nil {
		t.Fatal(err)
	}
	ff := &fakeForge{url: "u", listErr: context.Canceled}
	b := newCachedForgeSvc(t, ff, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := b.PullRequests(ctx); err == nil {
		t.Fatal("want an error")
	}
	if n := ff.detects.Load(); n != 0 {
		t.Fatalf("a cancelled listing ran Detect %d times", n)
	}
	ff.mu.Lock()
	ff.listErr, ff.detectErr = errors.New("401"), errors.New("logged out")
	ff.mu.Unlock()
	if _, err := b.PullRequests(context.Background()); err == nil {
		t.Fatal("want the 401")
	}
	if n := ff.detects.Load(); n != 1 {
		t.Fatalf("Detect after a live 401 = %d, want 1", n)
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/domain/ -run TestACancelledListingKeepsTheCachedVerdict -count=1`
Expected: FAIL `a cancelled listing ran Detect 1 times`.

- [ ] **Step 3: Implement** (in `PullRequests`, replacing the `s.forgeMu.Lock(); optimistic := …` lines)

```go
	v, err := s.flight.Do("forge-prs", func() (any, error) { return s.pullRequests(ctx, p) })
	// A listing the CALLER gave up on (a closed page, a spent budget — or the
	// coalesced caller's cancellation) says nothing about the provider: it
	// neither proves nor undoes the cached verdict.
	decisive := err == nil || (ctx.Err() == nil && !errors.Is(err, context.Canceled))
	s.forgeMu.Lock()
	optimistic := decisive && s.forgeOptimistic && s.forgeActive == p
```
(The rest of the function is unchanged.)

- [ ] **Step 4: Run the test, then the forge tests**

Run: `go test ./internal/domain/ -run 'Cancelled|PullRequests|ForgeStatus|Detect' -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(domain): a cancelled PR listing keeps the cached forge verdict`

---

### Task 3: Prefetch reads one ref list and only the entries it may warm (item 3)

**Files:**
- Modify: `internal/prcache/prcache.go` (add `Numbers`, delete `Entries`)
- Modify: `internal/prcache/prcache_test.go` (`TestEntriesNewestOpenedFirst` → `TestNumbersListsEntriesWithoutReadingThem`)
- Modify: `internal/domain/prprefetch.go` (`prefetchPRs`)
- Test: `internal/domain/prcache_test.go`

**Interfaces:**
- Produces: `func (s *Store) Numbers() []int`. Task 4 does not use it.

- [ ] **Step 1: Write the failing tests**

`internal/domain/prcache_test.go` (add `os`, `path/filepath` imports):
```go
// Item 3: a list refresh with nothing moved costs ONE ref read — no rev-parse
// per cached PR — and never reads the entry of a PR the listing does not show.
func TestPrefetchWithNothingMovedReadsNoEntry(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	svc, count := countingService(t, dir)
	cache := t.TempDir()
	svc.SetPRCacheStore(prcache.New(cache, 0))
	ff := &fakeForge{url: dir, open: []model.PullRequest{{Number: 7, State: "open", Target: "main", HeadSHA: head}}}
	svc.SetForgeProviders([]forge.Provider{refspecFake{ff, "refs/heads/feat"}})
	ctx := context.Background()
	if _, err := svc.PullRequests(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.PRPreview(ctx, ff.open[0]); err != nil {
		t.Fatal(err)
	}
	// An unlisted PR's entry that cannot be parsed: reading it would quarantine it.
	if err := os.WriteFile(filepath.Join(cache, "pr-9.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	const revParse = "git rev-parse verify commit (resolve)"
	before := count(revParse)
	svc.SetPRCachePolicy(8*time.Hour, 5)
	if n := svc.PRPrefetch(ctx); n != 0 {
		t.Fatalf("warmed %d", n)
	}
	if n := count(revParse) - before; n != 0 {
		t.Fatalf("%d rev-parse calls for an unmoved list", n)
	}
	if m, _ := filepath.Glob(filepath.Join(cache, "*.corrupt-*")); len(m) != 0 {
		t.Fatalf("an unlisted entry was read: %v", m)
	}
}
```
`internal/prcache/prcache_test.go`: replace `TestEntriesNewestOpenedFirst` with:
```go
func TestNumbersListsEntriesWithoutReadingThem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := New(dir, DefaultMax)
	_ = s.Save(Entry{Number: 1, OpenedAt: t0})
	_ = s.Save(Entry{Number: 2, OpenedAt: t0.Add(time.Hour)})
	if err := os.WriteFile(filepath.Join(dir, "pr-3.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := s.Numbers()
	slices.Sort(got)
	if !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("Numbers = %v", got)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.corrupt-*")); len(m) != 0 {
		t.Fatalf("Numbers read an entry: %v", m)
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run:
```
go test ./internal/prcache/ -run TestNumbers -count=1
go test ./internal/domain/ -run TestPrefetchWithNothingMovedReadsNoEntry -count=1
```
Expected: the prcache test fails to build (`s.Numbers undefined`); the domain test fails with `1 rev-parse calls` (or a quarantined `pr-9.json`).

- [ ] **Step 3: Implement**

`prcache.go`: delete `Entries` and add:
```go
// Numbers lists the PRs that have an entry, from the file names alone — no
// entry is read (prefetch loads only the few it may warm).
func (s *Store) Numbers() []int {
	names, _ := filepath.Glob(filepath.Join(s.root, "pr-*.json"))
	var out []int
	for _, p := range names {
		if n, ok := numberOf(p); ok {
			out = append(out, n)
		}
	}
	return out
}
```
Fix `numberOf`'s comment ("used by Numbers").

`prprefetch.go` `prefetchPRs`, replacing the loop from `warmed := 0` on (add the `slices` and `prcache` imports):
```go
	// ONE ref read answers every local head; only a listed open PR whose head
	// moved past it is loaded from disk (a failed read: all count as moved).
	heads := map[int]string{}
	refs, rerr := s.repo.ForEachRef(ctx, git.PRRefPrefix)
	for _, r := range refs {
		if n, ok := git.ParsePRRef(r.Ref); ok {
			heads[n] = r.Hash
		}
	}
	var due []prcache.Entry
	for _, n := range st.Numbers() {
		p, ok := listed[n]
		if !ok || !p.IsOpen() || p.HeadSHA == "" || (rerr == nil && heads[n] == p.HeadSHA) {
			continue // not listed, closed, or unmoved: nothing to warm
		}
		if e, ok := st.Load(n); ok {
			due = append(due, e)
		}
	}
	slices.SortFunc(due, func(a, b prcache.Entry) int { return b.OpenedAt.Compare(a.OpenedAt) }) // most recently opened first
	warmed := 0
	for _, e := range due {
		if warmed >= limit || ctx.Err() != nil || s.gateBusy(ctx) {
			break
		}
		p := listed[e.Number]
		op, err := s.PRFetchOp(ctx, p.Number)
```
(The fetch/preview tail is unchanged; Task 4 swaps `PRFetchOp` for `prFetchOp(…, false)`.)

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/prcache/ ./internal/domain/ -run 'Numbers|Prefetch|PRCache|Trim|Save' -count=1` → PASS.

- [ ] **Step 5: Commit** — `perf(domain): prefetch reads one ref list and only the entries it may warm`

---

### Task 4: Only an open stamps a cache entry's open time (item 4)

**Files:**
- Modify: `internal/domain/forge_cache.go` (`cachedPR`, `rememberPR`, `persistEntry`, `PRRevalidate`)
- Modify: `internal/domain/forge.go` (`PullRequest`, `PRFetchOp` → `prFetchOp`)
- Modify: `internal/domain/prprefetch.go`
- Test: `internal/domain/prcache_test.go`

**Interfaces:**
- Produces:
  - `func (s *Service) prFetchOp(ctx context.Context, n int, opened bool) (engine.FetchPRHead, error)`
  - `rememberPR(ctx, pr, full, opened bool)`
  - `persistEntry(ctx, e, opened bool)`
  - `cachedPR(ctx, p, n, opened bool)`

- [ ] **Step 1: Write the failing tests**

```go
// Item 4: the disk cache keeps the most recently OPENED PRs — the heartbeat's
// revalidate is not an open.
func TestARevalidateIsNotAnOpen(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	ff := &fakeForge{url: "u", byNum: map[int]model.PullRequest{1: pr(1, "open", 1), 2: pr(2, "open", 1)}}
	now := time.Date(2026, 10, 8, 9, 0, 0, 0, time.UTC)
	svc := newCachedForgeSvc(t, ff, dir)
	atClock(svc, &now)
	ctx := context.Background()
	for _, n := range []int{1, 2} {
		if _, err := svc.PRFetchOp(ctx, n); err != nil {
			t.Fatal(err)
		}
		now = now.Add(time.Minute)
	}
	if _, err := svc.PRRevalidate(ctx, 1); err != nil {
		t.Fatal(err)
	}
	st := prcache.New(dir, 0)
	e1, _ := st.Load(1)
	e2, _ := st.Load(2)
	if !e2.OpenedAt.After(e1.OpenedAt) {
		t.Fatalf("a revalidate re-stamped #1: opened %v, #2 opened %v", e1.OpenedAt, e2.OpenedAt)
	}
}
```
In `TestPRPrefetch`, before the final `svc.PRPrefetch(ctx)` (the one that must warm 1), record and then compare:
```go
	st := prcache.New(cacheDir, 0) // the store the test injected — name its TempDir cacheDir
	before, _ := st.Load(7)
	… existing `if n := svc.PRPrefetch(ctx); n != 1` …
	if after, _ := st.Load(7); !after.OpenedAt.Equal(before.OpenedAt) {
		t.Fatalf("a prefetch re-stamped the open time: %v → %v", before.OpenedAt, after.OpenedAt)
	}
```
(Change `svc.SetPRCacheStore(prcache.New(t.TempDir(), 0))` to `cacheDir := t.TempDir(); svc.SetPRCacheStore(prcache.New(cacheDir, 0))`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ -run 'TestARevalidateIsNotAnOpen|TestPRPrefetch$' -count=1`
Expected: FAIL `a revalidate re-stamped #1` and `a prefetch re-stamped the open time`.

- [ ] **Step 3: Implement**

`forge_cache.go`:
- `cachedPR(ctx, p, n, opened bool)`: its two calls become `s.persistEntry(ctx, e, opened)` and `s.rememberPR(ctx, pr, true, opened)`. Its comment: "An open (opened) stamps the entry's last-open time — the disk cache keeps the most recently opened PRs; a prefetch does not."
- `rememberPR(ctx, pr, full, opened bool)` passes `opened` to `persistEntry`.
- `persistEntry(ctx, e, opened bool)`: replace `x.OpenedAt = now` with:
```go
		if opened || x.OpenedAt.IsZero() { // a new entry is stamped once, whoever wrote it
			x.OpenedAt = now
		}
```
  and its comment: "…and, for an open, the open stamp the disk cache's bound keys on".
- `PRRevalidate`: `s.rememberPR(ctx, pr, true, false) // the heartbeat is not an open`.

`forge.go`:
- `PullRequest`: `s.rememberPR(ctx, pr, true, true) // the details view is an open`.
- `PRFetchOp` becomes:
```go
func (s *Service) PRFetchOp(ctx context.Context, n int) (engine.FetchPRHead, error) {
	return s.prFetchOp(ctx, n, true)
}

// prFetchOp is PRFetchOp; opened says whether a user opens the PR (prefetch
// warms it without counting as an open).
func (s *Service) prFetchOp(ctx context.Context, n int, opened bool) (engine.FetchPRHead, error) {
```
  with the old body and `pr, err := s.cachedPR(ctx, p, n, opened)`. The doc comment stays on `PRFetchOp`.

`prprefetch.go`: `op, err := s.prFetchOp(ctx, p.Number, false) // warming is not an open`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ -count=1 > $WS/t4.log 2>&1; tail -3 $WS/t4.log` → `ok`.

- [ ] **Step 5: Commit** — `fix(domain): only an open stamps a cached PR's open time`

---

### Task 5: prcache retries a rename a reader blocks (item 5)

**Files:**
- Modify: `internal/prcache/prcache.go` (`Store.rename`, `writeJSON` → method, `renameRetry`)
- Create: `internal/prcache/rename_windows.go`, `internal/prcache/rename_other.go`
- Test: `internal/prcache/prcache_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// Item 5 / Review Focus 5: on Windows a rename over a file another gg is
// reading fails for milliseconds; the write is retried, not dropped.
func TestSaveRetriesATransientRename(t *testing.T) {
	t.Parallel()
	s := New(t.TempDir(), DefaultMax)
	fails := 2
	s.rename = func(from, to string) error {
		if fails > 0 {
			fails--
			return &os.LinkError{Op: "rename", Old: from, New: to, Err: fs.ErrPermission}
		}
		return os.Rename(from, to)
	}
	if err := s.Save(Entry{Number: 5}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Load(5); !ok {
		t.Fatal("the retried write is missing")
	}
}

// Anything else fails at once: one rename, the error back, no temp left.
func TestSaveFailsAtOnceOnAHardRenameError(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	s := New(dir, DefaultMax)
	calls := 0
	s.rename = func(string, string) error { calls++; return errors.New("disk full") }
	if err := s.Save(Entry{Number: 5}); err == nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	if m, _ := filepath.Glob(filepath.Join(dir, "*.tmp-*")); len(m) != 0 {
		t.Fatalf("temp files left: %v", m)
	}
}
```
In `TestConcurrentSavesNeverTear`, collect the errors:
```go
	var mu sync.Mutex
	var errs []error
	…
			if err := s.Save(Entry{…}); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
	…
	if len(errs) != 0 {
		t.Fatalf("saves failed: %v", errs)
	}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/prcache/ -count=1`
Expected: build failure `s.rename undefined`.

- [ ] **Step 3: Implement**

`prcache.go`:
- `Store` gains `rename func(from, to string) error // os.Rename; tests inject a blocked one`.
- `New` sets `rename: os.Rename`.
- `writeJSON` becomes `func (s *Store) writeJSON(path string, v any) error`; its last step is `if err := s.renameRetry(tmp.Name(), path); err != nil {`.
- Every caller becomes `s.writeJSON(...)`.

Add:
```go
// renameRetry is writeJSON's last step. On Windows a rename over a file
// another gg process is reading (os.ReadFile holds it without
// FILE_SHARE_DELETE) fails with access denied or a sharing violation — a
// state that clears in milliseconds. Those retry for filelock.Wait, like the
// lock itself; anything else fails at once.
func (s *Store) renameRetry(from, to string) error {
	deadline := time.Now().Add(filelock.Wait)
	for {
		err := s.rename(from, to)
		if err == nil || !transientRename(err) || time.Now().After(deadline) {
			return err
		}
		time.Sleep(filelock.Poll)
	}
}

func transientRename(err error) bool {
	return errors.Is(err, fs.ErrPermission) || sharingViolation(err)
}
```

`rename_windows.go`:
```go
package prcache

import (
	"errors"
	"syscall"
)

// errSharingViolation is ERROR_SHARING_VIOLATION; syscall names no constant.
const errSharingViolation syscall.Errno = 32

func sharingViolation(err error) bool {
	var e syscall.Errno
	return errors.As(err, &e) && e == errSharingViolation
}
```

`rename_other.go`:
```go
//go:build !windows

package prcache

// sharingViolation is Windows-only: elsewhere a rename never fails on a reader.
func sharingViolation(error) bool { return false }
```

- [ ] **Step 4: Run the tests (+ a Windows build check)**

Run:
```
go test ./internal/prcache/ -count=1 -race
GOOS=windows go vet ./internal/prcache/
```
Expected: PASS; vet clean.

- [ ] **Step 5: Commit** — `fix(prcache): retry a rename another reader blocks (Windows)`

---

### Task 6: "PR #n updated: 2 new commits" (item 7)

**Files:**
- Modify: `internal/domain/forge.go` (add `PRNewCommits`)
- Modify: `internal/tui/pr_revalidate.go` (`prReland.from`)
- Modify: `internal/tui/pr_actions.go` (`openPRPreviewCmd`)
- Modify: `internal/tui/preview_open.go` (`previewOpenMsg.newCommits`, `prUpdatedText`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml`
- Test: `internal/domain/prpreview_test.go`, new `internal/tui/pr_new_commits_test.go`

**Interfaces:**
- Produces: `func (s *Service) PRNewCommits(ctx context.Context, n int, from string) int`; `func prUpdatedText(n, count int) string`.

- [ ] **Step 1: Write the failing tests**

Domain (`prpreview_test.go`):
```go
// Item 7: the commits PR n's head has that the head on screen had not; a
// force-push counts every commit not in the old head; a non-sha is 0.
func TestPRNewCommits(t *testing.T) {
	t.Parallel()
	dir, head := prPreviewRepo(t)
	svc, _ := countingService(t, dir)
	ctx := context.Background()
	if n := svc.PRNewCommits(ctx, 7, revParse(t, dir, head+"~1")); n != 1 {
		t.Fatalf("one commit on top = %d", n)
	}
	if n := svc.PRNewCommits(ctx, 7, "HEAD"); n != 0 {
		t.Fatalf("a non-sha counted %d", n)
	}
	forcePushPR(t, dir, 7)
	if n := svc.PRNewCommits(ctx, 7, head); n != 1 {
		t.Fatalf("after a force-push = %d, want the 1 commit not in the old head", n)
	}
}
```
(Read `forcePushPR` first: if its rewrite holds more than one commit not in `head`, set the expected count to that number.)

TUI (`pr_new_commits_test.go`):
```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/model"
)

func TestPRUpdatedText(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		n, c int
		want string
	}{
		{7, 0, "PR #7 updated: new commits"},
		{7, 1, "PR #7 updated: 1 new commit"},
		{7, 2, "PR #7 updated: 2 new commits"},
	} {
		if got := prUpdatedText(tc.n, tc.c); got != tc.want {
			t.Errorf("prUpdatedText(%d, %d) = %q, want %q", tc.n, tc.c, got, tc.want)
		}
	}
}

// Item 7: a moved-head reopen counts the commits against the head that was
// on screen (the reland's from). Serial: prSendModel.
func TestAMovedReopenCountsTheNewCommits(t *testing.T) {
	m, dir, _ := prSendModel(t)
	m.prReland = &prReland{n: 7, from: gitOut(t, dir, "rev-parse", "main")}
	msg := m.openPRPreviewCmd(model.PullRequest{Number: 7, State: "open", Target: "main"})().(previewOpenMsg)
	if msg.newCommits != 1 {
		t.Fatalf("newCommits = %d, want 1", msg.newCommits)
	}
}
```
(Read `prSendModel`'s return order first: if the middle value is not the repo dir, take the dir from where it is.)

- [ ] **Step 2: Run them to verify they fail**

Run:
```
go test ./internal/domain/ -run TestPRNewCommits -count=1
go test ./internal/tui/ -run 'TestPRUpdatedText|TestAMovedReopenCountsTheNewCommits' -count=1
```
Expected: build failures (`PRNewCommits` / `prUpdatedText` / `from` / `newCommits` undefined).

- [ ] **Step 3: Implement**

`forge.go`:
```go
// PRNewCommits counts the commits PR n's local head has that from — the
// head a view showed before the PR moved — has not ("2 new commits"). A
// force-push counts every commit not in the old head. 0 when from is not a
// full sha or git cannot answer: the caller then says "new commits" alone.
func (s *Service) PRNewCommits(ctx context.Context, n int, from string) int {
	if !isFullSHA(from) {
		return 0
	}
	c, err := s.repo.CountRange(ctx, from, git.PRRef(n))
	if err != nil {
		return 0
	}
	return c
}
```

`pr_revalidate.go`:
- `prReland` gains `from string // the head on screen before the move: the reopen counts the new commits against it`.
- In the handler: `r := &prReland{n: msg.n, path: m.previewSelectedPath(), from: m.previewOpen.srcHash}`. `m.previewOpen` is non-nil there: `openPRNumber() == msg.n` was checked.

`pr_actions.go` `openPRPreviewCmd`:
```go
	keep, from := "", "" // a moved-head reopen keeps the cursor's file and counts the new commits
	if r := m.prReland; r != nil && r.n == p.Number {
		keep, from = r.path, r.from
	}
	…
		if err == nil && r.Endpoints.Summary.State == domain.PreviewOK && r.Set.OK() {
			…
		}
		if err == nil && from != "" {
			msg.newCommits = svc.PRNewCommits(ctx, p.Number, from)
		}
```

`preview_open.go`:
- `previewOpenMsg` gains `newCommits int // a moved-head reopen: commits the new head added (0 = unknown)`.
- At the `prRevalidateSkip` site: `m.statusMsg = prUpdatedText(msg.prNumber, msg.newCommits)`.
- Add:
```go
// prUpdatedText is the moved-head reopen's notice (spec §2.4): with the
// count when the reopen could count, else without.
func prUpdatedText(n, count int) string {
	switch {
	case count == 1:
		return i18n.T("PR #%d updated: 1 new commit", n)
	case count > 1:
		return i18n.T("PR #%d updated: %d new commits", n, count)
	}
	return i18n.T("PR #%d updated: new commits", n)
}
```

Bundles, inserted after the existing `"PR #%d updated: new commits"` line:

| Bundle | `"PR #%d updated: 1 new commit"` | `"PR #%d updated: %d new commits"` |
|---|---|---|
| ja | `"PR #%d を更新しました: 新しいコミット 1 件"` | `"PR #%d を更新しました: 新しいコミット %d 件"` |
| ko | `"PR #%d 업데이트됨: 새 커밋 1개"` | `"PR #%d 업데이트됨: 새 커밋 %d개"` |
| zh | `"PR #%d 已更新：1 个新提交"` | `"PR #%d 已更新：%d 个新提交"` |
| ru | `"PR #%d обновлён: новых коммитов: 1"` | `"PR #%d обновлён: новых коммитов: %d"` |

- [ ] **Step 4: Run the tests + the i18n gates**

Run:
```
go test ./internal/domain/ -run TestPRNewCommits -count=1
go test ./internal/tui/ -run 'TestPRUpdatedText|TestAMovedReopenCountsTheNewCommits|I18n|Revalidat' -count=1
go test ./internal/i18n/ -count=1
```
Expected: PASS.

- [ ] **Step 5: Commit** — `feat(tui): a moved PR's notice counts its new commits`

---

### Task 7: TUI kept body belongs to its box (F-a, F-b, F-i abort)

**Files:**
- Modify: `internal/tui/forge_send.go` (`forgeSendState.req`, `handleForgeSendReady`, `forgeSendFinished`)
- Modify: `internal/tui/send_review_popup.go` (`(*keptSendBody).from`)
- Test: `internal/tui/forge_send_kept_test.go`

- [ ] **Step 1: Write the failing tests**

```go
// F-a: a send that did not come from the kept body's box — a resolve on the
// same PR, a send on another PR — leaves the typed text; its own send clears it.
func TestOnlyItsOwnSendClearsTheKeptBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	resolve := domain.PRSendRequest{PR: 7, Resolve: []string{"PRRT_1"}}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: resolve}, engine.Result{Changed: true}, nil)
	other := domain.PRSendRequest{PR: 8, Verdict: true, BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 8, req: other}, engine.Result{Changed: true}, nil)
	if m.keptSendBody == nil {
		t.Fatal("another send dropped the typed verdict body")
	}
	own := domain.PRSendRequest{PR: 7, Verdict: true, Body: "keep me", BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: own}, engine.Result{Changed: true}, nil)
	if m.keptSendBody != nil {
		t.Fatal("its own send did not clear it")
	}
}

// F-i: an aborted send of its own box keeps the text (nothing reached GitHub).
func TestAnAbortKeepsTheKeptBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	own := domain.PRSendRequest{PR: 7, Verdict: true, Body: "keep me", BodySet: true}
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7, req: own}, engine.Result{}, nil)
	if m.keptSendBody == nil {
		t.Fatal("an abort dropped the typed body")
	}
}

// F-b: a failed resolve says nothing about kept text.
func TestAFailedResolveDoesNotSayTheTextIsKept(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.keptSendBody = &keptSendBody{pr: 7, verdict: true, text: "keep me"}
	m, _ = m.handleForgeSendReady(forgeSendReadyMsg{gen: m.forgeGen, req: domain.PRSendRequest{PR: 7, Resolve: []string{"x"}}, err: errors.New("boom")})
	if strings.Contains(m.statusMsg, "kept") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}
```
(Add `errors`, `strings`, `domain`, `engine` imports as needed.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/tui/ -run 'TestOnlyItsOwnSendClearsTheKeptBody|TestAnAbortKeepsTheKeptBody|TestAFailedResolveDoesNotSayTheTextIsKept' -count=1`

Expected:
- A build failure first (`unknown field req`).
- After adding the field: `another send dropped…` and `status = "send: boom — the text you typed is kept"`.
- `TestAnAbortKeepsTheKeptBody` passes already: it is a pin for an F-i gap, not a RED. Ledger it as such.

- [ ] **Step 3: Implement**

`forge_send.go`:
- `forgeSendState` gains `req domain.PRSendRequest // what was asked: a kept body clears only for its own box's send`.
- `handleForgeSendReady`: `m.forgeSend = &forgeSendState{pr: msg.req.PR, req: msg.req, plan: msg.op.Plan}`; its error branch: `if m.keptSendBody.from(msg.req) {`.
- `forgeSendFinished`: replace `m.keptSendBody = nil // the typed body reached GitHub` with:
```go
		if m.keptSendBody.from(fs.req) {
			m.keptSendBody = nil // the typed body reached GitHub
		}
```

`send_review_popup.go`:
```go
// from reports whether req is the send this kept body's box made (C8): the
// same PR and the same box — Verdict…, my draft review, or that AI review.
func (k *keptSendBody) from(req domain.PRSendRequest) bool {
	if k == nil || !req.BodySet || k.pr != req.PR || k.verdict != req.Verdict {
		return false
	}
	switch {
	case req.Verdict:
		return true
	case req.Mine:
		return k.group == domain.GroupMine
	}
	return req.Review != "" && k.group == "review:"+req.Review
}
```
Check the other `&forgeSendState{` construction sites (`grep -rn 'forgeSendState{' internal/tui`); a site without a request leaves `req` zero, so `from` is false and the body is kept.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/tui/ -run 'Kept|ForgeSend|SendReview|Verdict' -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix(tui): a kept send body clears only for its own box's send`

---

### Task 8: Web kept body belongs to its box (F-a web, F-i web) + probe

**Files:**
- Create: `internal/web/static/prkept.js`
- Modify: `internal/web/static/prsend.js`
- Test: new `internal/web/prkeptjs_test.go`. `internal/web/prsendjs_test.go` keeps `TestPRSendKeepsTheVerdictBody` only if it still means something: a source grep is replaced, not kept.
- Probe: the scratchpad `probe/probe.mjs` gains scenario `keptacross`.

**Interfaces:**
- Produces: `keptText(kept, pr, group)`, `keptAfterSend(kept, n, body, ev)`; `onSendDone` hooks receive `(n, ev, body)`.

- [ ] **Step 1: Write the failing test** (`prkeptjs_test.go`, following `TestPRSendRowsJS`'s node harness)

```go
package web

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// C8: a kept body is cleared only by a changing send from its own box.
func TestPRKeptJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prkept.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prkept.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { keptText, keptAfterSend } from "./prkept.mjs";
const v = { pr: 7, group: "verdict", text: "keep me" };
const ok = { ok: true, changed: true };
const left = (k) => (k ? k.text : null);
console.log(JSON.stringify({
  text: keptText(v, 7, "verdict"), otherGroup: keptText(v, 7, "mine"), otherPR: keptText(v, 8, "verdict"),
  resolve: left(keptAfterSend(v, 7, { kind: "resolve", ids: ["PRRT_1"] }, ok)),
  otherPR2: left(keptAfterSend(v, 8, { kind: "verdict", body: "x" }, ok)),
  abort: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, { ok: true, changed: false })),
  failed: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, { ok: false })),
  own: left(keptAfterSend(v, 7, { kind: "verdict", body: "keep me" }, ok)),
  group: left(keptAfterSend({ pr: 7, group: "review:r1", text: "t" }, 7, { kind: "group", group: "review:r1" }, ok)),
  otherBox: left(keptAfterSend({ pr: 7, group: "review:r1", text: "t" }, 7, { kind: "group", group: "mine" }, ok)),
}));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got map[string]*string
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	keep := map[string]bool{"text": true, "resolve": true, "otherPR2": true, "abort": true, "failed": true, "otherBox": true}
	for k, v := range got {
		if keep[k] != (v != nil) {
			t.Errorf("%s = %v (want kept=%v)", k, v, keep[k])
		}
	}
	if got["otherGroup"] != nil || got["otherPR"] != nil {
		t.Errorf("keptText matched another box: %v / %v", got["otherGroup"], got["otherPR"])
	}
}
```

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run TestPRKeptJS -count=1`
Expected: FAIL `open static/prkept.js: no such file`.

- [ ] **Step 3: Implement**

`static/prkept.js`:
```js
// prkept.js — the body a send's box keeps until that send reaches GitHub
// (pure: node-tested by prkeptjs_test.go). kept = {pr, group, text}; group
// "verdict" is Verdict…, "mine" my draft review, "review:<id>" an AI review.

// keptText is the kept text for PR pr's box group, or null.
export function keptText(kept, pr, group) {
  return kept && kept.pr === pr && kept.group === group ? kept.text : null;
}

// keptAfterSend is what a finished send of PR n (its request body, its done
// event) leaves kept: only a send that changed GitHub FROM THAT BOX clears
// it — a resolve, a reply or another PR's send leaves the typed text.
export function keptAfterSend(kept, n, body, ev) {
  if (!kept || !ev.ok || !ev.changed || kept.pr !== n || !body) return kept;
  const group = body.kind === "verdict" ? "verdict" : body.kind === "group" ? body.group : null;
  return group === kept.group ? null : kept;
}
```
`prsend.js`:
- `import { keptAfterSend, keptText } from "./prkept.js";`
- `sendReviewBody`: `const kept = keptText(keptBody, pr, group);`.
- `verdictBody`: `const kept = keptText(keptBody, pr, "verdict") ?? "";`.
- `sendToGitHub`'s follow: `for (const fn of doneHooks) fn(n, ev, body);`.
- The hook: `onSendDone((n, ev, body) => { keptBody = keptAfterSend(keptBody, n, body, ev); });`.
- Fix the `onSendDone` comment to name the third argument.
- If `prsendjs_test.go`'s `TestPRSendKeepsTheVerdictBody` greps the old inline `keptBody.group === "verdict"`, rewrite it to grep `keptText(keptBody, pr, "verdict")` (the wiring). The behavior is now `TestPRKeptJS`'s.
- If a static-asset test lists modules (`grep -rn '"prsend.js"' internal/web/*_test.go`), add `prkept.js` there.

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web/ -run 'JS|Static|PRSend' -count=1` → PASS.

- [ ] **Step 5: Browser probe (visibility, old build first)**

1. Copy the probe into a dir of this run: `cp -r <a829a3fc scratchpad>/probe $SP/probe`.
2. Add scenario `keptacross` to `probe.mjs`, built from the `verdict` scenario's steps:
   1. Type "keep me please" in Verdict… and let it be refused (snapshot off).
   2. Restore the snapshot. Open the PR and big.go, then send the probe note (Send to GitHub → Send) until `SubmitReview` is in the writes.
   3. Reopen Verdict… on the PR row and assert the box holds "keep me please".
3. Run it against the old build first:

| Build | How | Expected |
|---|---|---|
| Old | `go build -o $SP/old/gg ./cmd/gg` from `/work/gigagit` (main) | FAIL at the last assert (the note's send cleared the kept body) |
| New | `go build -o bin/gg ./cmd/gg` here | PASS |

Each run: `SRC=/work/gigagit/.claude/worktrees/pr-cache-minors bash $SP/probe/fixtures.sh $SP/run-old $SP/old/gg <port>`, then `node $SP/probe/probe.mjs http://127.0.0.1:<port> $SP/run-old/fakegh keptacross $SP/shot-old`. Then kill the printed PID.

Also re-run scenarios `verdict` and `interrupted` on the new build. They must pass; `main` flakes pre-existingly at "big.go … has no size".

- [ ] **Step 6: Commit** — `fix(web): a kept send body clears only for its own box's send`

---

### Task 9: The page runs one PR forge read at a time (F-d, F-i ev.changed)

**Files:**
- Modify: `internal/web/static/prfresh.js` (add `serialReads`, `sentEvent`)
- Modify: `internal/web/static/prs.js` (`refreshPRComments`, `revalidate`, `refreshAfterSend`, the `onSendDone` hook)
- Test: `internal/web/prfreshjs_test.go`; a source guard in `internal/web/prs_static_test.go`

**Interfaces:**
- Produces: `serialReads(gate) → { run(fn), soon(key, fn) }`, `sentEvent(ev, seq) → {kind:"sent", seq} | null`.

- [ ] **Step 1: Write the failing tests**

In `prfreshjs_test.go` (add `reflect` to the imports if missing):
```go
// C9: one read at a time; a read that cannot start waits for the running one
// (one per key, the newest wins) with no give-up timer. F-i: only a send
// that changed GitHub arms the own-send absorb.
func TestPRFreshJSSerialReads(t *testing.T) {
	t.Parallel()
	out := runFreshModuleJS(t, `
import { serialReads, sentEvent } from "./prfresh.mjs";
let slot = null; // the one running read's "end" switch
const gate = (fn) => {
  if (slot) return null;
  fn();
  return new Promise((r) => (slot = r)).then(() => { slot = null; });
};
const end = async () => { slot(); await new Promise((r) => setTimeout(r, 0)); };
const log = [];
const mark = (name) => () => log.push(name);
const reads = serialReads(gate);
reads.run(mark("a"));
const busyRun = reads.run(mark("x")) === null;
reads.soon("k", mark("b"));
reads.soon("k", mark("c")); // the newest of key k wins
await end(); // a ends: c runs, b never
await end(); // c ends
reads.soon("j", mark("d")); // nothing running: at once
await end();
console.log(JSON.stringify({ order: log, busyRun,
  sent: [sentEvent({ ok: true, changed: true }, 4), sentEvent({ ok: true, changed: false }, 4), sentEvent({ ok: false }, 4)] }));
`)
	var got struct {
		Order   []string         `json:"order"`
		BusyRun bool             `json:"busyRun"`
		Sent    []map[string]any `json:"sent"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if !slices.Equal(got.Order, []string{"a", "c", "d"}) || !got.BusyRun {
		t.Fatalf("order=%v busyRun=%v", got.Order, got.BusyRun)
	}
	want := []map[string]any{{"kind": "sent", "seq": float64(4)}, nil, nil}
	if !reflect.DeepEqual(got.Sent, want) {
		t.Fatalf("sent = %v, want %v", got.Sent, want)
	}
}

// runFreshModuleJS runs script (an ES module that imports ./prfresh.mjs)
// under node and returns its stdout.
func runFreshModuleJS(t *testing.T, script string) []byte {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "prfresh.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "prfresh.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(script), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	return out
}
```
(If `runFreshJS` already copies `prfresh.js` the same way, make it call `runFreshModuleJS`; do not keep two copies of the copy step.)

Source guard in `prs_static_test.go`:
```go
// C9: prs.js reads through the one serial reader — revalidate included — and
// has no give-up timer.
func TestPRReadsAreSerial(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile(filepath.Join("static", "prs.js"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, want := range []string{`serialReads(`, `reads.soon("revalidate:"`, `reads.soon("after-send:"`, `sentEvent(ev, readSeq)`} {
		if !strings.Contains(s, want) {
			t.Errorf("prs.js lacks %s", want)
		}
	}
	if strings.Contains(s, "tries < 40") {
		t.Error("prs.js still gives up on a dropped post-send read")
	}
}
```

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'TestPRFreshJSSerialReads|TestPRReadsAreSerial' -count=1`
Expected: FAIL (`serialReads` is not exported; prs.js lacks `serialReads(`).

- [ ] **Step 3: Implement**

`prfresh.js` (append):
```js
// serialReads runs the open PR's forge reads one at a time through gate
// (prs.js: runOnce("pr-comments")). run(fn) starts fn or answers null when a
// read is running; soon(key, fn) starts it, or runs it once the running read
// ends — one waiting read per key, the newest wins. No give-up timer: a read
// always ends (the server bounds it).
export function serialReads(gate) {
  const waiting = new Map();
  const run = (fn) => {
    const p = gate(fn);
    if (p) p.then(next, next);
    return p;
  };
  function next() {
    const first = waiting.entries().next();
    if (first.done) return;
    const [key, fn] = first.value;
    waiting.delete(key);
    soon(key, fn);
  }
  function soon(key, fn) {
    if (run(fn) === null) waiting.set(key, fn);
  }
  return { run, soon };
}

// sentEvent is the freshness event a finished send makes (F1): only a send
// that changed GitHub is my own change; seq = the last read started.
export function sentEvent(ev, seq) {
  return ev.ok && ev.changed ? { kind: "sent", seq } : null;
}
```

`prs.js`:
- Import `serialReads, sentEvent` from `./prfresh.js`; add `const reads = serialReads((fn) => runOnce("pr-comments", fn));`.
- `refreshPRComments(n, moved)` becomes `return reads.run(commentsRead(n, moved));`, where `commentsRead(n, moved)` returns the old `async () => {…}` body, unchanged.
- `refreshAfterSend(n)` becomes `reads.soon("after-send:" + n, commentsRead(n, false));`; delete the `tries` parameter and comment.
- `revalidate(n)` becomes `reads.soon("revalidate:" + n, revalidateRead(n));`. `revalidateRead(n)` returns the old body as `async () => {…}`, except that `if (headMoved(n, rv)) await followMovedHead(n);` becomes `if (headMoved(n, rv)) setTimeout(() => followMovedHead(n), 0); // outside the gate: the reopen reads the comments again`.
- The `onSendDone` hook: `const sent = sentEvent(ev, readSeq); if (sent) freshEvent(n, sent); refreshAfterSend(n);`.
- `grep -n "revalidate(" internal/web/static/*.js`: a caller that awaited `revalidate` no longer gets the read's promise. Check that none relied on it (`openPR` does not await it).

- [ ] **Step 4: Run the web tests**

Run: `go test ./internal/web/ -count=1 > $WS/t9.log 2>&1; tail -3 $WS/t9.log` → `ok`.

- [ ] **Step 5: Probe** — re-run the probe's `verdict`, `interrupted` and `keptacross` scenarios on a rebuilt `bin/gg`: all PASS, and no "updated" after the own send in `keptacross` (assert `#pr-fresh` ≠ "updated" after the note's send, as `main` does).

- [ ] **Step 6: Commit** — `fix(web): one PR forge read at a time; a dropped post-send read waits instead of giving up`

---

### Task 10: Small fixes — empty remark label, plan budget, group-bar handler (F-f, F-h, F-i)

**Files:**
- Modify: `internal/domain/forge_send.go` (`interruptedNames`)
- Modify: `internal/web/prsend.go` (`prSendBudget`)
- Test: `internal/domain/forge_send_dedupe_test.go` (where the follow-ups' `interruptedNames` test lives — `grep -rn interruptedNames internal/domain/*_test.go`), `internal/tui/pr_send_serial_test.go`

- [ ] **Step 1: Write the failing tests**

Domain: next to the existing `interruptedNames` test, build a stored review whose remark has an empty summary. Copy that test's review fixture and blank the summary. Then assert:
```go
	if got := names[key]; got != key {
		t.Fatalf("empty-summary remark label = %q, want the key %q", got, key)
	}
```
(If the review store refuses an empty summary on save, the case cannot arise. Then ledger `Ruling: F-f unreachable — <store's refusal>` and skip this sub-item.)

TUI, extending `TestAPRReResolveKeepsTheGroupBars` (F-i): after building `msg`, feed it through `Update` on a model whose PR view is open, then assert the handler's state:
```go
	nm, _ := m.Update(msg)
	if g := nm.(Model).filesPreviewGroups["big.go"]; len(g) == 0 || g[0] != domain.GroupMine {
		t.Fatalf("after the handler groups = %v", nm.(Model).filesPreviewGroups)
	}
```
(If `prSendModel` does not open the PR view, open it first the way `prDiffModel` does: `m.Update(previewOpenMsg{…, prNumber: 7})`. The handler's same-tag path is what F8 fixed.)

- [ ] **Step 2: Run them**

Run: `go test ./internal/domain/ -run Interrupted -count=1; go test ./internal/tui/ -run TestAPRReResolveKeepsTheGroupBars -count=1`

Expected:
- Domain: FAIL, label `""`.
- TUI: PASS. It is a pin (F8 was fixed in the follow-ups); watch it fail with the F8 guard removed: in `preview_open.go`, make `resolvePreviewCmd` stop filling `msg.groups`, run it, FAIL, then re-apply with Edit.

- [ ] **Step 3: Implement**

`forge_send.go` `interruptedNames`: `if d.fp == fp && d.summary != "" {`, plus comment `// an empty summary keeps the key`.

`web/prsend.go`:
```go
// prSendBudget is a backstop over a send's planning: its forge reads (each
// already bounded by the provider's own 30 s call timeout), a wait on the
// repo gate, and the local anchoring of a big PR on a monorepo — which the
// old 30 s budget could cut (504) although nothing was stuck.
var prSendBudget = 2 * time.Minute
```
(No test: `TestWebSendPlanHasAForgeBudget` overrides it and still proves the 504; ledger C11.)

- [ ] **Step 4: Run the packages**

Run: `go test ./internal/domain/ ./internal/web/ -count=1 > $WS/t10.log 2>&1; tail -4 $WS/t10.log` → `ok` ×2. Then `go test ./internal/tui/ -run 'GroupBars|Interrupted' -count=1` → PASS.

- [ ] **Step 5: Commit** — `fix: an empty remark keeps its key label; the send plan's budget is a 2 min backstop; group-bar handler test`

---

### Task 11: Docs, item 6 note, F-c wording; full gate

**Files:**
- Modify: `docs/superpowers/specs/2026-10-07-github-write-design.md` (§2.3, one sentence)
- Modify: `internal/config/template.go` (the `cache_hours` settingDoc)
- Modify: `README.md` (`cache_hours` line, if it says more than the settingDoc)
- Modify: `docs/CLAUDE-details.md` (web own-send paragraph; TUI generation; PR cache paragraph)
- Modify: `internal/tui/pr_fresh_own_send_test.go:16`, `internal/web/prfreshjs_test.go` (the F-c test comments)
- Modify: `CHANGELOG.md` (new top section)

- [ ] **Step 1: Item 6 (C6)**
  - Spec §2.3, the last bullet becomes: "A PR row the user can see in the list still opens with its row data at once, as today — for any `cache_hours > 0`; at 0 every open reads GitHub first (§4.5)."
  - settingDoc: "…; 0 = always read first (an open from the list too)".
  - README `cache_hours` comment: same words.
  - Run `go test ./internal/config/ -count=1`; a golden over the template must be updated if one exists.
- [ ] **Step 2: F-c wording**
  - `docs/CLAUDE-details.md`: "…and a read that was already running neither shows nor absorbs it" becomes "…a read that was already running does not absorb it (it still shows "updated" when it reports a change)".
  - In the same paragraph, replace "`runOnce` is retried (`refreshAfterSend`, 250 ms, 40 tries)" with "waits for the running read (`serialReads`: one per key, no give-up timer); `revalidate` is under the same gate".
  - Kept bodies: "only a changing send from the same box clears them".
  - Fix both test comments the same way.
- [ ] **Step 3: CLAUDE-details**
  - TUI section: `forgeGen` (renamed), reRoot frees the read slot, the reland and the skip; "PR #n updated: N new commits" (`PRNewCommits`, against the head on screen); kept body per box.
  - PR cache section: prefetch's one ref read + `Store.Numbers`; only opens stamp `OpenedAt`; a cancelled listing is indecisive; the Windows rename retry; `cache_hours = 0` reads a listed row first too.
- [ ] **Step 4: CHANGELOG** — a top section "Sending to GitHub — cache minors", one bullet per user-visible change (the commit count, kept bodies, no lost refresh after a list read, the Windows write retry, the budget). README needs nothing more. No CLI surface change, so no skill bump.
- [ ] **Step 5: Full gate**

```bash
./test.sh race > $WS/race.log 2>&1; tail -15 $WS/race.log
```
Expected: the log says "all green". Then `go build -o bin/gg ./cmd/gg` and re-run the probe scenarios `verdict`, `interrupted` and `keptacross` once more on it.

- [ ] **Step 6: Commit** — `docs: GitHub write-back cache minors`

---

## After the last task

- Final review: one read-only subagent on the most capable model, over `review-package <plan> $(git merge-base main HEAD) HEAD`, with this plan's Review Focus verbatim and the ledger's `Ruling:` lines.
- Fix its Critical/Important findings with TDD. Re-run the race gate.
- Ask the user before merging (`gg merge -F <msgfile> --into main fix/pr-cache-minors`; conflicts: `--on-conflict keep`, stage only the conflicted paths; race-gate the merged tree). Then `./build.sh install`, `./build.sh web`, `gg init --update`.
