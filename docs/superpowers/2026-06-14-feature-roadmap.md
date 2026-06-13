# gigagit — refined feature roadmap (2026-06-14)

Source: a feature braindump from the maintainer, refined into discrete
sub-projects to execute one at a time. **This is a backlog, not an
implementation plan.** Each feature below gets its own
brainstorm → spec → plan → build cycle when we reach it; this document pins
*scope, integration point, dependencies, sequence, and the cross-cutting
decisions* so those cycles start from solid ground.

## How to read each entry

- **Goal** — one line.
- **Integration** — where it plugs into the current codebase.
- **Approach** — the recommended default (chosen here so the brainstorm starts
  from a position, not a blank page); alternatives noted.
- **Depends on** — hard prerequisites among these features.
- **Open at brainstorm** — feature-internal decisions deliberately deferred
  (earlier features inform them).
- **Blast radius** — merge-conflict risk against other branches: **low** =
  mostly new files; **medium** = touches `model.go` panel/route; **high** =
  rewrites imports or all key routing or every view. Several worktrees are
  active (`fix-junie-skill`, `tui-blame-view`, `tui-tooltip`, `q-quit-base`),
  so high-blast items are scheduled for quiet windows.
- **Size** — rough: S / M / L.

## Cross-cutting decisions (settled here)

1. **Shared index/commit foundation.** Staging, amend, hunk-staging, and
   conflict-resolution all need git index plumbing (`add`, `restore --staged`,
   `apply --cached`, `commit --amend`, `checkout --ours/--theirs`). Build it
   **once** in F1 and let F2/F3/F4 extend it — not four times. *(This is the
   one cross-cutting question explicitly converted into the roadmap below.)*
2. **Keybindings: assign now, configure later.** Each feature picks specific
   keys as a one-line decision; the remappable keybinding-config system is its
   own late sub-project (F9). Rationale: don't build the indirection layer
   before the new keys that justify it exist.
3. **Shelve and bookmarks are two sub-projects** (F5, F6), not one. Different
   data models — shelve stores file **content** to reapply; bookmarks store
   **references** (path@rev) to navigate/compare. They share only a
   "materialize a file's bytes at a rev" helper and the existing mark
   primitive.
4. **Persistence pattern.** Bookmarks (and the shelf index) persist via the
   established `internal/repos` XDG-state pattern, keyed per repo. Shelf file
   *content* lives under the git common dir (see F5). *(Default; confirm at
   each brainstorm.)*

## Known key-binding collisions to resolve (per-feature, one line each)

Current bindings already in use: `r` = reload-all-panels, `R` = repo switcher,
`p` = pull, `m` = mark, `l` = commit-files view, `h` = file history,
`P` = push, `S` = stash, `u` = undo, `o` = order, `,` = settings.
- F5 wants `p` for shelf "pick" — collides with pull. Pick a free key (e.g.
  shelf under a prefix, or reassign).
- F8 wants `r`/`R` for reload-current / reload-all — collides with today's
  `r`(reload-all) and `R`(repo). R=repo needs a new home.
These are resolved inside each feature's brainstorm, not globally up front.

---

## Features

### F1 — Staging surface + index/commit verb foundation
- **Goal:** stage/unstage individual changed files from a dedicated surface;
  lay the index/commit verb foundation the staging cluster shares.
- **Integration:** new git verbs in `internal/git` (`StagePaths`,
  `UnstagePaths` via `restore --staged`, later `apply --cached` for F3);
  wrap as engine ops behind `domain.Execute`; add to the `engine.GitOps`
  interface. New TUI surface (panel vs full-screen view — see open). Today only
  `commit -a` and `ResetSoft` exist; index manipulation is greenfield.
- **Approach:** start with whole-file stage/unstage; model the staged/unstaged
  split off the existing `model.WorkingTreeStatus`. Reuse the diff view to show
  a selected file's staged-vs-worktree diff.
- **Depends on:** nothing.
- **Open at brainstorm:** UI form — new 4th panel, a full-screen staging view
  (view-stack), or a lazygit-style staged/unstaged two-pane. Which staged vs
  unstaged diff to show on selection.
- **Blast radius:** medium (model.go panel enum + Update routing). **Size:** L.

### F2 — Commit & amend
- **Goal:** commit the staged index with a typed message; amend the last
  commit — edit its message, and/or fold newly-staged changes into it.
- **Integration:** verb `commit` (from index, no `-a`) + `commit --amend`
  (with/without message edit); a commit-message input popup (popup checklist in
  `adding-tui-windows`). Engine op + CLI surface (`gg commit` already exists —
  extend for amend).
- **Approach:** message-edit popup; "amend" = `commit --amend` reusing or
  replacing the message; "add to last commit" = stage (F1) then amend. Guard
  amend on pushed commits with a `DecisionRequest` warning (rewrites history).
