# Worktree inventory for agents — design (orchestration stage 1)

Date: 2026-09-30 · Branch: `feat/worktree-inventory`

## Context

The user wants an overseer agent that is handed an issue URL, picks or
recycles a worktree, writes a brief, and spawns a worker agent there. The gap
analysis split that into four stages:

1. **Worktree inventory** (this spec) — an agent can tell which worktrees are
   free, take one atomically, and give it back.
2. `gg agent start/list/screen/send/kill` through the running TUI.
3. Agent states + `gg agent wait` + a report channel.
4. The task brief (merged with the agent overview documents idea).

**Ruling (process owner):** spawned agents are owned by the running TUI; the
CLI reaches it through the steer inbox. No detached processes, no daemon.

Today `gg worktree list` prints `branch<TAB>path` and nothing else;
`gg worktree recycle --on-dirty=…` already runs non-interactively; agent
sessions live only inside the TUI process (`domain.Sessions()`), invisible to
any other `gg` process.

## Goals

- An overseer running inside a gg agent session can list worktrees with the
  facts that matter (dirty, last change, sessions, TUI, locks) and a computed
  `free` verdict with reasons.
- It can claim a free worktree atomically and release it.
- The user can reserve worktrees against agents, and sees and can remove both
  reserves and claims in the TUI.
- A claim never outlives its owner session; no timeouts.

## Non-goals

- Spawning agents, agent states, reports (stages 2–3).
- The web frontend (a follow-up: sidebar markers + menu rows).
- Claims by agents NOT running inside gg — they are refused, by ruling: gg
  only synchronises the agents it manages.

## Rulings (user, 2026-09-30 — do not re-ask)

1. **Free** = clean with a branch checked out; or dirty but untouched for
   longer than a threshold (default 14 days; recycled with `--on-dirty=shelve`);
   and no agent session there, no TUI rooted there, not reserved, not claimed.
2. Reserve and claim are **two mechanisms**: the reserve mark lives in config,
   the claim is a runtime file.
3. A claim is bound to its owner **session**. The agent claims, the agent
   releases; the user can remove any claim; a claim whose session is no longer
   running is dead and released automatically. No TTL, no heartbeat.
4. Only agents running inside gg (`GG_SESSION_ID` set) can claim.
5. Session liveness across processes = a **session registry** each TUI
   publishes (approach 1 of 3).
6. The main checkout and detached worktrees are not free (main can be allowed
   by config).

## Design

### 1. Session identity: `GG_SESSION_ID`

`agentsession.Manager.Start` mints the session id; it now also appends
`GG_SESSION_ID=<proc>/<id>` to the child's environment, where `<proc>` =
`<pid>-<process start unixnano>` of the hosting gg process (computed once per
process). Every session start path goes through `Manager.Start`, so every
session gets it. `<proc>` is also the registry file's name (§3), so a session
id resolves to its registry without a scan.

### 2. `gg worktree list --json` / `--free`

The plain text output is unchanged. `--json` prints an array, one object per
worktree:

```json
{
  "path": "/work/gigagit-wt/feat-x", "branch": "feat/x", "head": "a1b2c3d",
  "main": false, "detached": false,
  "dirty": { "staged": 0, "unstaged": 3, "untracked": 1,
             "last_change": "2026-09-10T14:02:00Z" },
  "paused_op": "", "git_lock": false,
  "tui": false,
  "sessions": [ { "id": "4711-1727…/s3", "agent": "claude", "state": "running" } ],
  "reserved": false,
  "claim": { "session": "4711-1727…/s3", "agent": "claude",
             "since": "2026-09-30T20:00:00Z", "note": "https://…/issues/42" },
  "free": false,
  "blocked_by": ["session", "claimed"],
  "recycle": null
}
```

- `dirty` is `null` when `git status` was skipped (§ cost); `last_change` is
  the newest mtime among the dirty paths (deleted paths contribute nothing),
  `null` when clean.
- `claim` is `null` when there is no LIVE claim (dead claims are swept, §4).
- `recycle`: `"none"` = clean, `"shelve"` = stale-dirty, `null` = not free —
  the `--on-dirty` value the overseer passes to `gg worktree recycle`.
- `sessions` lists running AND exited sessions whose dir is this worktree;
  only `running` ones block.

`blocked_by` reasons (free ⇔ empty):

| Reason | When |
|---|---|
| `main` | the main checkout, unless `[agents] allow_main = true` |
| `detached` | no branch checked out (also bare) |
| `paused-op` | merge / rebase / cherry-pick / revert in progress (`PausedOpIn`) |
| `git-lock` | a git lock file present (`LockFiles`) |
| `reserved` | path listed in `[agents] reserved` |
| `claimed` | a live claim exists |
| `tui` | a live registry names this worktree as its TUI's own |
| `session` | a live registry lists a running session with this dir |
| `dirty-recent` | dirty and `last_change` newer than `[agents] stale_after` |

`--free` keeps only free worktrees: clean first, then stale-dirty by oldest
`last_change`. `--free` without `--json` prints the text format.

**Cost.** The checks run cheapest first: registry + claim + config + stat
probes, all file-level. `git status` runs only for worktrees that are still
free after those, in parallel under the existing `LimitRunner` cap. Without
`--free`, `--json` still skips `git status` for a worktree already blocked
(its `dirty` is `null`) — status on a 20 GB checkout is seconds, and a
blocked worktree's dirt answers nothing the overseer needs.

