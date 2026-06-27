# `gg config populate` — design

**Date:** 2026-06-27
**Status:** approved (brainstorming)
**Branch:** `feat/config-populate`

## Problem

gg ships new config settings over time (`[ui]`, `[refresh]`, `[debug]`, …). A
user who wrote a `.gg.toml` (or global `config.toml`) months ago has a config
file that is missing every setting added since. Today the only way to see the
new settings in their file is `gg config init`, which writes the **whole**
commented template and refuses to run if the file exists (`--force` overwrites
it, destroying the user's overrides). There is no "top up my existing file with
the settings that shipped after I wrote it" command.

## Goal

Add `gg config populate (--repo | --global)`: a **purely additive** merge that
adds every supported setting not already present in the file, **as a commented
line** carrying its default, description, and a `[populated]` marker — while
leaving everything the user already has completely untouched.

## Non-goals

- Not a value editor — it never changes an existing line (active override or
  commented doc line).
- Not a reformatter — it preserves the user's layout, comments, blank lines,
  and any keys gg does not recognize.
- Does not change effective config: everything it writes is commented, so it
  can never override a default or a higher layer.

## Behavior

### Surface

```
gg config populate (--repo | --global)
```

- Exactly one of `--repo` / `--global` is required (same rule as
  `gg config init`); passing neither or both is a usage error (exit 2).
- `--repo` resolves the git toplevel via `svc.TopLevel` and targets
  `<top>/.gg.toml`, falling back to the workdir when not inside a repo
  (identical to `cmdConfigInit`).
- `--global` targets `config.DefaultGlobalPath()`
  (`$XDG_CONFIG_HOME/gg/config.toml` → `~/.config/gg/config.toml`),
  `MkdirAll`-ing the parent.
- **No `--force`.** populate never overwrites a user value, so re-running is
  always safe.
- A **missing target file** is created and fully populated (every key added,
  commented, marked) — equivalent to `config init` plus `[populated]` markers.

### Per-setting rule

Source of truth: the existing `settingDocs` registry in
`internal/config/template.go` (no second list; `TestSettingDocsCoverAllFields`
already guarantees it covers every config field).

For each entry in `settingDocs`:

| Current state of the key in the file | Action |
|---|---|
| Any line for the key exists — active (`wheel_step = 5`) **or** commented (`# wheel_step = 3`), within its section | **Untouched** |
| Key entirely absent, **scalar** default | Add commented: `# wheel_step = 3   # mouse-wheel scroll step, in rows [populated]` |
| Key entirely absent, **no scalar** default (`branch_templates`, `footer_actions`, `menu_actions`, `commit_graph_pan_step`) | Add commented, value-less: `# branch_templates   # …default: none [populated]` |

- "Present" is decided per `(section, key)`: while walking lines and tracking
  the current `[section]` header, a key counts as present if a line assigns it
  (active or commented) under its registry section. Reuse `lineAssignsKey`.
- Added keys are grouped under their existing `[section]` header (inserted
  after the header). A section with no header yet gets a fresh
  `[section]` block appended (blank-line separated when the file is non-empty),
  in `settingDocs` section order (`worktree`, `ui`, `debug`, `refresh`) —
  matching `Template()`.
- **Marker:** the terse `[populated]`, appended to the line's trailing
  description comment.
- **Idempotent:** a second run finds every key present and writes nothing
  new (the file content is byte-identical → still rewritten atomically, which
  is fine, or short-circuit if nothing changed; see Open question 1).

### Why commented-only

The user chose commented output so populate is behaviorally inert: it refreshes
the *documentation block* of the file with newly-shipped settings without ever
altering effective config. This also sidesteps the inverted-polarity /
zero-value settings (`refresh.status = 0`, `show_eol_only_changes = false`,
`debug.log_operations = false`) — pinning them active would be a no-op anyway,
and commenting them keeps the "track the default across versions" property the
config header advertises.

## Implementation

All in two files, mirroring the existing `init` / `Template` / `setScalarLine`
shapes. Pure file I/O — no engine or domain writes beyond the existing
`TopLevel` read.

### `internal/config` (new code in `template.go` or a new `populate.go`)

- **`populate(raw string) string`** — pure, unit-testable core. Takes the
  current file content (possibly `""`), returns the new content. Implements the
  line-preserving additive merge above. Reuses `lineAssignsKey` and
  `tomlScalar`. This is the sibling of `Template()`.
- **`PopulateFile(path string) error`** — thin wrapper: read the file
  (`os.IsNotExist` → treat as empty `raw`), call `populate`, write back via the
  existing `atomicWriteFile` (which `MkdirAll`s the parent). Sibling of
  `setScalarLine`'s write path.

Line rendering helper (shared with `Template()` where practical) so the added
lines match the template's column style.

### `internal/cli/config.go`

- Add a `case "populate":` to `cmdConfig` routing to **`cmdConfigPopulate`**.
- `cmdConfigPopulate` mirrors `cmdConfigInit`: parse `--repo`/`--global`,
  enforce exactly-one, resolve the path (TopLevel for repo, DefaultGlobalPath
  for global), call `config.PopulateFile(path)`, print `populated <path>` (or a
  count of keys added) to stdout, return 0. Usage string for bare
  `gg config` updates to mention `populate`.

## Testing

- `internal/config` table tests for `populate`:
  - empty input → full template, all commented + marked.
  - file with one active override (`wheel_step = 5`) → override kept verbatim,
    untouched; all other keys added commented.
  - file with a commented line already present (`# hscroll_step = 8`) → left
    exactly as-is (not re-added, no marker added).
  - idempotency: `populate(populate(x)) == populate(x)`.
  - section creation: input missing the `[refresh]` header → header + keys
    appended.
  - nil-default keys rendered value-less and commented.
  - unknown user key preserved.
- `internal/cli` test mirroring `init_test.go`: `--repo` writes `<top>/.gg.toml`
  and adds missing keys to a pre-seeded partial file; neither/both flags →
  exit 2.

## Documentation & maintenance reminder

Because populate is registry-driven, the only maintenance is keeping
`settingDocs` complete (already enforced). Make that explicit:

1. **`CHANGELOG.md`** — new entry.
2. **README** `## Configuration` — document `gg config populate` next to
   `gg config init`.
3. **`internal/agentskill/using-gg.md`** — document the command; bump
   `agentskill.Version`; note that `gg init --update` refreshes installed
   copies.
4. **`.claude/skills/adding-config-entries/SKILL.md`** — add a checklist step:
   "Add a `settingDoc` entry in `internal/config/template.go` — the single
   registry feeding both `gg config init` and `gg config populate`;
   `TestSettingDocsCoverAllFields` fails until you do." (This step is missing
   from the checklist today.)
5. **`CLAUDE.md`** — in the `config` package row, note that `config init` and
   `config populate` are both generated from the `settingDocs` registry.
6. **Project memory** — record that the `settingDocs` registry is the one place
   to touch when adding a config entry, and it auto-covers both `config init`
   and `config populate` (no command-specific maintenance).

## Open questions (low-stakes; default chosen)

1. **Idempotent no-op write:** when nothing is added, rewrite the file anyway
   (simple) or skip the write and print `nothing to add`? **Default:** skip the
   write, print `<path> already complete` — avoids touching mtime needlessly.
2. **stdout message:** `populated <path> (N added)` vs plain `wrote <path>`.
   **Default:** `populated <path> (N added)` so the user sees whether anything
   changed.
