# Agent orchestration stage 3b — `agent_wait` and the report channel

Status: design agreed in brainstorming 2026-10-03, awaiting spec review.
Builds on `2026-10-01-agent-spawn-design.md` (stage 2: spawn, the agent
channel, `agent_*` tools) and `2026-09-28-web-attach-design.md` §"Session
states (plan 3)" (stage 3a: `domain.SessionStates()`, activity per session).

## 1. Problem

A parent agent that started a worker (`agent_start`) can only poll
`agent_list` to learn that the worker's turn ended, and the worker has no
way to say *what* it did except by leaving it on its screen. Two things are
missing:

- **wait** — the parent blocks until something about a worker needs it
  (its turn ended, it asks a question, it exited, it reported);
- **report** — the worker hands a result sentence to whoever started it,
  and the human sees that a worker has spoken.

Out of scope (stage 4): the task brief merged with the report contract;
AI-task agents reporting through this channel (see §8).

## 2. Rulings (brainstorm 2026-10-03)

1. `agent_wait` is a **long-poll**: one call blocks until an event or a
   timeout; `id` optional (any direct child of the caller).
2. A report is **stored on the worker's session** and read by the parent's
   `agent_wait`, by `agent_list`/`agent_screen`, and by the human on the
   session row. Nothing is typed into any console.
3. A report **never ends the worker**; `final: true` only marks its last
   word. The parent kills it, as today.
4. The AI-task result viewer bridge is **deferred to stage 4** (§8).

## 3. `agent_wait`

### 3.1 Call

MCP `agent_wait {id?, until?, timeout_s?}` / CLI
`gg agent wait [<id>] [--until <what>] [--timeout <s>]`.

| field | meaning |
|---|---|
| `id` | a session id. Omitted: **any direct child** of the caller (its own workers — a grandchild's turn is its parent's business). Given: any descendant, the `agent_send` reach rule (spec 2 §5); another agent's session is refused with the same error. |
| `until` | `idle`, `question`, `exit`, `report` or `any` (default). `any` = any of the four. |
| `timeout_s` | 1 … 600, default **45**. The only cap on a long-poll is the MCP client's own tool timeout (the host has no write timeout, the SDK session idles out after an hour, the `agentlink` client has none), so the default stays under common client limits and agents loop. |

### 3.2 When it returns

The wait is **level-triggered and consumed once**. It is not built on the
notice ring: notices are a human channel (held for the 30 s grace, no
`exit` kind, exited sessions dropped), and a worker finishing inside grace
posts nothing. Instead the wait evaluates a predicate over the watcher's
level state (`StateWatcher.Get`), the session lifecycle (`Info().State`,
`Done()`) and the report store, re-evaluating on every wake from
`SessionStates().Subscribe()`, `Sessions().Subscribe()` and the report
store's own broadcaster. Nothing can be missed; nothing depends on grace.

An event is *deliverable* for (caller, worker) when all of these hold:

- **kind asked**: it is one of `until`.
- **freshness** (idle, question): the activity's `Since` postdates the
  worker's **last input** — `agentsession.Session.LastInput()`, the
  `LastOutput` twin, stamped by every input path (`SendKey`, `SendText`,
  `Paste`: the parent's `agent_send`, the human typing in a console, the web
  console alike); the session's start time while nothing was typed. So
  `agent_send` → `agent_wait until: idle` cannot return the idle the worker
  was already in before the send, and a human who answers a worker's
  question in its console makes that question stale for the parent too.
- **settle** (idle only): the idle has held for ≥ 2 s (`idleSettle`,
  behind the `UseStateTiming` seam for tests). Guards the known
  working→idle→working flicker a level-triggered wait would otherwise fire
  on. `question`, `exit`, `report` are immediate.
- **unread**: not yet delivered to this caller. Delivery is recorded per
  (caller, worker) as `{kind, mark}` where `mark` is the activity's `Since`
  (idle/question), the report's `seq` (report) or a flag (exit). A worker
  that is idle stays idle, but the same `(idle, Since)` pair is never
  returned twice to the same caller — "wait = what is new;
  `agent_list` / `agent_screen` = what is". Marks live in the spawn
  registry beside the records and are pruned with them.

Several deliverable events on one wake (or across workers for an `any`
wait): **report before exit before question before idle**; among workers,
list order (start order). One event per return; the parent calls again.

**Timeout** is not an error: `{timed_out: true, id?, activity, since,
stalled}` (the fields of the named worker; absent for an `any` wait). The
parent simply calls again — the skill says so.

**Immediate refusals** (errors): no such session / not yours; `until`
unknown; `timeout_s` out of range; an `any` wait by a caller with **no
children at all** ("you have no workers") — waiting on nothing would only
burn the timeout.

A worker that has **exited** still answers `agent_wait {id}`: its unread
reports first (one per call), then `exit` once, then timeouts. A worker that was **removed** is "no session".

### 3.3 Result

