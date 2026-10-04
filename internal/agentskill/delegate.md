# delegate — hand work to worker agents through gg

Use this when the user gives you a task to **delegate**: you become the
**overseer**, gg starts **worker** agents in their own worktrees, and you
drive them with gg's agent tools until the work is done. The user's prompt
holds the task; this skill holds the method. If you were *started* by gg as
a worker, skip to **Worker protocol**.

The tools exist only inside a gg console (the agent channel): MCP tools
`agent_*`, or their CLI twins `gg agent …`. If they are missing or answer
"run this inside a gg console", tell the user — delegation needs a console
started from gg's Start agent.

## Overseer

### 1. Read the task — wherever it comes from

The task may arrive as text in the prompt, a file path, an issue or ticket
URL (GitHub, Jira, …), or a mix. Resolve it with whatever tools YOU have —
read the file, `gh issue view <url>`, your Jira/Linear/… tools or MCP
servers, the web. Then:

- **The brief must stand alone.** A worker may have no access to the
  tracker, and it never sees this conversation: copy into the brief what it
  needs (the problem, acceptance criteria, relevant comments, error text),
  not just a link.
- **Keep the reference.** Put the source (URL, issue key, file path) on the
  brief's first lines and in `agent_start`'s `note` — the worktree claim
  carries it, so the user sees which task occupies which worktree.
- Cannot read the source (no access, private)? Ask the user to paste it —
  never invent the task from a URL's words.

### 2. Decide the kind of task

