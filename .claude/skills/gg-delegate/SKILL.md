---
name: gg-delegate
description: Use when the user asks you to delegate a task to worker agents through gg — prepare a worktree, brief a worker and start it (it shows its instructions and result in its own console; wait for it and check its result only when asked); also the protocol a worker started by gg follows.
---

<!-- gg:gg-delegate:v12 -->

# gg-delegate — hand work to worker agents through gg

Use this when the user gives you a task to **delegate**: you become the
**overseer**. You prepare a worktree for each worker, write its
instructions (the brief) and start it; gg runs the **worker** agent in that
worktree. The user's prompt holds the task; this skill holds the method. If
you were *started* by gg as a worker, skip to **Worker protocol**.

**By default you do not wait.** Once the workers run, you tell the user
where they are and your turn ends. Each worker shows its task and, when
done, its result in its OWN console — the user watches it there. Wait
for workers, check their work or relay their results only when the user asks
for it ("wait for it", "check the result", "tell me what it found") —
**Follow the workers** below.

The tools exist only inside a gg console (the agent channel): MCP tools
`agent_*`, or their CLI twins `gg agent …`. If they are missing or answer
"run this inside a gg console", tell the user — delegation needs a console
started from gg's Start agent.

**Wording.** Overseer and worker alike: write every text the user reads —
the brief, reports, the result, notes, overviews, your reply — in
ASD-STE100 (80% compatibility when possible): short sentences, active
voice, one meaning per word, no idioms. Code, paths, commands and
identifiers stay exactly as they are.