```json
{"id": "…", "event": "idle|question|exit|report",
 "activity": "working|idle|question|", "since": "…", "stalled": false,
 "options": [{"key":"1","label":"Yes"}],         // question only
 "exit_code": 0,                                   // exit only
 "report": {"seq": 3, "text": "…", "final": true, "at": "…"}}  // report only
```

CLI prints the `gg agent screen` style lines (`event: report`, `activity:
idle`, `report: …`, the options on one line) and exits **0** on an event,
**3** on a timeout (scripts loop on 3), **1** on an error, **2** without a
channel (as every `gg agent` verb).

### 3.4 Where it runs

`domain.AgentWait(ctx, caller, id, until, timeout) (AgentWaitResult, error)`
— entirely in `domain`, never through the TUI's Update loop (the 30 s
`AgentHost` starter seam is for `agent_start` only). It returns when the
ctx ends (a client that hangs up leaks no waiter), on the timeout, or on
the first deliverable event. Multiple concurrent waits by one caller are
allowed; each delivery is recorded once, so two `any` waits on the same
parent split the events between them.

Known edges, documented not solved:
- two dialogs back to back with no working stretch between them keep one
  `Since`; the second is visible only through the timeout result's
  `activity`/`options` (or `agent_screen`);
- a worker that never changes its screen after a send (it ignored the
  input) is reported by the timeout, with the old `since`.

### 3.5 Last input

`Session.LastInput()` is new in `agentsession` (leaf): an atomic UnixNano
set by `SendKey`/`SendText`/`Paste`, zero before the first input. It is
exposed nowhere on the wire; it feeds the wait's freshness (§3.2) and the
report badge (§5.1). `agent_list` entries gain nothing for the wait itself.

## 4. `agent_report`

### 4.1 Call

MCP `agent_report {text, final?}` / CLI `gg agent report [--final]
(<text…> | -F <file> | -F -)`. The caller reports **about itself** — there is
no target. `text` is required, non-blank, ≤ 64 KiB (`MaxSendBytes`;
`MaxReportBytes` aliases it). Any session with a token may report, parent
or not — a lone agent started by the human can say "done" too.

### 4.2 Storage

In the spawn registry (`agentspawn.go`), keyed by the reporter's full id:
`[]AgentReport{Seq uint64, Text string, Final bool, At time.Time}`, newest
last, capped at **20** (the oldest drops; `Seq` is process-global and
monotonic so delivery marks stay valid across drops). Reports live as long
as the session is **listed**: pruned with the records when the session is
removed, not when it exits. A worker reports only while running (only a
running session holds a working token), so "final report, then exit" is
the normal ending and the parent's wait delivers the report first.

`final` marks the worker's last word. A later non-final report is still
accepted (it is just a message); a later `final` replaces the badge. Nothing
ends the worker.