- **Depends on:** F1.
- **Open at brainstorm:** multi-line message editing in the popup; amend-pushed
  warning copy.
- **Blast radius:** low–medium. **Size:** M.

### F3 — Hunk selection for staging
- **Goal:** stage/unstage individual hunks (not just whole files).
- **Integration:** `git apply --cached` (and `--reverse` to unstage) fed a
  constructed patch of the selected hunks; hunk-level selection UI layered on
  the F1 staging surface + the diff renderer (which already parses diffs).
- **Approach:** reuse the diff view's hunk structure; select hunks, build the
  minimal patch, apply to the index. The hardest piece (patch construction,
  partial-hunk edge cases) — keep line-level splitting out of v1.
- **Depends on:** F1 (and reuses the diff renderer).
- **Open at brainstorm:** patch construction strategy; whether to support
  line-level (vs hunk-level) staging at all.
- **Blast radius:** low (extends F1's surface). **Size:** L.

### F4 — Conflict resolution
- **Goal:** resolve merge/rebase/pull conflicts inside gg.
- **Integration:** consumes the conflict states the SmartMerge/SmartRebase/
  SmartPull ops already produce (`in_progress`, conflicted files in
  `WorkingTreeStatus`). New verbs: list conflicted files, `checkout --ours`/
  `--theirs`, mark resolved (`add`), continue/abort the in-progress op. A
  full-screen conflict view (view-stack, diff-view template).
- **Approach:** start with per-file ours/theirs + mark-resolved + continue/
  abort; reuse the conflict-decision plumbing (`merge-conflict`/
  `rebase-conflict`) the smart ops emit.
- **Depends on:** nothing hard (independent of staging), though shares
  "mark resolved = stage" with F1's verbs — sequence after F1 to reuse them.
- **Open at brainstorm:** depth — per-file ours/theirs vs external mergetool
  launch vs in-TUI hunk-level conflict editor.
- **Blast radius:** low (new view + new verbs). **Size:** L.

### F5 — Shelve (file content stash)
- **Goal:** copy file(s) from a commit / staged / unstaged into a named shelf;
  reapply shelved files into the working dir (`p` = pick mode).
- **Integration:** a shelf store under the git common dir (e.g.
  `<gitcommondir>/gg/shelf/`), index persisted via the `repos`-style pattern;
  source the bytes with the shared "materialize file at rev" helper; reapply by
  writing into the working tree as unstaged. New surface to browse/pick the
  shelf.
- **Approach:** per-repo shelf; copy = snapshot bytes + metadata (origin
  path/rev); pick = write into a chosen dir as unstaged. Survives checkouts
  (unlike git stash, this is file-granular and non-destructive to the tree).
- **Depends on:** the "materialize file at rev" helper (trivial; also used by
  F6).
- **Open at brainstorm:** shelf key choice (the `p` collision with pull); shelf
  scope (per-repo vs global); conflict behavior when pasting over an existing
  file.
- **Blast radius:** low (new store + new surface). **Size:** M.

### F6 — Bookmarks (cross-surface file references)
- **Goal:** mark files anywhere (status / staged / unstaged / branches /
  worktrees / file history); jump back to a bookmark, compare two bookmarks,
  and paste a bookmarked file as unstaged into a chosen dir.
- **Integration:** generalize the single `m` mark (`mark.go`,
  stable-identity keys) into a persistent, multi-entry bookmark set keyed by
  (surface, path, rev); persisted via the `repos` XDG-state pattern. Compare =
  feed two bookmarks into the diff view; paste = the F5 "materialize + write
  unstaged" path.
- **Approach:** bookmarks are *references* (path@rev), not content; resolve
  content lazily on jump/compare/paste.
- **Depends on:** the mark primitive (exists) and the materialize helper
  (shared with F5). Sequence after F5 so the helper exists.
- **Open at brainstorm:** bookmark key(s) and the jump/compare/paste UX; how a
  bookmark to a working-tree file behaves after the file changes.
- **Blast radius:** low–medium (mark.go + a bookmark store + diff-view entry).
  **Size:** M.

### F7 — Op lifecycle: in-transition markers + surgical refresh
- **Goal:** during a long op (create/remove worktree, etc.) block other
  modifying actions, visually mark the element under change as "in transition,"
  and on completion refresh the source window and all affected windows.
- **Integration:** today `opsIdle()`/`m.running` already *block* new ops, and
  `opFinishedMsg` already refreshes by reloading **everything** via
  `loadCmd()`. This feature adds (a) a per-element "in transition" flag the
  affected panel renders, and (b) targeted refresh of only the affected
  windows instead of a full reload. This is the visible slice of the deferred
  CQRS-stage-5 "domain owns data, propagates to UI" narrative.
- **Approach:** tag the op with the element(s) it touches; render those rows
  dimmed/spinnered; on finish, invalidate+reload just the affected domain
  queries.
- **Depends on:** nothing (full-reload already correct — this is UX+perf, not a
  prerequisite for other features).
- **Open at brainstorm:** which ops declare which affected elements; spinner vs
  dim styling; whether to keep full-reload as the fallback.
- **Blast radius:** medium (op flow in model.go + panel render). **Size:** M.

### F8 — Reload keys (`r` current window, `R` whole data source)
- **Goal:** `r` reloads the focused window's content; `R` reloads the whole
  data source.
- **Integration:** today `r` = reload-all-panels and `R` = repo switcher. Remap:
  `R` → reload-all (whole data source), repo switcher → a new key; `r` →
  focused-window-only reload (needs per-window reload, related to F7's targeted
  refresh).
- **Approach:** bundle with F7 — both need the targeted-refresh machinery and
  both touch the reload key map. Resolve the `R`=repo relocation here.
- **Depends on:** F7 (targeted refresh) for the `r` = current-window semantics.
- **Open at brainstorm:** new home for the repo switcher key; what
  "current window" reload means per surface.
- **Blast radius:** low. **Size:** S.

### F9 — Keybinding configuration
- **Goal:** remappable keybindings with win/mac/linux profiles.
- **Integration:** an action-indirection layer over today's hardcoded key
  switch in `Update` + the `footer.go`/`avail.go` binding registry; config in
  `.gg.toml` (extend `internal/config`). Keys resolve through a named-action
  table rather than literals.
- **Approach:** **late** — after F1–F8 have introduced their keys, so the
  action set is stable and we don't refactor twice. Its own brainstorm.
- **Depends on:** ideally after the key-adding features (F1–F8).
- **Open at brainstorm:** action-naming scheme; config schema; per-OS profile
  resolution; conflict detection.
- **Blast radius:** high (touches all key routing). **Size:** L.

### F10 — Multilanguage / i18n
- **Goal:** translatable UI strings + locale selection.
- **Integration:** externalize user-facing strings (TUI labels, hints, footer,
  help, CLI messages) behind a message catalog; locale from config/env.
- **Approach:** **last** — touches every view; best done when the surface is
  stable. Its own brainstorm.
- **Depends on:** feature surfaces being largely settled.
- **Open at brainstorm:** catalog format; which strings are in scope (TUI only
  vs CLI too); pluralization/RTL needs.
- **Blast radius:** high (every view). **Size:** L.

### F11 — `go install` (module rename + distribution)
- **Goal:** `go install github.com/homeend/gigagit@latest` works.
- **Integration:** rename the module path `github.com/gigagit/gg` →
  `github.com/homeend/gigagit` (rewrites every import); decide entrypoint shape;
  verify `-ldflags` buildinfo injection still resolves; update docs.
- **Approach:** **recommended** — add a root-level `package main` so the bare
  `…/gigagit@latest` path works exactly as written (alternative: keep main at
  `cmd/gg` and document `…/gigagit/cmd/gg@latest`). *(This is an open decision —
  confirm at brainstorm.)*
- **Depends on:** nothing — but **HIGH blast radius**: it rewrites every import
  path. **Schedule for a quiet window** (few/no active worktrees) so open
  branches don't all have to rebase. Not a "quick warmup."
- **Open at brainstorm:** root-main vs `cmd/gg` suffix; whether to keep a
  `github.com/gigagit/gg` alias.
- **Blast radius:** high (all imports). **Size:** M.

---

## Recommended execution sequence

Ordered by dependency, value, and blast radius (low-blast items are
parallel-safe against the active worktrees; high-blast items wait for quiet
windows):

**Phase 1 — Staging cluster (highest user value; F1 is the shared foundation):**
1. **F1** Staging surface + index/commit foundation
2. **F2** Commit & amend
3. **F3** Hunk selection

**Phase 2 — Differentiators (low-blast, parallel-friendly):**
4. **F4** Conflict resolution
5. **F5** Shelve
6. **F6** Bookmarks

**Phase 3 — Lifecycle & freshness:**
7. **F7** Op lifecycle (in-transition + surgical refresh)
8. **F8** Reload keys `r`/`R` (bundled with F7)

**Phase 4 — Cross-cutting infra (high-blast; quiet windows, own brainstorms):**
9. **F11** Module rename / `go install` — slot into any quiet window; independent
   of feature work, so timing is flexible.
10. **F9** Keybinding configuration (after F1–F8 keys exist)
11. **F10** Multilanguage / i18n (last; surface stable)

**Next action when ready:** brainstorm **F1** (staging surface + index/commit
foundation) — it unblocks F2/F3 and anchors the conflict verbs F4 reuses.