Paths compare through `domain.SameCheckout` (WSL/Windows notation).

### 3. Session registry

New DAG-leaf package **`sessionreg`** (stdlib only):

- File: `<state>/gg/sessions/<proc>.json` where `<state>` is the project's
  state base (`$XDG_STATE_HOME` first, as every store).
- Content: `{ pid, started, worktree, sessions: [ {id, dir, agent, label,
  state, started} ] }` (`id` in the full `<proc>/<id>` form).
- `Write` = temp+rename; `Touch` = chtimes; `Live()` returns every registry
  whose mtime is within a live window (5 s, same as `steer.LiveWindow`) and
  sweeps older or unparsable files, exactly like `steer.Live`.
- `Remove` on clean shutdown.

Domain owns the writer: `domain.PublishSessions(ctx, worktree func() string)`
subscribes to `Sessions()`'s `Broadcaster`, rewrites on every change, touches
every second, removes the file when ctx ends. The TUI starts it at boot (it
already has the 1 s tick and the session subscription pattern); it survives
`reRoot` like `Sessions()` does, reading the current worktree through the
func.

A session is **alive** iff some live registry lists it with state running.

### 4. Claims

New DAG-leaf package **`wtclaim`** (stdlib + `filelock`):

- File: `<git-common-dir>/worktrees/<name>/gg-claim` for a linked worktree,
  `<git-common-dir>/gg-claim` for the main one. It disappears with
  `git worktree remove`.
- TOML: `session`, `agent`, `since`, `note`.
- `Create` is exclusive-create (O_EXCL) — of two racing claimers exactly one
  wins; `Read`, `Remove`.

Domain:

- `ClaimWorktree(ctx, path, sessionID, note)`: refuses an empty session id;
  computes the inventory entry; refuses when not free (returns the
  `blocked_by` list); otherwise `wtclaim.Create`. A dead claim found in the
  way is removed first, then the create is retried once.
- `ReleaseWorktree(ctx, path, sessionID, force)`: removes the claim when the
  caller's session id matches, or when `force`.
- **Dead-claim sweep:** any domain read of a claim whose session is not alive
  removes the file and reports no claim. So a crashed TUI, a killed or
  exited session, or a removed session never leaves a claim behind.
- One session may hold several claims.

Claims do not take the repogate reservation (no ref/tree write); they are not
git locks — `gg worktree recycle` on a claimed worktree still works (the
overseer claims, then recycles).

CLI:

- `gg worktree claim [--note <text>] <path>` — reads `GG_SESSION_ID`.
- `gg worktree release [--force] <path>`.

### 5. Reserve

- Config: `[agents] reserved = ["<path>", …]` in the active repo config file
  (committed or machine-private); paths relative to the main worktree (or
  absolute). Also `[agents] stale_after = "14d"` and `[agents] allow_main =
  false`. Config entries follow the `adding-config-entries` checklist
  (settings registry doc, overlay, defaults).
- `gg worktree reserve <path>` / `gg worktree unreserve <path>` write through
  the existing scoped config writers (via the config op, not a raw file
  edit).

### 6. TUI

- **Worktrees tab** row markers in their own column: `⚑ <agent>` (claimed),
  `⊘` (reserved). Translated.
- With the cursor on a claimed row the bottom bar shows agent, since, note.
- `.` menu: `Reserve (no agents)` / `Unreserve`; `Release claim (<agent>)`
  which asks first (the claim is by definition live). Footer + help entries.
- **Recycle picker** marks claimed and reserved worktrees like the existing
  `(agent session running)` and asks before recycling one.
- **Refresh:** the Worktrees source reloads on session-list change, on a
  claim file change (`gitwatch` path map learns `worktrees/*/gg-claim` and
  `gg-claim`), and on the repo config change it already watches.
- New ops mapped in `opAffectedSources`; every string in all four bundles.

### 7. Errors / exit codes

| Situation | Exit |
|---|---|
| `claim` without `GG_SESSION_ID` | 2 — "only an agent running inside gg can claim" |
| `claim` on a non-free worktree | 1 — prints `blocked_by` |
| `release` by a non-holder without `--force` | 1 |
| unknown worktree path | 1 |
| no claim to release | 0, "no claim" |

### 8. Agent docs

`internal/agentskill/using-gg.md` gets **Picking a worktree for another
agent**: `gg worktree list --free --json` → `gg worktree claim --note <url>
<path>` → `gg worktree recycle --on-dirty=<recycle> <path> <branch>` →
(stage 2: spawn) → `gg worktree release <path>`. Bump `agentskill.Version`.
CHANGELOG + README.

## Testing

- `sessionreg`, `wtclaim`: unit tests (live window via backdated mtime,
  unparsable sweep, O_EXCL race with two goroutines → one winner).
- `domain`: real git worktrees in `t.TempDir()`; one test per `blocked_by`
  reason; stale threshold via backdated dirty-file mtimes; `--free` order;
  status skipped for blocked worktrees (FakeRunner argv or runner count);
  dead-claim sweep via a stale registry and via an exited session;
  `PublishSessions` with a real `agentsession` manager.
- `agentsession`: `GG_SESSION_ID` reaches the child env.
- `cli`: JSON shape, `--free` text, exit codes, claim/release/reserve round
  trip. XDG state/config pinned in TestMain.
- `tui`: markers, bottom-bar reveal, menu rows, release confirmation,
  recycle picker marks; i18n gates.
