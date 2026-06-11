# gigagit M2 — Worktree Management — Design

**Date:** 2026-06-11
**Status:** Approved for implementation planning
**Scope:** The "Worktrees" half of M2. The repo-switcher ("Workspaces") is a
separate later feature (Plan B); multi-repo-open-at-once is backlog.

## 1. Summary

Make worktrees a first-class part of gigagit: show them in the UI, mark which
branches have a worktree, and create worktrees from a branch with user-defined,
templated branch names and paths — fast, from one keypress. This is the
monorepo-aligned half of "Workspaces": worktrees of the one big repo.

The feature also introduces gigagit's **configuration system** (global + per-repo
TOML) and a reusable **template engine**, both of which later features build on.

## 2. Decomposition (three sequential plans)

- **A1 — Config + template engine** (pure, no UI). The `config` loader (global +
  repo TOML, repo wins) and the `template` resolver. Fully unit-testable.
- **A2 — Worktree engine + display.** A `git worktree add` verb, a
  `CreateWorktree` engine operation, the **Worktrees panel**, and the
  **has-worktree icon** in the Branches panel. Headless-testable.
- **A3 — Create UX.** The `w`/`W` popup, keybindings, live preview, and
  create-and-switch (TUI re-root).

**Deferred:** removing worktrees; adding a worktree for an *existing* branch
(no new branch); the repo-switcher (Plan B). **Backlog:** multi-repo open at once.

## 3. Architecture — engine boundary preserved

Worktree creation is orchestration and lives in the engine, so all three
frontends (TUI, CLI, future MCP) reuse it.

- `internal/template` — **pure**, no I/O:
  - `Resolve(tmpl string, inputs map[string]string, ctx Ctx) (string, error)`
  - `UserLabels(tmpl string) []string` — distinct `<user:LABEL>` labels, in order,
    so a frontend knows which input fields to render.
  - `SeqNames(tmpl string) []string` — distinct `<seq:NAME:N>` counter names, so
    the create flow knows which counters to read for preview and to bump on
    success.
  - `Ctx` carries the non-`<user:>` inputs (`ParentBranch`, `Repo`), the current
    counter values (`Seqs map[string]int`, supplied by the caller — the resolver
    never mutates them), plus an injected `Now func() time.Time` and a seedable
    `Rand` source, for determinism.
- `internal/config` — loads `~/.config/gg/config.toml` then `<repo>/.gg.toml`,
  merging with repo-wins precedence; exposes a typed `Config`.
- `internal/git` — thin verb:
  - `AddWorktree(ctx, path, branch, startPoint string) error` →
    `git worktree add -b <branch> <path> <startPoint>`.
