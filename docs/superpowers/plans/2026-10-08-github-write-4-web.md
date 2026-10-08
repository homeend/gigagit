# GitHub write-back — Plan 4: gg web (and: agents never send)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.
> **gigagit rule (CLAUDE.md): NO implementer subagents** — this plan is executed by the session that wrote it (executing-plans); only the final whole-branch review may be a read-only subagent.
> Every commit message below ends with the session's attribution trailer lines (`Co-Authored-By: …` / `Claude-Session: …`); the steps show the subject only. `gg commit` has no `-F`: stage with `gg add <paths>`, commit with `git commit -F <msgfile>`.

**Goal:** In gg web, a pull request's notes show where they live (○ local, ◌ sending, ○! failed, ● on GitHub) and which group they belong to (a coloured left border), and the user sends them — one note, a whole group, a verdict, a reply — resolves GitHub threads, and finishes or discards an interrupted send, every send behind one confirm drawn from the send plan. First, per the user's ruling of 2026-10-08, every path by which an AI agent could get something posted to GitHub is removed.

**Architecture:** Two rulings change merged code before the web work starts. (1) **Agents never send** (user, 2026-10-08): the pending-send queue (plan 2's domain + CLI, plan 3's TUI notices) is deleted; `gg pr send|reply --send|resolve|unresolve` refuse inside any gg-started session and only ever post after an interactive answer at a terminal (`--yes` and `--event` go). (2) **An emptied AI-review body is posted empty** (user, 2026-10-08): `PRSendRequest.BodySet` says "the user saw the body box", so an empty box means an empty body. Then the web: domain gains the one group → colour-slot function (`domain.GroupSlot`, the TUI's FNV-1a moved down), the rendered wire note carries `group_slot`, a new `POST /api/pr/send` builds `domain.PRSendOp` in the HTTP handler (outside the repo gate, R12), validates every wire value against the PR's cached notes, returns `{op_id, plan}` (a projection of `engine.SendPlan` with no sha, no node id) and runs the op on the existing op lane; the page renders the `forge.send` decision from that plan inside the existing parking modal. The rest is page code: marks and bars, menu rows, the Send review… / Verdict… prompts, the "updated" freshness word, and an interrupted-send bar.

**Tech Stack:** Go 1.26, the web op transport (`internal/web/oprun.go`, SSE + parking decider), ES modules under `internal/web/static/` (node-tested pure modules), playwright for the browser probe, the fake gh (`internal/forge/testdata/fakegh`, `forgetest`).

**Spec:** `docs/superpowers/specs/2026-10-07-github-write-design.md` — §1.1 (marks), §1.3 (groups, colour), §2.4 (freshness), §3.3–§3.5 (op, confirm, gestures), §4.2 (web), §5 (errors), §6 (testing). **Amended by this plan's Task 1** (rule 8, §3.6, §3.7, §4.3, §4.4, §7 item 5) for the user's 2026-10-08 ruling. Plans 1–3 are on `main` (`49e37a6e`, `14dccb1f`, `56538a2d`, `69eaed52`); plan 3's TUI patterns and rulings T1–T9 are in `docs/superpowers/plans/2026-10-07-github-write-3-tui.md`.

## Global Constraints

- Nothing in this plan (code or tests) posts to real GitHub. Tests use an in-process fake forge with a recording `forge.Writer`, or the fake gh with `GG_GH_BIN`/`GG_FAKEGH_DIR`. The empty-body COMMENT probe stays unrun (needs the user's OK).
- **No agent path to GitHub** (user ruling 2026-10-08): no queue, no MCP write tool, no `--yes`; a send is posted only after a human answers the confirm in the TUI, the web page, or an interactive terminal. Agents write local notes and reviews only.
- The repo gate is not re-entrant: `PRSendOp` is called in the HTTP handler (the web) or a `tea.Cmd` (the TUI), never inside a running op.
- `internal/web` is domain-only (archtest): no `internal/git`, `forge`, `notes`, `prcache` imports outside `_test.go` files.
- **Wire values are allowlisted** (spec §4.2): the PR number (a listed PR), note / remark / draft-reply ids and GitHub thread ids that the PR's cached notes hold, a group id that `PRSendGroups` lists, one of a fixed set of `kind`s. Free text is only the review body (capped at 64 KiB). Nothing sent to the page is a sha or a forge node id; `prs.js` and the new modules never contain `head_sha` (`TestPRPageSendsOnlyTheNumber`, extended).
- gg web is English-only (it has no i18n layer). New TUI strings (Task 2 only removes some) still go through `i18n.T` in all four bundles; orphaned bundle keys are deleted (the orphan gate).
- Web markup: a feature module mounts its own markup and styles (`notifications.js` precedent); no global `.hidden` — hide by id.
- Tests: real git in `t.TempDir()`; new tests call `t.Parallel()` unless they use `t.Setenv` (e.g. `prFixture`'s `isolateState`) — those are serial with a comment saying why. Pure JS modules are import-free and node-tested (`*js_test.go` pattern).
- A "watch it fail" check means the guard removed, not the feature; never `git checkout -- <file>` to undo a probe — copy a backup and restore it.
- Browser verification asserts VISIBILITY (computed style / bounding box / a screenshot), is run against the UNFIXED build first, and proves the binary under test (md5 + a new file that 404s on the old build and 200s on the new).
- Race gate: `./test.sh race > <log> 2>&1`; green ONLY when the log says `all green`.

## Review Focus

1. **A send started from one tab while another tab (or the hosting TUI) runs an op** → `startRun` refuses (409, "another operation is running"); the page says so and nothing is planned twice. (Task 5 test.)
2. **The PR's head moved on GitHub between opening the diff and pressing Send** → `PRSendOp` refuses with `ErrPRHeadMoved`; the endpoint answers 409 with that text; the page follows the moved head (the existing `followMovedHead`) and posts nothing. (Task 5 test.)
3. **A forged wire value** (a note id from another PR, a sha, `../`, a GitHub thread id not in this PR, a group the PR does not list, an unknown kind, a 1 MB body) → 400 before any domain call that writes. (Task 5 table test.)
4. **A long review** (40 remarks, a 60-line body) → the confirm shows the body scrollable, at most 30 item rows plus "+ N more", every skipped row with its reason; the option buttons stay on screen. (Task 7 node test + probe.)
5. **Marks outside a PR's own diff** → a commit / working-tree / preview diff draws no `○`/`●` and no group border, but still `◌` sending and `○!` failed with its error (plan 3 ruling T3, ported). (Task 6 node test.)
6. **An agent inside a gg console runs `gg pr send … --yes`** → refused (exit 2 unknown flag; and without it, exit 1 "agents can't send to GitHub"); nothing is queued, nothing reaches the forge. (Task 1 test.)

## Rulings this plan makes (for the user's review)

- **W1 — agents never send (the user's ruling, made concrete).** Deleted: `domain/pendingsend.go`, `PendingSendsPath`, `PendingOutcome`, `cli/prpending.go` (`gg pr pending …`), `tui/pending_sends.go`, the `pendingID`/`event` plumbing of the TUI send, `PRSendRequest.Event` and `.Agent`. `gg pr send`, `gg pr reply --send`, `gg pr resolve|unresolve` (a) refuse inside any session gg started (`$GG_INBOX` set — that is every agent console AND every gg terminal tab: the user sends from the PR view, gg web, or a shell outside gg), (b) post only after an interactive answer on a terminal (`--yes` and `--event` removed; a pipe gets "sending to GitHub needs your answer at a terminal"). Cost: a human can no longer script a send from a plain shell. **Limit, stated in the docs:** gg cannot tell a human at a terminal from an agent that fakes one, and it cannot stop an agent that runs `gh` itself, or calls gg web's loopback API (which, like every web write, is guarded against other websites, not against local processes); gg's part is that no gg tool, skill or flag offers an agent a send. A leftover `pending-sends/<key>.toml` in the state dir is ignored (no cleanup: it held at most a day of entries).
- **W2 — an emptied AI-review body is posted empty** (the user's ruling): `PRSendRequest.BodySet` = "the frontend showed the body box"; with it, `Body` is used verbatim, empty included (no summary fallback, no trailer, no marker on an empty body). `gg pr send --review <id>` without `--body` still posts the stored summary; `--body ""` posts none. The TUI's body popup and the web's prompt set it.
- **W3 — one colour function, in domain.** `domain.GroupSlot(group) int` (the TUI's FNV-1a moved down, `GroupSlotMine = 5` pinned); the TUI calls it; the web gets the slot on the wire (`group_slot` on rendered notes only — the CLI/MCP JSON is unchanged) and per-path slots for the file badges. The web's six colours are CSS variables equal to `theme.Dark`'s `NoteGroup1..6` (a Go test reads both).
- **W4 — the web confirm is drawn from the plan the server built.** `POST /api/pr/send` answers `{op_id, plan}`; the page keeps the plan on `state.op` and, when that op's `forge.send` decision arrives, fills the existing modal with it. The modal's buttons are still exactly the decision's options. A page that did not start the send (none follow another tab's op today) would show the engine's English text.
- **W5 — "PR popup" on the web = the PR right-click menu** (sidebar row, open PR's header/bar) and the details overlay stays read-only. Rows: **Send review…** (group pick → body → the confirm, whose buttons are the verdicts) and **Verdict…** (body → the confirm). Shown for an open, fetched PR only.
- **W6 — the interrupted-send offer is a bar under the open PR's compare bar** (not the notification centre): the revalidate / comment refresh answers carry `interrupted: {count, joined}` from `PRInterrupted` (cache only, no forge call); the bar offers **Finish sending** and, unless the pending review is the user's own (`joined`), **Discard**.
- **W7 — plan 3's ten deferred TUI minors are NOT folded in.** Task 2 already rewrites part of the TUI send; the web gets its own versions of "updated" after your own send (Task 9: a send's own write-through never shows "updated") and a typed body kept on a refusal (Task 8). Strike W7 at review to fold them in as an extra task.

---

## File structure

| File | Responsibility |
|---|---|
| `internal/domain/pendingsend.go`, `pendingsend_test.go` | **deleted** (W1) |
| `internal/domain/forge_send_front.go` | `PendingSendsPath`, `PendingOutcome` removed |
| `internal/domain/forge_send.go` | `PRSendRequest`: `Event`, `Agent` removed, `BodySet` added; `planReview` honours `BodySet` (W2); `signedBody` unsigned |
| `internal/domain/note_group.go` (new) | `GroupSlot`, `GroupSlotMine` (W3) |
| `internal/domain/notewire.go` | `WireNote.GroupSlot` (rendered form only) |
| `internal/cli/prsend.go` | the session refusal, interactive-only sends, no `--yes`/`--event`; `inGGSession` moves here |
| `internal/cli/prpending.go`, `prpending_test.go` | **deleted** |
| `internal/cli/pr.go` | usage + `pending` verb removed |
| `internal/tui/pending_sends.go`, `pending_sends_test.go` | **deleted** |
| `internal/tui/forge_send.go`, `model.go`, `notify.go`, `send_review_popup.go`, `note_group.go` | queue plumbing out; `BodySet`; `domain.GroupSlot` |
| `internal/web/prsend.go` (new) | `POST /api/pr/send`, `GET /api/pr/send/groups`; allowlist; plan projection |
| `internal/web/prsend_test.go` (new) | a recording writer forge; endpoint tests |
| `internal/web/oprun.go` | `notes_changed` in a run's extra → emit notes after finish |
| `internal/web/prnotes.go`, `prs.go` | `groups` (slots) in the PR notes answer; `interrupted` in the refresh answers |
| `internal/web/static/notebox.js` | `noteMark`, `noteTitle` with origin; pure |
| `internal/web/static/sendplan.js` (new) | the confirm's HTML from a plan; option labels; skip words; pure |
| `internal/web/static/prfresh.js` (new) | the freshness word's state machine; pure |
| `internal/web/static/prsend.js` (new) | sending: starter, note rows, PR rows, prompts, the interrupted bar; mounts its own CSS |
| `internal/web/static/files.js` | marks/bars in `noteBoxHTML`, badge stripes, one `extraRows("note", …)` line |
| `internal/web/static/ops.js` | `showModal` takes `html` + `labels`; the send plan is used for `forge.send` |
| `internal/web/static/menus.js` | `"note"`, `"pr"` menu keys |
| `internal/web/static/prs.js` | freshness via `prfresh.js`; interrupted bar hook; help text no longer says "read-only" |
| `internal/web/static/style.css` | `--note-group-1..6`, `.notebox.g1..g6`, marks, `.notesenderr` |
| `internal/web/static/app.js` | `import "./prsend.js";` |
| docs | spec amendment (Task 1), CHANGELOG, README, `docs/CLAUDE-details.md`, CLAUDE.md rows, `using-gg` skill (+ version), memory |

---

### Task 1: agents never send — domain + CLI (and the spec amendment)

**Files:**
- Delete: `internal/domain/pendingsend.go`, `internal/domain/pendingsend_test.go`, `internal/cli/prpending.go`, `internal/cli/prpending_test.go`
- Modify: `internal/domain/forge_send_front.go` (drop `PendingSendsPath`, `PendingOutcome`, their tests in `forge_send_front_test.go`)
- Modify: `internal/domain/forge_send.go` (`PRSendRequest`: drop `Event`, `Agent`; `signedBody` no trailer)
- Modify: `internal/cli/prsend.go`, `internal/cli/pr.go`
- Modify: `docs/superpowers/specs/2026-10-07-github-write-design.md` (amendment)
- Test: `internal/cli/prsend_test.go`

**Interfaces:**
- Produces: `func inGGSession() bool` (in `prsend.go`); `var sendTerminal = func(stdin io.Reader) bool` (the interactive test seam: true when `stdin` is `os.Stdin` and a terminal); `var errAgentSend` (the refusal); `PRSendRequest` without `Event`/`Agent`.

- [ ] **Step 1: Write the failing tests**

In `internal/cli/prsend_test.go`, delete `TestPRSendNoteWithYes`, `TestPRSendHintSkipsYesWhenAReviewIsPending` and every `--yes` use; replace the send helpers' answers with the terminal seam. Add:

```go
// Serial: swaps sessionGetenv and sendTerminal.
func TestPRSendRefusesInsideAGGSession(t *testing.T) {
	svc, ff, _ := sendPRRepo(t) // the existing helper of this file
	old := sessionGetenv
	sessionGetenv = func(k string) string {
		if k == "GG_INBOX" {
			return "/tmp/inbox"
		}
		return ""
	}
	t.Cleanup(func() { sessionGetenv = old })
	for _, args := range [][]string{
		{"send", "7", "--mine"},
		{"reply", "7", "PRRT_1", "hi", "--send"},
		{"resolve", "7", "PRRT_1"},
	} {
		var out, errb bytes.Buffer
		code := runPR(svc, args, strings.NewReader("comment\n"), &out, &errb)
		if code != 1 || !strings.Contains(errb.String(), "agents can't send to GitHub") {
			t.Errorf("%v: code %d, stderr %q", args, code, errb.String())
		}
	}
	if w := ff.writeLog(); w != "" {
		t.Fatalf("an agent's send reached the forge: %s", w)
	}
}

func TestPRSendHasNoYesFlag(t *testing.T) {
	t.Parallel()
	svc, _, _ := sendPRRepoParallel(t)
	var out, errb bytes.Buffer
	if code := runPR(svc, []string{"send", "7", "--mine", "--yes"}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("--yes must be an unknown flag: code %d, %s", code, errb.String())
	}
	if code := runPR(svc, []string{"send", "7", "--mine", "--event", "approve"}, strings.NewReader(""), &out, &errb); code != 2 {
		t.Fatalf("--event must be an unknown flag: code %d", code)
	}
}

func TestPRSendInAPipeNeedsATerminal(t *testing.T) {
	t.Parallel()
	svc, ff, head := sendPRRepoParallel(t)
	addCLINote(t, svc, head, 5, "x")
	var out, errb bytes.Buffer
	code := runPR(svc, []string{"send", "7", "--mine"}, strings.NewReader("comment\n"), &out, &errb)
	if code != 1 || !strings.Contains(errb.String(), "needs your answer at a terminal") {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	if w := ff.writeLog(); w != "" {
		t.Fatalf("a piped send reached the forge: %s", w)
	}
}

// Serial: swaps sendTerminal.
func TestPRSendAnsweredAtATerminalPosts(t *testing.T) {
	svc, ff, head := sendPRRepo(t)
	addCLINote(t, svc, head, 5, "x")
	old := sendTerminal
	sendTerminal = func(io.Reader) bool { return true } // the test's reader IS the terminal
	t.Cleanup(func() { sendTerminal = old })
	var out, errb bytes.Buffer
	if code := runPR(svc, []string{"send", "7", "--mine"}, strings.NewReader("comment\n"), &out, &errb); code != 0 {
		t.Fatalf("code %d: %s", code, errb.String())
	}
	if !strings.Contains(ff.writeLog(), "SubmitReview COMMENT") {
		t.Fatalf("writes = %s", ff.writeLog())
	}
}
```

(`sendPRRepoParallel`, `addCLINote`, `runPR`: use this file's existing helpers under their real names — read `prsend_test.go` first; if the existing helper sets env, the parallel tests become serial with a comment.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/cli/ -run 'PRSendRefusesInside|PRSendHasNoYes|PRSendInAPipe|PRSendAnsweredAt' 2>&1 | tail -20`
Expected: FAIL — the session case queues instead of refusing; `--yes` parses; `sendTerminal` undefined.

- [ ] **Step 3: Implement**

`internal/cli/prsend.go`:

```go
// inGGSession: this process runs inside a session gg started (an agent
// console or a gg terminal tab) — gg hands each one GG_INBOX.
func inGGSession() bool { return sessionGetenv("GG_INBOX") != "" }

// errAgentSend: nothing reaches GitHub from a session gg started (user
// ruling 2026-10-08: agents never send — they write local notes).
var errAgentSend = errors.New("agents can't send to GitHub: the notes stay local — " +
	"the user sends them from gg (the PR view, gg web, or gg pr send in their own terminal)")

// errNeedsTerminal: a send is posted only after a human answers its confirm.
var errNeedsTerminal = errors.New("sending to GitHub needs your answer at a terminal: run it in an interactive shell")

// sendTerminal reports whether stdin is a human's terminal (test seam).
var sendTerminal = func(stdin io.Reader) bool { return stdin == io.Reader(os.Stdin) && stdinIsTerminal() }
```

`prSend`: drop the `yes`, `event` flags and the `--event` checks; `req := domain.PRSendRequest{PR: n, Review: *review, Mine: *mine, Notes: notes, Verdict: *verdict, Body: *body, Finish: *finish, Discard: *discard}`; call `runPRSend(context.Background(), svc, req, stdin, stdout, stderr)`.

`runPRSend`:

```go
func runPRSend(ctx context.Context, svc *domain.Service, req domain.PRSendRequest,
	stdin io.Reader, stdout, stderr io.Writer) int {
	if inGGSession() {
		fmt.Fprintln(stderr, "error:", errAgentSend)
		return 1
	}
	if !sendTerminal(stdin) {
		fmt.Fprintln(stderr, "error:", errNeedsTerminal)
		return 1
	}
	res, err := sendNow(ctx, svc, req, stdin, stderr)
	if res.Summary != "" {
		fmt.Fprintln(stdout, res.Summary)
	}
	if err != nil {
		fmt.Fprintln(stderr, "error:", err)
		return 1
	}
	return 0
}
```

`sendNow` loses `yes`/`answer` and the `--yes` refusals (`errJoinNeedsConfirm` goes); its decider is `cliDecider{in: stdin, out: stderr, interactive: true}`. `prReply`: drop `yes`; `runPRSend(ctx, svc, domain.PRSendRequest{PR: n, Notes: []string{d.ID}}, stdin, stdout, stderr)`. `prResolve`: same call shape. Move nothing else from `prpending.go`; delete it and its test. `internal/cli/pr.go`: usage lines become

```
       gg pr send <n> (--note <id>… | --review <id> | --mine | --verdict) [--body <text>]
       gg pr send <n> --finish | --discard
       gg pr reply <n> <thread-or-comment-id> <text> [--send]
```

and the `pending` verb and its usage lines go.

Domain: delete `pendingsend.go` + test; in `forge_send_front.go` delete `PendingSendsPath` and `PendingOutcome` (and their tests); in `forge_send.go` delete the `Event` and `Agent` fields and make `signedBody` return `strings.TrimSpace(req.Body)` (rename to `typedBody`, update its two callers). `go build ./...` then lists every remaining user (the TUI — Task 2 removes them; to keep this task's commit building, make the minimal TUI edits Task 2 describes in the same commit, or commit Tasks 1+2 together — **Ruling at execution if needed**).

Spec amendment, appended under the spec's status line:

```markdown
> **Amended 2026-10-08 (user ruling): agents never send to GitHub.** Rule 8
> now reads: AI-written content posts with an attribution trailer; **an
> agent never sends anything to GitHub — it writes local notes and reviews,
> and only the user sends them** (TUI, gg web, or `gg pr send` answered at
> the user's own terminal). §3.7 (the pending-send queue) is withdrawn and
> was removed in plan 4; §4.3 loses `--yes`, `--event` and `gg pr pending`;
> §4.4 has no write tools (`pr_send`/`pr_reply`/`pr_resolve` are not built);
> §7 item 5 is read tools + skill text only. Also 2026-10-08: an AI review's
> body box cleared by the user posts an empty body (no fallback to the
> stored summary).
```

and strike-through notes at §3.7's heading ("withdrawn 2026-10-08"), the §4.3 lines, §4.4, §7 item 5.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go test ./internal/cli/ ./internal/domain/ 2>&1 | tail -20`
Expected: PASS (and nothing in `internal/domain` mentions `PendingSend`).

- [ ] **Step 5: Commit**

```bash
gg add internal/cli internal/domain docs/superpowers/specs/2026-10-07-github-write-design.md
git commit -F <msgfile>   # "feat!: agents never send to GitHub — no pending-send queue, no --yes; sends only after a human answers"
```

---

### Task 2: agents never send — the TUI

**Files:**
- Delete: `internal/tui/pending_sends.go`, `internal/tui/pending_sends_test.go`
- Modify: `internal/tui/forge_send.go` (`forgeSendState` loses `pendingID`, `event`; `forgeSendCmd(req)`; `forgeSendReadyMsg` loses `pendingID`; `forgeSendFinished` loses the queue answer)
- Modify: `internal/tui/model.go` (`pendingSends`, `pendingWatch` fields; the four pending messages; the heartbeat's `pendingSendsTick`; `Init` / `reRoot` commands; the decision preselect)
- Modify: `internal/tui/notify.go` (drop `pendingSendNotices`)
- Modify: every `forgeSendCmd(…, "")` call site; tests in `forge_send_fixes_test.go`, `pr_send_serial_test.go` (drop `TestApprovedPendingSendIsSentAndAnswered`, the `pendingID` leg of `TestASendsFollowUpRunsWithTheStashListOpen`)
- Modify: `internal/i18n/lang/{ja,ko,zh,ru}.toml` (orphaned keys: "asked %s", "asks for: %s", "Nothing is posted until you confirm.", "Review and send…", "Reject", "Later" only if unused elsewhere, every "%s wants to …" key)
- Test: `internal/tui/forge_send_test.go` (append)

**Interfaces:**
- Consumes: Task 1's `PRSendRequest` (no `Event`).
- Produces: `func (m Model) forgeSendCmd(req domain.PRSendRequest) (Model, tea.Cmd)`.

- [ ] **Step 1: Write the failing test**

Append to `internal/tui/forge_send_test.go`:

```go
// Agents never send (user ruling 2026-10-08): the TUI has no notice source
// for an agent's queued send, so nothing an agent writes can raise "send".
func TestNoNoticeOffersAnAgentsSend(t *testing.T) {
	t.Parallel()
	m := newTestModel(t)
	m = m.rebuildNotices()
	for _, n := range m.notices {
		if strings.HasPrefix(n.id, "pending_send_") {
			t.Fatalf("a pending-send notice exists: %+v", n)
		}
	}
	src, err := os.ReadFile("notify.go")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(src), "pendingSendNotices") {
		t.Fatal("notify.go still builds pending-send notices")
	}
}
```

(`newTestModel`: this package's existing constructor helper — use its real name.)

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/tui/ -run NoNoticeOffersAnAgentsSend 2>&1 | tail`
Expected: FAIL — `notify.go still builds pending-send notices`.

- [ ] **Step 3: Implement**

Delete `pending_sends.go` and its test. In `notify.go` remove `next = append(next, pendingSendNotices(m)...)`. In `model.go` remove the `pendingSends`/`pendingWatch` fields, the `pendingSendsMsg`/`pendingStatMsg`/`pendingWatchMsg`/`pendingWakeMsg` cases, `m.pendingWatchCmd(m.noticeGen), m.pendingSendsReadCmd(m.noticeGen)` from `Init` and `reRoot`'s batch, the heartbeat's `pendingSendsTick` call, any `closePendingWatch()` call, and the preselect:

```go
	case opDecisionMsg:
		req := msg.req
		if fs := m.forgeSend; fs != nil && req.ID == engine.DecisionSendForge {
			// The TUI's own words for the plan it holds (plan 3, T4).
			w, h := m.overlayDims()
			req.Prompt, req.PromptMsg = sendConfirmTextFit(fs.plan, w-8, h-12), engine.Msg{}
		}
		m.modal = &decisionState{req: req, reply: msg.reply}
```

In `forge_send.go`: `type forgeSendState struct{ pr int; plan engine.SendPlan }`; `forgeSendReadyMsg{req, op, err}`; `forgeSendCmd(req domain.PRSendRequest)`; `handleForgeSendReady` sets `&forgeSendState{pr: msg.req.PR, plan: msg.op.Plan}`; `forgeSendFinished` drops the `pendingFinishCmd` leg. Fix every call site (`grep -n 'forgeSendCmd(' internal/tui/*.go`). Run the i18n gate and delete each orphaned key from all four bundles.

- [ ] **Step 4: Run the tests**

Run: `go build ./... && go test ./internal/tui/ ./internal/i18n/ 2>&1 | tail -20`
Expected: PASS (orphan gate included).

- [ ] **Step 5: Commit**

```bash
gg add internal/tui internal/i18n
git commit -F <msgfile>   # "feat(tui)!: no agent sends — the pending-send notices go"
```

---

### Task 3: an emptied AI-review body is posted empty (W2)

**Files:**
- Modify: `internal/domain/forge_send.go` (`PRSendRequest.BodySet`; `planReview`)
- Modify: `internal/cli/prsend.go` (`--body` given ⇒ `BodySet`)
- Modify: `internal/tui/send_review_popup.go` (the body popup sets `BodySet`)
- Test: `internal/domain/forge_send_body_test.go`, `internal/cli/prsend_test.go`

**Interfaces:**
- Produces: `PRSendRequest.BodySet bool` — "the user saw and answered the body box": `Body` is then used verbatim, empty included.

- [ ] **Step 1: Write the failing tests**

Append to `internal/domain/forge_send_body_test.go` (reuse the file's stored-review helper — read the file for its name; below it is `savedReview(t, svc, head)` returning the review id):

```go
func TestAnEmptiedReviewBodyIsPostedEmpty(t *testing.T) {
	t.Parallel()
	svc, _, head := sendRepo(t)
	id := savedReview(t, svc, head)
	ctx := context.Background()
	p, err := svc.planSend(ctx, PRSendRequest{PR: 7, Review: id, BodySet: true, Body: "   "})
	if err != nil {
		t.Fatal(err)
	}
	if p.Body != "" {
		t.Fatalf("an emptied body must post empty, got %q", p.Body)
	}
	for _, sk := range p.Skipped {
		if sk.Label == "review summary" {
			t.Fatalf("an emptied body is not a skipped summary: %+v", p.Skipped)
		}
	}
	// No body answer at all (the CLI without --body): the stored summary.
	p, err = svc.planSend(ctx, PRSendRequest{PR: 7, Review: id})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p.Body, "via gg") {
		t.Fatalf("no body given must post the signed summary, got %q", p.Body)
	}
}
```

Append to `internal/cli/prsend_test.go`:

```go
func TestPRSendBodyFlagSetsBodySet(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		args []string
		set  bool
	}{
		{[]string{"7", "--review", "r1"}, false},
		{[]string{"7", "--review", "r1", "--body", ""}, true},
		{[]string{"7", "--review", "r1", "--body", "mine"}, true},
	} {
		req, err := parsePRSend(tc.args, io.Discard)
		if err != nil || req.BodySet != tc.set {
			t.Errorf("%v: BodySet %v, err %v", tc.args, req.BodySet, err)
		}
	}
}
```

(`parsePRSend` is extracted from `prSend` in Step 3: the flag parsing, returning the request.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ ./internal/cli/ -run 'EmptiedReviewBody|BodyFlagSetsBodySet' 2>&1 | tail`
Expected: FAIL — `BodySet` undefined.

- [ ] **Step 3: Implement**

`PRSendRequest` gains `BodySet bool // the user answered the body box: Body is used as is, empty included`. In `planReview`'s `case req.Review != ""`:

```go
		plan.Key, plan.Body, plan.Verdict = r.ID, reviewSendBody(r), true
		edited := strings.TrimSpace(req.Body)
		switch {
		case req.BodySet && edited == "":
			plan.Body = "" // the user cleared it (W2): no body, no trailer, no marker
		case edited != "": // the user edited the summary (plan 3, T7)
			plan.Body = sendBody(model.Note{Source: model.NoteSourceAgent, Author: r.Agent, Summary: edited}, r.ID, "")
		}
		// The stored summary is on GitHub already: skip it — unless the user
		// typed a body of their own, which is new text and goes.
		if r.summarySent(pr.Number) && !(req.BodySet && edited == "") && (edited == "" || edited == r.summaryText()) {
			plan.Body = ""
			plan.Skipped = append(plan.Skipped, engine.SendSkip{Label: "review summary", Reason: SkipOnGitHub})
		}
```

CLI: extract `parsePRSend(args []string, stderr io.Writer) (domain.PRSendRequest, error)`; after `fs.Parse`, `fs.Visit(func(f *flag.Flag) { if f.Name == "body" { req.BodySet = true } })`. TUI `send_review_popup.go` line ~136: `req := domain.PRSendRequest{PR: p.pr, Body: strings.TrimSpace(p.body.Value()), BodySet: true}`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ ./internal/cli/ ./internal/tui/ -run 'Body|SendReview' 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain internal/cli internal/tui
git commit -F <msgfile>   # "feat(send): an emptied AI-review body is posted empty"
```

---

### Task 4: one group → colour slot, on the wire (W3)

**Files:**
- Create: `internal/domain/note_group.go`, `internal/domain/note_group_test.go`
- Modify: `internal/tui/note_group.go` (`groupSlot` → `domain.GroupSlot`; `groupSlotMine` → `domain.GroupSlotMine`), `internal/tui/note_group_test.go`
- Modify: `internal/domain/notewire.go` (`WireNote.GroupSlot`, set by `ToWireNoteRendered`)
- Modify: `internal/web/prnotes.go` (`groups`: path → slots)
- Modify: `internal/web/static/style.css` (`--note-group-1..6`)
- Test: `internal/web/prsend_test.go` (new file, first tests), `internal/web/style_groups_test.go` (new)

**Interfaces:**
- Produces: `func domain.GroupSlot(group string) int` (0 for "", else 1–6); `const domain.GroupSlotMine = 5`; `WireNote.GroupSlot int \`json:"group_slot,omitempty"\``; `/api/pr/notes` answers `"groups": {"<path>": [slot, …]}`.

- [ ] **Step 1: Write the failing tests**

`internal/domain/note_group_test.go`:

```go
package domain

import "testing"

func TestGroupSlotIsStableAndPinned(t *testing.T) {
	t.Parallel()
	if GroupSlot("") != 0 {
		t.Fatal("no group, no slot")
	}
	// Pinned: a change of hash would recolour every user's groups (the TUI's
	// values before the move).
	if GroupSlot(GroupMine) != GroupSlotMine || GroupSlotMine != 5 {
		t.Fatalf("GroupSlot(mine) = %d", GroupSlot(GroupMine))
	}
	for _, id := range []string{"review:r-1", "github:PRR_9", "review:abc"} {
		if s := GroupSlot(id); s < 1 || s > 6 {
			t.Fatalf("GroupSlot(%q) = %d", id, s)
		}
	}
}

func TestRenderedWireNoteCarriesItsSlot(t *testing.T) {
	t.Parallel()
	r := ResolvedNote{Note: model.Note{ID: "n1", Source: model.NoteSourceUser}, Group: GroupMine}
	if w := ToWireNoteRendered(r, true); w.GroupSlot != GroupSlotMine {
		t.Fatalf("rendered slot = %d", w.GroupSlot)
	}
	if w := ToWireNote(r); w.GroupSlot != 0 {
		t.Fatal("the agent-facing JSON stays unchanged: no group_slot")
	}
}
```

(add the `model` import.) `internal/web/style_groups_test.go`:

```go
package web

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/homeend/gigagit/internal/theme"
)

// The web's six group colours are the TUI dark theme's: a review keeps one
// colour in both frontends.
func TestWebGroupColoursAreTheDarkTheme(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile(filepath.Join("static", "style.css"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{theme.Dark.NoteGroup1, theme.Dark.NoteGroup2, theme.Dark.NoteGroup3,
		theme.Dark.NoteGroup4, theme.Dark.NoteGroup5, theme.Dark.NoteGroup6}
	for i, c := range want {
		re := regexp.MustCompile(fmt.Sprintf(`--note-group-%d:\s*([^;]+);`, i+1))
		m := re.FindStringSubmatch(string(b))
		if m == nil || !strings.EqualFold(strings.TrimSpace(m[1]), c) {
			t.Errorf("--note-group-%d = %v, want %s", i+1, m, c)
		}
	}
}
```

(the archtest allows a `_test.go` file to import `theme`; if archtest forbids it for web, read the hexes from `internal/theme/theme.go` source instead.)

In `internal/web/prsend_test.go` (new), the PR-groups leg:

```go
package web

import "testing"

// Serial: prNotesServer → prFixture isolates XDG state with t.Setenv.
func TestPRNotesCountsCarryGroupSlots(t *testing.T) {
	fs, head := prNotesServer(t)
	addWebNote(t, fs.ts, head, "pr7.txt", 1, "mine")
	var out struct {
		Groups map[string][]int `json:"groups"`
	}
	if code := getJSON(t, fs.ts, "/api/pr/notes?n=7", &out); code != 200 {
		t.Fatalf("code %d", code)
	}
	if got := out.Groups["pr7.txt"]; len(got) == 0 || got[0] != 5 {
		t.Fatalf("groups = %v (want my draft review's slot 5 first)", out.Groups)
	}
}
```

`addWebNote` (new helper in `prsend_test.go`) POSTs `/api/notes` the way the page writes a note on a commit (read `notes.go`'s add request shape: rev = `head`, path, side "new", line) and fails the test on a non-200.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/domain/ ./internal/web/ -run 'GroupSlot|RenderedWireNote|WebGroupColours|PRNotesCountsCarryGroupSlots' 2>&1 | tail -20`
Expected: FAIL — `undefined: GroupSlot`, missing CSS vars, no `groups`.

- [ ] **Step 3: Implement**

`internal/domain/note_group.go`:

```go
package domain

import "hash/fnv"

// GroupSlot is a note group's colour slot (spec 2026-10-07 §1.3): 0 for no
// group, else 1–6 from the group id's FNV-1a hash — stable across runs,
// processes and frontends, so a review and the GitHub review it became
// share a colour in the TUI and the web.
func GroupSlot(group string) int {
	if group == "" {
		return 0
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(group))
	return int(h.Sum32()%6) + 1
}

// GroupSlotMine pins "my draft review"'s slot: a hash change would
// recolour every user's groups, and the test catches it.
const GroupSlotMine = 5
```

TUI: `note_group.go` keeps `groupBarStyle`, drops `groupSlot`/`groupSlotMine`; every `groupSlot(` call → `domain.GroupSlot(`; its test drops the moved cases.

`notewire.go`: field `GroupSlot int \`json:"group_slot,omitempty"\`` beside `Group`; in `ToWireNoteRendered` after `attachMarkdown(&w, r)`: `w.GroupSlot = GroupSlot(r.Group)`.

`prnotes.go` `handlePRNotes`, after the counts: 

```go
	groups, gerr := svc.PreviewNoteGroups(ctx, set)
	if gerr != nil && !errors.Is(gerr, domain.ErrNotesDisabled) {
		writeErr(w, http.StatusInternalServerError, gerr)
		return
	}
	slots := map[string][]int{}
	for p, gs := range groups {
		for _, g := range gs {
			slots[p] = append(slots[p], domain.GroupSlot(g))
		}
	}
	out["groups"] = slots
```

(and `"groups": map[string][]int{}` in the initial `out`). `style.css` `:root` gains `--note-group-1: #E06C75; --note-group-2: #E5C07B; --note-group-3: #98C379; --note-group-4: #56B6C2; --note-group-5: #C678DD; --note-group-6: #D19A66;` (verify against `theme.Dark` when writing).

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/domain/ ./internal/web/ ./internal/tui/ -run 'Group|PRNotes' 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/domain internal/tui internal/web
git commit -F <msgfile>   # "feat(web): note group slots on the wire, one colour function in domain"
```

---

### Task 5: the send endpoint — plan outside the gate, allowlisted wire values

**Files:**
- Create: `internal/web/prsend.go`
- Modify: `internal/web/oprun.go` (`notes_changed` extra → `emitNotes` after finish)
- Test: `internal/web/prsend_test.go` (append; the recording writer forge)

**Interfaces:**
- Consumes: `domain.PRSendOp`, `PRSendGroups`, `ReviewBodyText`, `PRNotes`, `PRFetched`, `PRSendRequest.BodySet`.
- Produces:
  - `POST /api/pr/send?n=<n>` body `{"kind": "notes"|"group"|"verdict"|"resolve"|"unresolve"|"finish"|"discard", "ids": [...], "group": "...", "body": "...", "body_set": bool}` → `202 {"op_id": "...", "plan": sendPlanWire}`; 400 bad input, 404 unknown PR, 409 head moved / op busy, 422 nothing to send / refused.
  - `GET /api/pr/send/groups?n=<n>` → `{"groups": [{"id","agent","summary","count","body"}], "own_pr": bool}` (`body` = the AI review's summary as the body prefill, `""` for mine).
  - `type sendPlanWire struct{ Target, Mode string; Verdict, OwnPR, HasPending bool; Body string; Items []sendItemWire; Skipped []sendSkipWire }` (`Mode`: review | actions | finish | discard; no sha, no node id, no keys).

- [ ] **Step 1: Write the failing tests**

Append to `internal/web/prsend_test.go`:

```go
// writerForge is fakeForge + a recording forge.Writer + Snapshot: the
// web's send tests never reach a real forge.
type writerForge struct {
	*fakeForge
	wmu    sync.Mutex
	writes []string
}

func (f *writerForge) Snapshot(ctx context.Context, n int) (forge.Snapshot, error) {
	return snapForge{f.fakeForge}.Snapshot(ctx, n)
}
func (f *writerForge) log(s string) { f.wmu.Lock(); f.writes = append(f.writes, s); f.wmu.Unlock() }
func (f *writerForge) writeLog() string {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	return strings.Join(f.writes, "\n")
}
func (f *writerForge) StartReview(_ context.Context, prID, commit string) (string, error) {
	f.log("StartReview " + prID)
	return "PRR_1", nil
}
func (f *writerForge) AddThread(_ context.Context, review string, t forge.Thread) (forge.ThreadRef, error) {
	f.log(fmt.Sprintf("AddThread %s %s:%d", review, t.Path, t.Line))
	return forge.ThreadRef{ThreadID: "PRRT_1", CommentID: "PRRC_1"}, nil
}
func (f *writerForge) Reply(_ context.Context, review, thread, body string) (forge.CommentRef, error) {
	f.log("Reply " + thread)
	return forge.CommentRef{ID: "PRRC_2"}, nil
}
func (f *writerForge) SubmitReview(_ context.Context, review string, ev forge.Event, body string) error {
	f.log(fmt.Sprintf("SubmitReview %s %q", ev, body))
	return nil
}
func (f *writerForge) DeletePendingReview(_ context.Context, review string) error {
	f.log("DeletePendingReview " + review)
	return nil
}
func (f *writerForge) Resolve(_ context.Context, thread string) error   { f.log("Resolve " + thread); return nil }
func (f *writerForge) Unresolve(_ context.Context, thread string) error { f.log("Unresolve " + thread); return nil }

// sendServer is a fetched PR #7 (node id PR_7) on a writable fake forge.
func sendServer(t *testing.T) (*httptest.Server, *writerForge, string) {
	t.Helper()
	dir, bare, head := prFixture(t)
	pr := openPR(7, "Add a thing")
	pr.HeadSHA, pr.NodeID = head, "PR_7"
	wf := &writerForge{fakeForge: &fakeForge{open: []model.PullRequest{pr}, baseURL: bare, comments: prThreads()}}
	ts, _ := prServe(t, dir, wf)
	waitPRsLoaded(t, ts)
	if done := runPROp(t, ts, "pr-fetch", 7); done["ok"] != true {
		t.Fatalf("pr-fetch: %v", done)
	}
	return ts, wf, head
}

// followDecide reads op id's events, answers its forge.send decision with
// option, and returns every event through done.
func followDecide(t *testing.T, ts *httptest.Server, id, option string) []wireEvent {
	t.Helper()
	resp, err := (&http.Client{Timeout: 30 * time.Second}).Get(ts.URL + "/api/op/" + id + "/events")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var evs []wireEvent
	sc := bufio.NewScanner(resp.Body)
	for sc.Scan() {
		line, ok := strings.CutPrefix(sc.Text(), "data: ")
		if !ok {
			continue
		}
		var we wireEvent
		if err := json.Unmarshal([]byte(line), &we); err != nil {
			t.Fatal(err)
		}
		evs = append(evs, we)
		switch we["type"] {
		case "decision":
			if code, _ := postJSONAny(t, ts, "/api/op/"+id+"/decide", `{"option":"`+option+`"}`); code != 200 {
				t.Fatalf("decide = %d", code)
			}
		case "done":
			return evs
		}
	}
	t.Fatalf("no done: %v", evs)
	return nil
}

// Serial: sendServer → prFixture isolates XDG state with t.Setenv.
func TestWebSendsOneNoteBehindTheConfirm(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "rename this")
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != 202 {
		t.Fatalf("start = %d %v", code, out)
	}
	plan, _ := json.Marshal(out["plan"])
	if strings.Contains(string(plan), head) || strings.Contains(string(plan), "PR_7") {
		t.Fatalf("the plan leaks a sha or a node id: %s", plan)
	}
	if !strings.Contains(string(plan), "rename this") {
		t.Fatalf("plan = %s", plan)
	}
	if wf.writeLog() != "" {
		t.Fatal("nothing may be written before the confirm is answered")
	}
	evs := followDecide(t, ts, out["op_id"].(string), "send")
	done, _ := findEvent(evs, "done")
	if done["ok"] != true || !strings.Contains(wf.writeLog(), "SubmitReview COMMENT") {
		t.Fatalf("done %v, writes %s", done, wf.writeLog())
	}
}

// Serial: sendServer.
func TestWebSendAbortPostsNothing(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	_, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	followDecide(t, ts, out["op_id"].(string), "abort")
	if w := wf.writeLog(); w != "" {
		t.Fatalf("an aborted send wrote: %s", w)
	}
}

// Serial: sendServer.
func TestWebSendRefusesForgedValues(t *testing.T) {
	ts, wf, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "x")
	big := strings.Repeat("a", 70<<10)
	for _, tc := range []struct{ q, body string; code int }{
		{"n=0", `{"kind":"verdict"}`, 400},
		{"n=99", `{"kind":"verdict"}`, 404},
		{"n=7", `{"kind":"post-anything"}`, 400},
		{"n=7", `{"kind":"notes","ids":[]}`, 400},
		{"n=7", `{"kind":"notes","ids":["` + head + `"]}`, 400},
		{"n=7", `{"kind":"notes","ids":["../x"]}`, 400},
		{"n=7", `{"kind":"resolve","ids":["forge:NOPE"]}`, 400},
		{"n=7", `{"kind":"group","group":"review:not-listed"}`, 400},
		{"n=7", `{"kind":"verdict","body":"` + big + `"}`, 400},
	} {
		if code, out := postJSONAny(t, ts, "/api/pr/send?"+tc.q, tc.body); code != tc.code {
			t.Errorf("%s %s = %d %v, want %d", tc.q, tc.body[:min(len(tc.body), 40)], code, out, tc.code)
		}
	}
	if w := wf.writeLog(); w != "" {
		t.Fatalf("a refused request wrote: %s", w)
	}
}

// Serial: sendServer.
func TestWebSendRefusesAMovedHead(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	wf.mu.Lock()
	wf.open[0].HeadSHA = strings.Repeat("e", 40)
	wf.mu.Unlock()
	code, out := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`)
	if code != 409 || !strings.Contains(fmt.Sprint(out["error"]), "new commits") {
		t.Fatalf("= %d %v", code, out)
	}
}

// Serial: sendServer.
func TestWebSendGroupsListsMine(t *testing.T) {
	ts, _, head := sendServer(t)
	addWebNote(t, ts, head, "pr7.txt", 1, "x")
	var out struct {
		Groups []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
		} `json:"groups"`
	}
	if code := getJSON(t, ts, "/api/pr/send/groups?n=7", &out); code != 200 || len(out.Groups) != 1 || out.Groups[0].ID != "mine" || out.Groups[0].Count != 1 {
		t.Fatalf("= %d %+v", code, out)
	}
}

// Serial: sendServer.
func TestWebSendWhileAnOpRunsIs409(t *testing.T) {
	ts, _, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	_, first := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"notes","ids":["`+id+`"]}`) // parks on its confirm
	if code, _ := postJSONAny(t, ts, "/api/pr/send?n=7", `{"kind":"verdict"}`); code != 409 {
		t.Fatalf("a second send while one is parked = %d", code)
	}
	followDecide(t, ts, first["op_id"].(string), "abort")
}
```

(imports: `bufio`, `context`, `encoding/json`, `fmt`, `net/http`, `net/http/httptest`, `strings`, `sync`, `time`, `forge`, `model`.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'WebSend' 2>&1 | tail -20`
Expected: FAIL — 404/405 on `/api/pr/send`.

- [ ] **Step 3: Implement**

`internal/web/prsend.go`:

```go
package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/homeend/gigagit/internal/domain"
	"github.com/homeend/gigagit/internal/engine"
)

// Sending to GitHub from the page (spec 2026-10-07 §4.2). The page names
// WHAT to send with values this server handed it — the PR number, note and
// remark ids, GitHub thread ids, a group id — and every one is checked
// against the PR's cached notes before anything is planned. The plan is
// built here, in the handler, outside the repo gate (R12); the op parks on
// its one confirm in the page's modal. Agents never send (user ruling
// 2026-10-08): nothing here is reachable from a gg tool or skill.

func init() {
	RegisterRoutes(func(mux *http.ServeMux, s *Server) {
		mux.HandleFunc("POST /api/pr/send", writeGuard(s.handlePRSend))
		mux.HandleFunc("GET /api/pr/send/groups", s.handlePRSendGroups)
	})
}

const (
	maxSendBody = 64 << 10
	maxSendIDs  = 500
)

type prSendWire struct {
	Kind    string   `json:"kind"`
	IDs     []string `json:"ids"`
	Group   string   `json:"group"`
	Body    string   `json:"body"`
	BodySet bool     `json:"body_set"`
}

type sendItemWire struct {
	Label   string `json:"label"`
	Replies int    `json:"replies,omitempty"`
	Resolve bool   `json:"resolve,omitempty"`
}

type sendSkipWire struct {
	Label  string `json:"label"`
	Reason string `json:"reason"`
}

// sendPlanWire is what the page's confirm draws: never a sha, a node id or
// a local key — the page only ever posts back an option.
type sendPlanWire struct {
	Target     string         `json:"target"`
	Mode       string         `json:"mode"`
	Verdict    bool           `json:"verdict"`
	OwnPR      bool           `json:"own_pr"`
	HasPending bool           `json:"has_pending"`
	Body       string         `json:"body"`
	Items      []sendItemWire `json:"items"`
	Skipped    []sendSkipWire `json:"skipped"`
}

func planWire(p engine.SendPlan) sendPlanWire {
	mode := map[engine.SendMode]string{engine.SendReview: "review", engine.SendActions: "actions",
		engine.SendFinish: "finish", engine.SendDiscard: "discard"}[p.Mode]
	w := sendPlanWire{Target: p.Target, Mode: mode, Verdict: p.Verdict, OwnPR: p.OwnPR, HasPending: p.Pending != "",
		Body: p.BodyText(), Items: []sendItemWire{}, Skipped: []sendSkipWire{}}
	for _, it := range p.Items {
		w.Items = append(w.Items, sendItemWire{Label: it.Label, Replies: len(it.Replies), Resolve: it.Resolve})
	}
	for _, sk := range p.Skipped {
		w.Skipped = append(w.Skipped, sendSkipWire{Label: sk.Label, Reason: sk.Reason})
	}
	return w
}

// prSendIDs are the ids PR n's view holds: local roots, review remarks and
// draft replies (notes), and GitHub thread roots (threads).
func prSendIDs(ctx context.Context, svc *domain.Service, n int) (notes, threads map[string]bool, err error) {
	byPath, err := svc.PRNotes(ctx, n)
	if err != nil {
		return nil, nil, err
	}
	notes, threads = map[string]bool{}, map[string]bool{}
	for _, rs := range byPath {
		for _, r := range rs {
			if r.Note.Source == model.NoteSourceForge {
				threads[r.Note.ID] = true
			} else {
				notes[r.Note.ID] = true
			}
			for _, rep := range r.Replies {
				if rep.Note.Source != model.NoteSourceForge {
					notes[rep.Note.ID] = true
				}
			}
		}
	}
	return notes, threads, nil
}

// prSendRequest turns the page's request into the domain's, refusing any
// value the PR's cached notes do not hold.
func (s *Server) prSendRequest(ctx context.Context, svc *domain.Service, n int, in prSendWire) (domain.PRSendRequest, error) {
	req := domain.PRSendRequest{PR: n}
	if len(in.Body) > maxSendBody {
		return req, fmt.Errorf("the review body is over %d KiB", maxSendBody>>10)
	}
	if len(in.IDs) > maxSendIDs {
		return req, errors.New("too many ids")
	}
	needIDs := func(allowed map[string]bool) ([]string, error) {
		if len(in.IDs) == 0 {
			return nil, errors.New("ids required")
		}
		for _, id := range in.IDs {
			if !allowed[id] {
				return nil, fmt.Errorf("%q is not a note of pull request #%d", id, n)
			}
		}
		return in.IDs, nil
	}
	var err error
	switch in.Kind {
	case "notes", "resolve", "unresolve":
		notes, threads, lerr := prSendIDs(ctx, svc, n)
		if lerr != nil {
			return req, lerr
		}
		switch in.Kind {
		case "notes":
			req.Notes, err = needIDs(notes)
		case "resolve":
			req.Resolve, err = needIDs(threads)
		default:
			req.Unresolve, err = needIDs(threads)
		}
	case "group":
		groups, gerr := svc.PRSendGroups(ctx, n)
		if gerr != nil {
			return req, gerr
		}
		listed := false
		for _, g := range groups {
			listed = listed || g.ID == in.Group
		}
		switch {
		case !listed:
			err = fmt.Errorf("%q is not a group of pull request #%d", in.Group, n)
		case in.Group == domain.GroupMine:
			req.Mine = true
		default:
			req.Review = strings.TrimPrefix(in.Group, "review:")
		}
		req.Body, req.BodySet = in.Body, in.BodySet
	case "verdict":
		req.Verdict, req.Body = true, in.Body
	case "finish":
		req.Finish = true
	case "discard":
		req.Discard = true
	default:
		err = fmt.Errorf("unknown kind %q", in.Kind)
	}
	return req, err
}

func (s *Server) handlePRSend(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	var in prSendWire
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxSendBody+16<<10)).Decode(&in); err != nil {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("bad request body: %w", err))
		return
	}
	ctx := r.Context()
	if s.opInFlight() {
		writeErr(w, http.StatusConflict, errOpBusy)
		return
	}
	req, err := s.prSendRequest(ctx, svc, pr.Number, in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	op, err := svc.PRSendOp(ctx, req) // outside the gate (R12)
	if err != nil {
		writeErr(w, sendErrStatus(err), err)
		return
	}
	run, err := s.startRun("op", func(ctx context.Context, svc *domain.Service, events chan<- engine.Event, dec engine.Decider) (engine.Result, map[string]any, error) {
		res, err := svc.Execute(ctx, op, events, dec)
		return res, map[string]any{"notes_changed": true, "pr": pr.Number}, err
	})
	if err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"op_id": run.id, "plan": planWire(op.Plan)})
}

// sendErrStatus maps a planning refusal to its HTTP status.
func sendErrStatus(err error) int {
	switch {
	case errors.Is(err, domain.ErrPRHeadMoved), errors.Is(err, domain.ErrInterruptedPending):
		return http.StatusConflict
	case errors.Is(err, domain.ErrSendRequest), errors.Is(err, domain.ErrMixedSend):
		return http.StatusBadRequest
	}
	return http.StatusUnprocessableEntity
}

func (s *Server) handlePRSendGroups(w http.ResponseWriter, r *http.Request) {
	svc, pr, ok := s.knownPR(w, r)
	if !ok {
		return
	}
	ctx := readCtx(r)
	groups, err := svc.PRSendGroups(ctx, pr.Number)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err)
		return
	}
	out := []map[string]any{}
	for _, g := range groups {
		body := ""
		if id, ok := strings.CutPrefix(g.ID, "review:"); ok {
			body, _ = svc.ReviewBodyText(ctx, id)
		}
		out = append(out, map[string]any{"id": g.ID, "agent": g.Agent, "summary": g.Summary, "count": g.Count,
			"slot": domain.GroupSlot(g.ID), "body": body})
	}
	own := false
	if p, _, ok := svc.PRDetailsCached(pr.Number); ok {
		own = p.ViewerDidAuthor
	}
	writeJSON(w, map[string]any{"groups": out, "own_pr": own})
}
```

(add the `model` import; `errOpBusy` and `opInFlight` exist in `oprun.go` / `live.go` — use the real names.) `oprun.go` `runOpStream`: replace the `noteId` emit with

```go
	if id, _ := extra["noteId"].(string); id != "" || extra["notes_changed"] == true {
		s.emitNotes()
	}
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'WebSend|PRNotes|OpStart' 2>&1 | tail -20` then `go test ./internal/archtest/`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
gg add internal/web
git commit -F <msgfile>   # "feat(web): POST /api/pr/send — the plan outside the gate, every wire value allowlisted"
```

---

### Task 6: marks and group borders on the PR's notes; badge stripes

**Files:**
- Modify: `internal/web/static/notebox.js` (`noteMark`, origin in `noteTitle`)
- Modify: `internal/web/static/files.js` (`noteBoxHTML`, `noteBadgeHTML`, the PR counts' `groups`)
- Modify: `internal/web/static/previews.js` (`loadPRCounts` keeps `d.groups` in `state.previewGroups`)
- Modify: `internal/web/static/style.css`
- Test: `internal/web/noteboxjs_test.go` (extend), `internal/web/filesminjs_test.go` or a new `notemarkjs_test.go`

**Interfaces:**
- Produces (notebox.js, pure): `export function noteMark(n, inPR)` → `{glyph, cls, tip}` or `null`; `noteTitle(n, path, preview, nowMs)` appends `· from <origin>` when `n.origin` is set.
  - in a PR's own diff: `sync` `"local"` → `○` (`mark-local`), `"sending"` → `◌`, `"failed"` → `○!` (`mark-failed`, tip = `send_error`), `"github"` / a forge note → `●` (`mark-github`); outside: only `sending` and `failed`.

- [ ] **Step 1: Write the failing test**

Extend the runner in `noteboxjs_test.go` (import `noteMark` too):

```js
const marks = [
  noteMark({ sync: "local" }, true),
  noteMark({ sync: "sending" }, true),
  noteMark({ sync: "failed", send_error: "HTTP 403" }, true),
  noteMark({ source: "forge", read_only: true, sync: "github" }, true),
  noteMark({ sync: "local" }, false),
  noteMark({ sync: "failed", send_error: "x" }, false),
  noteMark({ sync: "github", source: "forge", read_only: true }, false),
];
const carried = noteTitle({ id: "n9", source: "user", side: "new", line: 3, status: "active", origin: "a1b2c3d" }, "a.go", true, now);
```

and in the Go assertions:

```go
	wantMarks := []string{`{"glyph":"○","cls":"mark-local"`, `{"glyph":"◌"`, `{"glyph":"○!","cls":"mark-failed","tip":"HTTP 403"}`,
		`{"glyph":"●","cls":"mark-github"`, `null`, `{"glyph":"○!"`, `null`}
	for i, w := range wantMarks {
		if !strings.HasPrefix(string(got.Marks[i]), w) {
			t.Errorf("mark %d = %s, want prefix %s", i, got.Marks[i], w)
		}
	}
	if !strings.HasSuffix(got.Carried, "· from a1b2c3d") {
		t.Errorf("carried title = %q", got.Carried)
	}
```

(`got.Marks []json.RawMessage`, `got.Carried string` added to the decoded struct; `console.log` gains `marks, carried`.)

Also a static check in the same file family (new `TestNoteBoxDrawsMarksInPRDiffOnly` in `prs_static_test.go` style): `files.js` must contain `noteMark(n, inPR)` and `"notebox " + kind` gaining `g${slot}` only under `inPR` — assert the strings `const inPR = !!(nc.ctx && nc.ctx.preview && nc.ctx.preview.pr)` and `noteMark(` exist in `files.js`.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run 'NoteboxJS|NoteBoxDrawsMarks' 2>&1 | tail`
Expected: FAIL — `noteMark` is not exported.

- [ ] **Step 3: Implement**

`notebox.js`:

```js
// noteMark is a box's sync mark (spec 2026-10-07 §1.1): where the note lives.
// In a PR's own diff every box says it; elsewhere a note is always local, so
// only the two states that need attention show (plan 3 ruling T3).
export function noteMark(n, inPR) {
  const s = n.sync || (n.read_only && n.source === "forge" ? "github" : "");
  if (s === "sending") return { glyph: "◌", cls: "mark-sending", tip: "being sent to GitHub" };
  if (s === "failed") return { glyph: "○!", cls: "mark-failed", tip: n.send_error || "the last send failed" };
  if (!inPR) return null;
  if (s === "github") return { glyph: "●", cls: "mark-github", tip: "on GitHub" };
  if (s === "local") return { glyph: "○", cls: "mark-local", tip: "local — not on GitHub yet" };
  return null;
}
```

and in `noteTitle`, both branches end with `+ (n.origin ? " · from " + n.origin : "")` (the forge branch never has one).

`files.js` `noteBoxHTML`: after `const prev = …`:

```js
  // A PR's own diff (its notes come from /api/pr/notes): marks on every box and
  // the group's colour as the left border (spec §1.3); elsewhere neither.
  const inPR = !!(nc.ctx && nc.ctx.preview && nc.ctx.preview.pr);
  const mark = noteMark(n, inPR);
  const slot = inPR && n.group_slot ? " g" + n.group_slot : "";
```

the box opens with `` `<div class="notebox ${kind}${stale ? " " + cls : ""}${slot}">` ``, the title span is preceded by `` (mark ? `<span class="notemark ${mark.cls}" title="${esc(mark.tip)}">${mark.glyph}</span> ` : "") ``, and after the root part a failed send shows its error: `` if (rootOn && n.sync === "failed" && n.send_error) box += `<div class="notesenderr">${esc(n.send_error)}</div>`; ``. Import `noteMark` from `./notebox.js`.

`noteBadgeHTML(n, slots)`: `n > 0 ? `<span class="notebadge">${(slots || []).slice(0, 3).map((s) => `<span class="gbar g${s}"></span>`).join("")}◆${n}</span>` : ""`; the PR / preview badge call passes `(state.previewGroups || {})[f.path]`. `previews.js` `loadPRCounts`: `state.previewGroups = d.groups || {};`; `fetchNotes`'s preview branch (files.js) sets it from `d.groups` too; any preview that is not a PR clears it (`state.previewGroups = {}` where `previewCounts` is reset).

`style.css`:

```css
.notebox.g1 { border-left: 4px solid var(--note-group-1); }
.notebox.g2 { border-left: 4px solid var(--note-group-2); }
.notebox.g3 { border-left: 4px solid var(--note-group-3); }
.notebox.g4 { border-left: 4px solid var(--note-group-4); }
.notebox.g5 { border-left: 4px solid var(--note-group-5); }
.notebox.g6 { border-left: 4px solid var(--note-group-6); }
.gbar { display: inline-block; width: 3px; height: 0.9em; margin-right: 1px; vertical-align: -0.1em; }
.gbar.g1 { background: var(--note-group-1); } .gbar.g2 { background: var(--note-group-2); }
.gbar.g3 { background: var(--note-group-3); } .gbar.g4 { background: var(--note-group-4); }
.gbar.g5 { background: var(--note-group-5); } .gbar.g6 { background: var(--note-group-6); }
.notemark { font-weight: bold; }
.notemark.mark-failed, .notesenderr { color: #f27a6a; }
.notemark.mark-sending { color: var(--dim); }
.notemark.mark-github { color: #4fb3a8; }
.notesenderr { font-size: .92em; margin-top: 4px; }
```

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'Notebox|NoteBox|NotesJS|FilesMin' 2>&1 | tail`
Expected: PASS.

- [ ] **Step 5: Browser probe (visibility), then commit**

Build the UNFIXED binary first (`git stash`-free: `git worktree add` is not needed — build `main`'s installed `gg`), then this branch's `bin/gg`; for each, start `gg web` in a scratch repo with the fake gh (see Task 10 Step 1 for the fixture script, written now and reused), open PR #7, open `pr7.txt`, and assert with playwright: a `.notemark` is VISIBLE (bounding box non-zero, `getComputedStyle(...).display !== "none"`) and the box's computed `border-left-color` equals `--note-group-5`'s colour. Expected: fails on the old build, passes on the new; the binary is proven by md5 and `GET /static/prsend.js` (404 old / 200 new — Task 8 adds it; until then use `/static/sendplan.js` from Task 7, or skip the 404 leg here and run it in Task 10).

```bash
gg add internal/web/static internal/web/*_test.go
git commit -F <msgfile>   # "feat(web): sync marks and group borders on a PR's notes, group stripes on its badges"
```

---

### Task 7: the confirm drawn from the plan

**Files:**
- Create: `internal/web/static/sendplan.js` (pure, import-free)
- Modify: `internal/web/static/ops.js` (`handleOpEvent`'s decision branch; `showModal` takes `html` / `labels`)
- Test: `internal/web/sendplanjs_test.go` (new)

**Interfaces:**
- Produces (sendplan.js): `export const SEND_CONFIRM_CAP = 30`; `export function sendOptionLabel(o)`; `export function skipWords(reason)`; `export function sendConfirmHTML(plan, esc)` → the modal's body HTML; `export function sendDecision(ev, plan, esc)` → `{...ev, html, labels}` (`labels[o]` per option).
- Consumes: Task 5's `plan` (kept on `state.op.sendPlan` by Task 8's starter).

- [ ] **Step 1: Write the failing test**

`internal/web/sendplanjs_test.go` (the `noteboxjs_test.go` shape: copy `sendplan.js` to a temp `.mjs`, run a node script):

```go
func TestSendPlanJS(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed; the JS guard needs it")
	}
	src, err := os.ReadFile(filepath.Join("static", "sendplan.js"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "sendplan.mjs"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	const runner = `
import { sendConfirmHTML, sendDecision, sendOptionLabel, SEND_CONFIRM_CAP } from "./sendplan.mjs";
const esc = (s) => String(s).replace(/&/g, "&amp;").replace(/</g, "&lt;");
const items = Array.from({ length: 40 }, (_, i) => ({ label: "a.go:" + (i + 1) + " remark " + i }));
const plan = { target: "o/r #7", mode: "review", verdict: true, body: "line1\n<b>x</b>", items,
  skipped: [{ label: "b.go:3 old", reason: "its lines changed" }] };
const html = sendConfirmHTML(plan, esc);
const ev = sendDecision({ id: "forge.send", prompt: "Send to …", options: ["comment", "approve", "request-changes", "abort"] }, plan, esc);
console.log(JSON.stringify({ html, labels: ev.labels, cap: SEND_CONFIRM_CAP,
  pending: sendOptionLabel("submit-with-pending"), abort: sendOptionLabel("abort") }));
`
	if err := os.WriteFile(filepath.Join(dir, "run.mjs"), []byte(runner), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, filepath.Join(dir, "run.mjs")).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
	var got struct {
		HTML    string            `json:"html"`
		Labels  map[string]string `json:"labels"`
		Cap     int               `json:"cap"`
		Pending string            `json:"pending"`
		Abort   string            `json:"abort"`
	}
	if err := json.Unmarshal(out, &got); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	for _, want := range []string{"o/r #7", "&lt;b>x", "remark 0", "remark 29", "+ 10 more", "b.go:3 old", "its lines changed"} {
		if !strings.Contains(got.HTML, want) {
			t.Errorf("confirm lacks %q:\n%s", want, got.HTML)
		}
	}
	if strings.Contains(got.HTML, "remark 30") || strings.Contains(got.HTML, "<b>x") {
		t.Errorf("not capped or not escaped:\n%s", got.HTML)
	}
	if got.Labels["request-changes"] != "Request changes" || got.Labels["abort"] != "Cancel" || got.Cap != 30 {
		t.Errorf("labels = %v cap %d", got.Labels, got.Cap)
	}
	if got.Pending != "Submit with my pending review" || got.Abort != "Cancel" {
		t.Errorf("pending %q abort %q", got.Pending, got.Abort)
	}
}
```

And extend `TestPRPageSendsOnlyTheNumber` to read `prs.js`, `prsend.js`, `sendplan.js` (a missing file is skipped until Task 8 adds it) and refuse `head_sha`, `refs/gg`, `source=`, `target=` in each.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run 'SendPlanJS|PRPageSendsOnlyTheNumber' 2>&1 | tail`
Expected: FAIL — `static/sendplan.js` does not exist.

- [ ] **Step 3: Implement**

`internal/web/static/sendplan.js`:

```js
// sendplan.js — the GitHub send confirm, drawn from the plan the server
// built (prsend.go's sendPlanWire). Import-free: sendplanjs_test.go runs it
// under node. The confirm's BUTTONS stay the decision's options: this file
// only words them and draws what will be posted.

export const SEND_CONFIRM_CAP = 30;

const LABELS = {
  comment: "Comment",
  approve: "Approve",
  "request-changes": "Request changes",
  send: "Send",
  "submit-with-pending": "Submit with my pending review",
  discard: "Discard",
  abort: "Cancel",
};

export function sendOptionLabel(o) {
  return LABELS[o] || o;
}

// skipWords are the domain's skip codes (domain.Skip*) as the confirm says them.
export function skipWords(reason) {
  return "skipped: " + reason;
}

export function sendConfirmHTML(plan, esc) {
  const p = plan || {};
  let h = `<div class="sc-target">Send to <b>${esc(p.target || "")}</b></div>`;
  if (p.mode === "finish") h += `<div class="sc-note">finish the review an interrupted send left pending on GitHub</div>`;
  if (p.mode === "discard") h += `<div class="sc-note">discard the review an interrupted send left pending — nothing of it is posted</div>`;
  if (p.has_pending) h += `<div class="sc-note">you have a review pending on GitHub: these comments join it</div>`;
  if (p.body) h += `<div class="sc-label">review body</div><pre class="sc-body">${esc(p.body)}</pre>`;
  const items = p.items || [];
  if (items.length) {
    h += `<div class="sc-label">${items.length === 1 ? "1 item" : items.length + " items"}</div><ul class="sc-items">`;
    for (const it of items.slice(0, SEND_CONFIRM_CAP)) {
      const extra = (it.replies ? ` (+ ${it.replies === 1 ? "1 reply" : it.replies + " replies"})` : "") + (it.resolve ? " (then resolved)" : "");
      h += `<li>+ ${esc(it.label)}${esc(extra)}</li>`;
    }
    if (items.length > SEND_CONFIRM_CAP) h += `<li class="sc-more">+ ${items.length - SEND_CONFIRM_CAP} more</li>`;
    h += `</ul>`;
  }
  const sk = p.skipped || [];
  if (sk.length) {
    h += `<div class="sc-label">not sent</div><ul class="sc-skipped">`;
    for (const s of sk.slice(0, SEND_CONFIRM_CAP)) h += `<li>− ${esc(s.label)} <span class="sc-why">(${esc(skipWords(s.reason))})</span></li>`;
    if (sk.length > SEND_CONFIRM_CAP) h += `<li class="sc-more">− ${sk.length - SEND_CONFIRM_CAP} more</li>`;
    h += `</ul>`;
  }
  return h;
}

// sendDecision is a forge.send decision event dressed for the modal.
export function sendDecision(ev, plan, esc) {
  const labels = {};
  for (const o of ev.options || []) labels[o] = sendOptionLabel(o);
  return { ...ev, html: sendConfirmHTML(plan, esc), labels };
}
```

`ops.js`: import `sendDecision` from `./sendplan.js`; in `handleOpEvent`:

```js
  } else if (ev.type === "decision") {
    const plan = state.op && state.op.sendPlan;
    showModal(plan && ev.id === "forge.send" ? sendDecision(ev, plan, esc) : ev);
```

`showModal(ev)`: `if (ev.html) $("modal-prompt").innerHTML = ev.html; else $("modal-prompt").textContent = ev.prompt;` and the buttons use `esc((ev.labels && ev.labels[o]) || o)` for their text (the `data-o` stays the option value). Styles (`.sc-*`, `#modal-prompt .sc-body { max-height: 30vh; overflow: auto; white-space: pre-wrap; }`, `#modal-prompt .sc-items, .sc-skipped { max-height: 40vh; overflow: auto; }`) go in `style.css` next to the modal's.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ -run 'SendPlanJS|PRPageSendsOnlyTheNumber|ModalEsc' 2>&1 | tail`
Expected: PASS (esc still answers `abort`: `escapeOption` reads the option VALUES).

- [ ] **Step 5: Commit**

```bash
gg add internal/web/static internal/web/*_test.go
git commit -F <msgfile>   # "feat(web): the GitHub send confirm drawn from the plan"
```

---

### Task 8: sending from the page — note rows, PR rows, the prompts

**Files:**
- Create: `internal/web/static/prsend.js`
- Modify: `internal/web/static/menus.js` (`"note"`, `"pr"` in `MENUS`)
- Modify: `internal/web/static/files.js` (the note menu: `noteRows.push(...extraRows("note", { n, rootId, ctx: noteSlotCtx(n) || state.diffCtx }))` before the fold row; the plain "Resolve thread" row stays for stored threads)
- Modify: `internal/web/static/app.js` (`import "./prsend.js";`)
- Test: `internal/web/prsendjs_test.go` (new: pure row logic), `prs_static_test.go` (extended guard)

**Interfaces:**
- Consumes: Task 5's endpoints, Task 7's `state.op.sendPlan`, `followOp(opID, label, kind, onDone)` (ops.js), `openPrompt` / `showCtxMenu` (layers.js), `fetchNotes` / `refreshNoteCounts` (files.js), `loadPRCounts` (previews.js).
- Produces:
  - `export function sendRows(n, pr)` (pure, in a pure helper module `prsendrows.js`, import-free) → `[{id, label}]` — the TUI's rows: on a local root (`!read_only || remark`, `sync` not `sending`/`github`): `send` ("Send to GitHub", "Retry sending to GitHub" when failed), `send-review` ("Send my draft review…" for group `mine`, else "Send this AI review…"); on a GitHub thread root (`source === "forge"`, no `parent_id`): `reply-send` ("Reply & send…"), `resolve` ("Resolve on GitHub" / "Reopen on GitHub" from `resolved`), `send-drafts` ("Send draft reply" / "Send N draft replies") when replies hold local drafts (`source !== "forge"`).
  - `export function sendToGitHub(n, body, label)` (prsend.js) — POST `/api/pr/send?n=`, `followOp(..., "pr-send", onSendDone)`, `state.op.sendPlan = resp.plan`.
  - `export function onSendDone(fn)` — a hook prs.js registers (freshness + interrupted re-read) without an import cycle.

- [ ] **Step 1: Write the failing test**

`internal/web/prsendjs_test.go` runs `prsendrows.js` under node:

```js
import { sendRows } from "./prsendrows.mjs";
const ids = (rows) => rows.map((r) => r.id + ":" + r.label);
console.log(JSON.stringify({
  local: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 7)),
  failed: ids(sendRows({ id: "n1", source: "user", sync: "failed", group: "mine" }, 7)),
  sending: ids(sendRows({ id: "n1", source: "user", sync: "sending", group: "mine" }, 7)),
  remark: ids(sendRows({ id: "review:r1:0", source: "agent", read_only: true, replyable: true, sync: "local", group: "review:r1" }, 7)),
  thread: ids(sendRows({ id: "forge:C1", source: "forge", read_only: true, replyable: true, sync: "github",
    replies: [{ id: "d1", source: "user" }, { id: "d2", source: "user" }, { id: "forge:C2", source: "forge" }] }, 7)),
  resolved: ids(sendRows({ id: "forge:C3", source: "forge", read_only: true, sync: "github", resolved: true }, 7)),
  notPR: ids(sendRows({ id: "n1", source: "user", sync: "local", group: "mine" }, 0)),
}));
```

Go assertions:

```go
	want := map[string][]string{
		"local":    {"send:Send to GitHub", "send-review:Send my draft review…"},
		"failed":   {"send:Retry sending to GitHub", "send-review:Send my draft review…"},
		"sending":  nil,
		"remark":   {"send:Send to GitHub", "send-review:Send this AI review…"},
		"thread":   {"reply-send:Reply & send…", "resolve:Resolve on GitHub", "send-drafts:Send 2 draft replies"},
		"resolved": {"reply-send:Reply & send…", "resolve:Reopen on GitHub"},
		"notPR":    nil,
	}
```

(compare each key with `slices.Equal`, nil ≡ empty.) Extend `TestPRPageSendsOnlyTheNumber`'s file list with `prsend.js` and `prsendrows.js`, and assert `prsend.js` posts only to `"/api/pr/send?n=" + n` and `"/api/pr/send/groups?n=" + n`.

- [ ] **Step 2: Run it to verify it fails**

Run: `go test ./internal/web/ -run 'PRSendJS|PRPageSendsOnlyTheNumber' 2>&1 | tail`
Expected: FAIL — `static/prsendrows.js` does not exist.

- [ ] **Step 3: Implement**

`internal/web/static/prsendrows.js` (import-free):

```js
// prsendrows.js — which GitHub rows a note's right-click menu offers in a
// PR's own diff (the TUI's forgeNoteRows). Pure: prsendjs_test.go runs it.
export function sendRows(n, pr) {
  if (!pr || !n) return [];
  const rows = [];
  const forge = n.source === "forge";
  if (forge && !n.parent_id) {
    rows.push({ id: "reply-send", label: "Reply & send…" });
    rows.push({ id: "resolve", label: n.resolved ? "Reopen on GitHub" : "Resolve on GitHub" });
    const drafts = (n.replies || []).filter((r) => r.source !== "forge").length;
    if (drafts) rows.push({ id: "send-drafts", label: drafts === 1 ? "Send draft reply" : "Send " + drafts + " draft replies" });
    return rows;
  }
  if (forge || n.parent_id || n.sync === "sending" || n.sync === "github") return rows;
  rows.push({ id: "send", label: n.sync === "failed" ? "Retry sending to GitHub" : "Send to GitHub" });
  rows.push({ id: "send-review", label: n.group && n.group !== "mine" ? "Send this AI review…" : "Send my draft review…" });
  return rows;
}
```

`internal/web/static/prsend.js`:

```js
// prsend.js — sending a pull request's notes to GitHub from the page (spec
// 2026-10-07 §4.2). Every send goes through POST /api/pr/send: the server
// plans it (outside the repo gate), checks every id against the PR's notes,
// and the op parks on ONE confirm — drawn from that plan (sendplan.js) in the
// ordinary decision modal. Nothing posts without it; agents never send.
import { $, esc, getJSON, postJSON, state } from "./core.js";
import { openPrompt, showCtxMenu } from "./layers.js";
import { followOp, opBusy, opLine } from "./ops.js";
import { registerHelp, registerRows } from "./menus.js";
import { fetchNotes, refreshNoteCounts } from "./files.js";
import { loadPRCounts } from "./previews.js";
import { sendRows } from "./prsendrows.js";

const doneHooks = [];
export function onSendDone(fn) {
  doneHooks.push(fn);
}

// sendToGitHub starts one send; label names it on the op line. A refusal
// (409 head moved, 400 a stale id) is said and nothing runs.
export async function sendToGitHub(n, body, label, onRefused) {
  if (opBusy()) {
    opLine("another operation is running — send again when it ends", true);
    return;
  }
  let resp;
  try {
    resp = await postJSON("/api/pr/send?n=" + n, body);
  } catch (err) {
    opLine("send: " + (err.message || err), true);
    if (onRefused) onRefused(err);
    return;
  }
  followOp(resp.op_id, label, "pr-send", async (ev) => {
    if (ev.ok) opLine(ev.summary || "sent");
    else opLine("send: " + (ev.error || "failed"), true);
    await Promise.all([fetchNotes(), refreshNoteCounts(), loadPRCounts(n)]);
    for (const fn of doneHooks) fn(n, ev);
  });
  if (state.op) state.op.sendPlan = resp.plan;
}

// openPR is the PR the diff on screen belongs to (0 = none): the note rows
// exist in a PR's OWN diff only.
function openPRNumber(ctx) {
  return (ctx && ctx.preview && ctx.preview.pr) || 0;
}

function replyAndSend(n, pr) {
  openPrompt({
    title: "Reply & send to “" + n.summary + "”",
    placeholder: "summary",
    body: { label: "rationale (optional)" },
    onSubmit: async (summary, rationale) => {
      let r;
      try {
        r = await postJSON("/api/notes/reply", { id: n.id, summary, rationale });
      } catch (e) {
        opLine("reply: " + (e.message || e), true);
        return;
      }
      await fetchNotes(); // the draft must be in the PR's notes before the send names it
      sendToGitHub(pr, { kind: "notes", ids: [r.id] }, "sending the reply to #" + pr);
    },
  });
}

registerRows("note", ({ n, rootId, ctx }) => {
  const pr = openPRNumber(ctx);
  const rows = [];
  for (const row of sendRows(n, pr)) {
    const act = {
      send: () => sendToGitHub(pr, { kind: "notes", ids: [n.id] }, "sending to #" + pr),
      "send-review": () => sendReviewBody(pr, n.group || "mine"),
      "reply-send": () => replyAndSend(n, pr),
      resolve: () => sendToGitHub(pr, { kind: n.resolved ? "unresolve" : "resolve", ids: [rootId] }, (n.resolved ? "reopening" : "resolving") + " a thread on #" + pr),
      "send-drafts": () => sendToGitHub(pr, { kind: "notes", ids: (n.replies || []).filter((r) => r.source !== "forge").map((r) => r.id) }, "sending replies to #" + pr),
    }[row.id];
    rows.push({ label: row.label, act });
  }
  return rows.length ? [{ sep: true }, ...rows] : [];
});

// sendReviewBody is Send review… for one group: its body (an AI review's
// summary prefilled, empty for my draft review), then the confirm, whose
// buttons are the verdicts. The typed body survives a refusal.
let keptBody = null; // {pr, group, text}: a body a refused send left
async function sendReviewBody(pr, group) {
  let g;
  try {
    const d = await getJSON("/api/pr/send/groups?n=" + pr);
    g = (d.groups || []).find((x) => x.id === group);
  } catch (e) {
    opLine("send review: " + (e.message || e), true);
    return;
  }
  if (!g) {
    opLine("send review: nothing of that group is left to send", true);
    return;
  }
  const kept = keptBody && keptBody.pr === pr && keptBody.group === group ? keptBody.text : null;
  const title = group === "mine" ? `Send my draft review (${g.count === 1 ? "1 note" : g.count + " notes"})` : `Send ${g.agent || "AI"} review: ${g.summary} (${g.count === 1 ? "1 remark" : g.count + " remarks"})`;
  openPrompt({
    title,
    value: kept !== null ? kept : g.body || "",
    multiline: true,
    allowEmpty: true,
    onSubmit: (text) => {
      keptBody = { pr, group, text };
      sendToGitHub(pr, { kind: "group", group, body: text, body_set: group !== "mine" }, "sending the review to #" + pr, null);
    },
  });
}

function verdictBody(pr) {
  openPrompt({
    title: "Verdict on #" + pr + " — the review body (optional)",
    value: "",
    multiline: true,
    allowEmpty: true,
    onSubmit: (text) => sendToGitHub(pr, { kind: "verdict", body: text }, "posting a verdict on #" + pr),
  });
}

// Send review… picks a group first when there is more than one.
async function sendReviewPick(pr, x, y) {
  let d;
  try {
    d = await getJSON("/api/pr/send/groups?n=" + pr);
  } catch (e) {
    opLine("send review: " + (e.message || e), true);
    return;
  }
  const gs = d.groups || [];
  if (!gs.length) {
    opLine("#" + pr + " has no local notes to send", false);
    return;
  }
  if (gs.length === 1) return sendReviewBody(pr, gs[0].id);
  showCtxMenu(
    gs.map((g) => ({
      label: g.id === "mine" ? `my draft review · ${g.count === 1 ? "1 note" : g.count + " notes"}` : `${g.agent}: ${g.summary} · ${g.count === 1 ? "1 remark" : g.count + " remarks"}`,
      act: () => sendReviewBody(pr, g.id),
    })),
    x,
    y
  );
}

registerRows("pr", (pr) => {
  if (!pr || pr.state !== "open" || !pr.fetched) return [];
  return [
    { sep: true },
    { label: "Send review…", act: (e) => sendReviewPick(pr.number, (e && e.clientX) || 200, (e && e.clientY) || 200) },
    { label: "Verdict…", act: () => verdictBody(pr.number) },
  ];
});

// A send that went through clears the kept body; a refused one keeps it.
onSendDone((n, ev) => {
  if (ev.ok) keptBody = null;
});

registerHelp({
  key: "send to GitHub",
  html:
    "in a pull request's own diff each note says where it lives — <b>○</b> local, <b>◌</b> sending, " +
    "<b>○!</b> the last send failed (the error under it), <b>●</b> on GitHub — and its left border is its " +
    "group's colour (one per AI review, one for your own notes). <b>Right-click</b> a note: Send to GitHub, " +
    "Send my draft review… / Send this AI review…, and on a GitHub thread Reply &amp; send…, Resolve / Reopen on " +
    "GitHub, Send draft replies. The pull request's right-click menu has <b>Send review…</b> and <b>Verdict…</b>. " +
    "Every send shows what will be posted and waits for your answer; agents never send",
});
```

(If `showCtxMenu` rows' `act` receive no event, the group pick opens at the last pointer position: keep `lastXY` from the triggering `contextmenu` in a module variable — **Ruling at execution** on the real signature.) `menus.js` `MENUS` gains `"note", "pr"`. `files.js` note menu: before `noteRows.push(foldRow);` add `noteRows.push(...extraRows("note", { n, rootId, ctx: noteSlotCtx(n) || state.diffCtx }));` (import `extraRows`). `app.js`: `import "./prsend.js";` beside `./prdetails.js`.

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Browser probe, then commit**

Against the unfixed build then this one (fake-gh scratch repo, Task 10's script): right-click the note on `pr7.txt` line 1 → the menu shows a VISIBLE row "Send to GitHub"; click it → the modal is visible with "Send to", the note's summary and the buttons Send / Cancel; click Cancel → no write in `$GG_FAKEGH_DIR/writes.jsonl`; repeat and click Send → `writes.jsonl` holds `addPullRequestReview` … `submitPullRequestReview`, and the box's mark becomes ● (or the note leaves, as GitHub's thread replaces it). Old build: no such row (fail), new: pass.

```bash
gg add internal/web/static internal/web/*_test.go
git commit -F <msgfile>   # "feat(web): send, send review, verdict, reply & send and resolve from the page"
```

---

### Task 9: the "updated" freshness word and the interrupted-send bar

**Files:**
- Create: `internal/web/static/prfresh.js` (pure)
- Modify: `internal/web/prs.go` (`handlePRRevalidate`: `interrupted`), `internal/web/prnotes.go` (`handlePRCommentsRefresh`: `interrupted`)
- Modify: `internal/web/static/prs.js` (freshness through `nextFresh`; the bar; `onSendDone` hook; help text)
- Test: `internal/web/prfreshjs_test.go` (new), `internal/web/prsend_test.go` (append)

**Interfaces:**
- Produces: `export function nextFresh(st, ev)` → `{seen, updated, ownSend, text}` where `st = {seen: n|0, updated: n|0, ownSend: bool}`, `ev = {n, kind: "start"|"ok"|"fail"|"sent", changed: bool, age: string}`; refresh answers gain `"interrupted": {"count": k, "joined": bool}` when `PRInterrupted` names a pending review.

- [ ] **Step 1: Write the failing tests**

`prfreshjs_test.go` runner:

```js
import { nextFresh } from "./prfresh.mjs";
let st = { seen: 0, updated: 0, ownSend: false };
const steps = [];
const step = (ev) => { st = nextFresh(st, ev); steps.push(st.text); };
step({ n: 7, kind: "start" });                 // refreshing…
step({ n: 7, kind: "ok", changed: true });      // first read of #7: fills the view, no "updated"
step({ n: 7, kind: "ok", changed: true });      // updated
step({ n: 7, kind: "ok", changed: false });     // clears
step({ n: 7, kind: "sent" });                   // own send: the next change is ours
step({ n: 7, kind: "ok", changed: true });      // no "updated" after your own send
step({ n: 7, kind: "ok", changed: true });      // someone else's: updated
step({ n: 7, kind: "fail", age: "3h" });        // offline · read 3h
step({ n: 8, kind: "ok", changed: true });      // another PR's first read
console.log(JSON.stringify(steps));
```

Want: `["refreshing…", "", "updated", "", "", "", "updated", "offline · read 3h", ""]`.

Append to `prsend_test.go`:

```go
// Serial: sendServer.
func TestRefreshAnswersCarryAnInterruptedSend(t *testing.T) {
	ts, wf, head := sendServer(t)
	id := addWebNote(t, ts, head, "pr7.txt", 1, "x")
	// The forge accepts the review, then gg "crashes": the note is stamped
	// with a pending review the snapshot still shows as the viewer's.
	stampPending(t, ts, id, "PRR_9") // test helper: domain stamp through the service (see below)
	wf.mu.Lock()
	wf.open[0].ViewerPendingReview = "PRR_9"
	wf.mu.Unlock()
	var out struct {
		Interrupted *struct {
			Count  int  `json:"count"`
			Joined bool `json:"joined"`
		} `json:"interrupted"`
	}
	if code, _ := postJSONAny(t, ts, "/api/pr/comments/refresh?n=7", `{}`); code != 200 {
		t.Fatal(code)
	}
	if code := postJSON(t, ts, "/api/pr/revalidate?n=7", `{}`, "application/json", "", &out); code != 200 || out.Interrupted == nil || out.Interrupted.Count != 1 {
		t.Fatalf("= %d %+v", code, out.Interrupted)
	}
}
```

(`stampPending`: reach the server's `*domain.Service` (`prServe` returns `*Server`; thread it through `sendServer`) and call the ledger the way `internal/domain/forge_send_recovery_test.go` stamps a note — through an exported test seam if none exists: `domain.StampForTest` is NOT added; instead run a send whose `SubmitReview` fails AND whose `DeletePendingReview` fails (the writer forge gets `failSubmit`, `failDelete` switches), which leaves exactly that state — **Ruling at execution**: use whichever the domain recovery tests already use.)

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/web/ -run 'PRFreshJS|InterruptedSend' 2>&1 | tail`
Expected: FAIL.

- [ ] **Step 3: Implement**

`prfresh.js`:

```js
// prfresh.js — the open PR's freshness word (spec 2026-10-07 §2.4), the
// TUI's prSeen/prUpdated rules: "refreshing…" while the forge is asked;
// "updated" when a refresh found new comments or commits — never on a PR's
// first read (that read fills the view) and never for the change your own
// send made; "offline · read <age>" when the forge could not be reached; the
// word clears once a refresh finds nothing new. Pure: prfreshjs_test.go.
export function nextFresh(st, ev) {
  const s = { ...st };
  switch (ev.kind) {
    case "start":
      return { ...s, text: "refreshing\u2026" };
    case "fail":
      return { ...s, text: ev.age ? "offline \u00b7 read " + ev.age : "offline" };
    case "sent":
      return { ...s, ownSend: true, updated: 0, text: "" };
  }
  if (s.seen !== ev.n) {
    return { seen: ev.n, updated: 0, ownSend: false, text: "" };
  }
  if (ev.changed && !s.ownSend) return { ...s, updated: ev.n, text: "updated" };
  if (ev.changed) return { ...s, ownSend: false, updated: 0, text: "" };
  return { ...s, updated: 0, text: "" };
}
```

`prs.js`: a module `let fresh = { seen: 0, updated: 0, ownSend: false };` and `function freshEvent(n, ev) { fresh = nextFresh(fresh, { n, ...ev }); setPRFresh(n, fresh.text); }`; `revalidate` → `freshEvent(n, {kind: "start"})` before, `freshEvent(n, {kind: "ok", changed: !!(rv.comments_changed || headMoved(n, rv))})` after, `offlineFresh` → `freshEvent(n, {kind: "fail", age})`; `refreshPRComments` the same; `onSendDone((n) => freshEvent(n, { kind: "sent" }))` plus a `refreshPRComments(n)` so the interrupted bar re-reads. The interrupted bar (mounted here, styles inline like notifications.js):

```js
// The interrupted-send bar (W6): a send gg started that GitHub still holds
// as a pending review — finish it, or discard it (never the user's own).
const ibar = document.createElement("div");
ibar.id = "pr-interrupted";
ibar.className = "hidden";
$("compare-bar").after(ibar);
function showInterrupted(n, info) {
  const po = state.previewOpen;
  if (!info || !po || po.pr !== n) {
    ibar.classList.add("hidden");
    return;
  }
  ibar.innerHTML =
    `<span>An earlier send to #${n} was interrupted: ${info.count === 1 ? "1 item waits" : info.count + " items wait"} in a pending review on GitHub.</span>` +
    `<button data-k="finish">Finish sending</button>` + (info.joined ? "" : `<button data-k="discard">Discard</button>`);
  ibar.classList.remove("hidden");
}
ibar.addEventListener("click", (e) => {
  const b = e.target.closest("button");
  const po = state.previewOpen;
  if (!b || !po || !po.pr) return;
  sendToGitHub(po.pr, { kind: b.dataset.k }, (b.dataset.k === "finish" ? "finishing" : "discarding") + " the interrupted send on #" + po.pr);
});
```

called with `rv.interrupted` / `r.interrupted` after each refresh answer (and hidden on a PR change). CSS: `#pr-interrupted { … } #pr-interrupted.hidden { display: none; }` in the module's own `<style>`. Server: in `handlePRRevalidate` and `handlePRCommentsRefresh`, after the refresh:

```go
	if rev, keys, joined := svc.PRInterrupted(ctx, n); rev != "" {
		body["interrupted"] = map[string]any{"count": len(keys), "joined": joined}
	}
```

(build the answer as a `map[string]any` named `body` first in both handlers.) prs.js's help text: "with a usable <b>gh</b> the sidebar lists the repository's open pull requests (read-only — gg never writes to the forge)" becomes "… open pull requests; gg writes to GitHub only when you send (see <i>send to GitHub</i>)".

- [ ] **Step 4: Run the tests**

Run: `go test ./internal/web/ 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Browser probe, then commit**

Probe (unfixed first): after a send from Task 8's probe the header's `#pr-fresh` is NOT "updated" (own send); seeding `snapshot-7.json` with a new thread from another author and pressing the header's ⟳ makes "updated" VISIBLE; seeding `fail-submitPullRequestReview` + `fail-deletePullRequestReview` and sending shows the interrupted bar VISIBLE with both buttons.

```bash
gg add internal/web
git commit -F <msgfile>   # "feat(web): the updated freshness word and the interrupted-send bar"
```

---

### Task 10: browser probe script, docs, skill, memory

**Files:**
- Create: the probe under the session scratchpad (never committed): `pr-send-probe/fixtures.sh`, `pr-send-probe/probe.mjs`
- Modify: `CHANGELOG.md`, `README.md`, `docs/CLAUDE-details.md` ("Sending to GitHub" — the queue section rewritten as "agents never send"; a "Sending to GitHub from gg web" section), `CLAUDE.md` (the `domain` row: drop "the pending-send queue"; the `cli` row: `gg pr` (read + send/reply/resolve/notes); the `web` row: one clause "PR sends via `/api/pr/send`"), `internal/agentskill/using-gg.md` (+ `agentskill.Version` 151), `.claude/skills/using-gg/SKILL.md` (rendered by `gg init --update` after install — regenerate with the repo's render step), memory `github-write-feature.md`
- Test: `internal/agentskill` tests (version / render), `./test.sh race`

- [ ] **Step 1: The probe script (written first, used by Tasks 6, 8, 9)**

`fixtures.sh <dir>` creates a scratch repo (main + a `feat` commit changing `big.go` line 5), points `refs/gg/pr/7` at it, and seeds `$GG_FAKEGH_DIR` with the TUI test's `prSendFixtures` set (copy `snapshot-7.json`, `snapshot-7-sent.json`, `pr-view-7.json`, `pr-list.json` = one open PR #7, `repo-view.json` from `internal/tui/pr_send_serial_test.go`'s strings), builds the fake gh (`go build -o "$dir/fakegh" ./internal/forge/testdata/fakegh`), and starts `GG_GH_BIN=$dir/fakegh GG_FAKEGH_DIR=… <bin> web --no-open` (read `gg web --help` for the real no-browser flag) in the scratch repo, printing the URL. `probe.mjs` (playwright, chromium headless) takes the URL and a step name (`marks`, `send`, `fresh`, `interrupted`) and asserts visibility as Tasks 6/8/9 describe, exiting non-zero on a failed assertion. Every run records `md5sum <bin>` and `curl -s -o /dev/null -w '%{http_code}' $URL/static/prsend.js` (404 on the old build, 200 on the new). Kill the server by PID.

- [ ] **Step 2: Docs and skill**

`using-gg.md`'s "Sending to GitHub — you QUEUE, the user approves." paragraph becomes:

```markdown
- **Sending to GitHub is the user's, never yours.** You cannot send anything to
  GitHub: inside any gg session `gg pr send`, `gg pr reply --send` and
  `gg pr resolve|unresolve` refuse ("agents can't send to GitHub"), and
  outside one they post only after a human answers the confirm at a terminal
  (there is no `--yes`). Write your findings as local notes (`gg note add`)
  or a stored review (`gg review save`), and draft replies to GitHub threads
  with `gg pr reply <n> <thread> <text>` (no `--send`: a local draft). Then
  tell the user what is ready; they send it from gg's PR view, gg web, or
  their own terminal. Your notes posted that way end with `— <agent> via gg`.
  Never run `gh` to post comments, reviews or resolves yourself.
```

Bump `agentskill.Version` to 151 and run its render test. CHANGELOG (new top entry): "Sending to GitHub from gg web" (marks, colours, menus, confirm, freshness, interrupted sends) and **Removed: agents can no longer queue sends to GitHub** (`gg pr pending`, the TUI's approval notices, `--yes`/`--event`; sends need a human answer; an emptied AI-review body is posted empty). README's GitHub section: the same in user words. Memory: plan 4 state, the rulings, deferred minors.

- [ ] **Step 3: Full gate**

Run: `./test.sh race > <workspace>/race.log 2>&1; tail -5 <workspace>/race.log`
Expected: the log says `all green`.

- [ ] **Step 4: Commit**

```bash
gg add CHANGELOG.md README.md CLAUDE.md docs/CLAUDE-details.md internal/agentskill .claude/skills/using-gg
git commit -F <msgfile>   # "docs: sending from gg web; agents never send (skill v151)"
```

- [ ] **Step 5: Merge (ASK THE USER FIRST)**

On the user's OK: `gg merge -F <msgfile> --into main feat/github-write-4` (conflicts: `--on-conflict keep`, stage the conflicted paths only), then `./build.sh install`, `./build.sh web`, `gg init --update`, remove the worktree and branch, check `main` is clean.
