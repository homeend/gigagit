# gg web: the AI tasks tab and ◆ rows (parity bundle A, part 1)

Port of the TUI's Headless tab (`internal/tui/task_tab.go`, `task_result_view.go`)
and the Worktrees ◆ task rows (`worktree_sessions.go`, the `.` menu's Cancel task /
Show result) to gg web. The TUI is the spec. Part 2 (web AI runs go through
`domain.Tasks()` + the launch dialog) and part 3 (conflict agent / tool) follow.

## Today

The ctrl+\ switcher's "AI tasks" tab lists `domain.Tasks().List()` (live + this
process's ended tasks), read-only; enter opens a running interactive task's console,
anything else says "its result is not shown here yet". No history, no actions, no
live refresh, no ◆ rows.

## What it gains

- **Rows = the TUI's `taskRows`:** `List()` first, then `History()` records List()
  does not hold; columns task · agent · state · age (live: elapsed since start or
  submit; ended: `MM-DD HH:MM · Nx ago`). `/` filter on key + agent (exists).
- **enter (the TUI's `openTask`):**
  1. a review with a stored note → opens that review (the web's openReview);
     a review whose save failed → "<err> — s retry save";
  2. a result (live `Results > 0`, else the history result file) → a read-only
     result overlay (markdown for .md, plain otherwise) with **copy**, and for a
     commit-message task in the served worktree **apply** (fills the commit
     message box; asks before replacing a non-empty one);
  3. a live task with a session → its console;
  4. a failed task → its output (live tail → history tail → error) in the overlay;
  5. otherwise "no result yet".
- **k k** cancels a live task (first k asks "press k again to cancel <key>");
  **x** removes an ended record (a live one: "cancel it first (k k)");
  **s** retries a failed review save. The footer hint shows only what applies
  (TUI `taskHint`).
- **Live refresh:** a `tasks` live event carries the rows on every
  `Tasks().Subscribe()` wake; the tab and the ◆ rows re-render from it.
- **◆ rows** under a Worktrees row: live HEADLESS tasks of that worktree,
  `└ ◆ <kind> <target>  <state> <elapsed>` (kind labels commit message / review /
  resolve conflict / resolve & complete; target = the key after " — "). Click →
  the switcher's AI tasks tab on that task; right-click → Cancel task / Show
  result. Not on the Branches tab.

## Wire

- `GET /api/tasks` → `{tasks:[…]}` merged rows: id, key, kind, agent, mode, state,
  worktree, session, submitted/started/ended, live (bool), has_result, note_id,
  save_err.
- `GET /api/tasks/result?id=` → `{title, text, ext, note_id, apply}`; 404 when
  there is nothing to show. ids resolve only against `List()` + `History()`.
- `POST /api/tasks/cancel|remove|retry-save {id}` (writeGuard); remove of a live
  task → 409; unknown id → 404.
