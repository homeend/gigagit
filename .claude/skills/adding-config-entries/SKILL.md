---
name: adding-config-entries
description: How gigagit's TOML config works (defaults → global → repo field-level overlay) and the checklist for adding a new config entry end to end.
---

# Adding a config entry

gg's configuration is three layers, each overlaying the previous **per
field** (not per file):

1. **Built-in defaults** — `Defaults()` in `internal/config/config.go`.
2. **Global file** — `$XDG_CONFIG_HOME/gg/config.toml`, falling back to
   `~/.config/gg/config.toml` (`DefaultGlobalPath()`).
3. **Repo file** — `<repo-top>/.gg.toml`, committed to the repository.
   Repo wins.

A missing file is skipped; a present-but-malformed file is an error.

## Overlay semantics (the part people get wrong)

Each config section has an `overlay<Section>(dst, src)` func that copies
ONLY set fields: non-empty string, non-empty slice, positive int. The zero
value means "unset", so **a higher layer can never reset a field to the
zero value** — setting `wheel_step = 0` in `.gg.toml` does not disable the
global's value, it is simply ignored. This is intentional and documented on
`overlayWorktree`.

Local mutable state is NOT config: `<seq>` counters live in
`<git-common-dir>/gg/state.toml` via `internal/config/state.go` (the
committed config is read-only at runtime).

## Checklist for a new entry

1. **Field**: add it to the right section struct in
   `internal/config/config.go` with a snake_case `toml:"…"` tag — or create
   a new section struct, add it to `Config`, and write its
   `overlay<Section>` func.
2. **Default**: set it in `Defaults()`.
3. **Overlay**: extend the section's overlay func (set-detection per the
   field type: `!= ""` / `len > 0` / `> 0`).
4. **Wire**: a NEW section's overlay must be called in `Load` for both
   layers (next to `overlayWorktree`).
5. **Test**: table-test default / global-only / repo-over-global /
   zero-ignored in `internal/config/config_test.go` (see
   `TestUIWheelStepLayers`).
6. **Consume**:
   - TUI: config arrives via `loadCmd` → `dataLoadedMsg` → `m.cfg`, which
     is the ZERO value before the first load — guard with a fallback
     helper (see `Model.wheelStep()` in `internal/tui/model.go`).
   - CLI: load on demand with
     `config.Load(config.DefaultGlobalPath(), filepath.Join(top, ".gg.toml"))`
     (see `internal/cli/worktree.go`).
7. **Document**: README's `## Configuration` section.
8. **e2e**: the harness pins its own `.gg.toml` (worktree templates); touch
   it only if scenarios need the new entry.

## Worked example: `[ui] wheel_step`

`UIConfig{WheelStep int}` (default 3, `> 0` = set) governs every
mouse-wheel tick in the TUI; `Model.wheelStep()` falls back to 3 pre-load.
Set per repo in `.gg.toml`:

```toml
[ui]
wheel_step = 5
```
