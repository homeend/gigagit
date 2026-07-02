---
name: adding-notifications
description: Use when adding a repo-health check/notice to gigagit's notification center (the blinking `! N notice` segment + `!` dialog), or when changing notice lifecycle, dismissal, or blink behavior in internal/tui/notify.go.
---

# Adding a notification (health check + notice)

## What this is

On repo load (startup, `R` switch, Settings open) gg runs `domain.RepoHealth`
in the background; `internal/tui/notify.go` turns the facts into **notices**
— each with a stable id, a title, detail lines, and an action list. Unread
notices blink the red `! N notice` status segment (style alternation on a
self-stopping ~800ms tick); `!` opens the dialog. Adding a notice =
(1) facts in `RepoHealth`, (2) one builder func, (3) one line in
`applyRepoHealth`.

## Checklist

1. **Detection facts** — extend `model.RepoHealth` + `domain.RepoHealth`
   (`internal/domain/repohealth.go`). Facts must be CHEAP: filesystem stats
   on the git common dir, or a `ConfigGet`. No git walks — the check runs on
   every load and must never delay first paint.
2. **Builder** — add `func <x>Notice(h model.RepoHealth) *notice` in
   `notify.go` returning nil unless the recommendation applies. Give it:
   - `id`: a stable snake_case key (e.g. `commit_graph_recommend`). It is
     the PERSISTED dismissal key — never rename a shipped id.
   - `repoKey`: `h.GitCommonDir` (the promptstate dismissal scope).
   - `actions`: label + `run func(Model) (Model, tea.Cmd)`; a fixing action
     must reuse existing engine ops via `m.startOp` (see
     `updating-git-config-options` for config writes). Always include
     `{label: "Not now (ask again next load)"}` (run nil) and
     `{label: "Never for this repo", never: true}` — never trap the user
     into fixing.
3. **Register** — in `applyRepoHealth`, add alongside the commit-graph line:
   `if n := <x>Notice(msg.health); n != nil && !dismissed[n.id] && !m.noticeSessionDismissed[n.id] { next = append(next, *n) }`
4. **Dismissal lifecycle** — free. "Not now" records
   `m.noticeSessionDismissed[id]` (cleared on reRoot → re-evaluated next
   load); "Never" persists via `promptstate.Store.DismissNotice(repoKey, id)`.
   Session-dismissal filtering is what stops a mid-session health re-read
   (Settings open triggers one) from resurrecting a Not-now'd notice.
5. **Chaining two ops** — set `m.pendingNoticeConfig = &engine.SetGitConfig{…}`
   before `m.startOp(firstOp)`; the `opFinishedMsg` handler chains it on
   success (`Changed:true`) and clears it unconditionally (the
   `pendingPushTags` pattern).

## Tests to write (exemplars: `notify_test.go`, `notice_popup_test.go`)

- Detection unit tests on the builder: fires on the exact conditions, nil on
  each negated condition.
- Drive `repoHealthMsg` through `Update`: notice appears, unread+blink armed
  only for NEW ids, stale gen dropped, persisted + session dismissals filter.
- Action wiring end-to-end on a real repo (`driveOp`) when the action runs ops.
- Always inject a temp prompt store
  (`promptstate.NewFileStore(filepath.Join(t.TempDir(), "prompts.toml"))`).

## Rules

- Health check failures are silent (best-effort) — never a statusMsg.
- The blink is style alternation; never emit terminal blink escapes.
- The segment hides while `m.proc != nil` (conflict process owns the screen).
- Notices are per-repo session state: reRoot clears the list, bumps
  `noticeGen`, resets session dismissals.
