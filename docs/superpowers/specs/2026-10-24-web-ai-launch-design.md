# gg web: AI actions go through the task scheduler (parity bundle A, part 2)

Port of the TUI's AI-task launch (`internal/tui/task_launch_popup.go`) and result
tracking (`task_track.go`, `commit_generate.go` applyCommitMessage, `review.go`
applyReviewResult) to gg web. The TUI is the spec. Part 1 (merged 57aadcae) gave the
web the AI tasks tab and ◆ rows over `domain.Tasks()`.

## Today

Review, AI conflict resolve and commit-message generation each run as ONE headless op
through `startRun` (review.go, conflictai.go, op_ai.go): they hold the single op lane,
never enter `domain.Tasks()` (so the AI tasks tab and the TUI never see them), have no
mode choice and no interactive agents. The review lane can be "parked" behind a chip.

## What it becomes

Every web AI action opens **one launch dialog** — the TUI's:

- title = the task key (`domain.Service.TaskKey`);
- **Agent ◀ name ▶ n/m** (←/→), rows per mode × command (↑/↓): *Interactive,
  background* (a session, no console), *Interactive, foreground* (its console opens
  when the session exists), *Headless* (queued, captured). A mode without a command is
  a greyed "(no command for this agent)"; a command whose program is not on PATH says
  "not found" and refuses; a command needing `<user:…>` input or `per_file` is left
  out; a conflict command whose `when_op` differs from the paused op is left out
  (`domain.TaskChoices` + the TUI's `launchChoices` filter, frontend "web");
- the scheduler's wait line ("N task(s) with this key ahead — this one will wait" /
  "N tasks running — it will start when one finishes", `Tasks().Load(key)`);
- the remembered choice per kind preselects (`promptstate.TaskLaunchChoice`, shared
  with the TUI);
- enter runs; an unapproved command shows the resolved command first ("Run this
  command? (name)", enter run / esc back) and the approval is remembered
  (`promptstate.CommandHash`, as today);
- first run for commit_message / review adds the detected agents' interactive rows to
  the global config (`domain.EnsureInteractiveCommands`), saying so.

Entry points: review rows (working / branch / commit / range / preview), the commit
box's generate button, the conflict banner's AI resolve (kind `conflict_complete`).
The conflict process's "Resolve with an agent…" (kind `conflict`) and its external
tool button are the next branch (part 3).

**Submit** goes through `domain.Tasks().Submit` with the kind's spec builder
(CommitMessageTask / ReviewTask / ConflictTask), `Env = GG_INBOX=<this page's steer
inbox>`, the session place's cwd, the console's grid size. The op lane is never held:
several AI tasks run at once under `[tasks] max_parallel`.

**Results** (the page applies only tasks THIS tab submitted, once per result — the
TUI applies its own; a page it hosts must not double-apply):

- commit message → into the commit box when it is on screen and empty; a box with
  text gets an offer ("message from <agent> — replace?"); not on screen → toast
  "commit message from <agent> ready — ctrl+\".
- review → the stored note opens as its review (`openReview`) when nothing owns the
  keyboard (no layer, no focused console, no modal); else toast "<key> ready —
  ctrl+\". A save failure → toast "<key>: <err> — ctrl+\ to retry".
- conflict → the status reloads; an overview result opens in the report viewer when
  nothing owns the keyboard, else the toast. A conflict run that ends with no result
  says "<key> finished" and reloads.
- a failure → toast "<key> failed — <err> (ctrl+\ for details)".
- foreground → the console opens once the task's session exists.

## Removed

The single-lane review/conflict/commit-message runs (`/api/review`, `/api/conflict/
complete`, `/api/commit-message/generate`) and the review lane's park chip: the AI
tasks tab is where a running or finished task is collected (TUI ruling — no lane to
park). The report viewer (`openReport`) stays and shows conflict overviews.

## Wire

- `GET /api/tasks/launch?kind=…&target=…` (review target fields as today:
  target/branch/sha/preview/shas) → `{key, choices:[{agent, identity, rows:[{mode,
  name, none, missing}]}], pick:{agent, mode, name}, wait, added, config_path}`.
- `POST /api/tasks/submit {kind, …target, identity, mode, name, approve, cols, rows}`
  → `{id, key, state}`; unapproved → 403 `{needs_approval, command, tool}`; a program
  not on PATH → 422.
- `taskWire` gains `results` (count) and the live rows carry it.

## Build order

1. `/api/tasks/launch` + `/api/tasks/submit` + the dialog, old endpoints untouched.
2. Re-point the entry points at the dialog; result tracking; browser check.
3. Remove the old lanes and the park chip; port their surviving tests.
