# GitHub write-back — follow-ups Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (the user chose NATIVE execution: no implementer subagents; one read-only final review subagent on the most capable model). Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close every deferred minor of plans 3 (TUI) and 4 (web) of GitHub write-back before plan 5.

**Architecture:** No new subsystem. Domain gets id dedupe, readable Finish/Discard rows and a sha-keyed file-lines cache; the web handler gets a forge budget and honest statuses; the page's freshness word and kept bodies get the done event's `changed` bit; the CLI's abort exits 1; the TUI gets the same freshness rule, a generation guard for answers that land after `R`, a kept body, group bars that survive a re-resolve, unique chooser rows and whole-sentence confirm rows.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla ES modules (node-run JS tests), playwright probe.

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` (amended 2026-10-08: agents never send; an emptied AI-review body posts empty). Source of the items: plan-3 ledger (`Final: minor (deferred)` lines) and plan-4 ledger, both copied in memory `github-write-feature.md`.

## Global Constraints

- An AI agent never sends anything to GitHub (user ruling 2026-10-08). No change here adds a send path.
- Nothing posts to real GitHub during this work; tests use `fakeForge` / `writerForge` / fake gh only.
- The repo gate is not re-entrant: `PRSendOp` is built outside ops (web handler, TUI `tea.Cmd`).
- Every user-visible TUI string goes through `i18n.T` with a literal key in all four bundles (ja/ko/zh/ru); removed keys are removed from all four (orphan gate).
- Engine/CLI prose stays English.
- Race gate is green only when the log says "all green".
- gg commit has no `-F`: `gg add <paths>` then `git commit -F <msgfile>`. Never `git add -A` (a built `bin/gg` may sit in the worktree).
- Never `git checkout -- <file>` to undo a watch-it-fail probe: re-apply the guard with Edit.

## Item map (the starter's numbering)

| # | Item | Task |
|---|------|------|
| 1 | web ownSend armed on failed/aborted send; dropped post-send refresh | 5 |
| 2 | handlePRSend plans with no forge budget | 4 |
| 3 | duplicate ids → two threads (web + CLI) | 1 |
| 4 | hosted TUI mid-op: web send blocks on the gate | 11 (doc) |
| 5 | CLAUDE.md cli row says "pending" | 11 |
| 6 | `gg pr send` abort exits 0 | 6 |
| 7 | test gaps (dead skip; web e2e resolve/unresolve/send-drafts) | 3 |
| 8 | web Verdict… body not kept on a refusal | 5 |
| 9 | prSendRequest maps every lookup failure to 400 | 4 |
| 10 | TUI "updated" after own send; prSeen/prUpdated not reset on reRoot | 7 |
| 11 | TUI send plan landing after R starts in the new repo | 8 |
| 12 | TUI group bars stale/wiped after local note edits | 9 |
| 13 | repeated ShowFile reads (remarkPlace, PreviewNotesAll) | 2 |
| 14 | raw keys in Finish/Discard rows | 1 (domain) + 10 (TUI) |
| 15 | duplicate chooser labels | 9 |
| 16 | composed translated fragments | 10 |
| 17 | plan error after the popup closed loses the typed body | 8 |

Moot (died with the pending-send queue, plan 4): plan-3 "failed queue read blanks the notices for 2 s", "no test of the watcher re-arm on reRoot", and the "asks for: %s" half of item 16 (the key is gone — `grep -rn 'asks for: %s' internal/` is empty).

Not included (the starter marks them optional, ask the user): plan-1 cache minors (stale-gen flag clearing, OpenedAt stamping, Windows rename retry, the count in the "new commits" notice).

## Rulings proposed (for the user's review)

- **F1** (items 1, 8, 10): "my own send" is a done event with `ok && changed` (web) / `err == nil && res.Changed` (TUI). An abort answers ok with `changed: false`, so an abort neither arms the freshness absorb nor clears a kept body (the user may edit and send again).
- **F2** (items 1, 10): the read that absorbs my own send is the first read that STARTED after the send finished — reads carry a sequence number, so a read already in flight when the send ended neither absorbs nor clears it. If the post-send read was dropped (one read at a time), it is re-asked when the running one lands.
- **F3** (item 3): dedupe in the domain (`planSend`), first occurrence wins, order kept; resolve/unresolve dedupe by the resolved THREAD id (a thread id and one of its comment ids name the same thread). Both frontends get it from there.
- **F4** (item 9): request faults → 400 (unchanged); a lookup the request cannot fix (the PR's diff not fetched) → 422; the forge out of budget → 504; the forge unreachable (`ErrForgeUnavailable`) → 502; any other lookup failure → 422.
- **F5** (item 4): documented, no busy hook: a web send while the hosting TUI runs an op waits on the repo gate, bounded by the item-2 budget (→ 504).
- **F6** (item 6): abort exits 1, detected as `err == nil && !res.Changed` (rebase.go's precedent); the message `aborted: sending to …` stays on stdout. README + CLAUDE-details say so; the skill does not (agents cannot run `gg pr send`), so no `agentskill.Version` bump.
- **F7** (item 10): prSeen/prUpdated/prOfflineSince/prRefreshing and the new own-send fields reset on `reRoot` only. `closeFilesView` is NOT a reset point: the moved-head reopen runs through it after setting "updated".
- **F8** (item 12): the PR's bars are wiped by the same-tag re-resolve (`resolvePreviewCmd` never fills `msg.groups`, the handler assigns nil). Fix there. A PR view has no saved-preview row (`po.id == ""`), so `afterPreviewsRefresh`'s row path is not a PR path and is left alone.
- **F9** (item 13): one sha-keyed file cache (`Service.shaFile`) for immutable `<full sha>:<path>` reads; any other rev (`HEAD`, `<sha>^`, a branch) reads through uncached. Used by `PreviewNotesAll`, `prReviewNotes` and `remarkPlace`.
- **F10** (item 16): skip reasons become whole formats (`"%s (skipped: not in this PR)"`); "resolved after sending" becomes `"%s · resolved after sending"`. The `(file)` / `(1 reply)` / `(%d replies)` tokens stay as suffix tokens (shared with other screens; a count token, not a sentence).
- **F11** (item 11): a TUI generation `forgeSendGen` bumped in `reRoot` guards `forgeSendReadyMsg`, `sendGroupsMsg` and `sendBodyMsg` (`prsGen` cannot: every PR-list read bumps it). A stale answer is dropped silently.
- **F12** (item 17): the TUI keeps the body like the web: `keptSendBody{pr, group, verdict, text}` set on ctrl+s, prefilled by the next Send review…/Verdict… of the same PR and group, cleared by a send with `err == nil && res.Changed` or by `reRoot`.
- **F13** (item 15): a repeated chooser label gets ` (2)`, ` (3)` … (`i18n.T("%s (%d)", …)`), and the pick resolves by index.

## Review Focus

1. Dedupe keeps the first occurrence's ORDER (a send's thread order is the order the user named them), and a duplicate never becomes a skip row.
2. A forge that never answers: the POST returns a status within the budget — never a hung socket — and nothing is written.
3. Abort vs success in the done event: an abort neither arms "own send" nor drops a kept body (web and TUI).
4. A same-tag re-resolve of a PR view (a note edit) keeps the group bars.
5. A plan/groups/body answer that lands after `R` does nothing in the new repo (no modal, no popup, no op).

---

### Task 1: Domain — dedupe ids; readable Finish/Discard rows (items 3, 14)

**Files:**
- Modify: `internal/domain/forge_send.go` (`planSend`, `planActions`)
- Test: `internal/domain/forge_send_dedupe_test.go` (create)

**Interfaces:**
- Produces: `SendPlan.Items[i].Summary` for `SendFinish`/`SendDiscard` = the note's summary, the remark's summary, or `"review summary"` for a summary mark; `Label` = `<summary> (waiting in the pending review)`. `Key` unchanged. Task 10 reads `Summary`.
- Produces: `uniqIDs([]string) []string` (unexported).

- [ ] **Step 1: Write the failing tests**

```go
package domain

