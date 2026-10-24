# gg web F3 — PR hub keys, notification centre, standalone op log

Parity bundle F3 from `docs/web-tui-parity.md` (rows "PR details overlay",
"Notification centre coverage", "Operation log in standalone gg web"). The
TUI is the spec: each part ports the TUI surface as it is; where a browser
cannot do what the terminal does, the ruling below says what stands in.

## 1. Standalone operation log

TUI (`internal/tui/oplog.go`, `run.go`, Settings → Operation log): with
`[debug] log_operations = true` every recorded span is appended as one JSON
line to `<state>/operations.log` (weekly rollover, `observ.WeeklyLog`), and a
`"gg log enabled"` marker span carrying `version=<build>` delimits the run.
The Settings toggle flips it live and persists it.

Web today: the Settings row writes the config key only; `gg web` installs only
the failure sink, so a standalone page never logs operations.

Port:
- `Serve` (standalone) opens the log when the effective config says
  `log_operations`, exactly as `tui.Run` does: `observ.OpenWeeklyLog(webOpLogPath())`,
  `observ.SetSpanSink(f)`, the marker span. Shutdown clears the sink.
- The Settings PUT with `op_log` flips it live (enable / `SetSpanSink(nil)` —
  which closes the file; never close it twice).
- **Ruling (hosted):** a page the TUI hosts never touches the sink — the TUI's
  `opLog` owns the process-global sink and its on/off state. A hosted toggle
  stays config-only, as today.

## 2. PR details: the TUI hub's keys

TUI `prHubPopup` (`internal/tui/pr_hub.go`): a content popup with
`y` copy URL · `L` copy gg link · `r` reload · `s` Send to GitHub… ·
`v` Verdict… · `/` filter · `q`/`esc`/`enter` close, and the key line
`[y] copy URL  [L] copy link  [r] reload  [s] send to GitHub  [v] verdict`.

Web today (`static/prdetails.js`): read-only, read once; scroll keys; a copy
URL button.

Port:
- Keys `y`, `L`, `r`, `s`, `v`, `/`, `enter` (close) and a key line under the
  title showing the same five actions as buttons (pointer users).
- `y` copies the URL (no URL → "this pull request has no URL").
  `L` is the PR menu's copy gg link (fetches an unfetched PR first) —
  shared code, not a copy. `s` opens the send panel and `v` the Verdict…
  prompt for the PR, ungated as in the TUI: the server answers (nothing to
  send, an error) on the op line. The send panel / prompt stack above the
  overlay; esc there returns to the overlay.
- `r` drops the filter, shows the loading line and reads the PR fresh (the
  POST).
- `/` filter = the TUI's `contentPopup.visible()`: case-insensitive substring;
  a heading stays only while something under it matches. Typing mode captures
  every key (j/k are query text, arrows still scroll); enter commits (the
  filter stays, the phrase is recorded in the `filetree` search-history ring —
  the scope the TUI's content popup uses — and alt+↓/↑ recall it); esc while
  typing cancels; esc on a committed filter clears it, the next esc closes.
- **Ruling (filter grain):** the web filters BLOCKS, not wrapped rows: the
  meta line, the URL, each paragraph / list item / code block of a
  description or comment, each comment's author line, each outdated thread's
  location line and hunk. A browser has no fixed row to match.
- The `?` help row and the overlay's own help text name every key.

## 3. Notification centre

TUI (`notify.go`, `notice_popup.go`): one `!` centre. Lifecycle: a notice id
not in the previous list marks the centre UNREAD and blinks the segment
(~800 ms) until the centre is opened. Every action except a copy removes the
notice for the SESSION (a re-read must not resurrect it; cleared by a repo
switch); "Not now (ask again next load)" is that alone; "Never for this repo"
also persists `promptstate.DismissNotice(commonDir, id)`.

### 3.1 Lifecycle

- **Session dismissals** live SERVER-SIDE, in memory, per process and per repo
  key (git common dir) — never browser storage (random port = empty storage),
  and a page reload is the same session. A repo switch starts the session
  afresh, as the TUI's reRoot does.
- **Ruling (hosted):** a page the TUI hosts keeps its own session; only
  "Never" (prompts.toml) is shared with the terminal.
- Every action removes its notice for the session; a fixing action also runs
  its op. "Never for this repo" persists as today.
- **Unread** is server-side too (one session, many tabs): a GET of the centre
  that sees an id the previous GET did not marks it unread; opening the centre
  (`POST /api/notifications/read`) marks it read. The bell blinks (CSS
  alternation, ~800 ms) while unread.
- The web's single "dismiss" button becomes the TUI's two rows:
  "Not now (ask again next load)" and "Never for this repo".
- Refresh: page load, every op end (exists), opening the centre (exists).
- An op that fails on a git lock (`domain.IsLockError`) clears the lock
  notice's session dismissal and the op line adds "— press ! to remove the
  lock" (`maybeStaleLockNotice`).

### 3.2 Coverage (TUI ids, PERMANENT)

| id | Actions |
|---|---|
| `stale_git_lock` | Remove the lock file(s) (the listed paths; `engine.RemoveGitLocks` re-validates each against the git dirs) · Not now · Never |
| `commit_graph_recommend` | Write commit-graph now + keep it fresh · Enable auto-refresh only · Not now · Never |
| `narrow_fetch_refspec` | exists; gains Not now |
| `feature_disabled_<id>` | every non-Satisfied, non-Silent preflight verdict (generalises the note-edits one): Not now · Never |
| `tool_template_update` | Open external tools · Not now (no Never: offers are counted) |
| `tool_agent_unsupported_<hash12>` | Open external tools · Not now · Never |
| `steer_switch` | Switch to <worktree> and show · Ignore (§3.3) |

Order as the TUI: stale lock, commit graph, refspec, feature-disabled, steer,
tool notices; then the web's own (none today beyond these).

**Rulings (not ported):** `clipboard_tool_missing` / `wsl_interop_broken` —
a browser copies through `navigator.clipboard`. Drift notices — the web's
drift panel (`versions.js`) already says the TUI notice's words with its copy
link, in place. Interrupted sends — the PR section's bar (listed *Web only*).
The locks bar and the big-repo banner stay; they share the ids above, so a
Never in either place silences both.

### 3.3 Steer switch ask

TUI (`steer_switch_ask.go`): a worktree-bound command (navigate to a file or
diff, a highlight) naming a worktree gg is not showing is not applied: ONE
parked slot (the latest wins), a `steer_switch` notice "<who> wants to show
<where> in <worktree>" / "gg is showing another worktree; nothing moves until
you choose.", and the agent hears at once: "gg is showing worktree X; asked
the user to switch to Y". Switch re-roots and replays a navigate; Ignore
drops it.

Web today: a navigate naming another worktree is applied to the page's own
checkout.

Port: `handleSteer` parks such a command server-side (same predicate,
same reply prose with "gg web" for "gg"), the centre lists it and blinks.
Switch: the server takes the slot, sets the replay (a navigate with
worktree/from/wait cleared) as the start-at command, and the page re-roots
to the worktree (`doReroot`, which a hosted page hands to the TUI); the next
page load replays it through the existing start-at lane — only when the
re-root lands on that worktree within a minute (a Switch whose re-root never
happened must not replay on a later, unrelated one). Ignore drops the slot;
any re-root clears it; Switch while an operation runs is refused out loud and
the ask stays. Who asks: "an agent" (the web does not map
sessions to inboxes).

## Non-goals

Moving the web's banners into the centre; a notice save (`s`) row; ctrl+w
wrap modes; `ctrl+t` maximize.
