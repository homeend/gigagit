# Worktree switch guard + cross-environment repair — design

**Date:** 2026-07-19 · **Status:** approved direction (user: "guard and repair"), spec for planning

## Problem

A linked git worktree is held together by two absolute-path records written at creation
time in the creating environment's notation: the main repo's
`.git/worktrees/<name>/gitdir` (→ the worktree) and the worktree's `.git` file (→ back
to the repo). A repo shared between WSL and Windows (`/mnt/t/…` vs `T:\…` — same disk,
two spellings) can therefore hold worktrees whose linkage the *other* environment cannot
read: `git worktree list` still prints the recorded strings, the gg Worktrees panel
shows them, and switching dies with a raw
`error: git worktree list failed (exit -1): chdir /mnt/t/…: The system cannot find the path`
(observed on `gg.exe`; the mirror case exists under WSL for Windows-created worktrees).
Translation at the chdir level alone cannot fix it — git itself then reads the
foreign-notation back-link and refuses — so the linkage must be *repaired*
(`git worktree repair`), which rebinds it to the current environment's notation
(and unbinds it from the other, until repaired back).

## Behavior (user decision: guard AND repair)

Before any TUI switch (`reRoot`), gg checks the target:

1. **Reachable** (stat succeeds) → switch as today. Zero behavior change.
2. **Unreachable, but a cross-environment translation of the path exists on disk**
   (e.g. on Windows the recorded `/mnt/t/others/x` translates to `T:\others\x`, which
   stats) **and the target is a worktree switch** → a modal offer:
   *"This worktree is linked for another environment. Repair it for this one? It will
   stop working there until repaired back."* — options **repair / cancel** (esc =
   cancel, never-trap). **repair** runs `git worktree repair <translated-path>` as an
   engine op from the current (main) repo, then, on success, switches to the
   **translated** path. **cancel** stays put, no change.
3. **Unreachable, no usable translation** (deleted dir, unplugged drive, foreign
   notation on a machine that has no counterpart, repo-switcher entries) → the guard:
   refuse the switch, stay exactly where we are, translated status message
   `cannot switch: %s is not reachable from here`. No teardown, no raw chdir error.

The repair offer applies only where the target is a worktree of the current repo (the
Worktrees panel enter site). The repo switcher and the post-create jump get the plain
guard (a just-created worktree is always native-notation, so the guard is a no-op
there; a foreign repo-switcher entry is refused — repairing someone else's repo
linkage from a switcher row is out of scope).

## Components

### 1. Pure cross-environment path logic (`internal/tui/crossenv.go`)

Testable on any platform via injected parameters — no `runtime.GOOS` reads inside the
logic:

- `translatePath(goos, path string) (string, bool)` — pure notation translation:
  on `windows`, `/mnt/<x>/rest` → `<X>:\rest`; on `linux` (WSL case), `<X>:\rest` or
  `<X>:/rest` → `/mnt/<x>/rest`; anything else → not translatable. It does NOT stat.
- `type switchVerdict int` — `switchOK` / `switchRepairable` / `switchUnreachable`.
- `checkSwitchTarget(stat func(string) error, goos, path string) (switchVerdict, string)`
  — stat the path (OK), else translate + stat the translation (Repairable + the
  translated path), else Unreachable. Production callers pass a thin
  `func(p string) error { _, err := os.Stat(p); return err }` and `runtime.GOOS`.

WSL detection nuance: on linux, a `C:\…` string can only mean a Windows-created
worktree; translating and statting `/mnt/c/…` is harmless on non-WSL Linux (the stat
simply fails → Unreachable). No osrelease probe needed — the stat IS the detection.

### 2. `git worktree repair` verb + engine op

- Verb `Repo.WorktreeRepair(ctx, path string) error` — `git worktree repair <path>`,
  one invocation, run from the current repo (repairs both link directions for the
  named worktree; verified behavior in the real-git test via the moved-worktree
  scenario, which is notation-independent and reproducible on Linux).
- Op `engine.RepairWorktree{Path string}` — streams nothing fancy: progress step
  `repairing worktree`, runs the verb, summary `repaired worktree link: %s` (dual
  i18n channel via the msg.go helpers, stage-5 contract). Default `TreeWrite` lock
  (it rewrites `.git` metadata; conservative). No decisions — the confirm is TUI-side.

### 3. TUI wiring (`internal/tui`)

- The three `reRoot` production call sites (worktrees panel `model.go:~1460`, repo
  switcher `~2044`, post-create jump `~1269`) route through a new
  `m.guardedReRoot(path string, offerRepair bool) (tea.Model, tea.Cmd)`:
  - `switchOK` → `m.reRoot(path)`.
  - `switchRepairable && offerRepair` → frontend-only modal (the `onResolve`
    pattern, `op.go:161`) with options `repair`/`cancel`; `repair` →
    `m.pendingRepairSwitch = translated` + `startOp(engine.RepairWorktree{Path:
    translated})`; on `opFinishedMsg` success the pending path is captured-and-
    cleared and `reRoot(translated)` runs (the `pendingPushTags` capture-only-on-
    success pattern; cleared unconditionally, also by `reRoot` itself).
  - anything else → `m.statusMsg = i18n.T("cannot switch: %s is not reachable from here", path)`,
    no state change.
  - Sites: worktrees panel passes `offerRepair: true`; repo switcher and post-create
    jump pass `false`.
- `opAffectedSources`: `RepairWorktree` → `{worktrees}` (its admin metadata changed;
  nothing else).

### 4. i18n (adding-translations skill applies)

New keys ×4 bundles (ja/ko/zh/ru): the status message, the modal prompt
(`This worktree is linked for another environment. Repair it for this one? It will stop working there until repaired back.`),
`optionDisplayName` cases + bundle entries for the new `"repair"` option value
(`"cancel"` already exists), the engine op's summary format + `repairing worktree`
step (engine_prose gates), and the footer/help text if any binding surfaces. All
enforced by the existing AST gates (`options_vocab_test`, `engine_prose_test`,
`i18n_scan_test`).

## Errors

- Repair op failure → the standard `friendlyOpError` frame; `pendingRepairSwitch`
  cleared, no switch attempted, current session intact.
- The guard never errors — it refuses with the status message.

## Testing

- `crossenv` pure tests: translation table (both directions, drive-letter case
  handling, non-translatable inputs), `checkSwitchTarget` verdicts with an injected
  stat (ok / translated-ok / both-fail).
- Verb + op: real-git moved-worktree scenario — create a worktree, `mv` its
  directory, assert git can't use it, run the op with the new path, assert
  `git worktree list` shows the new path and the worktree works. FakeRunner argv test.
- TUI: `guardedReRoot` unit tests — OK path reRoots; Unreachable sets the translated
  status and leaves focus/repo untouched; Repairable+offer pushes the modal, `cancel`
  stays, `repair` dispatches the op and, on synthetic `opFinishedMsg{Changed:true}`,
  reRoots to the translated path; `Changed:false`/error does not chain.
- Bundles/gates: the i18n AST gates cover the rest.
- `./test.sh race` before merge.

## Documentation

`CHANGELOG.md`; `CLAUDE.md` (engine ops list + tui row sentence); `README.md` only if
it documents worktree switching today (check; likely no change). `using-gg.md`
untouched (no CLI surface change).

## Out of scope

Automatic/silent repair (always ask); repairing repo-switcher entries; a CLI
`gg worktree repair` verb (add later if asked); relative-links
(`worktree.useRelativePaths`, needs git ≥ 2.48 — worth revisiting when the user's
gits upgrade, noted here as the eventual real fix); dimming unreachable rows in the
panel list (later polish).
