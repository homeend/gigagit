# PR Follow-ups 3 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans (the user chose native execution) to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the follow-ups-2 final review's deferred minors and the open
PR-note call: a PR-view note is never stored plain, a PR link always lands or
says why (web and TUI), and two read races stop losing state.

**Architecture:** Each fix stays in the layer that owns the behavior. Domain
answers "which scope does this note get" and now refuses instead of returning
""; the web handler maps the refusals to 409. The web's PR landing waits for
the work already in flight (a list read, another PR open) instead of failing;
the pure parts go into `prfresh.js` so node can test them. The TUI's PR
landing takes the enter path's fetch when the head is not local, and its link
copy drops answers from a repo it left.

**Tech Stack:** Go 1.26, Bubble Tea, vanilla ES modules (node-tested pure
helpers), real `git` in temp repos.

**Spec:** none written. The authority is the follow-ups-2 final review
(memory `github-write-feature.md`, deferred minors 3–9) plus the user's rulings
of 2026-10-08:
- **A = Refuse.** A PR-view note that cannot get its PR stamp is refused (409),
  never stored plain.
- **B3 = Clear message.** A link to a PR the web page does not list says one
  line and does not fall back to a plain preview.

## Global Constraints

- No implementer subagents; one read-only final review subagent at the end.
- An AI agent never sends anything to GitHub; tests use the fake forge only.
- A test fixture that writes a PR note stamps it (`"pr":7`, or Preview `main...refs/gg/pr/7`).
- Every user-visible TUI string goes through `i18n.T` with a literal key in all four bundles (ja/ko/zh/ru).
- Web and CLI prose stays English.
- `gg commit` has no `-F`: commit with `gg add <paths>` and then `git commit -F <msgfile>`.
- Before each commit, confirm that the suite printed `ok` for every package. A green grep is not enough.

## Review Focus

1. **A PR note on a merged or unfetched PR.** A note should be refused, not
   stored plain. A note on an open PR whose ref is present must still be
   stamped.
2. **A web landing that arrives while a PR row's open is still fetching.** It
   should wait for that open and then land. It must never say nothing.
3. **A cold page (`gg open --web <PR link>`) on a repo with no cached
   listing.** It should wait for the live list and then land.
4. **A TUI landing on a listed PR whose fetch fails** (no forge, or an op
   already running). The agent should get a failed reply at once, not a
   timeout after two seconds.
5. **A moved-head comments read replaced by a plain one while it waits.** The
   PR view should still show "updated".

---

### Task 1: A — a PR-view note is refused when it cannot be stamped

**Files:**
- Modify: `internal/domain/forge_send.go` (`ErrPRDiffGone` var next to `ErrNoteOffPR`; `PRNoteScope`)
- Modify: `internal/web/notes.go:286-298` (`handleNoteAdd`)
- Test: `internal/domain/pr_note_scope_test.go`, `internal/web/pr_note_scope_test.go`

**Interfaces:**
- Produces: `domain.ErrPRDiffGone`. `PRNoteScope(ctx, pr, commit) (string, error)` never returns `("", nil)` any more. It returns a scope, `ErrNoteOffPR`, `ErrPRDiffGone`, or a git error.

- [ ] **Step 1: Write the failing domain test** (append to `internal/domain/pr_note_scope_test.go`)

```go
// Ruling A (2026-10-08): a PR whose diff is gone here (gg pr forget ran
// elsewhere) refuses the note — it would otherwise be stored plain and
// leave the PR view.
func TestPRNoteScopeRefusesWhenTheDiffIsGone(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	runGitIn(t, repoDir(t, svc), "update-ref", "-d", git.PRRef(7))
	pr := model.PullRequest{Number: 7, State: "open", Target: "main", HeadSHA: head}
	sc, err := svc.PRNoteScope(context.Background(), pr, head)
	if !errors.Is(err, ErrPRDiffGone) || sc != "" || !strings.Contains(err.Error(), "#7") {
		t.Fatalf("scope %q err %v", sc, err)
	}
}
```

- [ ] **Step 2: Run it.** `go test ./internal/domain/ -run TestPRNoteScopeRefusesWhenTheDiffIsGone -count=1`
  Expected: FAIL. It does not compile (`ErrPRDiffGone` is undefined).

- [ ] **Step 3: Implement** in `forge_send.go`:

```go
	// ErrPRDiffGone: a note written in a PR's view whose diff this
	// repository no longer holds (its head was forgotten, or it merged) —
	// refused, never stored plain (ruling A, 2026-10-08).
	ErrPRDiffGone = errors.New("the pull request's diff is not here any more")
```

```go
func (s *Service) PRNoteScope(ctx context.Context, pr model.PullRequest, commit string) (string, error) {
	prev, err := s.PRPreview(ctx, pr)
	if err != nil {
		return "", err
	}
	if !prev.Set.OK() {
		return "", fmt.Errorf("%w — reopen pull request #%d", ErrPRDiffGone, pr.Number)
	}
	...unchanged...
}
```

Update the doc comment above it: the scope, `ErrNoteOffPR`, or `ErrPRDiffGone`; never "".

- [ ] **Step 4: Run it.** Same command. Expected: PASS. Then run `go test ./internal/domain/ -count=1`. Expected: `ok`.

- [ ] **Step 5: Write the failing web tests.** Change `TestWebNoteForAnUnknownPRReadsNoForge` so it expects a refusal, and add a test for a forgotten diff:

```go
// A1 + ruling A: a PR the page does not know is never read from the forge
// for a note, and the note is refused — stored plain it would leave the PR.
func TestWebNoteForAnUnknownPRReadsNoForge(t *testing.T) {
	ts, wf, srv, head, _ := sendServerFull(t)
	reads := func() int {
		wf.mu.Lock()
		defer wf.mu.Unlock()
		return wf.prReads
	}
	before := reads()
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"pr7.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":99}`, head))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#99") {
		t.Fatalf("add = %d %v", code, out)
	}
	if n := reads() - before; n != 0 {
		t.Fatalf("the note add read the forge %d times", n)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[head] != 0 {
		t.Fatal("the refused note was stored")
	}
}