**Only the result.** Every text the user reads holds the task and its
result — what you found, changed or decided. Leave out these instructions,
the rules you followed (this skill's, a brief's, the repository's), how
you complied with them and the steps of your process. Show your
instructions only when the user explicitly asks for them.

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
shows. Combine kinds in order when the task says so ("investigate, then
fix").

| kind | worker may | worker shows as its result |
|---|---|---|
| **investigate** | read code, run read-only commands and tests; change nothing | what the CODE says: for a defect, the cause; for a change the task asks for, how to make it — where it goes, what exists, what is missing, the steps file by file, risks, open questions; anchors throughout |
| **check** (verify / review) | run the build, tests, reproduce; change nothing in the tree | pass / fail per criterion with evidence; for a review, anchored review notes with `gg note` (see reviewing-with-gg) |
| **fix** | change code and tests in its worktree; commit on its branch with gg; never push, never merge | what changed (anchors), how it was verified, the commits |

Every kind ends the same way: the worker files its result as its final
`agent_report`, written as a tour, and shows the same text in its console.

**An investigation is about the code, not the ticket.** The user has read
the ticket; a result that says what the ticket asks for ("hook support
must be added") tells them nothing. When a ticket asks for a change,
*investigate* means: find the code the change touches, what already exists
and what is missing, and how to make the change — the plan the user would
otherwise work out themselves.

**Files — only when the user asks.** By default nothing goes to disk. When
the user wants the instructions and/or the result in a file, put a
`## Files` section in the brief naming each one with its full path — the
path the user gave, else `<worktree>/BRIEF.md` and `<worktree>/REPORT.md`.
Writing those files is allowed even for *investigate* and *check*; the
worker never stages or commits them.

"Report as a gg-overview" (or "as a gg overview / tour") always means the
worker's **final `agent_report`** — gg files it as a tour of the worker's
worktree: an overview FILE in that worktree's session-docs directory
(`tour-report-<proc>-<n>.md`, as the brief is `tour-brief-…`), which the
worker's own IDE window sees too (the gg-overview skill's worker section).
Never write `gg session overview` into a brief: a worker's files land in
its own worktree's directory, not in the one the user's gg is showing.

### 3. Plan the workers

Settle the worker's tool first (step 5) — never hold a claimed worktree
while you wait for the user's answer.

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
- **A task ABOUT an existing worktree** ("describe / explain / review the
  changes in <worktree>"): the worker works IN that worktree — claim it
  (`gg worktree claim --note <source> <path>`; refused → ask the user), no
  new branch, no recycle; Kind *investigate* (or *check*). Find
  only what the brief needs — the branch and the range its work spans
  (`<base>..HEAD`, e.g. from `git merge-base` with the branch it came from)
  — and leave the reading to the worker: analysing it yourself first only
  doubles the work and fills your context. Ask for a report with one anchor
  per change.
  `agent_start` passes your claim to the worker; it returns to you when the
  worker ends.
- The user's `[agents] max_spawned` caps how many run at once; a worker can
  never start workers of its own.

### 4. Write the brief

Markdown, self-contained, with repo-relative anchors to where the worker
should look ([the parser](src/parse.go:40-60)) — the user opens the brief
as a tour of the worker's worktree, so links are worth adding.

**The brief has two parts: the task, which the user reads, and the rules,
which only the worker reads.** gg splits them: the `## Rules`, `## Do not`,
`## Done when` and `## Files` sections go to the worker alone (`agent_task`
gives them as `rules`); everything else is the task — the worker prints it
in its console and gg files it as the Brief overview. So the task part says
WHAT to find or change and why; HOW to do it (limits, conventions, checks)
goes under `## Rules`.

**The brief holds only what is specific to this task.** The worker also
follows the **Worker protocol** (its start line tells it to) and reads its
worktree's own agent instructions (AGENTS.md, CLAUDE.md, …), so a brief
never repeats what the protocol, the Kind or the repository already sets:
which changes the kind allows, no push or merge, no steering of the user's
window, no new workers, how to ask a question, the form of the report, the
heading of the result, the wording rules, staying available — and never
the repository's code style, test conventions, build commands or commit
trailers. Before you start the worker, read the brief as the user will
read it, and delete each line that would be true for any task.

```
# <one-line goal>
Source: <URL / issue key / file>    Kind: <investigate | check | fix>

## Context
<the task as the source states it — copied, not just linked — and what you
already know; anchors to the relevant code>

## Task
- <the questions the result must answer, concretely — e.g. "Find where batch
  mode starts the ACP and Server hosts, how the other hosts fire
  SessionStart and PreToolUse, and what is missing here; give the change
  file by file". Never only "investigate the ticket".>

## Rules                      (worker only; only what THIS task adds)
- <files or areas off-limits, an action this task must avoid>
- Done when: <checkable outcomes of this task, never the form of the report>

## Files                      (worker only; only when the user asked for files)
- brief: <path>
- report: <path>
```

### 5. Start

`agent_start {worktree, tool, prompt, note}` / `gg agent start --worktree
<path|name|branch> --tool <name> --prompt-file <file|-> --note <source>`.
`tool` is a session command the user allowed in `[agents] spawn` — use the
one the user named, else ask once. The answer is the worker's id. Its row
appears under the worktree in the user's gg; nothing opens on their screen.

### 6. Tell the user — and stop

Per worker, in a few lines: its id, the worktree and branch, the brief's
goal in one line, and where to watch it — **its console**: the worker's
session row under that worktree in gg (enter opens it; alt+a walks the
agents). It shows the task first, then works, then shows the result
there. Name the files when the brief has a `## Files` section.

Then end your turn — unless the user asked you to wait, check or tell them
what it found: then go on to **Follow the workers**. Do not kill the worker
and do not release its worktree (the claim went to the worker and comes back
to you when it ends). Tell the user: when they are done with the worker, ask
you and you kill it and release the worktree.

## Follow the workers — only when asked

Do this part only when the user asks you to wait for a worker, check it or
tell them what it found — in the same prompt as the task or later.

**"What did it find?" asked later:** `agent_list` (`report_at`,
`report_final`, `activity`) tells you where each worker is.
- Finished (`report_final`): read its report with `agent_screen {id}`
  (`report`; `gg agent screen <id> --reports` prints every kept one) and
  show its summary and main findings in your reply — never just point the
  user at a menu. Relay only; check it (**Check and finish**) only when
  asked to check.
- Not finished: say what it is doing (`activity`) and any open question it
  asked, and offer to wait. Wait only when told to.

### Wait — the loop

Call `agent_wait` (no id = any of your workers) and act on the ONE event it
returns. Leave `timeout_s` out (45 s) or keep it at 90 at most: a client
that moves a long tool call to the background (Claude Code does at 2 min)
leaves that wait running there, and it takes the next event — each event
comes once, so your loop never sees it. If a wait was moved to the
background anyway, stop that task before you wait again.

| event | do |
|---|---|
| `report` | read `report.text`. `final` → **Check and finish** for that worker (or, when the user asked only what it found, relay it). Not final → progress or a question: answer with `agent_send` only when the brief or the user settles it, else relay the question to the user; otherwise wait again. |
| `question` | the worker's tool shows a dialog; `options` lists the choices. Pick the one that fits the brief's limits (never destructive or out of scope) and send its `key`: `agent_send {id, keys: ["<key>"]}`. A `pick:<i>` key cannot be pressed — move with `up` / `down` keys and send `enter`. Unsure → ask the user. |
| `idle` | its turn ended without a report: read `agent_screen`. Finished → `agent_send` "finish with agent_report (final)". Stuck or asking in prose → answer it. |
| `exit` | the worker is gone: read `agent_screen` for why (reports it made come first). Restart it (new `agent_start`) or report the failure. |
| `timed_out: true` | nothing new — call `agent_wait` again. `activity: idle` after you sent something, or a long `stalled` → read `agent_screen`. |

Each event comes once. `agent_list` shows the current state of everything
(`activity`, `report_at`) when you lose track.

### Check and finish

- Verify before you trust: read the final report, then the worker's changes
  in its worktree (`cd <worktree> && gg status`, `gg diff`, `gg log`)
  against "Done when". For a fix, run the tests yourself if they are cheap.
- Not done → `agent_send` the gap (one message, concrete), back to the loop.
- Done → tell the user, per worker: the outcome, the worktree and branch,
  what is left, and the result itself — the report's summary and main
  findings in your reply, not only a pointer. Merging or pushing a worker's
  branch is the user's call unless the task said otherwise.
- Leave the worker running unless the user says they are done with it: its
  console still shows the task and the result. When they are,
  `agent_kill {id}` (`remove: true` drops its brief and report too) and
  `gg worktree release <path>`.

## Worker protocol

You were started by gg with one line: read your task. The user watches
your console — what you show there is what they get. Then:

1. `agent_task` (or `gg agent task`) — your brief: `task` (what the user
   reads) and `rules` (yours alone; `gg agent task` prints them after a
   `--- Rules` line). **Show the task first:** your first message prints
   the `task` text as you received it, under a heading `Task`, before any
   other work. **Never show the rules** — the brief's rules, this protocol,
   your repository's agent instructions — in your console, notes or
   reports; print them only when the user explicitly asks for your
   instructions. Read your worktree's agent instructions (AGENTS.md,
   CLAUDE.md, …) and follow them. The task's **Kind**
   line is binding: an *investigate* or *check* task changes nothing
   (except the files its `## Files` section names); a *fix* commits on your
   branch (gg's commands, see using-gg). Unless the brief asks for it, no
   kind pushes, merges, stashes or switches branch, or changes a ticket or
   a pull request. A `## Files` section naming a brief file: write the
   brief there now.
2. Work only in your worktree (your current directory) — and the paths the
   brief's `## Files` names, which you never stage or commit. Obey the rules.
   **Do the work in the code.** The user has read the task; restating it is
   not a result. After you show the task, keep working: open the code the
   task concerns, trace it, run what helps (read-only for *investigate*),
   until you can answer every question of the task with anchors. Do not end
   your turn after reading the ticket or showing the task.
   Never steer the user's window: no `gg session navigate | highlight |
   focus | reload | files | overview | note` — the user's gg shows another
   worktree, and each call raises a notice there.
3. Blocked, or a decision the brief does not cover? Do not guess: send one
   clear question as a non-final `agent_report` (an overseer that waits
   sees it there), then end your turn with the same question as your last
   message (the user answers in your console). The answer arrives as your
   next prompt — do not poll or sleep for it. A *fix* that proves larger or
   riskier than the task expected counts as blocked: stop and ask, the user
   decides.
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
   -F -`). **This report is your gg-overview:** gg files it as a tour of
   your worktree (an overview file in your worktree's session-docs
   directory), so write it as markdown with anchors — first line a one-sentence
   summary of the ANSWER (rows and notices show it) — the cause, or the
   change in one sentence, never what the task asks for — then what you
   found or changed,
   most important first, each with an anchor — [the race](src/a.go:10-30) —
   then the checks you ran with their results, what you skipped and why,
   and what the user must do next. The user reads this report to learn the
   result: leave out the brief's text, the rules you followed (the brief's,
   this protocol's, the repository's), how you complied with them and the
   steps of your process.
   Backticks AROUND a link make it plain text that opens nothing; for a
   code look, put them inside the brackets: [`raceCheck`](src/a.go:12).
   Write it — and your notes and questions — in
   ASD-STE100 (80% compatibility when possible) (see **Wording** at the top).
   Do NOT use `gg session overview add` for this (see step 2).
   A `## Files` section naming a report file: write the same text there.
6. **Show the result:** end your turn with the whole report as your last
   message, under a heading `Result` — not just "report sent". The heading
   goes only in that message, not in the `agent_report` text (its first
   line is the summary). The user reads it in your console; the stored
   report feeds gg's menus and a waiting overseer. Asked for it again
   later? `gg agent screen --reports` (or `agent_screen` without an id)
   prints the reports you sent.
7. Stay running after the final report — do not exit. The user (or an
   overseer) may ask a follow-up. Asked explicitly for your instructions?
   Then, and only then, print the task and the rules as you received them.