- `internal/engine` — `CreateWorktree{StartPoint, Branch, Path string}` Operation
  that calls `AddWorktree`, streaming `Progress` (it's a real checkout — §8).

**Boundary rule:** `<user:…>` collection and the live preview are a **frontend
concern, NOT routed through the `Decider`.** The `Decider` models option-lists
(rebase/merge/abort); free-text fields and "edit the name" do not fit it. The
frontend resolves the template (collecting `<user:>` inputs) and hands the engine
a fully-resolved `{StartPoint, Branch, Path}`. The engine op never prompts.

`CreateWorktree`'s fields are sufficient for every frontend: the TUI popup and a
future `gg worktree add` both reduce to "resolve template → call the same op".

## 4. Configuration system

- **Global:** `~/.config/gg/config.toml` (honor `$XDG_CONFIG_HOME`).
- **Per-repo:** committed `<repo-root>/.gg.toml` (so a team shares conventions).
- **Precedence:** repo overrides global, per top-level key.
- **Format:** TOML via `github.com/pelletier/go-toml/v2` (one small, standard dep).
- **Worktree schema (illustrative):**
  ```toml
  [worktree]
  path_template           = "../<repo>.worktrees/<branch>"
  default_branch_template = "b/from-<parent-branch>-<random-alpha:4>"
  branch_templates = [
    "b/from-<parent-branch>-<date:yyyy-MM-dd>-<random-alpha:4>",
    "issue/<user:issue-id>",
    "<user:user>/auto-fix/<user:issue-id>",
  ]
  ```
- Missing files are not errors: absent config yields built-in defaults
  (`path_template` and `default_branch_template` above).

**Local per-repo state (counters):** mutable runtime state lives separately from
the committed config, at `<repo>/.git/gg/state.toml` — inside `.git/`, so git
never tracks it (no merge conflicts, machine-local). It holds the `<seq:NAME>`
counters:
```toml
[seq]
issue = 42
deploy = 7
```
`config` exposes:
- `PeekSeq(repoGitDir, name string) int` — current value (0 if unset), no mutation
  (used to build the preview).
- `BumpSeq(repoGitDir, name string) (int, error)` — atomically increment and
  persist; called once per used counter after a successful create.

The committed `.gg.toml` is read-only at runtime; only `.git/gg/state.toml` is
written.

## 5. Template engine

**Function tokens** (auto-computed, no user input):
- `<parent-branch>` — the start-point branch the worktree is based on.
- `<repo>` — repository name (basename of the worktree root).
- `<branch>` — **path template only**: the resolved new branch name with `/`→`-`
  so it is a safe single directory segment.
- `<date:FMT>` — timestamp; `FMT` uses human tokens `yyyy MM dd HH mm ss`
  (mapped internally to Go's reference layout). E.g. `<date:yyyy-MM-dd HH:mm>`.
- `<random-alpha:N>` — N random lowercase ASCII letters.
- `<random-num:N>` — N random digits.
- `<seq:NAME:N>` — a **persistent per-repo named counter** (`NAME`) that is
  incremented on every successful worktree creation and zero-padded to `N`
  digits. `N` is optional (`<seq:NAME>` = no padding). The counter is stored in
  local repo state, **not** the committed `.gg.toml` (see §4) — it is mutable,
  machine-local, and would otherwise cause merge conflicts. To preserve the
  resolver's purity, the *current* counter value is supplied to `Resolve` via
  `Ctx`; the resolver only substitutes it. The increment + persist happens in the
  create flow **only after a worktree is successfully created** (§7/§11), so
  previews and cancellations never consume a number.

**Input token:**
- `<user:LABEL>` — a free-text value the frontend collects; the same LABEL
  appearing more than once is filled once and reused.

**Rules:**
- The **branch template** resolves to a git branch name, validated with
  `git check-ref-format --branch` (illegal names → clear error).
- The **path template** resolves to the worktree directory, relative to the repo
  worktree root unless absolute.
- Determinism: `<date>`/`<random>` draw from the injected `Now`/`Rand`, so tests
  are reproducible.
- Unknown `<...>` tokens are an error (surfaced to the frontend), not silently
  passed through.

## 6. Default placement

`path_template` default `../<repo>.worktrees/<branch>`. Repo `aaa` at `/work/aaa`,
branch `issue/123` → container `/work/aaa.worktrees/`, worktree dir `issue-123/`
(`/`→`-`). The container is created if absent.

## 7. TUI integration

- **Layout:** the left column becomes **three stacked panels** — Branches,
  Worktrees, Status — beside the Commits panel. The existing height/width-aware
  renderer is extended so the fit-invariant (output never exceeds the terminal)
  still holds with the extra region; each left panel gets a share of body height.
- **Has-worktree icon:** a branch checked out in any worktree shows a marker
  (e.g. `◫`) in the Branches panel, derived from the already-loaded worktree list
  (no extra git call).
- **Worktrees panel:** lists each worktree as `<branch>  <path>` (the current
  worktree marked); selectable, navigable like other panels.
- **`w` / `W` popup** (on the selected Branches-panel branch = `<parent-branch>`):
  fills the default branch template, renders one input field per `<user:LABEL>`,
  shows a **live preview** of the resulting branch and path as you type.
  - **Enter** — create as-is.
  - **`e`** — edit the final branch name freely before creating.
  - **`w`** — create only (stay in the current worktree).
  - **`W`** — create **and switch** (re-root, below).
  - **Esc** — cancel.
- **Create-and-switch = re-root:** `W` re-roots the live `Model` to a new
  `git.Repo` at the new worktree path and reloads all panels — an in-process
  state change (the process does not move; the TUI just operates on a new repo
  root). This "open repo at path" primitive is built cleanly here because **Plan
  B (the repo-switcher) reuses it exactly.**
- **Counter consumption:** the preview reads each `<seq:NAME>` via `PeekSeq`; only
  after the worktree is successfully created does the flow call `BumpSeq` once per
  counter the template used (so cancels, errors, and previews never advance a
  counter). Free-editing the name with `e` does not change which counters the
  *template* referenced.

**Keys:** `w`/`W` are currently unbound. (Terminals collapse Ctrl+letter case, so
`Ctrl+w`/`Ctrl+W` cannot be distinguished — plain `w`/`W` keep case and give the
intended lower=create / upper=create-and-switch split.)

## 8. Monorepo performance (the implied constraint)

`git worktree add` **materializes a full checkout** of the start-point's tree — on
a 20GB head this is seconds-to-minutes and gigabytes written. Therefore:
- `CreateWorktree` is a **streamed, cancellable** Operation (honoring the
  non-blocking invariant): it streams progress and respects `ctx` cancellation.
- **Sparse-checkout:** if the repo has sparse-checkout enabled, the new worktree
  must inherit the sparse set rather than materializing the full tree. A2 detects
  an active sparse configuration and applies it to the new worktree (e.g. via
  `git -C <newpath> sparse-checkout` init/set mirroring the source). Full sparse
  *management* (editing the set) remains M3; worktree-create must simply not
  force a full 20GB checkout when the repo is sparse.

## 9. Resolved semantics

`w`/`W` **create a new templated branch off the selected branch** (the selected
branch is the start-point / `<parent-branch>`). Adding a worktree for an
*existing* branch (no new branch) is a later variant. The has-worktree icon is
pure display, independent of creation.

## 10. Error & edge cases (surfaced as errors, never crashes)

- Resolved branch already exists → error (suggest editing the name with `e`).
- Target worktree path already exists → error.
- Start-point branch already checked out in another worktree → git refuses; the
  op surfaces that clearly.
- Branch name illegal after template resolution → `check-ref-format` error.
- Missing/parent dirs for the container are created; a non-writable location is
  reported.

## 11. CLI

- `gg worktree add [start-point]` — resolves the configured default template
  (prompting on stdin for each `<user:LABEL>`), then calls `CreateWorktree`;
  prints the created branch and path.
- `gg worktree list` — lists worktrees (reuses the worktree loader).
- **Switch caveat:** a child `gg` process cannot `cd` the parent shell, so the
  CLI cannot "switch into" the worktree — it creates and **prints the path**
  (the user `cd`s, or wraps it in a shell function). Re-rooting is TUI-only.
- (Wiring `gg worktree …` into `cmd/gg` and `IsCommand` happens with A3, or as a
  small follow-up — the engine op and template/config are the substance.)

## 12. Testing

- `template`: table-driven, injected `Now`/`Rand` — every function token, `<user>`
  label extraction/reuse, date-format mapping, sanitization, unknown-token error.
- `config`: global-only, repo-only, merged precedence, missing-files-defaults;
  `PeekSeq`/`BumpSeq` round-trip + padding + counter persists across loads and is
  not written to the committed `.gg.toml`.
- `git.AddWorktree` + `CreateWorktree`: real throwaway repos — assert the worktree
  directory and new branch exist, progress streamed, and the error cases in §10.
- TUI: Worktrees panel render + has-worktree icon; popup `Update` (field entry,
  live preview, Enter vs `e`, cancel); the re-root path (Model now points at the
  new repo and panels reload). Fit-invariant test extended for the 3-panel left
  column.
- CLI: `worktree add`/`list` against a temp repo.

## 13. Plan sequence

- **A1** — config + template engine (this spec, §3–§5).
- **A2** — `AddWorktree` verb + `CreateWorktree` op + Worktrees panel + branch icon
  (§3, §6, §7 display, §8, §10).
- **A3** — `w`/`W` popup + create-and-switch re-root + (optional) `gg worktree`
  CLI (§7 interaction, §11).
- Later: worktree removal, existing-branch worktrees, repo-switcher (Plan B),
  multi-repo (backlog).
