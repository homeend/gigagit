# Agent spawn — an agent starts another agent through gg (orchestration stage 2)

Status: design agreed in brainstorming 2026-10-01, awaiting spec review.
Stage 1 (worktree inventory, claims, guards, session registry) is on `main`
(`47ca77c5`); its spec is `2026-09-30-worktree-inventory-design.md`.

## 1. Goal

An overseer agent running in a gg console starts a worker agent in a
worktree, hands it a free-form brief, and can list, read, type into and kill
the agents it started. The worker is an ordinary gg agent session — started
by the running TUI through the same path a user takes from the `.` menu — so
the user sees its sub-row, can open its console and take over at any time.

Out of scope here (stage 3): agent states (working / idle / needs input),
`report` (worker → parent) and `wait`. Stage 4: the task brief merged with
overview documents. Moving the existing `gg session` steering verbs off the
inbox is a later, separate stage.

## 2. Rulings (do not re-ask)

1. The running TUI owns spawned agents (no daemon, no detached process).
2. Spawn approval is a config allow-list; no per-spawn prompt.
3. Only sessions gg started take part.
4. The worker starts through the normal Start agent path: the same session
   commands (`[[tools.command]]` category `session`) and `StartSession`.
   No new tool category.
5. Brief delivery (Q1 = A): the free-form text never rides a command line.
   The session template's `<prompt>` slot carries a FIXED kick-off line;
   the worker fetches its brief with the `agent_task` tool.
6. Claim fate (Q2 = A): when the worker's session ends, its claim reverts to
   its parent if the parent is alive; otherwise it is swept as dead.
7. Reach (Q3 = A): `agent_send` / `agent_kill` reach only the caller's
   descendants; `agent_list` / `agent_screen` read any session of the TUI.
8. Limits (Q4 = A): `[agents] max_spawned` (default 4) caps live spawned
   sessions per TUI; a spawned worker may not spawn (no nesting).
9. Channels: the inbox FILES are discovery only (who is alive, where, the
   MCP endpoint) — no commands, replies or secrets. The MAIN channel is MCP
   served by the TUI process. Existing `gg session` verbs stay on the inbox
   for now.
10. MCP covers every agent verb; `gg agent …` CLI verbs are thin twins over
    the same MCP connection.
11. Spawned workers start in the background: a sub-row and a status line,
    never a focus change.

## 3. Architecture

```
 overseer agent ──stdio MCP──► gg mcp  (child of the agent; env GG_MCP_URL, GG_SESSION_TOKEN)
 `gg agent …` CLI ─────────────┐   │ agent_* tools, forwarded as an MCP client
                               ▼   ▼
              TUI process: agent MCP host (streamable HTTP, loopback, random port)
                 │ token → caller session (domain spawn registry)
                 ├─ list / screen / send / kill / task → domain.Sessions() + registry
                 └─ start → request/reply into the Bubble Tea loop
                            → normal Start agent path (StartSession, childEnv)
                            → domain claim handover, registry record, status line
 worker agent ──stdio MCP──► its own gg mcp ──► same host ──► agent_task → its brief

 discovery (read-only files): sessionreg/<proc>.json and the inbox tui.json
 gain "mcp": "<url>" — never a token.
```

### 3.1 Units

