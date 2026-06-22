# `gg config init` — design

**Date:** 2026-06-22
**Status:** Approved (brainstorm)
**Feature:** A CLI command that scaffolds a fully-documented gg config file (repo
or global) listing every setting commented-out with its default and a
description, backed by a test that keeps the listing complete.

## Goal

Make gg's configuration discoverable: one command writes a `.gg.toml` (or the
global config) containing every `[worktree]`/`[ui]` setting as a commented line
with its default value and a one-line description. The user uncomments what they
want to change. A reflection test guarantees the generated file lists every
setting, so it can never silently fall behind the code.

## Problem this solves

There is no single source of truth for "all defaults" today: `config.Defaults()`
covers only some fields, while others default at their use-sites in other
packages (`reflog_limit`=200 in `domain`, `search_history_size`=20 in
`searchhist`, `commit_graph_pan_step` derived from terminal width,
`footer_actions`/`menu_actions` = "all"). Users have to read the README or source
to learn what is configurable. This feature establishes a documented registry and
a generator, enforced for completeness by a test.

## Decisions (from brainstorm)

- **Commented template**, not active values: each key is written commented-out
  with its default, so writing the file changes nothing until a line is
  uncommented, and untouched keys keep tracking gg's real defaults across
  versions.
- **CLI only**: `gg config init (--repo | --global) [--force]`. No TUI Settings
  entry in this feature.
- **Centralizing the scattered defaults is OUT OF SCOPE.** `reflog_limit`'s
  default must remain in `domain` (the startup `Snapshot` runs before config
  loads), so `config.Defaults()` cannot become the one true source; the registry
  holds literals for the few use-site defaults instead.

## Components

### 1. Settings registry (`internal/config`)

An ordered slice — the single source of truth for the generated template:

```go
// settingDoc is one documented configuration setting. value is the TOML-rendered
// default ("3", "200", `"../<repo>.worktrees/<branch>"`); a nil value means the
// setting has no honest scalar default and is rendered comment-only.
type settingDoc struct {
	section string // "worktree" or "ui"
	key     string // toml key, e.g. "reflog_limit"
	value   *string
	comment string // one-line description (incl. the default note when value is nil)
}

var settingDocs = []settingDoc{ /* every field, in display order */ }
```

- Fields with a static default carry a `value` (rendered as it appears in TOML —
  ints bare, strings quoted).
- Fields with **no honest scalar default** carry `value == nil` and explain the
  default in the comment, never emitting a misleading `= 0`:
  - `commit_graph_pan_step` — "default: derived from terminal width (max(1, cols/2))"
  - `footer_actions`, `menu_actions` — "default: empty = show all actions"
  - `branch_templates` — "default: none"
- The two use-site defaults carry literal values whose authoritative source lives
  in another package; the comment names that (e.g. `reflog_limit` → 200,
  `search_history_size` → 20). The reflection test below guarantees the field is
  present; the value is kept correct by the comment + the maintenance note.

### 2. Renderer

```go
// Template renders the commented config file: a header, then [worktree] and [ui]
// sections, each setting as "# <key> = <value>   # <comment>" (or comment-only
// when value is nil). Everything is commented, so the file is inert until edited.
func Template() string
```

Header text (verbatim):

```
# gg configuration — every setting with its default.
# Uncomment a line to override the default. Values shown are gg's built-in
# defaults; leaving a line commented keeps tracking the default across versions.
```

Sections are emitted in fixed order (`[worktree]` then `[ui]`); within a section,
entries follow `settingDocs` order. Column-aligning the trailing `# comment` is
cosmetic and not required.

### 3. CLI command — `gg config init`

`internal/cli/config.go`, registered as a `config` command with an `init`
subcommand:

- Flags: exactly one of `--repo` / `--global` (error if neither or both);
  `--force` to overwrite.
- `--repo` target: `./.gg.toml` (current working directory; it is shared repo
  config — not gitignored).
- `--global` target: `config.DefaultGlobalPath()`, creating `~/.config/gg/`
  (`os.MkdirAll` of the parent) first.
- If the target exists and `--force` is not set: print
  `config init: <path> already exists (use --force to overwrite)` to stderr,
  return non-zero. The message always names the path.
- Otherwise write `config.Template()` to the path and print
  `wrote <path>` to stdout.

**Routing:** add `"config"` to the `commands` map and a `case "config"` in
`Run` that dispatches its own subcommand (`init`), distinct from the existing
top-level `gg init` (agent-skill installer). The dispatch is verified by reading
`cli.Run`, not assumed.

## Maintenance guard (tests in `config_test.go`)

1. **Field coverage (reflection)** — walk every `toml` tag on `WorktreeConfig`
   and `UIConfig`; assert each tag has a `settingDocs` entry with that
   `section`+`key`. Fails when a new setting is added without registering it.
   *This is the mechanized "update the registry when adding a setting."*
2. **Value-sync** — for each field whose default is set in `Defaults()`
   (`wheel_step`, `hscroll_step`, `commit_graph_lanes`, `commit_graph_min_lanes`,
   `commit_graph_step`, `worktree.path_template`,
   `worktree.default_branch_template`), assert the registry's rendered value
   equals the value in `Defaults()`. No drift for those.
3. **Round-trip** — pass `Template()` through the existing `decodeFile` path
   (write to a temp file, decode); assert it parses with no error and yields a
   zero-valued `Config` (everything commented ⇒ nothing active). Proves the
   template is valid TOML and that `config init` is inert until uncommented.

A CLI test covers: `--repo` writes the file; refusing when it exists; `--force`
overwriting; `--global` honoring a `XDG_CONFIG_HOME`/temp path; the
neither/both-flags error.

## Maintenance note (memory)

A project memory records: the `settingDocs` registry in `internal/config` is the
single source for the generated config file; **a reflection test
(`config_test.go`) enforces that every struct field is registered** — when adding
a `[ui]`/`[worktree]` setting, add its `settingDoc`. The note points at the test;
it does not replace it.

## Docs to update on completion

- `README.md` — the `## Configuration` section documents `gg config init
  --repo|--global`.
- `CHANGELOG.md` — Added entry.
- `internal/agentskill/using-gg.md` — document the new command; **bump
  `agentskill.Version` and run `gg init --update`** in the same step
  (`TestDogfoodSkillCopyInSync` fails on bump-without-update *and*
  update-without-bump).
- `CLAUDE.md` — unchanged (no architecture/package-map change).

## Out of scope

- Centralizing use-site defaults into `config` (see Decisions).
- A TUI Settings entry.
- Migrating/merging an existing config, or writing only non-default keys.
