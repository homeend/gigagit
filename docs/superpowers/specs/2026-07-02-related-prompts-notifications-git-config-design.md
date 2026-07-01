# Related-option prompts, notification center, git-config explorer — design

Date: 2026-07-02 · Status: approved (brainstorm 2026-07-02)
Scope decision: three stages, implemented and merged one at a time; each stage
is independently usable. Each stage also ships a project skill documenting how
to extend what it built.

Motivation (from the 2026-07-01 perf investigation): the per-batch commit
loading delay is governed by git-side facts the user can fix in one keystroke —
a missing commit-graph file (babel: 150ms→16ms per batch after one 0.18s
`git commit-graph write`) and a `commit_sort` that buys nothing when the graph
is hidden. gg should surface these as actionable recommendations instead of
leaving them to folklore.

---

## Stage 1 — generic related-option prompts

### Behavior

When the user changes a Settings option, gg may ask ONE follow-up question
about a related option. Initial registry entries:

1. **Show graph → off** (and commit sort is currently `date-order`):
   > "Ordering only matters for graph lanes — also switch Commit sort to
   > `plain` (much faster on big repos)?"
   Options: **Yes, set plain** / **Not now** / **No — don't ask again**.
2. **Show graph → on** (and commit sort is currently `plain`):
   > "The graph draws correct lanes only with `date-order` — switch Commit
   > sort back to `date-order`?"
   Same options.

"Yes" runs exactly the code path of the corresponding Settings row (here:
`cycleCommitSort`-equivalent targeted set). "Not now" just closes. "Don't ask
again" suppresses that prompt id permanently (all repos).

### Mechanics

- `internal/tui/related_prompts.go`: a data-driven registry
  `[]relatedPrompt{id, trigger(settingID, newValue, cfg) bool, question,
  options, apply(Model) (Model, tea.Cmd)}`. After a Settings toggle applies,
  the chokepoint consults the registry; the first matching non-suppressed
  entry pushes a `relatedPromptPopup` (layer stack, option list styled like
  the modal Decider; esc = Not now — never trap).
- Suppression store: `prompts.toml` in the gg **state dir** (pattern:
  `internal/searchhist`/`internal/repos` — machine-local UX memory, NOT
  committed config; no `.gg.toml`/settingDocs plumbing). New tiny package
  `internal/promptstate` behind a fixed `Store` interface (atomic-rewrite
  TOML) with TWO record kinds: globally suppressed prompt ids (stage 1 —
  a prompt you never want is never wanted in any repo) and per-repo
  dismissed notice ids keyed by git common dir (consumed by stage 2).
  Prompts are pure UX with no git semantics: the TUI owns the store (like
  searchhist), archtest unaffected.
- The popup footer names the state file so the choice is discoverable and
  resettable.

### Deliverables