import (
	"context"
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

// Review Focus 1: a note named twice is one thread, in first-occurrence order.
func TestPlanSendCollapsesDuplicateNotes(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 25, "second line")
	b := addPRNote(t, svc, head, "big.go", 5, "first line")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{a, b, a, b}})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 2 || len(p.Skipped) != 0 || p.Items[0].Key != a || p.Items[1].Key != b {
		t.Fatalf("items %+v skipped %+v", p.Items, p.Skipped)
	}
}

// A thread named by its id and by one of its comments is resolved once; a
// draft reply named twice is posted once.
func TestPlanSendCollapsesDuplicateActions(t *testing.T) {
	t.Parallel()
	svc, _, _, _ := prThreadSvc(t)
	d := draftReply(t, svc, "on it")
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Notes: []string{d, d},
		Resolve: []string{"PRRT_t1", model.ForgeNoteIDPrefix + "PRRC_c1", "PRRT_t1"}})
	if err != nil {
		t.Fatal(err)
	}
	if p.Mode != engine.SendActions || len(p.Items) != 2 {
		t.Fatalf("plan = %+v", p.Items)
	}
}

// Item 14: an interrupted send's rows name what waits, not its ledger key.
func TestFinishRowsNameTheNotes(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendRepo(t)
	a := addPRNote(t, svc, head, "big.go", 5, "rename this")
	stampNote(t, svc, a, model.NoteSend{PR: 7, Review: "PRR_p", Err: "network down"})
	pendingOnGitHub(ff)
	p, err := svc.planSend(context.Background(), PRSendRequest{PR: 7, Finish: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Items) != 1 || p.Items[0].Key != a || p.Items[0].Summary != "rename this" ||
		p.Items[0].Label != "rename this (waiting in the pending review)" {
		t.Fatalf("items = %+v", p.Items)
	}
}
```

- [ ] **Step 2: Run them — they fail**

Run: `go test ./internal/domain/ -run 'TestPlanSendCollapses|TestFinishRowsNameTheNotes' -count=1`
Expected: FAIL — 4 items (duplicates kept), 4 action items, and the finish row's Summary empty / Label = the raw key.

- [ ] **Step 3: Implement**

In `forge_send.go` add, and call at the top of `planSend` (before `PRRevalidate`):

```go
// uniqIDs drops repeated ids, keeping each first occurrence in place: a
// note named twice is one thread (Review Focus 1).
func uniqIDs(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
```

```go
	req.Notes, req.Resolve, req.Unresolve = uniqIDs(req.Notes), uniqIDs(req.Resolve), uniqIDs(req.Unresolve)
```

In `planActions`' resolve/unresolve loop, skip a thread already planned for the same kind:

```go
		done := map[string]bool{}
		for _, id := range x.ids {
			th, err := s.threadIDFor(ctx, plan.PR, id)
			if err != nil {
				return engine.SendPlan{}, err
			}
			if done[th] { // a thread named by its id and by one of its comments
				continue
			}
			done[th] = true
			plan.Items = append(plan.Items, engine.SendItem{Label: x.verb + th, Kind: x.kind, ThreadID: th})
		}
```

In the Finish/Discard branch, replace the item loop with readable rows:

```go
		names := s.interruptedNames(ctx, keys)
		for _, k := range keys {
			plan.Items = append(plan.Items, engine.SendItem{Key: k, Summary: names[k],
				Label: names[k] + " (waiting in the pending review)"})
		}
```

and add:

```go
// interruptedNames is what each ledger key of an interrupted send says to a
// person: a note's summary, a remark's summary, "review summary" for a
// summary mark; the key itself when nothing better is stored.
func (s *Service) interruptedNames(ctx context.Context, keys []string) map[string]string {
	out := make(map[string]string, len(keys))
	byID, _ := s.storedNotes(ctx) // nil on error: every key falls back to itself
	for _, k := range keys {
		out[k] = k
		if rid, fp, ok := parseRemarkKey(k); ok {
			if fp == summaryFP || strings.HasPrefix(fp, summaryFP+":") {
				out[k] = "review summary"
				continue
			}
			if r, err := s.Review(ctx, rid); err == nil {
				for _, d := range r.docRemarks() {
					if d.fp == fp {
						out[k] = cutLabel(d.summary)
					}
				}
			}
			continue
		}
		if n, ok := byID[k]; ok && n.Summary != "" {
			out[k] = cutLabel(n.Summary)
		}
	}
	return out
}
```

(Check `summaryKey()`'s actual format in `review_threads.go` while implementing; match it in the `HasPrefix` test.)

- [ ] **Step 4: Run them — they pass; run the package**

Run: `go test ./internal/domain/ -count=1 > $WS/t1.log 2>&1; tail -5 $WS/t1.log`
Expected: `ok  github.com/homeend/gigagit/internal/domain`.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain/forge_send.go internal/domain/forge_send_dedupe_test.go
git commit -F $WS/msg-t1.txt   # "fix(send): a repeated id is one thread; interrupted rows name their notes"
```

---

### Task 2: Domain — sha-keyed lines cache for note placement (item 13)

**Files:**
- Modify: `internal/domain/query.go` (add `shaFile`), `internal/domain/previewnotes.go` (`PreviewNotesAll`), `internal/domain/pr_reviews.go` (`prReviewNotes.read`), `internal/domain/review_doc.go` (`revLines`)
- Test: `internal/domain/sha_lines_test.go` (create)

**Interfaces:**
- Produces: `func (s *Service) shaFile(ctx context.Context, rev, path string) ([]byte, error)` — cached iff `rev` is a full 40- or 64-hex object id; the bytes are shared, read-only.

- [ ] **Step 1: Write the failing test**

```go
package domain

import (
	"context"
	"testing"
)

// Item 13: a PR's note badges re-read on every comment change. Content at a
// commit sha never changes, so a second read — even after a note write
// bumped the notes generation — runs no `git show`.
func TestPRNoteGroupsReadEachFileOnce(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	addPRNote(t, svc, head, "big.go", 5, "mine")
	saveHeadReview(t, svc, head, twoRemarks)
	cr := newCountingRunner(svc.repo.Runner)
	svc.repo.Runner = cr
	set := prNoteSetOf(t, svc)
	ctx := context.Background()
	if _, err := svc.PreviewNoteGroups(ctx, set); err != nil {
		t.Fatal(err)
	}
	addPRNote(t, svc, head, "big.go", 25, "another") // bumps the notes generation
	cr.reset()
	if _, err := svc.PreviewNoteGroups(ctx, set); err != nil {
		t.Fatal(err)
	}
	if _, _, err := svc.PreviewNoteCounts(ctx, set); err != nil {
		t.Fatal(err)
	}
	if n := cr.count("git show"); n != 0 {
		t.Fatalf("the second read ran %d git show", n)
	}
}
```

- [ ] **Step 2: Run it — it fails**

Run: `go test ./internal/domain/ -run TestPRNoteGroupsReadEachFileOnce -count=1`
Expected: FAIL `the second read ran N git show` (N ≥ 2: PreviewNotesAll's tip read + remarkPlace's).

- [ ] **Step 3: Implement**

`query.go`:

```go
// shaFile is path's content at rev. Content at a full object id never
// changes, so those reads are cached for the session (the PR view re-places
// its notes on every comment change); any other rev — HEAD, <sha>^, a
// branch — is read through. The bytes are SHARED: callers never write them.
func (s *Service) shaFile(ctx context.Context, rev, path string) ([]byte, error) {
	if !isFullOID(rev) {
		return s.ShowFile(ctx, rev, path)
	}
	v, err := s.factory.Cache("sha-file").GetOrLoad("sha-file:"+rev+":"+path, func() (any, error) {
		b, err := s.ShowFile(ctx, rev, path)
		if err != nil {
			return nil, err
		}
		return cachedFile(b), nil
	})
	if err != nil {
		return nil, err
	}
	return v.(cachedFile), nil
}

// cachedFile weighs its bytes for the cache's byte budget.
type cachedFile []byte

func (c cachedFile) Size() int { return len(c) }

// isFullOID: a 40- (sha1) or 64- (sha256) lowercase hex object id.
func isFullOID(rev string) bool {
	if len(rev) != 40 && len(rev) != 64 {
		return false
	}
	for _, c := range rev {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return true
}
```

(If a twin of `isFullOID` already exists — `grep -rn 'func isHex\|func isFullSHA\|func isOID' internal/domain` — use it.) Bytes, not lines, are cached: `splitLines` (CRLF-folding) and `revLines` (no folding) keep their own splits, so no placement changes behaviour.

`previewnotes.go` `PreviewNotesAll`: `s.ShowFile(ctx, set.Tip, p)` → `s.shaFile(ctx, set.Tip, p)` (the `splitLines` beside it stays).

`pr_reviews.go` `prReviewNotes` `read`: `s.ShowFile(ctx, rev, path)` → `s.shaFile(ctx, rev, path)`.

`review_doc.go` `revLines`: `s.ShowFile(ctx, rev, path)` → `s.shaFile(ctx, rev, path)` (its split unchanged) — this is what `remarkPlace` reads through.

- [ ] **Step 4: Run it — it passes; run the package**

Run: `go test ./internal/domain/ -count=1 > $WS/t2.log 2>&1; tail -5 $WS/t2.log`
Expected: `ok`.

- [ ] **Step 5: Commit** — `perf(notes): place a PR's notes from cached file text at a sha`

---

### Task 3: Web — end-to-end resolve / unresolve / send-drafts; drop the dead skip (item 7)

**Files:**
- Modify: `internal/web/prsend_test.go` (thread ids on `sendServer`'s comments; three tests), `internal/web/prs_static_test.go` (dead skip)

These pin existing behaviour (the routes work today), so the RED step is a guard-removal check, not a missing feature.

- [ ] **Step 1: Thread ids in `sendServer`**

```go
	cs := prThreads()
	for i := range cs { // the threads a send resolves and replies to
		switch cs[i].ID {
		case "C1", "C2":
			cs[i].ThreadID = "PRRT_c1"
		case "C3":
			cs[i].ThreadID = "PRRT_c3"
		}
	}
	wf := &writerForge{fakeForge: &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: cs}}
```

- [ ] **Step 2: The tests**

```go
// sendKind posts one send, answers its confirm with "send", and returns the
// forge's writes.
func sendKind(t *testing.T, ts *httptest.Server, wf *writerForge, body string) string {
	t.Helper()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", body)
	if code != 202 {
		t.Fatalf("start %s = %d %v", body, code, out)
	}
	evs := followDecide(t, ts, out["op_id"].(string), "send")
	if done, _ := findEvent(evs, "done"); done["ok"] != true {
		t.Fatalf("done = %v", done)
	}
	return wf.writeLog()
}

// Serial: sendServer.
func TestWebResolvesAndReopensAThread(t *testing.T) {
	ts, wf, _ := sendServer(t)
	if w := sendKind(t, ts, wf, `{"kind":"resolve","ids":["forge:C1"]}`); !strings.Contains(w, "Resolve PRRT_c1") {
		t.Fatalf("writes %s", w)
	}
	if w := sendKind(t, ts, wf, `{"kind":"unresolve","ids":["forge:C3"]}`); !strings.Contains(w, "Unresolve PRRT_c3") {
		t.Fatalf("writes %s", w)
	}
}

// Serial: sendServer.
func TestWebSendsADraftReply(t *testing.T) {
	ts, wf, _ := sendServer(t)
	code, out := postJSONAny(t, ts, "/api/notes/reply", `{"id":"forge:C1","summary":"done in the next push"}`)
	id, _ := out["id"].(string)
	if code != 200 || id == "" {
		t.Fatalf("reply = %d %v", code, out)
	}
	if w := sendKind(t, ts, wf, `{"kind":"notes","ids":["`+id+`"]}`); !strings.Contains(w, "Reply PRRT_c1") {
		t.Fatalf("writes %s", w)
	}
}
```

- [ ] **Step 3: Run them; then prove they can fail**

Run: `go test ./internal/web/ -run 'TestWebResolvesAndReopensAThread|TestWebSendsADraftReply' -count=1`
Expected: PASS. Then temporarily change the `"resolve"` case in `prSendRequest` to `req.Unresolve, err = pick(threads)`, re-run: FAIL (`Unresolve PRRT_c1`). Restore it with Edit (never `git checkout --`), re-run: PASS. Ledger the guard-removal check.

- [ ] **Step 4: Drop the dead skip** in `TestPRPageSendsOnlyTheNumber`: delete the `if os.IsNotExist(err) && f != "prs.js" { continue }` block (every listed module exists now). Run `go test ./internal/web/ -run TestPRPageSendsOnlyTheNumber -count=1` → PASS.

- [ ] **Step 5: Run the package; commit** — `test(web): resolve, reopen and draft replies end to end`

Run: `go test ./internal/web/ -count=1 > $WS/t3.log 2>&1; tail -5 $WS/t3.log` → `ok`.

---

### Task 4: Web — a forge budget on the send's plan; honest statuses (items 2, 9)

**Files:**
- Modify: `internal/web/prsend.go`, `internal/web/prs.go` (budget var)
- Test: `internal/web/prsend_test.go`

**Interfaces:**
- Produces: `var prSendBudget = prRevalidateBudget` (package var, tests shorten it); `func prSendLookupStatus(err error) int`; `sendErrStatus` gains `context.DeadlineExceeded → 504`.
- Consumes: `domain.ErrSendRequest`, `domain.ErrForgeUnavailable`.

- [ ] **Step 1: Failing tests**

`writerForge` gains a snapshot gate:

```go
	snapGate chan struct{} // when non-nil, Snapshot waits for it or its ctx
```

```go
func (f *writerForge) Snapshot(ctx context.Context, n int) (forge.Snapshot, error) {
	if g := f.snapGate; g != nil {
		select {
		case <-g:
		case <-ctx.Done():
			return forge.Snapshot{}, ctx.Err()
		}
	}
	// …the existing body
}
```

(set `snapGate` only after `sendServer` returned: its pr-fetch reads a snapshot.)

```go
// Review Focus 2: a forge that never answers costs the POST its budget, not
// a hung socket; nothing is written. Serial: sendServer + a package var.
func TestWebSendPlanHasAForgeBudget(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	prev := prSendBudget
	prSendBudget = 300 * time.Millisecond
	defer func() { prSendBudget = prev }()
	wf.snapGate = make(chan struct{}) // never closed
	start := time.Now()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != http.StatusGatewayTimeout || time.Since(start) > 5*time.Second {
		t.Fatalf("= %d %v after %v (want 504 within the budget)", code, out, time.Since(start))
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("wrote %s", w)
	}
}

func TestPRSendLookupStatus(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		err  error
		want int
	}{
		{fmt.Errorf("%w: #8's diff is not available here", domain.ErrSendRequest), 422},
		{context.DeadlineExceeded, 504},
		{errors.Join(domain.ErrForgeUnavailable, errors.New("gh: not logged in")), 502},
		{errors.New("disk on fire"), 422},
	} {
		if got := prSendLookupStatus(tc.err); got != tc.want {
			t.Errorf("%v → %d, want %d", tc.err, got, tc.want)
		}
	}
}
```

Plus one integration leg in `TestWebSendRefusesForgedValues`' neighbourhood: a second listed, never-fetched PR:

```go
// Item 9: a PR whose diff is not fetched is not the request's fault: 422.
// Serial: sendServer.
func TestWebSendOnAnUnfetchedPRIs422(t *testing.T) {
	ts, wf, _ := sendServer(t)
	wf.mu.Lock()
	pr8 := openPR(8, "Another")
	pr8.HeadSHA = strings.Repeat("d", 40)
	wf.open = append(wf.open, pr8)
	wf.mu.Unlock()
	waitPRsListed(t, ts, 8) // see below
	if code, out := postJSONAny(t, ts, "/api/pr/send?n=8", `{"kind":"notes","ids":["x"]}`); code != 422 {
		t.Fatalf("= %d %v", code, out)
	}
}
```

(`waitPRsListed`: if the list is cached, force a re-list the way the existing tests do — look at `waitPRsLoaded` / a `?refresh=1` list read in `prs_test.go` and reuse it; if `knownPR` 404s an unlisted PR the test fails loudly and the helper is wrong, not the code.)

- [ ] **Step 2: Run — they fail**

Run: `go test ./internal/web/ -run 'TestWebSendPlanHasAForgeBudget|TestPRSendLookupStatus|TestWebSendOnAnUnfetchedPRIs422' -count=1`
Expected: compile failure first (`prSendBudget`, `prSendLookupStatus` undefined) — the RED for these is the missing seam; after adding empty stubs (`prSendBudget` var, `prSendLookupStatus` returning 400) the budget test hangs until the test timeout or answers non-504, the table test fails, the 422 test gets 400.

- [ ] **Step 3: Implement** (`prsend.go`)

```go
// prSendBudget bounds the forge reads a send's plan makes (the PR, its
// comments): a forge that hangs answers 504, never a hung request. A var so
// tests can shorten it.
var prSendBudget = prRevalidateBudget
```

In `handlePRSend`, after the `opInFlight` check:

```go
	ctx, cancel := context.WithTimeout(r.Context(), prSendBudget)
	defer cancel()
```

(the op itself runs under `startRun`'s own context — the budget bounds only the planning.)

Request faults inside `prSendRequest` (body too long, too many ids, `ids required`, an id not of the PR, unknown kind, a group not listed) wrap `errBadSend`:

```go
// errBadSend marks a request the page got wrong: 400. Every other refusal
// is a lookup the request cannot fix (prSendLookupStatus).
var errBadSend = errors.New("bad send request")
```

e.g. `fmt.Errorf("%w: %q is not a note of pull request #%d", errBadSend, id, n)`; the error TEXT the page shows must not gain a prefix — use a small wrapper type instead if `%w` would change the words:

```go
type badSend struct{ error }

func (b badSend) Unwrap() error { return errBadSend }
```

and `return req, badSend{fmt.Errorf("%q is not a note of pull request #%d", id, n)}`.

Lookup failures (`prSendIDs`, `PRSendGroups`) return unwrapped. The handler:

```go
	req, err := prSendRequest(ctx, svc, pr.Number, in)
	if err != nil {
		status := http.StatusBadRequest
		if !errors.Is(err, errBadSend) {
			status = prSendLookupStatus(err)
		}
		writeErr(w, status, err)
		return
	}
```

```go
// prSendLookupStatus maps a failure to read the PR's notes or groups — never
// the request's fault — to its HTTP status.
func prSendLookupStatus(err error) int {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, domain.ErrForgeUnavailable):
		return http.StatusBadGateway
	}
	return http.StatusUnprocessableEntity
}
```

`sendErrStatus` gains `case errors.Is(err, context.DeadlineExceeded): return http.StatusGatewayTimeout` first.

Every existing row of `TestWebSendRefusesForgedValues` keeps its status (all are request faults → still 400) — run it.

- [ ] **Step 4: Run — pass; run the package**

Run: `go test ./internal/web/ -count=1 > $WS/t4.log 2>&1; tail -5 $WS/t4.log` → `ok`.

- [ ] **Step 5: Commit** — `fix(web): a send's plan has a forge budget; lookup failures are not 400`

---

### Task 5: Web page — freshness after my own send; Verdict… keeps its body (items 1, 8)

**Files:**
- Modify: `internal/web/static/prfresh.js`, `internal/web/static/prs.js`, `internal/web/static/prsend.js`
- Test: `internal/web/prfreshjs_test.go`, `internal/web/prsendjs_test.go`
- Probe: scratchpad `probe/` (already in this session's scratchpad)

**Interfaces:**
- `nextFresh(st, ev)`: `st.ownSend` is `null` or a read sequence number; `ev.seq` = the read's sequence (ok/fail), `{kind:"sent", seq}` arms with the last STARTED read's seq. An ok read whose `seq > st.ownSend` (or with no seq) consumes it — changed or not; a read with `seq <= st.ownSend` started before the send and leaves it armed.
- `onSendDone(fn)` hooks receive the done event `ev` (`ok`, `changed`, …) as today.

- [ ] **Step 1: Failing JS test** — append to `TestPRFreshJS` a second runner (keep the first's steps; its `sent` events carry no seq and stay valid):

```go
	const raced = `
import { nextFresh } from "./prfresh.mjs";
let st = { seen: 0, updated: 0, ownSend: null };
const steps = [];
const step = (ev) => { st = nextFresh(st, ev); steps.push(st.text); };
step({ n: 7, kind: "ok", changed: false, seq: 1 });   // first read fills the view
step({ n: 7, kind: "sent", seq: 2 });                 // read 2 was in flight when the send ended
step({ n: 7, kind: "ok", changed: false, seq: 2 });   // that stale read: own send stays armed
step({ n: 7, kind: "ok", changed: true, seq: 3 });    // the post-send read: my own change, absorbed
step({ n: 7, kind: "ok", changed: true, seq: 4 });    // someone else's: updated
step({ n: 7, kind: "sent", seq: 4 });
step({ n: 7, kind: "ok", changed: false, seq: 5 });   // nothing new after my send: disarmed
step({ n: 7, kind: "ok", changed: true, seq: 6 });    // a genuine change is not swallowed
console.log(JSON.stringify(steps));
`
```

want: `["", "", "", "", "updated", "", "", "updated"]`. (Write it as a table-driven helper running both runners; same node plumbing as the existing test.)

`prsendjs_test.go` gains static guards (the existing file's style):

```go
// Item 1/F1: only a send that changed something is "my own send"; Item 8: the
// verdict's typed body survives a refusal and an abort.
func TestPRSendKeepsBodiesAndArmsOnlyRealSends(t *testing.T) {
	t.Parallel()
	b, _ := os.ReadFile(filepath.Join("static", "prsend.js"))
	p, _ := os.ReadFile(filepath.Join("static", "prs.js"))
	for _, want := range []string{
		`keptBody = { pr, group: "verdict", text }`,
		`if (ev.ok && ev.changed) keptBody = null;`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("prsend.js lacks %q", want)
		}
	}
	for _, want := range []string{
		`if (ev.ok && ev.changed) freshEvent(n, { kind: "sent", seq: readSeq });`,
		`refreshAfterSend(n);`,
	} {
		if !strings.Contains(string(p), want) {
			t.Errorf("prs.js lacks %q", want)
		}
	}
}
```

and update `TestPRsFreshnessAndInterruptedAreWired`'s want list: replace `freshEvent(n, { kind: "sent" });` with the new line.

- [ ] **Step 2: Run — fails**

Run: `go test ./internal/web/ -run 'TestPRFreshJS|TestPRSendKeepsBodies|TestPRsFreshness' -count=1`
Expected: FAIL (steps differ: the stale read clears/the post-send read marks; the static strings are missing).

- [ ] **Step 3: Implement**

`prfresh.js` (header comment updated to say the sequence rule):

```js
export function nextFresh(st, ev) {
  const s = { seen: 0, updated: 0, ownSend: null, ...st };
  switch (ev.kind) {
    case "start":
      return { ...s, text: "refreshing…" };
    case "fail":
      return { ...s, text: ev.age ? "offline · read " + ev.age : "offline" };
    case "sent":
      return { ...s, ownSend: ev.seq === undefined ? 0 : ev.seq, updated: 0, text: "" };
  }
  if (s.seen !== ev.n) return { seen: ev.n, updated: 0, ownSend: null, text: "" };
  // The first read that STARTED after my send carries my own change (or
  // nothing): it is absorbed either way. A read already in flight when the
  // send ended says nothing about it.
  const after = s.ownSend !== null && s.ownSend !== false && (ev.seq === undefined || ev.seq > s.ownSend);
  if (after) return { ...s, ownSend: null, updated: 0, text: "" };
  if (ev.changed) return { ...s, updated: ev.n, text: "updated" };
  return { ...s, updated: 0, text: "" };
}
```

(the first runner's `sent` without seq → `ownSend: 0`; any ok read after it has `seq` undefined → consumed: the old steps hold.)

`prs.js`:

```js
let readSeq = 0; // every comments read's start, in order (prfresh.js's sequence)
```

in `refreshPRComments`, inside the `runOnce` task: `const seq = ++readSeq;` and `freshEvent(n, { kind: "ok", changed: …, seq });`. Initial `fresh` = `{ seen: 0, updated: 0, ownSend: null, text: "" }`.

```js
// refreshAfterSend re-reads PR n once a send ended. A read already running
// started before the send landed: wait for it, then read again — the read
// that absorbs the send's own change must start after it.
function refreshAfterSend(n, tries = 0) {
  if (refreshPRComments(n) === null && tries < 40) setTimeout(() => refreshAfterSend(n, tries + 1), 250);
}

onSendDone((n, ev) => {
  if (ev.ok && ev.changed) freshEvent(n, { kind: "sent", seq: readSeq });
  refreshAfterSend(n);
});
```

`prsend.js` `verdictBody`:

```js
function verdictBody(pr) {
  const kept = keptBody && keptBody.pr === pr && keptBody.group === "verdict" ? keptBody.text : "";
  openPrompt({
    title: "Verdict on #" + pr + " — the review body (optional)",
    value: kept,
    multiline: true,
    allowEmpty: true,
    onSubmit: (text) => {
      keptBody = { pr, group: "verdict", text };
      sendToGitHub(pr, { kind: "verdict", body: text }, "posting a verdict on #" + pr);
    },
  });
}
```

and `onSendDone((n, ev) => { if (ev.ok && ev.changed) keptBody = null; });` (comment: an abort keeps it too — F1).

- [ ] **Step 4: Run — pass; package; node syntax check**

Run: `go test ./internal/web/ -count=1 > $WS/t5.log 2>&1; tail -5 $WS/t5.log` → `ok`; `node --check internal/web/static/prs.js internal/web/static/prsend.js internal/web/static/prfresh.js` → no output.

- [ ] **Step 5: Browser probe (visibility) — old build first**

In `$SCRATCH/probe/`: a `verdict-kept` scenario. Build the OLD binary (`git -C <worktree> stash`-free: build main's `~/go/bin/gg`, which is the unfixed one) and the new one (`go build -o bin/gg ./cmd/gg` in the worktree). Steps the probe drives:
1. open the page, click `#prs-header`, open PR #7 (fake gh fixtures from `fixtures.sh`);
2. right-click the PR row → `Verdict…`, type `keep me please` into the prompt's textarea;
3. before submitting, make the send refuse: start a long op the page knows about (the probe's existing busy trigger, or remove the fake gh snapshot fixture so the plan answers an error);
4. submit → the op line shows the refusal;
5. right-click → `Verdict…` again → assert the textarea is VISIBLE and its value is `keep me please`.
Expected: OLD build fails step 5 (empty textarea), NEW build passes. Record both runs' output in the ledger.

- [ ] **Step 6: Commit** — `fix(web): my own send never hides a genuine update; Verdict… keeps its body`

---

### Task 6: CLI — an aborted `gg pr send` exits 1 (item 6)

**Files:**
- Modify: `internal/cli/prsend.go` (`runPRSend`)
- Test: `internal/cli/prsend_test.go`

- [ ] **Step 1: Failing test** — reuse the file's terminal seam (`sendTerminal`) and fake forge helpers (`runPRAt`, the sendPRRepo fixture). Model it on the existing abort/confirm tests in `prsend_test.go`:

```go
// Item 6: an answered "abort" sent nothing — a wrapper must not read exit 0
// as "posted". Serial: the terminal seam is a package var.
func TestPRSendAbortExitsOne(t *testing.T) {
	// …same setup as the file's confirm test (sendTerminal → true, a note on
	// the PR), stdin answering the abort option…
	code, stdout, _ := runPRAt(t, dir, stdinAbort, "pr", "send", "7", "--note", id)
	if code != 1 || !strings.Contains(stdout, "aborted: sending to") {
		t.Fatalf("exit %d, stdout %q", code, stdout)
	}
}
```

(Read the file's existing test that answers the confirm and copy its exact stdin answer format for "abort".)

- [ ] **Step 2: Run — fails** (`exit 0`).

- [ ] **Step 3: Implement** in `runPRSend` after printing the summary:

```go
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	if !res.Changed { // answered abort: nothing was posted (rebase.go's signal)
		return 1
	}
	return 0
```

- [ ] **Step 4: Run** `go test ./internal/cli/ -count=1 > $WS/t6.log 2>&1; tail -5 $WS/t6.log` → `ok`.

- [ ] **Step 5: Commit** — `fix(cli): an aborted gg pr send exits 1`

---

### Task 7: TUI — "updated" never for my own send; reset on a repo switch (item 10)

**Files:**
- Modify: `internal/tui/model.go` (fields + `reRoot`), `internal/tui/pr_revalidate.go`, `internal/tui/forge_send.go` (`forgeSendFinished`)
- Test: `internal/tui/pr_fresh_own_send_test.go` (create)

**Interfaces:**
- New Model fields: `prReadSeq int` (bumped per `prRefreshCmd` start), `prOwnSend int` (PR of my last changing send, 0 = none), `prOwnSendSeq int` (the `prReadSeq` when it was armed), `prRefreshAgain int` (PR whose post-send read was dropped).
- `prRevalidatedMsg` gains `seq int`.

- [ ] **Step 1: Failing tests**

```go
package tui

import (
	"testing"

	"github.com/homeend/gigagit/internal/engine"
	"github.com/homeend/gigagit/internal/model"
)

func revalidated(m Model, n, seq int, changed bool) Model {
	nm, _ := m.Update(prRevalidatedMsg{n: n, gen: m.prsGen, seq: seq, pr: model.PullRequest{Number: n}, commentsChanged: changed})
	return nm.(Model)
}

// Item 10: the change my own send made is not "updated"; a read already in
// flight when the send ended does not absorb it (it still shows "updated" when it reports a change).
func TestMyOwnSendIsNotUpdated(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false) // the open's first read
	m.prReadSeq = 2                 // read 2 is in flight
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	m = revalidated(m, 7, 2, false)
	m = revalidated(m, 7, 3, true) // the post-send read: my own comment
	if m.prUpdated == 7 {
		t.Fatal(`my own send marked the PR "updated"`)
	}
	m = revalidated(m, 7, 4, true)
	if m.prUpdated != 7 {
		t.Fatal("a genuine change after it was swallowed")
	}
}

// F1: an aborted send (nothing changed) arms nothing.
func TestAnAbortedSendArmsNothing(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{}, nil)
	if m.prOwnSend != 0 {
		t.Fatal("an abort armed the own-send absorb")
	}
}

// A post-send read dropped because one was running is asked again when that
// one lands.
func TestADroppedPostSendReadIsReasked(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m = revalidated(m, 7, 1, false)
	m.prRevalidateInflight, m.prCommentsInflight = true, true
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	if m.prRefreshAgain != 7 {
		t.Fatal("the dropped read was not queued")
	}
	nm, cmd := m.Update(prRevalidatedMsg{n: 7, gen: m.prsGen, seq: m.prReadSeq, pr: model.PullRequest{Number: 7}})
	if mm := nm.(Model); mm.prRefreshAgain != 0 || cmd == nil || !mm.prRevalidateInflight {
		t.Fatalf("again=%d cmd=%v inflight=%v", mm.prRefreshAgain, cmd != nil, mm.prRevalidateInflight)
	}
}

// A repo switch forgets the old repo's PR freshness.
func TestReRootForgetsPRFreshness(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m.prSeen, m.prUpdated, m.prOwnSend, m.prRefreshAgain = 7, 7, 7, 7
	nm, _ := m.reRoot(t.TempDir())
	mm := nm.(Model)
	if mm.prSeen != 0 || mm.prUpdated != 0 || mm.prOwnSend != 0 || mm.prRefreshAgain != 0 || !mm.prOfflineSince.IsZero() {
		t.Fatalf("seen %d updated %d own %d again %d", mm.prSeen, mm.prUpdated, mm.prOwnSend, mm.prRefreshAgain)
	}
}
```

(If `reRoot` on a bare temp dir misbehaves in tests, use the repo-switch test helper the existing reRoot tests use — `grep -n 'reRoot(' internal/tui/*_test.go`.)

- [ ] **Step 2: Run — fail** (`go test ./internal/tui/ -run 'TestMyOwnSend|TestAnAbortedSend|TestADroppedPostSend|TestReRootForgetsPR' -count=1`): compile errors for the new fields first; after adding the bare fields, the behaviour assertions fail.

- [ ] **Step 3: Implement**

`prRefreshCmd`: when it starts a read, `m.prReadSeq++` and the msg carries `seq: m.prReadSeq` (capture before the closure).

`forgeSendFinished`:

```go
	if err == nil && res.Changed && fs.pr != 0 { // my own change (F1): not "updated"
		m.prOwnSend, m.prOwnSendSeq = fs.pr, m.prReadSeq
	}
	if fs.pr != 0 && fs.pr == m.openPRNumber() {
		var c tea.Cmd
		m, c = m.prRefreshCmd(fs.pr, false)
		if c == nil && (m.prRevalidateInflight || m.prCommentsInflight) {
			m.prRefreshAgain = fs.pr // one read at a time: ask again when it lands
		}
		cmds = append(cmds, c, m.prCountsCmd())
	}
```

`handlePRRevalidatedMsg`, the freshness switch:

```go
	own := m.prOwnSend == msg.n && msg.seq > m.prOwnSendSeq
	switch {
	case m.prSeen != msg.n:
		m.prSeen = msg.n // the open's first read: it fills the view
	case own && !moved:
		m.prOwnSend, m.prUpdated = 0, 0 // my own send's change (or nothing): absorbed
	case moved || msg.commentsChanged:
		m.prUpdated = msg.n
		if own {
			m.prOwnSend = 0
		}
	case m.prUpdated == msg.n:
		m.prUpdated = 0 // nothing new: the mark clears (spec §2.4)
	}
```

and, before the `!moved || …` early return:

```go
	if m.prRefreshAgain == msg.n && m.openPRNumber() == msg.n {
		m.prRefreshAgain = 0
		var again tea.Cmd
		m, again = m.prRefreshCmd(msg.n, false)
		cmd = tea.Batch(cmd, again)
	}
```

(only on a successful read — it sits after the `msg.err != nil` return.)

`reRoot`, next to `m.interrupted = nil`:

```go
	// …and the old repo's PR freshness (a PR #7 here is another PR there).
	m.prSeen, m.prUpdated, m.prOwnSend, m.prRefreshAgain = 0, 0, 0, 0
	m.prOfflineSince, m.prRefreshing = time.Time{}, false
```

- [ ] **Step 4: Run** — pass; then `go test ./internal/tui/ -count=1 > $WS/t7.log 2>&1; tail -5 $WS/t7.log` → `ok`.

- [ ] **Step 5: Commit** — `fix(tui): my own send never reads as "updated"; a repo switch forgets PR freshness`

---

### Task 8: TUI — answers from before `R` are dropped; a failed send keeps the typed body (items 11, 17)

**Files:**
- Modify: `internal/tui/forge_send.go`, `internal/tui/send_review_popup.go`, `internal/tui/model.go` (fields, `reRoot`), the four i18n bundles
- Test: `internal/tui/forge_send_kept_test.go` (create)

**Interfaces:**
- New Model fields: `forgeSendGen int`; `keptSendBody *keptSendBody`.
- `type keptSendBody struct { pr int; group string; verdict bool; text string }`.
- `forgeSendReadyMsg`, `sendGroupsMsg`, `sendBodyMsg` gain `gen int`.

- [ ] **Step 1: Failing tests**

```go
package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// Item 11 / Review Focus 5: a plan, a group list or a body read that lands
// after a repo switch does nothing in the new repo.
func TestSendAnswersFromBeforeASwitchAreDropped(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	old := m.forgeSendGen
	m.forgeSendGen++ // what reRoot does
	for _, msg := range []tea.Msg{
		forgeSendReadyMsg{gen: old, req: domain.PRSendRequest{PR: 7, Mine: true}, op: engine.SendToForge{}},
		sendGroupsMsg{gen: old, pr: 7, groups: []domain.SendGroup{{ID: domain.GroupMine, Count: 1}, {ID: "review:r1", Count: 1}}},
		sendBodyMsg{gen: old, pr: 7, group: "review:r1", body: "x"},
	} {
		nm, cmd := m.Update(msg)
		mm := nm.(Model)
		if cmd != nil || mm.modal != nil || mm.running || layerOf[*sendReviewPopup](mm) != nil {
			t.Fatalf("%T acted in the new repo", msg)
		}
	}
}

// Item 17: a plan that fails after the body popup closed keeps the text; the
// next Verdict… of the same PR starts from it; a send that changed GitHub
// drops it.
func TestAFailedPlanKeepsTheTypedBody(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	m, _ = m.openVerdict(7)
	p := layerOf[*sendReviewPopup](m)
	p.body = newTextField("a long thought-out verdict")
	m, _ = p.update(m, tea.KeyMsg{Type: tea.KeyCtrlS})
	nm, _ := m.Update(forgeSendReadyMsg{gen: m.forgeSendGen, req: domain.PRSendRequest{PR: 7, Verdict: true, BodySet: true},
		err: errors.New("network down")})
	m = nm.(Model)
	m, _ = m.openVerdict(7)
	if got := layerOf[*sendReviewPopup](m).body.Value(); got != "a long thought-out verdict" {
		t.Fatalf("body = %q", got)
	}
	m = m.popLayer()
	m, _ = m.forgeSendFinished(&forgeSendState{pr: 7}, engine.Result{Changed: true}, nil)
	m, _ = m.openVerdict(7)
	if got := layerOf[*sendReviewPopup](m).body.Value(); got != "" {
		t.Fatalf("a sent body came back: %q", got)
	}
}
```

- [ ] **Step 2: Run — fail** (`go test ./internal/tui/ -run 'TestSendAnswersFromBeforeASwitch|TestAFailedPlanKeepsTheTypedBody' -count=1`): compile errors, then a modal/popup appears and the body comes back empty.

- [ ] **Step 3: Implement**

- `forgeSendCmd`, `openSendReview`, `openSendReviewBody` capture `gen := m.forgeSendGen` and put it in their msg.
- `handleForgeSendReady`, `handleSendGroups`, `handleSendBody` start with `if msg.gen != m.forgeSendGen { return m, nil } // planned in the repo before R`.
- `reRoot`: `m.forgeSendGen++` and `m.keptSendBody = nil` beside the Task 7 resets.
- `sendReviewPopup.update` ctrl+s, after the opsIdle check and before `popLayer`: `m.keptSendBody = &keptSendBody{pr: p.pr, group: p.group, verdict: p.verdict, text: p.body.Value()}`.
- `openVerdict`, `openSendReviewBody` (mine branch) and `handleSendBody` prefill: a helper

```go
// keptBodyFor is the text a failed send of the same PR and group left
// (F12): the next body box starts from it.
func (m Model) keptBodyFor(pr int, group string, verdict bool) (string, bool) {
	k := m.keptSendBody
	if k == nil || k.pr != pr || k.group != group || k.verdict != verdict {
		return "", false
	}
	return k.text, true
}
```

  `handleSendBody` uses the kept text over `msg.body` when present.
- `forgeSendFinished`: `if err == nil && res.Changed { m.keptSendBody = nil }`.
- `handleForgeSendReady` error arm: when `m.keptSendBody != nil && m.keptSendBody.pr == msg.req.PR`, say `i18n.T("send: %s — the text you typed is kept", firstLine(msg.err.Error()))`, else the existing `"send: %s"`.
- Bundles: add `"send: %s — the text you typed is kept"` to ja/ko/zh/ru next to `"send: %s"`:
  - ja `"送信: %s — 入力したテキストは保持されています"`
  - ko `"전송: %s — 입력한 텍스트는 유지됩니다"`
  - zh `"发送：%s — 已保留您输入的文本"`
  - ru `"отправка: %s — введённый текст сохранён"`

- [ ] **Step 4: Run** — pass; i18n gates: `go test ./internal/i18n/ ./internal/tui/ -run 'I18n|EngineProse|DecisionOptionValues|ActionMenuLabels|CheckVerbs|FooterRendersTranslated' -count=1` → ok; whole package → `ok`.

- [ ] **Step 5: Commit** — `fix(tui): send answers from before a repo switch are dropped; a failed send keeps the body`

---

### Task 9: TUI — group bars survive a re-resolve; unique chooser rows (items 12, 15)

**Files:**
- Modify: `internal/tui/preview_open.go` (`resolvePreviewCmd`), `internal/tui/send_review_popup.go` (`sendGroupOptions`, `handleSendGroups`), the four bundles
- Test: `internal/tui/pr_send_serial_test.go` (serial: needs `prSendModel`'s PR ref), `internal/tui/forge_send_fixes_test.go`

- [ ] **Step 1: Failing tests**

```go
// Item 12 / Review Focus 4: the re-resolve a note edit triggers keeps the
// PR's group bars (it used to hand the view nil groups). Serial: prSendModel.
func TestAPRReResolveKeepsTheGroupBars(t *testing.T) {
	m, _, head := prSendModel(t)
	addTUINote(t, m, head, 5, "mine")
	msg := m.reopenPreviewCmd("", "refs/gg/pr/7", "main", "", "")().(previewOpenMsg)
	if g := msg.groups["big.go"]; len(g) == 0 || g[0] != domain.GroupMine {
		t.Fatalf("re-resolve groups = %v", msg.groups)
	}
}
```

```go
// Item 15: two AI reviews with the same agent and summary get distinct rows,
// and each row opens its own review.
func TestSendGroupChooserRowsAreUnique(t *testing.T) {
	t.Parallel()
	m := prDiffModel(t)
	gs := []domain.SendGroup{{ID: "review:r1", Agent: "claude", Summary: "nits", Count: 1},
		{ID: "review:r2", Agent: "claude", Summary: "nits", Count: 1}}
	opts := sendGroupOptions(gs)
	if opts[0] == opts[1] {
		t.Fatalf("rows %q", opts)
	}
	nm, _ := m.Update(sendGroupsMsg{gen: m.forgeSendGen, pr: 7, groups: gs})
	mm := nm.(Model)
	_, cmd := mm.modal.onResolve(mm, opts[1])
	if b, ok := cmd().(sendBodyMsg); !ok || b.group != "review:r2" {
		t.Fatalf("the second row opened %+v", b)
	}
}
```

(`sendGroupsMsg.gen` exists after Task 8. `ReviewBodyText("r2")` errors in this model — that is fine: the msg still names the group.)

- [ ] **Step 2: Run — fail** (`go test ./internal/tui/ -run 'TestAPRReResolveKeepsTheGroupBars|TestSendGroupChooserRowsAreUnique' -count=1`): groups empty; identical rows / the second row opens r1.

- [ ] **Step 3: Implement**

`resolvePreviewCmd`, beside `PreviewNoteCounts`:

```go
				msg.groups, _ = svc.PreviewNoteGroups(context.Background(), set) // a PR's colour bars; empty elsewhere
```

`sendGroupOptions`:

```go
func sendGroupOptions(groups []domain.SendGroup) []string {
	opts := make([]string, 0, len(groups)+1)
	seen := map[string]int{}
	for _, g := range groups {
		label := sendGroupLabel(g)
		if seen[label]++; seen[label] > 1 { // two reviews alike: number the repeats
			label = i18n.T("%s (%d)", label, seen[label])
		}
		opts = append(opts, label)
	}
	return append(opts, "Cancel")
}
```

(the index loop in `onResolve` then matches exactly one row.) Bundles: `"%s (%d)" = "%s (%d)"` in all four (zh: `"%s（%d）"`).

- [ ] **Step 4: Run** — pass; i18n gates; whole package `ok`.

- [ ] **Step 5: Commit** — `fix(tui): a note edit keeps a PR's group bars; repeated review rows are numbered`

---

### Task 10: TUI — confirm rows as whole sentences; Finish/Discard rows show summaries (items 16, 14)

**Files:**
- Modify: `internal/tui/forge_send.go` (`sendItemText`, `sendSkipText`, `sendSkipReasonText`), the four bundles
- Test: `internal/tui/forge_send_test.go`

**Interfaces:**
- Consumes: Task 1's `SendItem.Summary` for `SendFinish`/`SendDiscard`.

- [ ] **Step 1: Failing tests**

```go
// Item 14: an interrupted send's rows say what waits, not a ledger key.
func TestFinishRowsShowSummaries(t *testing.T) {
	t.Parallel()
	got := sendItemText(engine.SendFinish, engine.SendItem{Key: "n-123", Summary: "rename this"})
	if got != "rename this (waiting in the pending review)" {
		t.Fatalf("row = %q", got)
	}
	if got := sendItemText(engine.SendFinish, engine.SendItem{Key: "n-123"}); !strings.Contains(got, "n-123") {
		t.Fatalf("no summary falls back to the key: %q", got)
	}
}

// Item 16: each skip row and the resolve suffix is ONE format — a translation
// orders the whole row.
func TestSendRowsAreWholeFormats(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("forge_send.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{`what + " " + sendSkipReasonText`, `" · " + i18n.T("resolved after sending")`} {
		if strings.Contains(string(src), bad) {
			t.Errorf("forge_send.go still composes %q", bad)
		}
	}
	if got := sendSkipText(engine.SendSkip{Path: "a.go", Line: 3, Summary: "x", Reason: domain.SkipNotInPR}); got != "a.go:3 x (skipped: not in this PR)" {
		t.Fatalf("skip row = %q", got)
	}
}
```

- [ ] **Step 2: Run — fail.**

- [ ] **Step 3: Implement**

```go
func sendItemText(mode engine.SendMode, it engine.SendItem) string {
	if mode == engine.SendFinish || mode == engine.SendDiscard {
		what := it.Summary
		if what == "" {
			what = it.Key
		}
		return i18n.T("%s (waiting in the pending review)", what)
	}
	// …unchanged up to the resolve suffix…
	if it.Resolve {
		s = i18n.T("%s · resolved after sending", s)
	}
	return s
}

func sendSkipText(sk engine.SendSkip) string {
	what := sk.Summary
	switch {
	case sk.Path != "":
		what = sendWhere(sk.Path, sk.Line) + " " + sk.Summary
	case sk.Reason == domain.SkipOnGitHub:
		what = i18n.T("review summary")
	}
	return sendSkipReasonText(what, sk.Reason)
}

// sendSkipReasonText is one skipped row: what, then why — one format per
// reason, so a translation orders the whole row. An unknown code (a newer
// domain) shows as data in a generic frame.
func sendSkipReasonText(what, reason string) string {
	switch reason {
	case domain.SkipNotInPR:
		return i18n.T("%s (skipped: not in this PR)", what)
	case domain.SkipLinesChanged:
		return i18n.T("%s (skipped: its lines changed)", what)
	case domain.SkipBeingSent:
		return i18n.T("%s (skipped: already being sent)", what)
	case domain.SkipOnGitHub:
		return i18n.T("%s (skipped: already on GitHub)", what)
	case domain.SkipGone:
		return i18n.T("%s (skipped: it no longer exists)", what)
	case domain.SkipThreadNotInPR:
		return i18n.T("%s (skipped: its thread is not in this PR)", what)
	}
	return i18n.T("%[1]s (skipped: %[2]s)", what, reason)
}
```

Update `TestEverySkipReasonHasItsOwnWords` to the new signature. Bundles (all four): rename the seven `"(skipped: …)"` keys to the `"%s (skipped: …)"` / `"%[1]s (skipped: %[2]s)"` forms (translation = `%s ` + the old text; ru/ja/ko/zh keep their own parenthesis style, e.g. zh `"%s（跳过：不在此 PR 中）"`, the generic one `"%[1]s（跳过：%[2]s）"`), and `"resolved after sending"` → `"%s · resolved after sending"` (ja `"%s · 送信後に解決"`, ko `"%s · 보낸 뒤 해결"`, zh `"%s · 发送后标为已解决"`, ru `"%s · закрывается после отправки"`). First `grep -rn '"resolved after sending"\|"(skipped: ' internal/tui internal/web --include=*.go` — remove a bundle key only when no other call site uses it (the orphan gate decides).

- [ ] **Step 4: Run** — pass; i18n gates; whole package `ok`.

- [ ] **Step 5: Commit** — `fix(tui): confirm rows are whole sentences; interrupted rows show what waits`

---

### Task 11: Docs (items 4, 5, 6 + the stage)

**Files:** `CLAUDE.md` (cli row), `docs/CLAUDE-details.md` (the three "Sending to GitHub" sections), `README.md` (Sending to GitHub), `CHANGELOG.md` (new top section), memory `github-write-feature.md`.

- [ ] **Step 1:** `CLAUDE.md` cli row: `gg pr` (read + send/reply/resolve/notes) — "pending" removed. (Architecture unchanged otherwise.)
- [ ] **Step 2:** `docs/CLAUDE-details.md`:
  - "…from gg web": a send while the HOSTING TUI runs an op waits on the repo gate (the web's `opInFlight` sees only the page's own ops); the plan's forge budget (`prSendBudget` = 30 s) bounds it → 504. Statuses: 400 request fault, 409 busy / head moved (`code: head_moved`) / interrupted pending, 422 lookup or planning refusal, 502 forge unreachable, 504 forge out of budget. Ids are deduped in the domain. The freshness sequence rule (F2) and kept bodies (verdict included).
  - "…from the TUI": `forgeSendGen` (answers from before `R` dropped), `keptSendBody`, own-send absorb via `prReadSeq`/`prOwnSend`/`prRefreshAgain`, resets on `reRoot`, re-resolve carries groups.
  - "Sending to GitHub" (CLI): `gg pr send` exits 1 when the confirm is answered abort; Finish/Discard rows name the notes; `Service.shaFile`.
- [ ] **Step 3:** README Sending to GitHub: one line "Answering abort exits 1 (nothing was posted)."
- [ ] **Step 4:** CHANGELOG top section "GitHub write-back follow-ups" listing the user-visible fixes (one line each).
- [ ] **Step 5:** `grep -n 'pr send' internal/agentskill/using-gg.md` — the skill names no exit code for `gg pr send` (agents cannot run it): no skill change, no version bump. If the grep shows an exit-code line, update it and bump `agentskill.Version`.
- [ ] **Step 6:** Commit — `docs: GitHub write-back follow-ups`.

---

### Task 12: Gate

- [ ] `./test.sh race > $WS/race.log 2>&1; grep -c 'all green' $WS/race.log` → `1` (green ONLY on "all green"; `| tail` exits 0 on FAIL).
- [ ] `go build -o bin/gg ./cmd/gg` in the worktree (verify binary; `bin/` is ignored — never `git add -A`).
- [ ] Final whole-branch review: read-only subagent on the most capable model with the review package, this plan, the spec, the Review Focus verbatim and the ledger's `Ruling:` lines.