// Ruling A: the PR's diff is gone (gg pr forget elsewhere) — refused.
func TestWebPRNoteWithTheDiffGoneIsRefused(t *testing.T) {
	ts, _, srv, head, dir := sendServerFull(t)
	gitRun(t, dir, "update-ref", "-d", "refs/gg/pr/7")
	code, out := postJSONAny(t, ts, "/api/notes/add",
		fmt.Sprintf(`{"path":"pr7.txt","rev":%q,"state":"commit","side":"new","line":1,"summary":"x","pr":7}`, head))
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "#7") {
		t.Fatalf("add = %d %v", code, out)
	}
	if c, err := srv.service().NoteCounts(context.Background()); err == nil && c.ByCommit[head] != 0 {
		t.Fatal("the refused note was stored")
	}
}
```

- [ ] **Step 6: Run them.** `go test ./internal/web/ -run 'TestWebNoteForAnUnknownPR|TestWebPRNoteWithTheDiffGone' -count=1`
  Expected: both FAIL. The first gets code 200; the second gets 200, or 500 if the ref deletion breaks PRPreview.

- [ ] **Step 7: Implement** in `handleNoteAdd`:

```go
	if req.PR > 0 && addr.State == model.StateCommitted {
		// The row the view was opened from (cachedPR): never a forge read.
		// A note that cannot take the PR's stamp is refused (ruling A): stored
		// plain it would silently leave the PR's view.
		pr, ok := s.cachedPR(s.service(), req.PR)
		if !ok {
			writeErr(w, http.StatusConflict, fmt.Errorf("pull request #%d is not in the pull request list any more — reopen it", req.PR))
			return
		}
		sc, err := s.service().PRNoteScope(r.Context(), pr, addr.Commit)
		switch {
		case errors.Is(err, domain.ErrNoteOffPR), errors.Is(err, domain.ErrPRDiffGone):
			writeErr(w, http.StatusConflict, err)
			return
		case err != nil:
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		preview = sc
	}
```

- [ ] **Step 8: Run it.** Run `go test ./internal/web/ ./internal/domain/ -count=1`. Expected: `ok` for both packages. If `TestWebPRNoteIsStamped` or another web test now gets 409, its fixture wrote a note with `"pr"` for a PR that was not listed or not fetched. Fix that fixture so it stamps the note the Global Constraints way, and record a ruling.

- [ ] **Step 9: Commit** `fix(notes): a PR-view note that cannot take the PR's stamp is refused`.

---

### Task 2: B6 — the TUI's PR link copy drops a stale repo's answer

**Files:**
- Modify: `internal/tui/pr_link.go` (`prLinkMsg.gen`, `prLinkCmd`, `handlePRLinkMsg`)
- Test: `internal/tui/pr_link_test.go`

**Interfaces:**
- Produces: `prLinkMsg{n int; source, target string; err error; gen int}`. `gen` is `m.forgeGen` at the time of the ask.

- [ ] **Step 1: Write the failing test.**

```go
// B6: a repo switch between L and the answer drops the old repo's pair —
// copying it would hand out a link to the wrong repository.
func TestPRLinkAnswerFromALeftRepoIsDropped(t *testing.T) {
	t.Parallel()
	m := prModel(t)
	var copied string
	m = withClip(m, &copied)
	m.forgeGen = 2
	nm, cmd := m.Update(prLinkMsg{n: 7, source: "refs/gg/pr/7", target: "main", gen: 1})
	if cmd != nil {
		runCopy(t, cmd)
	}
	if copied != "" || strings.Contains(nm.(Model).statusMsg, "copied") {
		t.Fatalf("a stale answer copied %q (status %q)", copied, nm.(Model).statusMsg)
	}
}
```

Also change `TestPRListLCopiesThePRLink`. Set `m.forgeGen = 3` before pressing L, assert `msg.gen == 3` on the message L produces, and feed back `gen: 3`. With the zero value, the check would compare 0 to 0 and prove nothing.

- [ ] **Step 2: Run it.** `go test ./internal/tui/ -run 'TestPRLinkAnswerFromALeftRepoIsDropped|TestPRListLCopiesThePRLink' -count=1`. Expected: FAIL, because `gen` is an unknown field.

- [ ] **Step 3: Implement.** Add `gen int // m.forgeGen when asked: a repo switch drops the answer` to `prLinkMsg`. In `prLinkCmd`, capture `gen := m.forgeGen` and return `prLinkMsg{..., gen: gen}`. In `handlePRLinkMsg`, the first line is `if msg.gen != m.forgeGen { return m, nil }`.

- [ ] **Step 4: Run it.** Same command. Expected: PASS. Then run `go test ./internal/tui/ -run 'PRLink|PRHub|OpenPRMenu' -count=1`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(tui): a PR link answer from a repo the TUI left is dropped`.

---

### Task 3: B5 — the TUI keeps the "not in the pull request list" notice for a file-less link

**Files:**
- Modify: `internal/tui/steer_nav.go` (`steerNavigatePreview`)
- Test: `internal/tui/pr_link_test.go`

- [ ] **Step 1: Write the failing test.**

```go
// B5: a file-less link to an unlisted PR keeps the notice that says why it
// opened as a merge preview (steerNotice used to overwrite it).
func TestAFileLessUnlistedPRLinkKeepsItsNotice(t *testing.T) {
	t.Parallel()
	m, _ := prLinkSteerModel(t)
	m.prs = nil
	c := steer.Command{ID: "pr-5", Cmd: "navigate", Origin: steer.OriginStartAt,
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"}, Wait: true}
	m, _ = m.applySteer(c)
	if !strings.Contains(m.statusMsg, "PR #7 is not in the pull request list here") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}
```

(Check the origin field's real name with `grep -n "func startAtOrigin" -A5 internal/tui/*.go` before writing it. Also run a second case without the origin; the notice must survive "agent moved the focus" too.)

- [ ] **Step 2: Run it.** `go test ./internal/tui/ -run TestAFileLessUnlistedPRLinkKeepsItsNotice -count=1`. Expected: FAIL. The status is "▸ opened preview main...refs/gg/pr/7".

- [ ] **Step 3: Implement.** In `steerNavigatePreview`, keep the PR notice in a local variable `prNote`. In the file-less branch, choose the notice like this:

```go
		switch {
		case prNote != "":
			m = m.steerNotice(prNote) // why a PR link opened as a plain preview outranks where it went
		case startAtOrigin(c):
			m = m.steerNotice(i18n.T("▸ opened preview %s", tgt+"..."+src))
		default:
			m = m.steerNotice(i18n.T("▸ agent moved the focus"))
		}
```

Leave `m.statusMsg = prNote` where it is now, so the with-file path still shows it.

- [ ] **Step 4: Run it.** Same command, then `go test ./internal/tui/ -run 'PRLink|Steer' -count=1`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(tui): a file-less unlisted PR link keeps its notice`.

---

### Task 4: B4 — the TUI fetches a listed but unfetched PR before it lands

**Files:**
- Modify: `internal/tui/pr_link.go` (`steerNavigatePR`, new `openPRLandingCmd`, new `failPRLanding`)
- Modify: `internal/tui/pr_actions.go` (`handlePRFetchReady`)
- Modify: `internal/tui/model.go` (the op-finished arm, `prOpen != nil && msg.err != nil`)
- Test: `internal/tui/pr_link_test.go`

**Interfaces:**
- Consumes: `svc.PRFetched(ctx) map[int]bool`, `svc.PRFetchOp`, `prFetchReadyMsg`, and `pendingSteer.prNumber`.
- Produces:
  - `func (m Model) openPRLandingCmd(p model.PullRequest) tea.Cmd`. Off the UI thread it opens the local head at once (`openPRPreviewCmd`'s message), or resolves the fetch op (`prFetchReadyMsg`). This is the same as the web's `openPR`.
  - `func (m Model) failPRLanding(n int, reason string) (Model, tea.Cmd)`. It calls `failPending` when the parked landing is PR n's, and does nothing otherwise.

- [ ] **Step 1: Write the failing tests.**

```go
// prLinkUnfetchedModel lists PR #7 but holds no refs/gg/pr/7.
func prLinkUnfetchedModel(t *testing.T) (Model, string) {
	t.Helper()
	m, dir := previewSteerModel(t)
	m.forgeShown, m.prs = true, testPRs()
	return m, dir
}

// B4: a listed PR whose head is not local is fetched first — the enter
// path — instead of opening an empty view.
func TestAPRLinkToAnUnfetchedPRFetchesIt(t *testing.T) {
	t.Parallel()
	m, _ := prLinkUnfetchedModel(t)
	c := steer.Command{ID: "pr-6", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, cmd := m.applySteer(c)
	if cmd == nil {
		t.Fatal("no open")
	}
	if msg, ok := cmd().(prFetchReadyMsg); !ok || msg.pr.Number != 7 {
		t.Fatalf("landing → %T, want the PR's fetch", msg)
	}
	if m.pendingSteer == nil || m.pendingSteer.prNumber != 7 {
		t.Fatalf("pendingSteer = %+v", m.pendingSteer)
	}
}

// B4: a fetch that cannot run answers the parked landing at once.
func TestAFailedPRLandingFetchAnswersTheAgent(t *testing.T) {
	t.Parallel()
	m, dir := prLinkUnfetchedModel(t)
	c := steer.Command{ID: "pr-7", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, _ = m.applySteer(c)
	nm, cmd := m.Update(prFetchReadyMsg{pr: testPRs()[0], err: errors.New("no forge")})
	if nm.(Model).pendingSteer != nil {
		t.Fatal("the landing is still parked")
	}
	if cmd != nil {
		drainMsgs(t, nm.(Model), cmd, 4)
	}
	if rep := readOneReply(t, dir, "pr-7"); rep.OK || !strings.Contains(rep.Detail, "no forge") {
		t.Fatalf("reply = %+v", rep)
	}
}
```

```go
// B4: the whole chain — fetch op armed, the landing stays parked (and its
// clock restarts: steerPendingTTL is 5s, a fetch can take longer), the op
// finishes, the PR's view opens and the line lands.
func TestAPRLinkToAnUnfetchedPRLandsAfterTheFetch(t *testing.T) {
	t.Parallel()
	m, dir := prLinkUnfetchedModel(t)
	c := steer.Command{ID: "pr-8", Cmd: "navigate", File: "a.txt",
		Target: &steer.Target{State: "preview", Source: "refs/gg/pr/7", Target: "main"},
		Line:   &steer.Line{Side: "new", No: 1}, Wait: true}
	m, _ = m.applySteer(c)
	m.pendingSteer.at = time.Now().Add(-4 * time.Second)
	nm, _ := m.Update(prFetchReadyMsg{pr: testPRs()[0], op: engine.FetchPRHead{Remote: "origin", Refspec: "refs/pull/7/head", Number: 7}})
	m = nm.(Model)
	if m.pendingPROpen == nil || m.pendingSteer == nil || time.Since(m.pendingSteer.at) > time.Second {
		t.Fatalf("pendingPROpen=%v pendingSteer=%+v", m.pendingPROpen != nil, m.pendingSteer)
	}
	runGit(t, repoTop(t, m), "update-ref", "refs/gg/pr/7", "feat/x") // what the fetch op would have written
	nm, cmd := m.Update(opFinishedMsg{res: engine.Result{Summary: "fetched"}})
	m = drainMsgs(t, nm.(Model), cmd, 12)
	if m.previewOpen == nil || m.previewOpen.prNumber != 7 || m.diffLayer() == nil {
		t.Fatalf("previewOpen = %+v diff=%v", m.previewOpen, m.diffLayer() != nil)
	}
	if rep := readOneReply(t, dir, "pr-8"); !rep.OK {
		t.Fatalf("reply = %+v", rep)
	}
}
```

(`testPRs()[0]` is PR #7 (pr_panel_test.go). The existing `TestAPRLinkOpensThePRView` group keeps passing, because their ref is local. If `drainMsgs` runs the reload batch's git reads and that is slow, raise its step cap instead of skipping it.)

- [ ] **Step 2: Run them.** `go test ./internal/tui/ -run 'TestAPRLinkToAnUnfetchedPR|TestAFailedPRLandingFetchAnswersTheAgent' -count=1`. Expected: FAIL. The first gets a `previewOpenMsg`; the second keeps the pending (`handlePRFetchReady` only sets status).

- [ ] **Step 3: Implement.**
  - In `pr_link.go`:

```go
// openPRLandingCmd opens PR p for a link as a row click would: a head that
// is already local opens at once; otherwise the enter path fetches it first
// (prFetchReadyMsg → the fetch op → openPRPreviewCmd).
func (m Model) openPRLandingCmd(p model.PullRequest) tea.Cmd {
	svc, open := m.svc, m.openPRPreviewCmd(p)
	return func() tea.Msg {
		ctx := context.Background()
		if svc.PRFetched(ctx)[p.Number] {
			return open()
		}
		op, err := svc.PRFetchOp(ctx, p.Number)
		return prFetchReadyMsg{pr: p, op: op, err: err}
	}
}

// failPRLanding answers a PR link's parked landing when its open failed —
// never left to expire in silence.
func (m Model) failPRLanding(n int, reason string) (Model, tea.Cmd) {
	if ps := m.pendingSteer; ps == nil || ps.prNumber != n {
		return m, nil
	}
	return m.failPending(reason)
}
```

  - In `steerNavigatePR`, both branches call `m.openPRLandingCmd(p)` instead of `m.openPRPreviewCmd(p)`. Do NOT refuse up front when an op is running. A fetched PR's landing is a pure read and must keep working during an unrelated op. Only the fetch needs idle ops, and `handlePRFetchReady` answers that case.
  - `handlePRFetchReady`: on `msg.err`, set the status as now, then call `return m.failPRLanding(msg.pr.Number, firstLine(msg.err.Error()))`. On `!m.opsIdle()`, call `return m.failPRLanding(msg.pr.Number, "an operation is running; open the link again when it finishes")`. When the op is armed and `m.pendingSteer.prNumber == pr.Number`, set `m.pendingSteer.at = time.Now()`, because the fetch's network time must not count against `steerPendingTTL`.
  - `model.go` op-finished: in the `prOpen != nil && msg.err == nil` arm, also restart the clock: `if ps := m.pendingSteer; ps != nil && ps.prNumber == prOpen.Number { ps.at = time.Now() }`. After it, add the arm `else if prOpen != nil { var fc tea.Cmd; m, fc = m.failPRLanding(prOpen.Number, firstLine(msg.err.Error())); prCmd = fc }`. Keep the batch shape it feeds.
  - Keep the file-less branch's immediate reply, the same as every other file-less landing. Record that as a ruling.

- [ ] **Step 4: Run them.** Same command, then `go test ./internal/tui/ -count=1 > $WS/t4.log 2>&1; tail -3 $WS/t4.log`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(tui): a PR link to an unfetched PR fetches it first`.

---

### Task 5: B7 — a waiting moved-head comments read keeps its "updated" mark

**Files:**
- Modify: `internal/web/static/prfresh.js` (new `stickyFlag`)
- Modify: `internal/web/static/prs.js` (`refreshPRComments`, `commentsRead` signature unchanged)
- Test: `internal/web/prfreshjs_test.go`

**Interfaces:**
- Produces: `stickyFlag(reads) → (key, flag, make) => void`. `make(flag)` returns the async read. The flag is ORed across reads that replace each other while they wait, and is cleared when one of them starts.

- [ ] **Step 1: Write the failing test.** Add it to `prfreshjs_test.go` and run it with the same node harness the file's other `serialReads`/`oncePerKey` tests use. Copy their runner (`grep -n "serialReads\|oncePerKey" internal/web/prfreshjs_test.go`).

```js
// a gate that holds the first read until release()
let release; const gate = (fn) => (busy ? null : (busy = true, Promise.resolve(fn()).finally(() => (busy = false))));
let busy = false;
const reads = serialReads(gate);
const soon = stickyFlag(reads);
const seen = [];
reads.run(() => new Promise((r) => (release = r)));        // a read in flight
soon("comments:7", true, (f) => async () => seen.push(f));  // moved, waits
soon("comments:7", false, (f) => async () => seen.push(f)); // replaces it
release();
await new Promise((r) => setTimeout(r, 10));
soon("comments:7", false, (f) => async () => seen.push(f)); // a later plain read
await new Promise((r) => setTimeout(r, 10));
console.log(JSON.stringify(seen)); // want [true,false]
```

- [ ] **Step 2: Run it.** `go test ./internal/web/ -run TestStickyFlagJS -count=1`. Expected: FAIL, because `stickyFlag` is not exported.

- [ ] **Step 3: Implement** in `prfresh.js`:

```js
// stickyFlag is soon() for reads that carry a flag a newer waiting read must
// not drop (prs.js: the moved-head read's "updated"): soon(key, flag, make)
// queues make(flag) — the flag ORed over every read that replaced another
// while waiting, cleared when one starts.
export function stickyFlag(reads) {
  const raised = new Set();
  return (key, flag, make) => {
    if (flag) raised.add(key);
    reads.soon(key, () => make(raised.delete(key))());
  };
}
```

In `prs.js`:

```js
const soonComments = stickyFlag(reads);
export function refreshPRComments(n, moved = false) {
  // Queued, never dropped — and a moved read replaced while it waits keeps
  // its "updated" (stickyFlag).
  soonComments("comments:" + n, moved, (mv) => commentsRead(n, mv));
}
```

- [ ] **Step 4: Run it.** `go test ./internal/web/ -run 'JS' -count=1`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(web): a waiting moved-head comments read keeps its updated mark`.

---

### Task 6: B2 — a cold page's PR landing waits for the list

**Files:**
- Modify: `internal/web/static/prfresh.js` (new `coveredReads`, `readyLatch`)
- Modify: `internal/web/static/prs.js` (`fetchPRs` through `coveredReads`; `take` opens the latch; `openPRLanding` waits)
- Test: `internal/web/prfreshjs_test.go`

**Interfaces:**
- Produces:
  - `coveredReads(gate, read) → call()`. `call()` returns a promise that settles when a read that started after the call ends. A call made mid-flight queues one more read.
  - `readyLatch() → {open(), wait(ms) → Promise<bool>}`.

- [ ] **Step 1: Write the failing tests.** Use the node harness.

```js
// coveredReads: a call mid-flight waits for the follow-up read, not just the one in flight
let busy = false, n = 0; const log = [];
const gate = (fn) => (busy ? null : (busy = true, fn().finally(() => (busy = false))));
let rel = []; const read = () => new Promise((r) => rel.push(() => { n++; r(); }));
const call = coveredReads(gate, read);
const a = call().then(() => log.push("a:" + n));
const b = call().then(() => log.push("b:" + n));   // mid-flight
rel.shift()(); await tick(); rel.shift()(); await Promise.all([a, b]);
console.log(JSON.stringify(log)); // want ["a:2","b:2"] — b never settles before the 2nd read
// readyLatch
const l = readyLatch(); const w = l.wait(1000); l.open();
console.log(await w, await l.wait(5), await readyLatch().wait(5)); // true true false
```

(Note: `a` also settles after read 2. The chain in flight includes its follow-up, and that is intended. A caller who asked before a newer read should see that read too.)

- [ ] **Step 2: Run them.** `go test ./internal/web/ -run 'TestCoveredReadsJS|TestReadyLatchJS' -count=1`. Expected: FAIL, because the functions are not exported.

- [ ] **Step 3: Implement** in `prfresh.js`:

```js
// coveredReads is a single-flight read whose late callers are not dropped:
// a call while a read runs queues ONE more read, and its promise settles
// when that read ends — so the caller sees an answer newer than its call
// (prs.js fetchPRs: a cold page's link landing must see the listing).
export function coveredReads(gate, read) {
  let again = false;
  let tail = null;
  function call() {
    const run = gate(read);
    if (!run) {
      again = true;
      return tail || Promise.resolve();
    }
    tail = run.then(() => {
      if (!again) return;
      again = false;
      return call();
    });
    return tail;
  }
  return call;
}

// readyLatch: wait(ms) resolves true once open() ran (at once if it did),
// false after ms.
export function readyLatch() {
  let ready = false;
  const waiters = [];
  return {
    open() {
      if (ready) return;
      ready = true;
      waiters.splice(0).forEach((f) => f(true));
    },
    wait(ms) {
      if (ready) return Promise.resolve(true);
      return new Promise((res) => {
        waiters.push(res);
        setTimeout(() => res(false), ms);
      });
    },
  };
}
```

In `prs.js`:
- Add `const readPRs = coveredReads((fn) => runOnce("prs", fn), async () => { try { take(await getJSON("/api/pr")); } catch {} });`. Keep `export function fetchPRs() { return readPRs(); }` as a declaration: it is hoisted, so no import cycle can meet an uninitialised `const` export. Keep its comment, reworded.
- Add `const listLoaded = readyLatch();`. In `take()`, call `if (body.loaded) listLoaded.open();`. The server's `loaded` is `state != prsUnprobed` (prs.go `writePRs`), so a repo with no forge answers `loaded: true` once probed, and the latch does not hold a no-forge link for the full wait. Check whether prs.js resets on a repo switch (`grep -n "reset\|Switch" prs.js`). If it does, recreate the latch there; if it does not, record a ruling.
- In `openPRLanding`, after the first `await fetchPRs()`, add: `if (!knownPR(n)) await listLoaded.wait(LANDING_LIST_MS);` with `const LANDING_LIST_MS = 10000; // the server's first listing (BACKOFF_MS sums to 7s)`.

- [ ] **Step 4: Run them.** Run `go test ./internal/web/ -count=1 > $WS/t6.log 2>&1; tail -3 $WS/t6.log`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(web): a cold page's PR link waits for the pull request list`.

---

### Task 7: B1 + B3 — a web PR landing waits for an open in flight, and an unlisted PR gets one clear line

**Files:**
- Modify: `internal/web/static/prfresh.js` (new `exclusive`)
- Modify: `internal/web/static/prs.js` (`openPR` through `exclusive`; `openPRLanding` waits for it)
- Modify: `internal/web/static/live.js:541-558` (the preview arm)
- Test: `internal/web/prfreshjs_test.go`, `internal/web/prlinkjs_test.go` (source assertion on live.js)

**Interfaces:**
- Produces: `exclusive() → {try(fn) → Promise|null, idle() → Promise}`. `try` starts `fn` or answers `null` while a task runs. `idle()` settles when no task runs.

- [ ] **Step 1: Write the failing tests.**

```js
// exclusive: one at a time; idle() waits for the running one
const ex = exclusive(); let rel; const log = [];
const p = ex.try(() => new Promise((r) => (rel = r)));
console.log(ex.try(async () => 1) === null);          // true
const w = ex.idle().then(() => log.push("idle"));
log.push("before"); rel(); await p; await w;
console.log(JSON.stringify(log));                     // ["before","idle"]
console.log(await Promise.race([ex.idle().then(() => "now"), new Promise((r) => setTimeout(() => r("late"), 5))])); // now
// the landing's loop: busy → wait → retry wins once the other open ends
let rel2; ex.try(() => new Promise((r) => (rel2 = r)));
let p2; const got = (async () => { while (!(p2 = ex.try(async () => "mine"))) await ex.idle(); return p2; })();
rel2(); console.log(await got);                       // mine
```

And a Go source test in `prlinkjs_test.go`:

```go
// B3 (ruling 2026-10-08): the page cannot open refs/gg/pr/<n> as a plain
// preview, so a link to a PR it does not list says so once and stops —
// never the doomed fallback and its second red line.
func TestUnlistedPRLinkSaysOneLine(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "live.js"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	if strings.Contains(src, "opened as a merge preview") {
		t.Error("live.js still falls back to a plain preview for an unlisted PR")
	}
	if !strings.Contains(src, "is not in the pull request list") {
		t.Error("live.js does not say the PR is unlisted")
	}
}
```

- [ ] **Step 2: Run them.** `go test ./internal/web/ -run 'TestExclusiveJS|TestUnlistedPRLinkSaysOneLine' -count=1`. Expected: FAIL.

- [ ] **Step 3: Implement.**
  - In `prfresh.js`:

```js
// exclusive runs one task at a time: try(fn) starts fn or answers null while
// one runs; idle() settles once none runs (prs.js: a PR link landing waits
// for a row's open in flight instead of failing in silence).
export function exclusive() {
  let running = null;
  return {
    try(fn) {
      if (running) return null;
      const p = Promise.resolve().then(fn);
      running = p.finally(() => (running = null));
      return p;
    },
    async idle() {
      while (running) await running.catch(() => {});
    },
  };
}
```

  - In `prs.js`, replace `let opening = 0` and its guard with `const opens = exclusive();`. Then rewrite `openPR` as `async function openPR(pr) { const p = opens.try(() => openPRNow(pr)); return p ? p : false; }`. `openPRNow` is the old body without the `opening` lines; `maskOff` stays in its `finally`. Check for other readers of `opening` with `grep -n "opening" prs.js` and point them at a `let openingN` kept inside `openPRNow` if any exist.
  - `openPRLanding` must not call `openPR`. `openPR`'s `false` means both "busy" and "failed", and a row click between the waits and the open would silence the landing again. Use `opens.try` directly, so busy (`null`) waits and retries while a failure (the promise resolving `false`) has already said why:

```js
export async function openPRLanding(n) {
  await opens.idle(); // a row's open in flight runs first (B1)
  if (!knownPR(n)) await fetchPRs();
  if (!knownPR(n)) await listLoaded.wait(LANDING_LIST_MS);
  const pr = knownPR(n);
  if (!pr) return null;
  if (state.previewOpen && state.previewOpen.pr === n) return true; // already on screen
  let p;
  while (!(p = opens.try(() => openPRNow(pr)))) await opens.idle();
  return await p;
}
```
  - In `live.js`, replace the else-branch for `n` with:

```js
    } else if (n) {
      // The page cannot open refs/gg/pr/<n> as a plain preview (ruling
      // B3): say why once instead of a fallback that fails twice.
      opLine("gg link: pull request #" + n + " is not in the pull request list — list or search for it, then open the link again", true);
      return;
    } else {
      ...the plain preview branch, unchanged, minus its "PR #… opened as a merge preview" line...
    }
```

- [ ] **Step 4: Run them.** Run `go test ./internal/web/ -count=1 > $WS/t7.log 2>&1; tail -3 $WS/t7.log`. Expected: `ok`.

- [ ] **Step 5: Commit** `fix(web): a PR link waits for an open in flight; an unlisted PR says one line`.

---

### Task 8: Probes, docs, race gate

**Files:**
- Modify: `CHANGELOG.md` (a "Pull request follow-ups 3" section, Fixed entries for A, B1–B7)
- Modify: `docs/CLAUDE-details.md` (the "Pull request links (2026-10-08)" paragraph: unfetched landing fetches, unlisted web link refuses; the note-scope paragraph: refusal on a missing row or a missing diff)

- [ ] **Step 1: Browser probe, OLD build (`~/go/bin/gg`) first, then `bin/gg` from this worktree.** Use `probe/fixtures.sh` with `SRC=<worktree>`, and a new `probe/pfu3.mjs` modelled on `pfu2.mjs`:
  - (A) Open PR #7, run `git update-ref -d refs/gg/pr/7` in the fixture, add a note on a line through the `#prompt-input` prompt. Old build: the note is stored, nothing is said. New build: a red line containing "#7", and `/api/notes` gets no new note.
  - (B3) `gg session navigate` a link to PR #99. Old build: two red lines. New build: one red line, "not in the pull request list".
  - (B1) Delay `/api/pr/open` by 2s with a playwright route, click PR #7's row, and navigate PR #7's link at once. Old build: the page says nothing and the link does not land. New build: it lands on the file and line after the open.
  - (B2) Clear the PR cache, then start `gg web` with the link (`gg open --web`). New build: it lands.
  - Assert visibility (the element is shown), not only DOM presence.
- [ ] **Step 2: TUI probe** (`./tui-capture.sh --gg <wrapper>`). Paste PR #7's link with `#` on a fixture where refs/gg/pr/7 is absent but the PR is listed. Old build: an empty view. New build: "fetching PR #7…", then the PR view on the file.
- [ ] **Step 3: Docs.** Write the CHANGELOG and CLAUDE-details entries.
- [ ] **Step 4: Race gate.** `./test.sh race > $WS/race.log 2>&1; grep -c "all green" $WS/race.log`. Expected: `1`.
- [ ] **Step 5: Commit** `docs: PR follow-ups 3`.
