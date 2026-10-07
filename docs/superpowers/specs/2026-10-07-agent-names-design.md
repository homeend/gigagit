# Agent names — design

Date: 2026-10-07 · Branch: `feat/agent-names`

## Problem

Start agent offers only a list of session commands (Claude, Claude (yolo),
Junie, Junie (yolo), …). Two Claude sessions in the same repository show the
same label everywhere — the Worktrees/Branches sub-rows, the console title,
the sessions popup, alt+a, the activity notices — so a "viewer" agent cannot
be told apart from a "worker" agent at a glance.

## Outcome (user's words, condensed)

- After picking an agent, an **optional** name prompt. Enter on an empty
  field changes nothing: the agent starts unnamed, exactly as today.
- A given name is displayed on the row that presents the running agent.
- The prompt **remembers** names: alt+↓ opens the same recall dropdown other
  search fields have, suggesting names used before.

## Rulings

| Question | Ruling |
|---|---|
| Surfaces | TUI **and** gg web (start dialog + display). No `name` input on `agent_start` / `gg agent start`. |
| Row format | Label first, name in brackets: `└ ● Claude (yolo) [viewer]  idle`. Unnamed rows read as today. |
| Name memory | Per repository: a new ring in the existing per-repo search-history store. |
| Rename later | Out of scope: the name is fixed at start. |

## Design

### 1. Model — a separate `Name`, one `Title()`

- `agentsession.StartSpec` and `agentsession.Info` gain `Name string`
  (user-given, `""` = unnamed). `Label` keeps meaning "which command runs"
  (`Claude (yolo)`).
- `agentsession.Info.Title()` is the one display string:
  `Label + " [" + Name + "]"` when named, `Label` otherwise. Every frontend
  display site uses it; no site concatenates the brackets itself.
- `domain.CleanAgentName(s string) string` normalises user input once, in
  domain, for both frontends: trim; control characters (incl. newlines)
  removed; at most 40 runes (cut, no ellipsis); whitespace-only → `""`.
- The name reaches `domain.Service.StartAgentSession` as a new field
  `SpawnRecord.Name` — the struct already describes how a session was
  started (`Parent == ""` = by the user) and every caller already passes one,
  so no signature changes. `StartAgentSession` cleans it and hands it down
  (`startSessionPrompt` → `startLine` → `StartSpec.Name`). Callers that do
  not name (MCP `agent_start`, task launches, tests) leave it empty.
- Terminals (`StartTerminal`) are never named.

Why not fold the name into `Label`: `Label` is also the tool identity —
`agent_list`'s `tool`, the task rows' `Label · key`, the session registry's
fallback agent name. A bracketed label would leak into all of them.

### 2. TUI flow (`internal/tui/agent_start_popup.go`)

- New stage `stageName` after choose/approve:
  choose → (approve, only for an unapproved command, as today) → **name** →
  start. With a single configured command the popup opens on the name stage
  (after approval when needed).
- The stage renders: title `Name this agent (optional)`, the chosen command
  (`Claude (yolo) in <worktree>`), a one-line text input, and below a blank
  line the hints `[enter] start  [alt+↓] recent names  [esc] back`.
