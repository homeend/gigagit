# Multilanguage support (TUI i18n) — design

**Date:** 2026-07-14
**Status:** approved design, pre-plan
**Languages:** Japanese, Korean, Chinese, Russian built in; custom languages via
user config files; English is the base (and the fallback).

## Goal

Let a human run the gg TUI in their own language. The CLI, git's own output,
and everything agent-facing stays English.

## Decisions (settled during brainstorming)

| Question | Decision |
|---|---|
| Scope | **TUI only.** CLI output is agent-facing (the using-gg skill teaches agents to parse it; e2e asserts on it) and stays English. |
| Language selection | `[ui] language` config key + a Settings `,` **"Language"** picker row that persists the choice. No `$LANG` auto-detection. |
| Custom languages | User files in `$XDG_CONFIG_HOME/gg/lang/<code>.toml`. A code matching a built-in **overlays per-key**; a new code adds a language. Missing keys always fall back to English. |
| Key format | **English-text-as-key** (gettext style). No invented message IDs; a lookup miss returns the key itself. |
| Rollout | **Staged.** Stage 1 = infrastructure + core surfaces; later stages sweep remaining files. Untranslated strings show English meanwhile. |

## Architecture

### `internal/i18n` (new package — archtest DAG leaf, like `exttool`/`gitconfdocs`)

- `T(key string, args ...any) string` — the one call sites use. Looks up
  `key` in the active catalog: on hit, formats the **translation** with
  `args`; on miss, formats the **key itself** (= English fallback). No args →
  the string is returned verbatim (no `Sprintf` pass, so stray `%` in
  arg-less strings is harmless).
- The active catalog is an `atomic.Pointer[Catalog]` — process-global, safe
  from Bubble Tea's goroutines. Threading a translator through hundreds of
  value-receiver view funcs would be churn for zero benefit.
- `SetLanguage(code, customDir string) error` builds and swaps the catalog:
  1. `code == ""` or `"en"` → empty catalog (everything falls back).
  2. Embedded bundle for `code`, if one exists.
  3. `<customDir>/<code>.toml`, if it exists, **overlaid per-key** on top.
  4. Neither exists → error (caller falls back to English + notices).
- `Available(customDir) []Lang` — `{Code, Name}` list for the picker:
  English first, then embedded bundles, then custom-only files.
- Built-in bundles are `go:embed`-ded: `lang/ja.toml`, `ko.toml`, `zh.toml`,
  `ru.toml`. **No `en.toml`** — English is the key.

### Bundle file format

```toml
[meta]
name = "日本語"        # native display name, shown in the picker

[strings]
"Compare branches" = "ブランチを比較"
"committed %s %s" = "%[1]s %[2]s をコミットしました"   # verbs may reorder
```

- Keys keep Go format verbs. A translation may reorder them with `%[n]`
  indexing but must use the same verb multiset as its key.
- Parsed with the same TOML library the `config` package already uses; no
  new dependency.

### Placeholder rule

Dynamic text (branch names, paths, SHAs, counts) is always an **argument**,
never part of the key: `i18n.T("committed %s %s", sha, subj)`. The AST-scan
test (below) enforces that `T`'s first argument is a string literal.

## Config + Settings

- New `[ui] language` key (string; empty = English) with a `settingDocs`
  entry, so `gg config init`/`populate` document it.
- New line-edit writer `SetGlobalUILanguage` — writes the **global** config
  (a language is per-human, not per-repo); the normal `[ui]` overlay still
  lets a repo `.gg.toml` override it for the odd shared-demo repo.
- The TUI applies the language at **both** config-arrival paths
  (`configReadyMsg` startup + `dataLoadedMsg`/reRoot) — the `show_graph`
  lesson.
- Settings `,` gains a **"Language"** row → a small picker popup listing
  `Available()`; enter calls `SetLanguage` (takes effect on the next frame
  for anything rendered through `T()` at view time; a popup that snapshots
  its labels at open-time refreshes on reopen — acceptable, since the picker
  is itself the top layer when the switch happens) and persists via
  `SetGlobalUILanguage`.

## Error handling — always fail soft to English

- Unknown `[ui] language` code or a malformed custom TOML file → English +
  a one-line status notice ("language 'xx' not found" / "failed to parse
  <path>"). Never a startup error — the `ValidateToolCommand`
  inert-at-load convention.
- A translation whose format verbs don't match its key's (multiset
  comparison; `%[n]` reordering allowed) is rejected **per-key at load** —
  that one string falls back to English, the rest of the bundle loads.

## Testing

- **Unit tests** (`internal/i18n`): lookup, fallback, overlay precedence,
  verb-mismatch rejection, `Available` ordering, meta parsing, concurrent
  `T`/`SetLanguage`.
- **AST-scan test** (the `gitconfdocs` staleness pattern, applied to code):
  parse `internal/tui` with `go/ast`, collect every `i18n.T(...)` literal
  first argument → the key catalog. Assert:
  - **(a) Orphans:** every key in every built-in bundle exists in the
    catalog — editing an English string in code without updating bundles
    fails the test.
  - **(b) Coverage:** every catalog key exists in **all four** built-in
    bundles — no mixed-language surfaces; adding a `T()` string means
    adding four translations in the same change.
  - **(c) Literal keys only:** a non-literal first argument to `T` is a
    test failure (dynamic keys would break extraction and translation).
- Built-in translations are authored in-change (machine-drafted, reviewed);
  test (b) keeps them complete.

## Stage 1 scope

**Infrastructure** (everything above) **plus extraction of the core
surfaces:**

- footer labels (audit `fitFooter` width budgets while there)
- help overlay
- Settings menu rows + the new Language picker
- command palette entries
- `.` menus (all panels' common rows)
- confirm modals
- common popups: commit popup, create branch/worktree
- TUI-originated status hints (`⟳ <source>…`, `⏸ <op> paused …`, copy/notice
  one-liners)

Later stages sweep the remaining popups/views file-by-file; anything not yet
extracted simply renders English.

## Out of scope (by design, documented limitations)

- **CLI** — entirely English (agent-facing contract).
- **Engine-originated prose** — op summaries, decision prompt text, `GitLine`
  output. These reach the TUI as English strings from `internal/engine`;
  translating them needs message IDs through the `Result`/`Event` contracts —
  a possible later stage, explicitly not now. Some mixed-language seams in
  the status bar are accepted in the interim.
- Key names themselves (`ctrl+p`, `c`), config keys, log files, git output.

## Language-kind notes

- **CJK (ja/ko/zh):** double-width rendering already works — gg measures
  with `lipgloss.Width`/runewidth today (CJK paths and commit subjects render
  correctly). The extraction pass must audit spots whose budgets are tuned to
  English label lengths (footer fitting, popup min-widths).
- **Russian:** the only quirk is plural forms; naive `%d files` is accepted
  initially. A small plural helper can come later if it grates.
- The mechanism itself is language-agnostic — all bundles are the same TOML
  shape.

## Doc updates on completion

`CHANGELOG.md`, `README.md` (new user-facing surface), `CLAUDE.md` (package
map row for `internal/i18n`, `[ui] language` in the config section), the
`config-settings-registry` maintenance rule (settingDocs entry ships with the
key).