Name the kind in the brief; it fixes what the worker may touch and what it
hands back. Combine kinds in order when the task says so ("investigate, then
fix, then report").

| kind | worker may | worker hands back |
|---|---|---|
| **investigate** | read code, run read-only commands and tests; change nothing | findings: cause or answer, evidence as anchors, options with trade-offs |
| **check** (verify / review) | run the build, tests, reproduce; change nothing in the tree | pass / fail per criterion with evidence; for a review, anchored review notes with `gg note` (see reviewing-with-gg) |
| **fix** | change code and tests in its worktree; commit on its branch with gg; never push, never merge | what changed (anchors), how it was verified, the commits |
| **report** | — (added to any kind) | the final report written as a tour (below) |

"Report as a gg overview / tour" always means the worker's **final
`agent_report`** — gg files it as a tour of the worker's worktree. Never
write `gg session overview` into a brief: it targets the worktree the
user's gg is showing, not the worker's.

A fix that the investigation shows is larger or riskier than the task
expected: stop at the report and let the overseer (you) or the user decide.

### 3. Plan the workers

- Split only into pieces a worker can finish alone, in one worktree, without
  waiting for another. One worker is usually right; several only when the
  pieces are truly independent (e.g. one per issue).
- Each worker needs its own worktree ON THE RIGHT BRANCH — a free worktree
  still holds whatever branch it held last. For each worker:
  1. `gg worktree list --free --json` — free worktrees, best first (never
     your own).
  2. `gg worktree claim --note <source> <path>` (exit 1: taken — next one).
  3. Put the right branch there:
     - *investigate* / *fix*: a FRESH branch from the base the task names,
       else the remote's main branch: `gg branch create <b> origin/main`
       (name it by the user's scheme — `gg prefix ls`, see using-gg), then
       `gg worktree recycle <path> <b>`.
     - *check*: the branch under review — `gg worktree recycle <path>
       <branch>`; if that branch is checked out elsewhere, branch off its
       tip first (`gg branch create check/<x> <branch>`) and recycle onto
       that.
     Pass `--on-dirty=shelve` only when the listing's `recycle` says
     `shelve`.
  4. Name the branch in the brief's Context.
  None free → ask the user, or `gg branch create <b> <start>` then `gg
  worktree add --branch <b> <path>` (`--branch` takes an EXISTING branch).
  `agent_start` passes your claim to the worker; it returns to you when the
  worker ends.
- The user's `[agents] max_spawned` caps how many run at once; a worker can
  never start workers of its own.

### 4. Write the brief

Markdown, self-contained, with repo-relative anchors to where the worker
should look (`[the parser](src/parse.go:40-60)`) — the user opens the brief
as a tour of the worker's worktree, so links are worth adding.

```
# <one-line goal>
Source: <URL / issue key / file>    Kind: <investigate | check | fix> [+ report]

## Context
<the task as the source states it — copied, not just linked — and what you
already know; anchors to the relevant code>

## Do
- <steps or outcome>

## Do not
- <the kind's limits; files or areas off-limits; no push, no merge>

## Done when
- <checkable criteria>

## Report
Finish with agent_report (final), written as a tour: <what the report must
contain for this task>.
```

### 5. Start

`agent_start {worktree, tool, prompt, note}` / `gg agent start --worktree
<path|name|branch> --tool <name> --prompt-file <file|-> --note <source>`.
`tool` is a session command the user allowed in `[agents] spawn` — use the
one the user named, else ask once. The answer is the worker's id. Its row
appears under the worktree in the user's gg; nothing opens on their screen.

### 6. Wait — the loop

Call `agent_wait` (no id = any of your workers; `timeout_s` 45 is safe) and
act on the ONE event it returns:

| event | do |
|---|---|
| `report` | read `report.text`. `final` → step 7 for that worker. Not final → progress or a question: answer with `agent_send` if it asks, else wait again. |
| `question` | the worker's tool shows a dialog; `options` lists the choices. Pick the one that fits the brief's limits (never destructive or out of scope) and send its `key`: `agent_send {id, keys: ["<key>"]}`. A `pick:<i>` key cannot be pressed — move with `up` / `down` keys and send `enter`. Unsure → ask the user. |
| `idle` | its turn ended without a report: read `agent_screen`. Finished → `agent_send` "finish with agent_report (final)". Stuck or asking in prose → answer it. |
| `exit` | the worker is gone: read `agent_screen` for why (reports it made come first). Restart it (new `agent_start`) or report the failure. |
| `timed_out: true` | nothing new — call `agent_wait` again. `activity: idle` after you sent something, or a long `stalled` → read `agent_screen`. |

Each event comes once. `agent_list` shows the current state of everything
(`activity`, `report_at`) when you lose track.

### 7. Check and finish

- Verify before you trust: read the final report, then the worker's changes
  in its worktree (`cd <worktree> && gg status`, `gg diff`, `gg log`)
  against "Done when". For a fix, run the tests yourself if they are cheap.
- Not done → `agent_send` the gap (one message, concrete), back to the loop.
- Done → tell the user: per worker, the outcome, the worktree and branch,
  what is left — and that **Open report** on the worker's row shows its
  report as a tour (and **Open brief** the brief). Merging or pushing a
  worker's branch is the user's call unless the task said otherwise.
- Then `agent_kill {id}` — WITHOUT `remove`: removing the session also
  drops its brief and report, so the user's **Open report** would show
  nothing. The exited row stays until the user removes it (Remove session)
  — pass `remove: true` only when they say they are done looking. If the
  user wants to talk to the worker, leave it running and say so.
- Release the worktree when its worker is gone: `gg worktree release
  <path>`.

## Worker protocol

You were started by gg with one line: read your task. Then:

1. `agent_task` (or `gg agent task`) — your brief, the whole of it. Its
   **Kind** line is binding: an *investigate* or *check* task changes
   nothing; a *fix* commits on your branch (gg's commands, see using-gg) and
   never pushes or merges.
2. Work only in your worktree (your current directory). Respect "Do not".
   Never steer the user's window: no `gg session navigate | highlight |
   focus | reload | files | overview | note` — the user's gg shows another
   worktree, and each call raises a notice there.
3. Blocked, or a decision the brief does not cover? Do not guess: send a
   non-final `agent_report` with one clear question, then wait — the answer
   arrives as typed input.
4. Review notes (reviewing-with-gg's `gg note`) work from your worktree —
   they persist and the user sees them in gg's review views; skip that
   skill's `gg session` steps (see step 2). Your checkout is usually clean, so
   the working-tree diff is empty: review a BRANCH through a merge preview —
   `gg preview add <branch> <target>`, read it with `gg diff --preview <id>
   --hunks`, note with `gg note add --preview <id> --file … --new-line …`,
   and put `gg link --preview <id>` in your report. A *check* that asks for
   a review reviews the branch under review; a *fix* asked to annotate its
   change reviews its own branch.
5. Finish with `agent_report {text, final: true}` (`gg agent report --final
   -F -`). **This report is your overview:** gg files it as a tour of your
   worktree, so write it as markdown with anchors — first line a one-sentence
   summary (rows and notices show it), then what you found or changed,
   most important first, each with an anchor (`[the race](src/a.go:10-30)`),
   then what you skipped and why, and what the overseer must do next.
   Do NOT use `gg session overview add` for this (see step 2).
6. Stay running after the final report — do not exit. The overseer may read
   your screen or ask a follow-up; it ends you when it is done.
