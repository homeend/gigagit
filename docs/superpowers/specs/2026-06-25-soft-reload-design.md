# Soft reload for `r` — design

Date: 2026-06-25
Status: approved (brainstorm)
Scope: TUI-only (`internal/tui`)

## Problem

Pressing `r` reloads the whole app. `View()` (model.go:1970) returns
`"gigagit (loading…)\n"` for the entire duration of `m.loading`, throwing away
the rendered panels. On a giga monorepo `loadCmd()` (a `Snapshot` plus the
initial commit walk) takes multiple seconds, so the UI goes blank — looks like a
black screen / freeze — until `dataLoadedMsg` arrives.

## Goal

Make `r` a **soft** reload: keep the panels on screen with their still-valid old
data, show a ⏳ in each affected panel's title plus a `reloading…` status line,
and swap in fresh data atomically when it lands. Navigation stays live; the UI
never blanks for a same-repo reload.

Out of scope (explicitly): window-by-window / partial reload (refreshing only
the panels a given op touched). `r` by definition wants everything refreshed, so
partial reload buys nothing here; it is a separate future increment for the
post-op reload paths (commit → Status+Commits, resolve → Status, …).

## Why it is safe

The model holds the previous `status` / `branches` / `remoteBranches` /
`commits` / `worktrees` / `tags` / `reflog` until `dataLoadedMsg` replaces them
in one shot (model.go:450+). Rendering the stale panels during the reload shows
nothing incorrect — only slightly out-of-date data — and the swap is atomic.
Selection clamping already runs on every `dataLoadedMsg` (model.go:477–485), so
a row removed during the reload cannot leave the cursor past the end.

## The reRoot collision (the load-bearing decision)

`m.loading` is shared by two callers with **opposite** needs:

- `r` reload (model.go:732) — same repo, old data still valid → soft-render.
- `reRoot` (model.go:1949) — repo *switch*; it deliberately wipes `sel`,
  `mark`, `fileMarks`, `stashView`, `filesView`, `diffLayer` because the data
  belongs to a different repo. Its stale panels would be wrong/misleading →
  must keep the blank screen.

Initial startup is the same as `reRoot` here: there is no old data yet, so it
must keep the blank screen too.

Resolution: a new `softReload bool` flag set **only** by the `r` handler. `View()`
blanks when `m.loading && !m.softReload` (startup + reRoot) and soft-renders
otherwise. `reRoot` explicitly sets `softReload = false` — a repo switch can be
triggered while a soft reload is in flight (the UI stays live, that's the point),
and the old repo's panels must stop soft-rendering immediately.

### Flag lifecycle (the double-`r` subtlety)

- **Set true:** only the `r` handler.
- **Set false:** the **current-generation** `dataLoadedMsg` path (load finished),
  and `reRoot` (hard reload).
- **Left untouched on the superseded-gen `dataLoadedMsg` branch.** When two `r`
  reloads are in flight (`loadGen` bumped twice), the first load's
  `dataLoadedMsg` arrives with a stale `gen` and is dropped. It must NOT clear
  `softReload`, or the panels would blank until the second (current) load
  finishes — reintroducing the flicker. The newer load owns the flag and clears
  it on its own completion. This never leaves the flag stuck: every load issued
  by `loadCmd` produces exactly one `dataLoadedMsg`, and the latest generation's
  message always reaches the current-gen path.

## Design

Three changes, all in `internal/tui`:

### 1. `Model.softReload bool`

New field. The `r` handler sets it alongside `m.loading = true`. Cleared in the
current-generation `dataLoadedMsg` path (next to `m.loading = false`) and in
`reRoot`. **Not** cleared on the superseded-gen early return (see the lifecycle
note above). The startup load leaves it false (zero value).

`r` handler (model.go:732):

```go
case "r":
    if !m.running {
        m.loadGen++
        m.loading = true
        m.softReload = true
        return m, m.loadCmd()
    }
```

`dataLoadedMsg` handler (model.go:443): clear `m.softReload = false` on the
current-gen (normal) path only, next to `m.loading = false`. Leave the
superseded-gen early return untouched.

`reRoot` (model.go:1949): set `m.softReload = false` alongside its existing
`m.loading = true`, so a repo switch never soft-renders the outgoing repo.

### 2. `View()` blanks only for hard reload

```go
func (m Model) View() string {
    if m.modal != nil {
        return m.render()
    }
    if m.loading && !m.softReload {
        return "gigagit (loading…)\n"   // startup + repo-switch keep the blank screen
    }
    if m.err != nil {
        return "error: " + m.err.Error() + "\n"
    }
    return m.render()
}
```

### 3. Per-panel ⏳ + status hint

`panelLabel` (viewstate.go:517) is the single chokepoint that decorates every
panel title. Today it appends `commitsLoadingGlyph` ("⏳") for the Commits panel
only when `m.commitsLoading`. Generalize: while `m.softReload`, append the same
glyph to **every** panel's title.

```go
if m.softReload {
    base += " " + commitsLoadingGlyph
}
```

(Keep the existing Commits-only `commitsLoading` branch — it still drives the
glyph for feed-only reloads/paging that aren't full soft reloads. Guard against
a double glyph when both are true.)

Status line: prefix `reloading…` using the existing `⏳ ` status-line prefix
pattern (view.go:360) while `m.softReload`.

Graceful degradation for "too little space": the title is already width-clamped
by the existing render path, so on a narrow panel the appended glyph is dropped
automatically — covering the "or not if there is too little space" requirement
with no extra code.

## Behavior decisions

- **Navigation stays live.** Nav keys (j/k, arrows, tab) operate on the
  in-memory model and are not gated by `m.loading`; they keep working during the
  soft reload. This is the whole point of "soft".
- **Write-ops stay gated** by the existing `!m.loading` guards (model.go:739,
  747, 812, 1079, …) — unchanged. Pressing `r` again during a soft reload just
  bumps `loadGen` and supersedes the in-flight load.
- **No dimming.** Stale content renders at full brightness; the ⏳ glyphs + the
  `reloading…` status line are the only cues. (Chosen for minimalism; matches the
  existing `commitsLoading` pattern exactly.)
- **reRoot / startup unchanged** — they keep the blank `loading…` screen.

## Testing

Drive-tests through `Update`/`View` (table-style, matching existing tui tests):

1. After a `r` keypress on a loaded model, `View()` contains panel content (not
   `"gigagit (loading…)"`) and a ⏳ glyph; `m.softReload` is true.
2. `reRoot` still yields the blank `"gigagit (loading…)"` screen
   (`m.softReload` false).
3. `dataLoadedMsg` for the current `loadGen` clears both `m.loading` and
   `m.softReload`; a superseded-gen `dataLoadedMsg` leaves `m.softReload`
   untouched (the newer in-flight load keeps soft-rendering).
4. `reRoot` clears `m.softReload` even when a soft reload was in flight.
5. Status line shows `reloading…` while `m.softReload`, gone after the load.

## Docs to update on completion

- `CHANGELOG.md` (always).
- `README.md` only if the user-facing description of `r` changes (it gains a
  visible ⏳/`reloading…` cue — a one-line note at most).
- No CLI surface change → no `agentskill` bump.