- Keys:
  - enter → start with `CleanAgentName(input)`; a non-empty name is recorded
    via `recordSearch(scopeAgentName, name)` (dedup-to-top, size cap from
    `[ui] search_history_size`, persisted per repo).
  - esc → back to the choose stage when there are several commands, else
    closes the popup (mirrors the approve stage's esc).
  - alt+↓ / alt+↑ / enter / esc inside the open dropdown: `recallUpdate` with
    the new `scopeAgentName = "agentname"` ring — the shell-command prompt's
    exact mechanics. Enter on a recalled entry accepts it **and starts**
    (commit = start), as the shell prompt runs its recalled command.
  - ctrl+c quits, as in the other stages.
- The recall dropdown overlays via the existing `withRecall` path; the popup
  resets recall state on open/close (`recallReset`).
- `agentStartedMsg.name` (the status line "starting …" / "could not start …")
  uses the title, so the message names `Claude (yolo) [viewer]`.

### 3. TUI display

Every site that shows a session's `Label` to the user switches to `Title()`:

- Worktrees / Branches sub-rows (`sessionRowBody`): `└ ● Claude (yolo)
  [viewer]  idle`. When the row is too narrow, the existing row truncation
  applies; the plan checks it keeps the bracketed name visible (cut the
  label's middle before the name) and fixes it if not.
- Console title (`console.go` title triple), status lines (alt+a/alt+t
  cycle, kill, exited, removed, repo-scope "keeps running"), the sessions
  popup rows **and its filter** (typing `viewer` finds the session), the
  "is running in this worktree — kill it first" refusal, the steer-switch
  question, all-notes session names.
- Activity notices (`domain.ActivityNotice.Label`, built in
  `session_tracker.go` and `agentreport.go`) carry the title, so "needs your
  input / finished its turn / stalled / reports" name `[viewer]`.
- Agent tour titles (`domain/agenttour.go`) use the title.

### 4. gg web

- `/api/session-commands` additionally returns `names`: the
  `agentname` ring for the repo (newest first).
- `/api/session-start` accepts optional `name`; the server cleans it,
  starts the session with it and records a non-empty name in the ring.
- The Start agent dialog (`static/sessions.js`) gains a `name` phase between
  choose/approve and starting: an `<input>` with a `<datalist>` filled from
  `names` (Chrome opens a datalist on alt+↓ natively; typing filters it).
  Enter starts, Esc goes back (choose, or close with one command). While the
  name phase is up the dialog's `onKey` must let typing reach the input
  (today it `preventDefault`s every key). A start that comes back
  `needs_approval` goes to the approve phase and re-posts **with the typed
  name**. `dialogStep` stays pure and gets tests for the new phase.
- Session JSON (`sessions_http.go`): `label` becomes the title (rows and the
  console header show it with no JS change); a new `name` field carries the
  raw name. Activity notices already ride `Label` → the title.

### 5. Agent-facing / cross-process surfaces

- `domain.AgentEntry` (`agent_list`, `gg agent list`): `tool` and `label`
  stay the command label; new `name` (omitempty).
- `sessionreg.Entry` (machine-wide registry other gg processes read) gains
  `name` (omitempty); readers that display a foreign session use
  label + name the same way (`Title` logic, shared helper).
- No new `agent_start` input (ruled out).

## Error handling

- A name is never an error: bad input is cleaned, an over-long one cut.
- Recording the ring is fire-and-forget (as every search ring); a failed
  write loses only the suggestion.

## Testing

- `agentsession`: `Name` reaches `Info`; `Title()` named/unnamed.
- `domain`: `CleanAgentName` table (trim, control chars, newline, 40-rune
  cap on multibyte input, whitespace-only); `StartAgentSession` carries the
  name; `AgentList` / registry snapshot expose `name`.
- `tui`: stage transitions — choose→name→start, choose→approve→name,
  single command opens on name, esc from name (several / one command),
  empty enter starts unnamed, alt+↓ recall picks + starts and records;
  `sessionRowBody` named/unnamed; sessions-popup filter matches the name.
- `web`: `dialogStep` name-phase table (JS test harness already used for
  sessions.js); `/api/session-start` with `name` sets it and records it;
  `/api/session-commands` returns `names`.
- i18n: every new string in all four bundles (AST gates).
- e2e: a TUI golden screen of a named session row if the harness can start
  a fake session; otherwise the TUI unit tests cover the row text.

## Docs

`CHANGELOG.md`; `README.md` (Start agent paragraph); `using-gg.md` + skill
version bump only if the `agent list` output change is documented there;
`docs/CLAUDE-details.md` agent-sessions section (name vs label).

## Out of scope

Renaming a running session; naming terminals; a `name` on `agent_start`;
machine-wide name memory.
