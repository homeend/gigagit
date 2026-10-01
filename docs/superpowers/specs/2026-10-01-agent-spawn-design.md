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
| Spawn registry | `internal/domain/agentspawn.go` | Process-global, mutex-guarded: `token → session id`, `session id → {Parent, Brief, Worktree, Spawned bool}`. Mints a token per started AGENT session (`crypto/rand`, 32 bytes hex). Answers `Descends(target, caller)`, `Parent(id)`, `LiveSpawned()`, `Verify(token) (session id, ok)` (false for an unbound token and for a session no longer RUNNING — a killed worker's token stops working at once). Entries of sessions no longer listed are pruned lazily on every registry read (the Manager has no removal hook; no goroutine to own). |
| Agent verbs | `internal/domain/agentverbs.go` | `AgentList`, `AgentScreen`, `AgentSend`, `AgentKill`, `AgentTask` over `Sessions()` + the registry, with the reach rule; `AgentStartCheck` (allow-list, prompt slot, cap, nesting, worktree resolution + guards) used by the TUI before it starts anything. |
| Claim handover | `internal/domain/wtclaim.go`, `internal/wtclaim` | `Claim.Parent` field; `HandOverWorktree(ctx, path, from, to, parent, note, pol)` under the claim lock; `liveClaim` rewrites a dead holder with a live `Parent` to that parent instead of sweeping. |
| Prompt slot | `internal/template` | `<prompt>` / `<prompt:FLAG>` command token (added to `commandTokens`, so `ValidateCommandTokens` accepts it): empty on a manual start, `[FLAG ]"<kick-off>"` when `CmdCtx.Prompt` is set. `CmdCtx.Prompt` is only ever the domain constant `AgentKickoff` (never caller text), so the plain `quoteArgFor` quoting is safe on cmd.exe too; a test pins that the constant holds no `"`, `%`, `!`, `^` or `&`. `template.HasPromptSlot(cmd)`. |
| Built-in session rows | `internal/exttool` | Claude/Codex `<bin> <prompt>`, Junie `<bin> <prompt:--prompt>`, Antigravity `<bin> <prompt:--prompt-interactive>` (yolo rows likewise, flag order kept); Kimi unchanged. Family `Version` bumped (golden guard). |
| Session-row upgrade | `internal/exttool`, `internal/domain` | No new migration: the family `Version` bump makes the existing template-update flow (`domain.ToolTemplateStatuses` → `ToolUpdateAvailable`, offered in Settings → External tools) offer the `<prompt>` command to every config holding an old stamped OR unstamped built-in session block. A block without the slot is refused by `agent_start` with "<name> has no <prompt> slot — accept its template update in Settings → External tools, or add <prompt> to its command". An updated command's text changes, so its one-time approval is asked again on the next manual start. |
| Agent MCP host | `internal/mcp/agenthost.go` | `NewAgentHost(starter)` → `Start()/URL()/Close()`. ONE `*sdk.Server` behind `sdk.NewStreamableHTTPHandler`, wrapped in go-sdk's `auth.RequireBearerToken` with a verifier that calls the domain registry's `Verify` on EVERY request and returns `TokenInfo{UserID: <session id>, Expiration: now+24h}` (the SDK rejects a zero expiration); a missing/unknown/dead token → 401. Tool handlers read the caller from `req.Extra.TokenInfo.UserID`; the SDK's own hijack check pins an MCP session to that user id. Loopback listener (`127.0.0.1:0`); the SDK's automatic loopback Host check plus an explicit Origin refusal (any `Origin` header that is not absent → 403) — the bearer token is the real guard. Reaches sessions only through domain (archtest: `mcp` never imports `agentsession`/`sessionreg`/`wtclaim`). |
| Agent tools | `internal/mcp/agenttools.go` | `agent_start`, `agent_list`, `agent_screen`, `agent_send`, `agent_kill`, `agent_task` — one registration function shared by the host (direct) and the stdio forwarder. |
| Stdio forwarder | `internal/mcp/server.go` | When `GG_MCP_URL` + `GG_SESSION_TOKEN` are set, `gg mcp` registers forwarding versions of the agent tools next to its own repo tools and connects LAZILY on the first agent-tool call (`sdk.StreamableClientTransport` with an `HTTPClient` whose transport adds the bearer header), reconnecting once on a failed call — a 401 on `initialize` fails `Connect`, so an eager connect at startup would fail the whole server. Unset → the agent tools are absent; an unreachable host → each agent tool returns an error naming the cause. Agent tools work even when `gg mcp` runs outside a repo (only the repo tools report the repo error). |
| TUI seam | `internal/tui/agenthost.go` | `AgentHost` interface (`Start(starter) error`, `URL() string`, `Close()`) on `tui.RunOptions`, set by `cmd/gg` — the `WebHost` precedent; `tui` never imports `mcp`. The starter is a function the host calls for `agent_start`; it posts a request into the Update loop and waits on a reply channel (the `webhost.go` switch precedent), timeout 30 s. |
| TUI start handler | `internal/tui/agent_spawn.go` | In the loop: the cap and nesting checks (re-run here so two racing overseers cannot both pass), the console size, `childEnv()` + `GG_PARENT_SESSION` + `GG_MCP_URL` + `GG_SESSION_TOKEN`, `StartSession` on the CALLER's repo service with `CmdCtx.Prompt`, `childInbox` bookkeeping, registry record, claim handover, status line. Every AGENT session the TUI starts (manual too) gets `GG_MCP_URL` + a token, so a manually started overseer can spawn; an Open-terminal shell gets neither. |
| Discovery field | `internal/sessionreg`, `internal/steer` | `Registry.MCP` and `Presence.MCP` (URL only). |
| CLI twins | `internal/cli/agent.go` | `gg agent start|list|screen|send|kill|task`, MCP clients through the same env (the forwarder's client code, shared). Outside gg, `gg agent list` reads the registry files through a domain query (`cli` never imports `sessionreg`). |
| Config | `internal/config` | Global-only `[agents] spawn` (list of session command names) and `max_spawned` (default 4, clamp 1..16); a repo-file value is ignored, as `reserved` ignores the global one. Settings registry entry. |
| Registration | `internal/agentinit` | `gg init` writes Claude Code's user-scope MCP entry for `gg mcp` (via `claude mcp add -s user gg -- <gg>` where `<gg>` is the running binary's absolute path, and `claude` is found with `exec.LookPath`, which resolves the `.cmd` shim on Windows); an existing `gg` entry is left alone and reported. Other agents: documented manual step. |

### 3.2 Environment every gg-started session gets

`GG_INBOX` (existing), `GG_SESSION_ID` (existing, `<ProcTag>/<id>`),
and — agent sessions only, never an Open-terminal shell —
`GG_MCP_URL`, `GG_SESSION_TOKEN`; a spawned worker also gets
`GG_PARENT_SESSION` (display/diagnostic only — authority comes from the
registry, never from env the caller could change). `agentsession.childEnv`
strips inherited `GG_MCP_URL`, `GG_SESSION_TOKEN`, `GG_PARENT_SESSION` like
it strips `GG_SESSION_ID`.

The session id and token exist only once `Manager.Start` returns, but the
env is fixed at start: the TUI mints the token BEFORE `StartSession`, passes
it in env, and binds `token → id` right after `Start` returns. A tool call
racing that gap (impossible in practice — the agent has not booted) gets
401; the forwarder's lazy connect makes the first real call well after it.

**Ambient authority.** The token is inherited by everything the agent runs
(its shells, the repo's build scripts). A holder can do what the agent can:
read any session's screen and list (ruling 7), read its own brief, and
start/send/kill within the agent's own reach. That is the same authority
the agent already has over the user's machine; the token adds no reach
beyond the TUI's sessions. It is never written to disk by gg.

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
   command name to [agents] spawn in the global config". The resolved
   block's command TEXT must also be approved in promptstate
   (`CommandHash`, the same approval the Start agent popup asks for once):
   a name-only allow-list would otherwise admit a block a cloned repo's
   `.gg.toml` redefined under that name. Unapproved → "approve <name> once
   by starting it from the TUI". The allow-list replaces the per-spawn
   prompt; it never bypasses the one-time approval of the command text.
4. Prompt slot: refuse a command without `<prompt>` ("<name> takes no
   initial prompt").
5. Cap: refuse when live spawned sessions ≥ `max_spawned`.
6. Worktree: resolve in the CALLER's repository — the host opens (and
   caches by git common dir) a domain service for the caller session's
   `Dir`, so a TUI that switched repos still resolves where the overseer
   works. A path, or a worktree's directory name or branch name; an
   ambiguous name refuses listing the candidates. Then
   the claim step: caller holds the claim → keep it (re-stamped in step 9);
   no claim → `ClaimWorktree` for the CALLER with the full guard set (a
   blocked worktree refuses with the guard reason); a foreign claim →
   refuse naming the holder. The caller's own session in that worktree does
   not block: `sessionGuard` gains the `CallerSession` exemption
   `claimedGuard` already has (stage-1 change: `gg worktree list --free`
   run by an agent now lists its own worktree when nothing else blocks).
7. Start: in the TUI loop, `StartSession` with the kick-off prompt
   `You were started by gg as a worker agent. Call the gg MCP tool agent_task to read your task, then do it.`
   A start failure returns the error; a claim step 6 CREATED is released,
   a claim the caller already held stays.
8. Record: registry `{Parent: caller, Brief, Worktree, Spawned: true}`,
   bind the token.
9. Handover: `HandOverWorktree(path, from: caller, to: worker, parent:
   caller)` — the claim file now names the worker and `Parent = caller`.
   A handover failure leaves the worker running and the claim with the
   caller (safe: nobody else can take it); the tool result carries a
   `warning` and the status line says so.
10. Status line "<tool> started in <worktree> by <caller label>"; no focus
    change; `reload worktrees` so the ⚑ mark shows the worker.

### 4.2 Claim revert

`liveClaim` (stage 1's locked sweep) gains one branch: a claim whose holder
is dead and whose `Parent` is a live session is rewritten to
`{Session: Parent, Agent: <the parent's agent name>, Parent: "", Since:
now}` instead of removed. A dead parent too → removed as before. A
foreign-host claim is never judged dead (unchanged). The revert is lazy (on
the next inventory/claim read) and idempotent under the lock (`sameClaim`).

**Young-claim grace.** The session registry is written asynchronously, so
another gg process could read a fresh worker claim before the registry
lists the worker and judge it dead. `claimDead` therefore treats a claim
younger than `sessionreg.LiveWindow` whose owning process is live as alive
(the stage-1 `emptyClaimGrace` precedent). A worker may still release its
own claim (`gg worktree release`); then nothing reverts — that is the
worker's choice.

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
- Repo switch (`reRoot`): sessions, the registry and the host survive
  (process-global). The host holds no `*domain.Service`: repo-bound work
  (worktree resolution, guards, claims, `StartSession`) runs on a service
  for the caller session's `Dir` (§4.1 step 6); `list/screen/send/kill/task`
  need no repo at all.
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
  inherits `GG_MCP_URL`/`GG_SESSION_TOKEN` from the agent's environment.
  (Token enforcement needs no spike: `auth.RequireBearerToken` verifies
  every request — pinned by the loopback HTTP test, including a killed
  session's token → 401.)

## 9. Plans

Two plans on the one branch, merged together after plan B:

- **Plan A — core:** domain registry + verbs + handover/revert/grace +
  `sessionGuard` exemption, template token + exttool rows + migration,
  config, agent MCP host + tools, TUI seam + start handler. Tested over
  in-memory transports and loopback HTTP.
- **Plan B — reach:** Task 0 spike first, then the stdio forwarder,
  `gg agent` CLI twins, discovery field + outside-gg `gg agent list`,
  `gg init` registration, docs.

## 10. Docs

CHANGELOG, README (Agents and worktrees), using-gg skill (agent tools +
`gg agent`, version bump), CLAUDE-details section, CLAUDE.md map rows only
if a package's responsibility line changes (`mcp`: also hosts the agent
channel), settings registry entries.