A report wakes: every waiter (the report store's broadcaster) and the
human channel (§5).

### 4.3 Level reads

- `AgentEntry` (`agent_list`, `gg agent list`): `report_at` (time of the
  latest report, omitzero), `report_final` (bool, omitempty).
- `AgentScreenResult` (`agent_screen`, `gg agent screen`): `report` — the
  latest report `{seq, text, final, at}`, omitted when none.
- `domain.AgentReports(caller, id) ([]AgentReport, error)` for frontends
  (reach rule as `agent_screen`); the CLI exposes it as
  `gg agent screen --reports` (all kept reports, oldest first).

## 5. What the human sees

### 5.1 Row badge

A report **replaces the activity word** on every session row (TUI
worktree/branch sub-rows, the ctrl+\ popup, the console title; web sidebar
sub-rows, the switcher, the console title) **until someone answers it** —
the next input to that session (`LastInput` after the report's `At`), from
the parent's `agent_send` or the human's keystrokes — or until a newer
report replaces it:

| condition | badge |
|---|---|
| latest report final, unanswered | `done 2m` (age of the report) |
| latest report non-final, unanswered | `reported 2m` |
| answered (input after the report), or no report | the stage-3a badge (`working …` / `idle …` / `needs input` / `stalled`) |

Precedence: `needs input` (question) **beats** a report badge (the human
must act); a report badge beats `stalled` and `idle`/`working`. Both report
badges wear the attention colour (`--act-attn` / `activityAttn`). Final
versus non-final changes only the word: a `done` worker the human asks a
follow-up of is working again, and its row says so.

Rule in one sentence: a report is news until somebody talks to the worker.
(Level state cannot tell "idle after reporting" from "idle after the next
turn", but it can tell "someone answered" — hence this rule rather than
"until working again".)

Implementation: one rule in `domain` — `SessionReportOf(id) (AgentReport,
bool)` returns the latest report only while it is unanswered; the frontends
add the question precedence. The TUI reads it next to
`SessionActivityOf`; the web wire carries `report_at`, `report_final`,
`report_line` (first line, ≤ 120 runes) only while the badge shows.

### 5.2 Notice

One notice per report, kind `report`, posted at once (no grace — an
explicit report is trusted): TUI status line / web toast
`<label> · <worktree> reports: <first line>` elided to the bar/toast width;
the focused console's own session is exempt as for the other kinds. It
rides the existing ring: `ActivityNotice.Kind = "report"` plus a new
`Text string` field (first line); `PostNotice` is the entry (the watcher
is not involved — the report store posts).

The web `liveMsg.Notices` wire gains `text`; `noticeText(n)` (core.js) and
`activityNoticeText` (TUI) get the new case; the sentence goes to all four
bundles.

### 5.3 Reading the text

The ctrl+\ popup and the web switcher show the unanswered report's first
line on the row, after the badge (`done 2m — merged feat/x`; the rows are
wide, and neither list has sub-lines). The full text is an agent surface
(`agent_screen`, `gg agent screen --reports`, `gg agent list --json`); no
new viewer — a report is a sentence for the parent, not a document.

## 6. Protocol for agents (skill `using-gg.md` v131)

- **Worker**: `agent_task` first; do the task; `agent_report {text,
  final: true}` (a short result: what changed, what was skipped, what the
  parent must do); then **stay alive** until killed — do not exit. Report
  non-final progress sparingly (one per milestone, not per step).
- **Parent**: `agent_wait` (optionally `until: report`); on `report` read
  it; on `question` read `options` from the result and answer with
  `agent_send`; on `idle` without a report the worker's turn ended without
  reporting — read `agent_screen`; on `timed_out` call again; `agent_kill
  {remove: true}` when done. Keep `timeout_s` under your client's tool
  timeout (45 s default).
- Replaces the stage-2 sentence "Poll `agent_list` to wait for a worker".

`gg init --update` syncs `.claude/skills/using-gg/SKILL.md`.

## 7. Components

| part | where | change |
|---|---|---|
| Last input | `internal/agentsession/io.go`, `session.go` | `lastIn` atomic + `LastInput()` |
| Report store + delivery marks | `internal/domain/agentspawn.go` (registry) + new `agentreport.go` | `AgentReport`, `addReport`, `reportsOf`, `markDelivered`/`delivered`; a `Broadcaster` for waiters |
| Wait | new `internal/domain/agentwait.go` | `AgentWait`, `AgentWaitResult`, `waitUntil` parse, `idleSettle` under `UseStateTiming` |
| Verbs | `internal/domain/agentverbs.go` | `AgentReportVerb(caller, text, final)`, `AgentReports`, `AgentEntry`/`AgentScreenResult` fields, `SessionReportOf` |
| Notice | `internal/domain/session_states.go` | `ActivityNotice.Text`, kind `report` posted by the store |
| MCP | `internal/mcp/agenttools.go` (+ forwarder in `server.go`) | `agent_wait`, `agent_report`; new out fields |
| CLI | `internal/cli/agent.go` | `wait`, `report`, `screen --reports`, list columns |
| TUI | `internal/tui/session_activity.go`, `sessions_popup.go`, `console.go`, `worktree_sessions.go` | badge helper reads the report; popup first line; notice text; i18n ×4 |
| Web | `sessions_http.go`, `live.go`, static `core.js`/`sidebar.js`/`console.js`/`live.js`, `style.css` | wire fields, badge helper, switcher line, toast |
| Skill + docs | `agentskill/using-gg.md` v131, CHANGELOG, README, CLAUDE-details | |

`archtest`: nothing new crosses a boundary (`agentstate` untouched; the
report store is `domain`-only; `agentsession` stays a leaf).

## 8. Deferred (explicit)

- **AI-task bridge**: interactive AI tasks start through `startLine`, so
  their agent has no `GG_SESSION_TOKEN` and cannot report. Stage 4 threads
  the channel URL into `TaskSpec`, starts through `StartAgentSession` (no
  parent) and lets `runInteractive` take a report as a result beside the
  `$GG_MESSAGE_FILE` write.
- A parent that **restarts** (new session id) loses its delivery marks — a
  new caller sees every pending event once more. Acceptable: marks are per
  caller by design.
- Hysteresis in the classifier itself (3a deferred item) — the settle rule
  here is the wait's own guard, not a fix.

## 9. Testing

- `agentsession`: `LastInput` zero before input, set by key/text/paste.
- `domain`: fake-fed watcher (`NewStaticStates` + a mutable source) —
  freshness (idle before input not delivered; idle after input delivered),
  settle (idle < 2 s not delivered), once-only per caller, two callers each
  get the event, order report > exit > question > idle, `any` across two
  children (grandchild excluded), exited worker answers exit then reports,
  removed worker = error, ctx cancel returns promptly, timeout shape, no
  children refusal, report cap 20 keeps seqs valid, prune on removal.
- `mcp`: tool registration + forwarder round trip for both tools (the
  stage-2 harness).
- `cli`: exit codes 0/3/1, `-F -` stdin, `--reports`.
- `tui`: badge precedence (unanswered report, answered report, question
  wins) as a pure test, popup line, notice text, i18n gates.
- `web`: wire fields, pure-section JS tests for the badge/notice, and the
  browser check (`TestAttachBrowserHost` + `checkstates.mjs`) gains a
  reported row — asserted visible, seen failing on the unfixed build.
- Live: one real worker (Claude) reporting to a real parent inside a gg
  console, screen read before every key.