| Unit | Where | Responsibility |
|------|-------|----------------|
| Spawn registry | `internal/domain/agentspawn.go` | Process-global, mutex-guarded: `token → session id`, `session id → {Parent, Brief, Worktree, Spawned bool}`. Mints a token per started session (`crypto/rand`, 32 bytes hex). Answers `Descends(target, caller)`, `Parent(id)`, `LiveSpawned()`. Entries are removed when the session is removed from `Sessions()`. |
| Agent verbs | `internal/domain/agentverbs.go` | `AgentList`, `AgentScreen`, `AgentSend`, `AgentKill`, `AgentTask` over `Sessions()` + the registry, with the reach rule; `AgentStartCheck` (allow-list, prompt slot, cap, nesting, worktree resolution + guards) used by the TUI before it starts anything. |
| Claim handover | `internal/domain/wtclaim.go`, `internal/wtclaim` | `Claim.Parent` field; `HandOverWorktree(ctx, path, from, to, parent, note, pol)` under the claim lock; `liveClaim` rewrites a dead holder with a live `Parent` to that parent instead of sweeping. |
| Prompt slot | `internal/template` | `<prompt>` / `<prompt:FLAG>` command token: empty on a manual start, `[FLAG ]"<kick-off>"` when `CmdCtx.Prompt` is set (quoted per the existing per-token-kind rules). `template.HasPromptSlot(cmd)`. |
| Built-in session rows | `internal/exttool` | Claude/Codex `<bin> <prompt>`, Junie `<bin> <prompt:--prompt>`, Antigravity `<bin> <prompt:--prompt-interactive>` (yolo rows likewise, flag order kept); Kimi unchanged. Family `Version` bumped (golden guard). |
| Agent MCP host | `internal/mcp/agenthost.go` | `NewAgentHost(svc, starter)` → `Start()/URL()/Close()`. Streamable HTTP via `sdk.NewStreamableHTTPHandler(getServer, …)`; `getServer` reads `Authorization: Bearer <token>`, maps it through the registry, and returns a per-caller `*sdk.Server` whose tools close over the caller id; an unknown token → 401. Loopback listener, the web host's Host/Origin guards. |
| Agent tools | `internal/mcp/agenttools.go` | `agent_start`, `agent_list`, `agent_screen`, `agent_send`, `agent_kill`, `agent_task` — one registration function shared by the host (direct) and the stdio forwarder. |
| Stdio forwarder | `internal/mcp/server.go` | When `GG_MCP_URL` + `GG_SESSION_TOKEN` are set, `gg mcp` opens an MCP client (`sdk.StreamableClientTransport`, bearer header) to the TUI host and registers forwarding versions of the agent tools next to its own repo tools. Unset → the agent tools are absent; a host that cannot be reached → each agent tool returns an error naming the cause. |
| TUI seam | `internal/tui/agenthost.go` | `AgentHost` interface (`Start(starter) error`, `URL() string`, `Close()`) on `tui.RunOptions`, set by `cmd/gg` — the `WebHost` precedent; `tui` never imports `mcp`. The starter is a function the host calls for `agent_start`; it posts a request into the Update loop and waits on a reply channel (the `webhost.go` switch precedent), timeout 30 s. |
| TUI start handler | `internal/tui/agent_spawn.go` | In the loop: the console size, `childEnv()` + `GG_PARENT_SESSION` + `GG_MCP_URL` + `GG_SESSION_TOKEN`, `svc.StartSession` with `CmdCtx.Prompt`, `childInbox` bookkeeping, registry record, claim handover, status line. Every session the TUI starts (manual too) gets `GG_MCP_URL` + a token, so a manually started overseer can spawn. |
| Discovery field | `internal/sessionreg`, `internal/steer` | `Registry.MCP` and `Presence.MCP` (URL only). |
| CLI twins | `internal/cli/agent.go` | `gg agent start|list|screen|send|kill|task`, MCP clients through the same env. |
| Config | `internal/config` | Global-only `[agents] spawn` (list of session command names) and `max_spawned` (default 4, clamp 1..16); a repo-file value is ignored, as `reserved` ignores the global one. Settings registry entry. |
| Registration | `internal/agentinit` | `gg init` writes Claude Code's user-scope MCP entry for `gg mcp` (via `claude mcp add -s user gg -- gg mcp` when `claude` is on PATH); an existing `gg` entry is left alone and reported. Other agents: documented manual step. |

### 3.2 Environment every gg-started session gets

`GG_INBOX` (existing), `GG_SESSION_ID` (existing, `<ProcTag>/<id>`),
`GG_MCP_URL`, `GG_SESSION_TOKEN`; a spawned worker also gets
`GG_PARENT_SESSION` (display/diagnostic only — authority comes from the
registry, never from env the caller could change). `agentsession.childEnv`
strips inherited `GG_MCP_URL`, `GG_SESSION_TOKEN`, `GG_PARENT_SESSION` like
it strips `GG_SESSION_ID`.

The session id and token exist only once `Manager.Start` returns, but the
env is fixed at start: the TUI mints the token BEFORE `StartSession`, passes
it in env, and binds `token → id` right after `Start` returns. A tool call
racing that gap (impossible in practice — the agent has not booted) gets
401 and the client retries.

## 4. Tools

All values are protocol English. Errors are MCP tool errors (`IsError`)
with one-line reasons; the CLI twin prints the reason on stderr, exit 2 for
a refusal, 1 for a transport failure.

