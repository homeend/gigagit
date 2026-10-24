# gg web G — the TUI's create-worktree dialog

Parity row "Worktree create options" in `docs/web-tui-parity.md`. The TUI is
the spec (`internal/tui/worktree_popup.go`); where a browser differs, a
ruling says what stands in.

## The TUI

One popup behind every "new worktree" entrance:

- **Branch** (`w` on a branch, `W` = the same with create & switch as
  enter's default): the branch is the selection (checked out as is); only
  the PATH template's `<user:LABEL>` fields and `<seq>` counters apply.
  Title `Create worktree for <branch>`; once `e` renames it,
  `Create worktree from <branch>` and a NEW branch is cut from the selection.
- **Commit** ("Create worktree from this commit"): start = the sha, the
  branch name is typed (prefill `<current-branch>_<short7>`, `wt_<short7>`
  when detached), the dialog opens editing it. A mode line
  `start:  at this commit | at parent, changes staged | at parent, changes
  unstaged` cycles with `m`; a root/merge commit pins it with
  `(root/merge commit — at this commit only)`.
- **Tag** (`check out as a new worktree`) and **remote branch** ("Create
  worktree from <remote>"): start = the ref, prefill the tag name / the
  de-prefixed branch, opens editing it. No mode line.
- Every non-branch entrance's labels come from BOTH the default branch
  template and the path template; an empty name previews the branch template.
- `<random>` and `<date>` are fixed when the popup opens (seed + now), so
  the preview never jitters and the created names are what was shown.
  `<seq>` counters are PEEKED at open and BUMPED only after a successful
  create: the path template's when the path was not hand-edited, plus a
  picked prefix's.
- Lines: one `> LABEL: value` field per label, `branch: …`, `path:   …`,
  `⚠ <error>` when the templates cannot resolve, `[x] run post-create hook
  ([h] toggle)` when a hook is configured, the mode line.
- Keys. Fields: type, tab/enter next field (the last one → actions), esc
  cancels the dialog. Actions: `e` edit name, `E` edit path, `p` use a
  prefix (seeds the name editor with the resolved prefix), `w` create, `W`
  create & switch, enter = the entrance's default, `h` hook, `m` mode, esc
  cancel. Name/path editor: enter done (an emptied name falls back to the
  selection on a branch; an emptied path reverts to the template), esc
  discards the edit.
- Hint lines (verbatim): `[type] value  [tab/enter] next field  [esc]
  cancel` · `[type] edit name  [enter] done  [esc] discard` · `[type] edit
  path  [enter] done  [esc] discard` · `[w] create  [W] create & switch  [e]
  edit name  [E] edit path  [p] use a prefix  [esc] cancel` (with `[h] hook`
  before `[esc]` when a hook is configured; opened by `W`: `[enter/W] create
  & switch  [w] create only  …`).
- Create refuses with `branch name required` (a non-branch entrance with no
  name) or `cannot create: <error>`; create & switch re-roots at the new
  worktree after a successful create.

## The web today

Chained `openPrompt`s: a branch name, then a path prefilled by the
client-side guess `<main>-<branch>`; no template, fields, counters, prefix,
hook toggle or create & switch. The commit menu's keep modes are two extra
rows with their own prompts. No remote-branch entrance.

## Port

- **Domain** `PreviewNewWorktree(ctx, NewWorktreeSpec) NewWorktreePreview`:
  the TUI's `recompute` — labels, resolved branch and path, error — over
  the given templates, fixed seed/now, peeked counters, `<repo>` = the main
  worktree's name. `NewWorktreeLabels` and `WorktreeSeqNames` pass
  `internal/worktree` through (the `PrefixSeqNames` pattern).
- **`POST /api/worktree/preview`** (writeGuard + MaxBytesReader, like
  `/api/prefixes/resolve`): `{from: branch|commit|tag|remote, start,
  branch, path, inputs, seed, now}` → `{labels, branch, path, error, seed,
  now, hook, existing}`. The first call carries no seed: the server picks
  seed + now and the page echoes them back (a decimal string; a uint64 does
  not fit a JS number). Templates come from the effective config.
- **`create-worktree`** gains: `remote` (a start checked against a fresh
  remote-branch read), `mode` (commit only, through `worktreeKeepModes`),
  `no_hook` (skip the post-create hook — the TUI's `h`), `path_seq` (the
  path template was used: its counters, derived server-side from the
  config, bump after success), `prefix_id`/`prefix_scope` (that prefix's
  counters bump after success). On a branch start an edited name cuts a NEW
  branch from it (`CreateWorktree{StartPoint: branch}`), the same name
  checks it out. The done event carries `path` (the created worktree).
- **`/api/prefixes/resolve`** takes an optional `parent`: the dialog's start
  point stands in for `<parent-branch>` (the TUI resolves a prefix against
  the popup's own context).
- **Page** `wtnew.js`: the dialog (`#wtnew`), the TUI's lines, states, keys
  and hint words, with buttons for every key. Every edit re-previews; a
  stale reply is dropped. Create & switch follows the op and calls
  `doReroot(ev.path)` on success.
- **Entrances**: the branch menu's "create worktree for this branch…" plus
  "create worktree & switch…" (the TUI's `W`), offered for any branch (the
  TUI's `w` gate: a branch with a worktree can still cut a new branch); the
  commit menu's "create worktree here…" with the mode line; the tag menu's
  "check out <tag> as a new worktree…"; a new remote-branch row "create
  worktree from <remote>…".
- **Ruling (keep rows):** the commit menu's two "worktree here, keeping the
  change staged/unstaged…" rows go: the TUI has one entrance with a mode
  line, and the dialog's `m` reaches both. `create-worktree-keep` stays on
  the wire (its tests pin it). Cost if wrong: two clicks become three.
- **Ruling (prefix random/date):** a prefix's `<random>`/`<date>` resolve
  server-side per call, not with the dialog's fixed seed. Cost if wrong: a
  `<random>` prefix differs from what a second pick would show.
- Help: the worktree help entry describes the dialog and its keys.

## Out of scope

The TUI's switch-dirty "worktree" answer (the web's switch has no dirty
ask); worktree move/rename (already ported).