- Registry + popup + store + the two show_graph entries.
- Skill: `.claude/skills/adding-related-option-prompts/SKILL.md` — checklist:
  add a registry entry (id naming, trigger precondition against live cfg,
  apply must reuse the Settings row's code path), suppression id lifecycle,
  tests (trigger unit test, suppression round-trip, popup wiring).
- Docs: CHANGELOG, README (Settings section), CLAUDE.md (tui row).

---

## Stage 2 — notification center + commit-graph recommendation

### Behavior

- On repo load (startup and repo switch), gg runs cheap background health
  checks producing zero or more **notices**.
- While unread notices exist, the footer shows a **blinking red `! N notice`**
  segment (blink = style alternation on a dedicated ~800ms tick that runs ONLY
  while unread notices exist; terminal-native blink escape is unreliable).
- **`!`** opens the Notifications dialog (layer-stack popup): list of notices;
  `enter` shows the selected notice's actions (option list); `esc` closes.
  Opening marks all as read (stops blinking). Acting or dismissing removes a
  notice. `!` is a global binding (any panel), inert while a filter/text
  field is capturing input — same routing rules as the `g`/`G` switchers.
- Notice states: unread → read (session) → dismissed (session) or **never for
  this repo** (persisted in the stage-1 state store, keyed by git common dir +
  notice id).

### First check: commit-graph recommendation

Fires when ALL hold:
- repo is big: total size of `.git/objects/pack` ≥ 100 MB (one directory
  stat-walk; no git subprocess);
- no `.git/objects/info/commit-graph` file and no `commit-graphs/` chain dir;
- `fetch.writeCommitGraph` unset in local AND global scope (existing
  `git.ConfigGet`).

Notice: "Commit browsing can be ~10× faster in this repo" with actions:
1. **Write commit-graph now + keep it fresh** — runs new op
   `engine.WriteCommitGraph` (`git commit-graph write --reachable`,
   LockMode Read, progress via normal op events/busy line — it can take
   ~a minute on a 1.4M-commit repo), then `git config --local
   fetch.writeCommitGraph true` via `engine.SetGitConfig`.
2. **Enable auto-refresh only** — just the config set (graph appears on next
   fetch/gc).
3. **Not now** — dismiss for this session; re-evaluated next load.
4. **Never for this repo** — persisted.

### Settings entry

Settings `,` gains **"Commit-graph: <state>"** where state is one of
`present, auto-refresh on` / `present, auto-refresh off` / `missing`.
`enter` applies the same write+enable action (same code path as notice
action 1). This is the "add option in menu to set it" requirement.

### Mechanics

- `domain.RepoHealth(ctx)` query: pack bytes, commit-graph presence,
  `fetch.writeCommitGraph` effective value — all cheap; runs as a background
  cmd after load, never blocks first paint.
- New git verb: none needed for detection (file stats via GitCommonDir);
  new engine ops: `WriteCommitGraph{}`, `SetGitConfig{Scope, Key, Value}`
  (generalizes the identity feature's config write; identity's `SetIdentity`
  stays as-is for now).
- TUI: `internal/tui/notify.go` (notice model, tick, footer segment,
  dialog popup). Footer + help both advertise `!` (advertise-in-both
  convention).

### Deliverables

- Notification framework + commit-graph check + Settings entry + `!` dialog.
- Skills:
  - `.claude/skills/adding-notifications/SKILL.md` — how to add a health
    check/notice: detection in `domain.RepoHealth` (must be cheap /
    stat-level), notice id + copy, actions must reuse existing ops, dismissal
    semantics, tests (detection unit, action wiring, never-persist).
  - `.claude/skills/updating-git-config-options/SKILL.md` — the whole
    git-config write workflow: `git.ConfigGet/ConfigSet` verbs (scopes),
    `engine.SetGitConfig` op (why writes are ops: repogate, events, oplog),
    `domain.Execute` from a surface, argv/FakeRunner tests, when to use
    local vs global scope, and how the three surfaces (notice action,
    Settings row, explorer) share this one path.
- Docs: CHANGELOG, README (notifications + `!`), CLAUDE.md (tui + engine rows).

---

## Stage 3 — searchable git-config explorer

### Behavior

Settings → **"Git config explorer"** opens a wide full-height popup:
one row per key git knows. Columns: **key | local | global | default**.
Unset scopes render an explicit dim `(unset)`. `/` filters (move-while-typing
per house convention), `z` cycles display modes, esc closes.

Curated rows (~60 common keys, including `fetch.writeCommitGraph`,
`core.commitGraph`, `gc.writeCommitGraph`, `core.fsmonitor`,
`core.untrackedCache`, …) additionally show a one-line description + real
default, and support **`l` set local / `g` set global / `u` unset** (standard
text field; bool/enum keys get an option list; unset confirms). Non-curated
rows are read-only; default column shows `—`.

### Mechanics

- Data on open (background cmd, spinner in title): `git help -c` (full key
  list) + `git config list --show-scope` (all set values, both scopes
  distinguished). Merge into rows.
- Curated table: new pure package `internal/gitconfdocs` — data only
  (key → default, description, kind: bool/enum/string/int), unit test guards
  that every curated key still exists in `git help -c` output (staleness
  gate, skipped if git absent).
- Writes: `engine.SetGitConfig` (from stage 2); unset = new
  `git.ConfigUnset` verb (`git config --unset`), routed through the same op
  (Value "" + Unset flag) — decided: extend `SetGitConfig` with `Unset bool`
  rather than a second op.
- The explorer is the third surface over the same config-write path.

### Deliverables

- Explorer popup + `internal/gitconfdocs` + unset support on the op.
- Skill update: `updating-git-config-options` gains the explorer surface +
  curated-table maintenance section (how to add a curated key).
- Docs: CHANGELOG, README (Configuration + Settings), CLAUDE.md (package map
  row for `gitconfdocs`).

---

## Cross-cutting

- **One write path:** notice action, Settings commit-graph row, and explorer
  set-actions all go through `engine.SetGitConfig` / `engine.WriteCommitGraph`
  via `domain.Execute` — never ad-hoc `git config` calls from the TUI
  (archtest keeps enforcing tui ↛ git).
- **Never trap:** every popup esc-closes; prompts default to "Not now".
- **Testing:** TDD throughout; FakeRunner argv assertions for new verbs/ops;
  real-git `t.TempDir()` tests for RepoHealth + explorer data merge; TUI
  wiring tests per house style.
- **Out of scope:** notification sources beyond the commit-graph check
  (framework is generic; more checks come later); editing non-curated keys;
  `%(default)` parity with git docs beyond the curated set; MCP surface.

## Stage order and merge gates

1. `feat/related-prompts` — stage 1 + its skill.
2. `feat/notification-center` — stage 2 + its two skills (branched after 1
   merges).
3. `feat/git-config-explorer` — stage 3 + skill update (after 2 merges).

Each: full `./test.sh race` green → user verifies in a real repo → user
merges.