| Tool | Input | Result |
|------|-------|--------|
| `agent_start` | `worktree` (path or name), `tool` (session command name, case-insensitive), `prompt` (text, ≤ 256 KiB), `note` (optional, the claim note) | `{id, worktree, tool}` |
| `agent_list` | — | `[{id, parent, tool, label, worktree, state: running\|exited, exit_code, started, spawned, mine}]` (`mine` = a descendant of the caller) |
| `agent_screen` | `id` | `{id, state, text}` — the visible screen as plain text, trailing blanks trimmed |
| `agent_send` | `id`, `text` (optional), `enter` (default true), `keys` (optional list of key names from `domain.ConsoleKey`'s allowlist) | `{}` — text is pasted (bracketed when the child enabled it), then Enter, then the keys in order |
| `agent_kill` | `id`, `remove` (default false) | `{}` |
| `agent_task` | — | `{brief, parent, worktree}` — the caller's own brief; error "you were not started by an agent" for a manual session |

The server `instructions` text tells an agent the tools exist only inside a
gg console and that a worker's first act is `agent_task`.

### 4.1 `agent_start` order

1. Caller: the token's session must be live in this TUI.
2. Nesting: refuse when the registry says the caller was spawned.
3. Allow-list: `tool` must name a session command (frontend `tui`) whose
   name is in `[agents] spawn`; empty list → "spawning is off — add the
   command name to [agents] spawn in the global config".
4. Prompt slot: refuse a command without `<prompt>` ("<name> takes no
   initial prompt").
5. Cap: refuse when live spawned sessions ≥ `max_spawned`.
6. Worktree: resolve (path, or a worktree's directory name / branch), then
   the claim step: caller holds the claim → keep it (re-stamped in step 9);
   no claim → `ClaimWorktree` for the CALLER with the full guard set (a
   blocked worktree refuses with the guard reason); a foreign claim →
   refuse naming the holder. The caller's own session in that worktree does
   not block (it is the caller).
7. Start: in the TUI loop, `StartSession` with the kick-off prompt
   `You were started by gg as a worker agent. Call the gg MCP tool agent_task to read your task, then do it.`
   A start failure leaves the claim with the caller and returns the error.
8. Record: registry `{Parent: caller, Brief, Worktree, Spawned: true}`,
   bind the token.
9. Handover: `HandOverWorktree(path, from: caller, to: worker, parent:
   caller)` — the claim file now names the worker and `Parent = caller`.
10. Status line "<tool> started in <worktree> by <caller label>"; no focus
    change; `reload worktrees` so the ⚑ mark shows the worker.

### 4.2 Claim revert

`liveClaim` (stage 1's locked sweep) gains one branch: a claim whose holder
is dead and whose `Parent` is a live session is rewritten to
`{Session: Parent, Parent: "", Since: now}` instead of removed. A dead
parent too → removed as before. A foreign-host claim is never judged dead
(unchanged). The revert is lazy (on the next inventory/claim read), so it
needs no exit hook and cannot race a sweep.

## 5. Reach rule

`Descends(target, caller)` walks the registry's `Parent` chain from target;
true when it meets caller. Depth is at most 1 under ruling 8, but the walk
is general. `agent_send` / `agent_kill` on a non-descendant → "s4 is not
an agent you started (started by s2)" / "(started by the user)".

## 6. Discovery

`sessionreg.Registry` and `steer.Presence` gain `MCP string` (the host URL,
"" when the host is not running). The host starts with the TUI (no config
switch) and closes in `Run`'s tail. Nothing reads the field in stage 2
except `gg agent list` run OUTSIDE gg, which prints the live TUIs and their
sessions from the registry files (read-only, no MCP call — it has no token).

## 7. Error handling

- Host fails to listen → the TUI starts without it, a status line says why,
  sessions get no `GG_MCP_URL`; agent tools then report "gg's agent channel
  is not running".
- `agent_start` loop request times out (30 s, TUI wedged) → tool error; a
  late start still records itself, so `agent_list` shows it.
- Repo switch (`reRoot`): sessions survive (process-global manager); the
  registry and host are process-global too, so nothing changes.
- TUI quit with live sessions: existing quit guard; host closes after.

## 8. Testing

- Domain: registry (mint/bind/Descends/remove-on-session-removal), verbs
  with real `sh`/`sleep` sessions (send text + enter appears on screen,
  kill, reach refusals), `AgentStartCheck` table (each refusal), claim
  handover + revert (dead worker + live parent → parent; both dead → swept;
  foreign host untouched).
- Template: `<prompt>` empty vs set, with and without a flag, quoting on
  linux and windows; `HasPromptSlot`.
- exttool: golden version-bump guard.
- MCP: tools over `sdk.NewInMemoryTransports`; one real loopback HTTP test
  for the token (valid → tools, missing/unknown → 401, Host guard); the
  stdio forwarder against an in-process host.
- TUI: the start handler through the request/reply seam with a fake host;
  status line, no focus change, env contains the four vars.
- CLI: `gg agent` twins against an in-process host; outside gg → exit 2.
- e2e: one scenario for `gg agent list` outside gg (registry files).
- Task 0 (throwaway spike, STOP and report): Claude's stdio `gg mcp` child
  inherits `GG_MCP_URL`/`GG_SESSION_TOKEN`; go-sdk v1.6.1 calls `getServer`
  once per MCP session and a later request with a different token in the
  same session is refused (or document how to enforce it).

## 9. Docs

CHANGELOG, README (Agents and worktrees), using-gg skill (agent tools +
`gg agent`, version bump), CLAUDE-details section, CLAUDE.md map rows only
if a package's responsibility line changes (`mcp`: also hosts the agent
channel), settings registry entries.
